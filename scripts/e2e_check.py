#!/usr/bin/env python3
"""HTTP 端到端冒烟：覆盖装箱签收核对、途中改派、差异复核、补送链、退款幂等与守恒。"""
import json
import sys
import urllib.request
import urllib.error

BASE = "http://localhost:18080/api"
PASS = 0

def call(method, path, user=None, body=None, expect_ok=True, expect_status=None):
    global PASS
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if user:
        req.add_header("X-User", user)
    try:
        with urllib.request.urlopen(req) as r:
            payload = json.loads(r.read())
            status = r.status
    except urllib.error.HTTPError as e:
        payload = json.loads(e.read())
        status = e.code
    if expect_status is not None:
        assert status == expect_status, f"{method} {path}: 期望 HTTP {expect_status}，实际 {status}：{payload}"
    if expect_ok:
        assert payload.get("ok"), f"{method} {path}: 期望成功，实际 {payload}"
        PASS += 1
        return payload.get("data")
    assert not payload.get("ok"), f"{method} {path}: 期望失败，实际 {payload}"
    PASS += 1
    return payload.get("error")

def consv(batch_id):
    return call("GET", f"/batches/{batch_id}/conservation", user="reviewer")

def assert_eq(actual, expected, msg):
    assert actual == expected, f"{msg}: 期望 {expected}，实际 {actual}"

# 1) 厨房装箱 + 封箱
items = [
    {"name": "红烧肉饭", "unit_price": 2500, "packed_qty": 20},
    {"name": "素食套餐", "unit_price": 2200, "packed_qty": 10},
]
b = call("POST", "/kitchen/batches", "kitchen",
         {"enterprise_id": 1, "meal_date": "2026-10-10", "meal_type": "lunch", "items": items})
bid = b["id"]
call("POST", f"/kitchen/batches/{bid}/seal", "kitchen",
     {"seal_numbers": ["HTTP-SEAL-A", "HTTP-SEAL-B", "HTTP-SEAL-C"]})

# 权限：前台改装箱被拒
call("PUT", f"/kitchen/batches/{bid}/packing", "desk1",
     {"items": items}, expect_ok=False, expect_status=403)

# 2) 调度指派北线 -> 配送员接收/出发
call("POST", f"/dispatch/batches/{bid}/assign", "admin", {"route_id": 1, "note": "交接"})
call("POST", f"/routes/batches/{bid}/accept", "c1", {"note": "接收"})
call("POST", f"/routes/batches/{bid}/depart", "c1", {"note": "出发"})

# 3) 途中改派：北线 -> 机动线；二次直接指派必须冲突
call("POST", f"/dispatch/batches/{bid}/assign", "admin", {"route_id": 2},
     expect_ok=False, expect_status=409)
call("POST", f"/dispatch/batches/{bid}/reassign", "admin",
     {"route_id": 3, "note": "堵车改派机动"})
# 旧配送员不能再操作
call("POST", f"/routes/batches/{bid}/deliver", "c1", {}, expect_ok=False, expect_status=403)
# 新配送员接收/出发/送达
call("POST", f"/routes/batches/{bid}/accept", "c3", {})
call("POST", f"/routes/batches/{bid}/depart", "c3", {})
call("POST", f"/routes/batches/{bid}/deliver", "c3", {"note": "送达"})

# 4) 前台签收：少 3 盒（红烧肉饭 17），错餐 1 盒
detail = call("GET", f"/batches/{bid}", "desk1")
lines = [{"batch_item_id": it["id"], "name": it["name"],
          "expected_qty": it["packed_qty"],
          "received_qty": 17 if it["name"] == "红烧肉饭" else it["packed_qty"]}
         for it in detail["items"]]
lines.append({"name": "鱼香肉丝饭(错送)", "expected_qty": 0, "received_qty": 1, "wrong": True})
call("POST", f"/frontdesk/batches/{bid}/sign", "desk1",
     {"receiver_name": "前台小李", "seal_ok": True, "lines": lines})

# 实收超过装箱必须被拒（新批次验证）
b2 = call("POST", "/kitchen/batches", "kitchen",
          {"enterprise_id": 2, "meal_date": "2026-10-10", "meal_type": "lunch",
           "items": [{"name": "牛腩饭", "unit_price": 2600, "packed_qty": 5}]})
call("POST", f"/kitchen/batches/{b2['id']}/seal", "kitchen", {"seal_numbers": ["B2-S1"]})
call("POST", f"/dispatch/batches/{b2['id']}/assign", "admin", {"route_id": 2})
call("POST", f"/routes/batches/{b2['id']}/accept", "c2", {})
call("POST", f"/routes/batches/{b2['id']}/depart", "c2", {})
call("POST", f"/routes/batches/{b2['id']}/deliver", "c2", {})
over = [{"batch_item_id": b2["items"][0]["id"], "name": "牛腩饭",
         "expected_qty": 5, "received_qty": 6}]
call("POST", f"/frontdesk/batches/{b2['id']}/sign", "desk2",
     {"receiver_name": "前台", "seal_ok": True, "lines": over},
     expect_ok=False, expect_status=400)

# 5) 差异复核：3 盒短少 = 2 补送 + 1 退款；错餐退回
r = consv(bid)
assert r["balanced"], f"签收后守恒失败：{r}"
assert_eq((r["produced"], r["received"], r["open_claim"], r["wrong_held"]),
          (30, 27, 3, 1), "少3错1状态")

discs = call("GET", "/discrepancies?status=open", "reviewer")
d = next(x for x in discs if x["batch_id"] == bid)
alloc = []
for l in d["lines"]:
    if l["wrong"]:
        alloc.append({"line_id": l["id"], "resupply_qty": 0, "loss_qty": 0, "refund_qty": 0})
    elif l["name"] == "红烧肉饭":
        alloc.append({"line_id": l["id"], "resupply_qty": 2, "loss_qty": 0, "refund_qty": 1})
# 先提交错误分配（合计 != 3）应被拒
bad = [dict(x, resupply_qty=1) if not x.get("wrong") else x for x in alloc]
call("POST", f"/review/discrepancies/{d['id']}/approve", "reviewer",
     {"lines": bad, "note": "错误"}, expect_ok=False, expect_status=400)

res = call("POST", f"/review/discrepancies/{d['id']}/approve", "reviewer",
           {"lines": alloc, "note": "2盒补送1盒退款，错餐退回", "auto_post_pay": False})
child_id = res["resupply_batch_id"]
assert child_id, "必须返回补送批次"
r = consv(bid)
assert r["balanced"], f"复核后守恒失败：{r}"
assert_eq((r["produced"], r["resupplied"], r["refund_wait"], r["wrong_return"]),
          (30, 2, 1, 1), "复核后数量桶")

# 6) 支付回执：重复推送只入账一次
resolution_id = res["resolution"]["id"]
call("POST", f"/review/resolutions/{resolution_id}/refund-receipt", "reviewer",
     {"gateway_receipt": "GW-E2E-1"})
dup = call("POST", f"/review/resolutions/{resolution_id}/refund-receipt", "reviewer",
           {"gateway_receipt": "GW-E2E-2"})
assert dup["duplicate"] is True, "重复回执必须标记 duplicate"

# 7) 补送批次沿原批次树完成：厨房封箱 -> 调度 -> 配送 -> 前台全量签收
call("POST", f"/kitchen/batches/{child_id}/seal", "kitchen", {"seal_numbers": ["CHILD-S1"]})
call("POST", f"/dispatch/batches/{child_id}/assign", "admin", {"route_id": 1, "note": "补送"})
call("POST", f"/routes/batches/{child_id}/accept", "c1", {})
call("POST", f"/routes/batches/{child_id}/depart", "c1", {})
call("POST", f"/routes/batches/{child_id}/deliver", "c1", {})
cd = call("GET", f"/batches/{child_id}", "desk1")
clines = [{"batch_item_id": it["id"], "name": it["name"],
           "expected_qty": it["packed_qty"], "received_qty": it["packed_qty"]}
          for it in cd["items"]]
call("POST", f"/frontdesk/batches/{child_id}/sign", "desk1",
     {"receiver_name": "前台小李", "seal_ok": True, "lines": clines})

r = consv(bid)
assert r["balanced"], f"补送完成后守恒失败：{r['detail']}"
# 原 30 盒 + 补送 2 盒 = 32 产出；27+2 正常实收；补送核销 2；退款 1
assert_eq((r["produced"], r["received"], r["resupplied"], r["refunded"],
           r["resupply_approved"], r["resupply_packed"]),
          (32, 29, 2, 1, 2, 2), "最终数量恒等式")
assert r["in_flight"] == 0 and r["open_claim"] == 0, f"不应有在途/待复核：{r}"

# 8) 批次树全部关闭
for x in call("GET", f"/batches/{bid}", "admin")["children"]:
    assert x["status"] == "closed", f"补送批次应关闭：{x['status']}"

print(f"\n✅ E2E 全部通过（{PASS} 个调用断言 + 数量恒等式断言）")
print(json.dumps(r, ensure_ascii=False, indent=2))
