#!/usr/bin/env python3
"""端到端走查：装箱/签收核对、途中改派、差异复核（补送 + 退款幂等）、数量守恒。"""
import json
import os
import signal
import subprocess
import sys
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:8080/api"
DATE = "2026-10-03"
_passed = 0


def call(method, path, body=None, user=None, expect=None):
    headers = {"Content-Type": "application/json"}
    if user:
        headers["X-User-Id"] = str(user)
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req) as r:
            code, payload = r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        code, payload = e.code, json.loads(e.read() or b"{}")
    if expect and code != expect:
        raise AssertionError(f"{method} {path} 期望 {expect}，实际 {code}: {payload}")
    return code, payload


def check(name, cond, detail=""):
    global _passed
    if not cond:
        print(f"  ✗ {name} {detail}")
        sys.exit(1)
    _passed += 1
    print(f"  ✓ {name}")


def wait_up(proc):
    for _ in range(60):
        try:
            urllib.request.urlopen("http://127.0.0.1:8080/api/users", timeout=1)
            return
        except Exception:
            if proc.poll() is not None:
                raise SystemExit("服务进程提前退出")
            time.sleep(0.5)
    raise SystemExit("服务未在 30s 内就绪")


def main():
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    db = os.path.join(root, "backend", "e2e.db")
    if os.path.exists(db):
        os.remove(db)

    env = dict(os.environ, DB_DSN=db, ADDR=":8080",
               PATH=os.path.expanduser("~/.local/go/bin") + ":" + os.environ["PATH"])
    proc = subprocess.Popen(
        ["go", "run", "."], cwd=os.path.join(root, "backend"),
        env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        preexec_fn=os.setsid,
    )
    try:
        wait_up(proc)
        _, users = call("GET", "/users")
        by_name = {u["name"]: u["id"] for u in users}
        kitchen, courier1, courier2 = by_name["周大厨"], by_name["王骑手"], by_name["李骑手"]
        desk1, reviewer = by_name["陈前台"], by_name["赵复核"]
        _, companies = call("GET", "/companies")
        c1 = next(c["id"] for c in companies if c["name"] == "星云科技")
        c2 = next(c["id"] for c in companies if c["name"] == "云帆网络")
        desk2 = by_name["林前台"]

        print("场景一：装箱 → 改派 → 少餐签收 → 补送")
        _, b = call("POST", "/batches", {"company_id": c1, "meal_date": DATE, "meal_type": "lunch"}, kitchen, 201)
        bid = b["id"]
        check("按订单装箱 24 盒", b["packed_qty"] == 24, b)
        call("POST", f"/batches/{bid}/seals", {"boxes": [
            {"box_no": "BOX-A", "seal_no": "SEAL-A1", "boxed_qty": 14},
            {"box_no": "BOX-B", "seal_no": "SEAL-B1", "boxed_qty": 10},
        ]}, kitchen, 200)
        # 封签盒数与装箱数不符必须被拒
        code, _ = call("POST", f"/batches/{bid}/seals",
                       {"boxes": [{"box_no": "X", "seal_no": "S9", "boxed_qty": 23}]}, kitchen)
        check("封签盒数与装箱不符被拒", code == 422)
        call("POST", f"/batches/{bid}/handover", {"courier_id": courier1, "route_name": "园区北线"}, kitchen, 200)

        # 途中改派：王骑手 → 李骑手
        call("POST", f"/batches/{bid}/reroute",
             {"courier_id": courier2, "route_name": "园区南线", "note": "北线堵车"}, courier1, 200)
        _, d = call("GET", f"/batches/{bid}")
        active = [r for r in d["routes"] if r["active"]]
        check("改派后只有一条 active 路线且归属新骑手",
              len(active) == 1 and active[0]["courier_id"] == courier2, d["routes"])
        check("改派经过已留痕", any(e["type"] == "reroute" for e in d["events"]))

        call("POST", f"/batches/{bid}/deliver", {}, courier2, 200)

        # 前台少签：排骨饭 12→10（少2），其余足额，共 22
        _, rc = call("POST", f"/batches/{bid}/sign", {
            "receiver_name": "陈前台", "seal_intact": True,
            "lines": [{"dish_name": "红烧排骨饭", "received_qty": 10},
                      {"dish_name": "宫保鸡丁饭", "received_qty": 8},
                      {"dish_name": "素食菌菇饭", "received_qty": 4}],
        }, desk1, 201)
        check("实收 22，差额 2", rc["signed_qty"] == 22, rc)
        _, d = call("GET", f"/batches/{bid}")
        check("签收后 24 = 22 签收 + 2 在途",
              d["conservation"]["outstanding"] == 2 and d["conservation"]["balanced"])
        short = next(x for x in d["discrepancies"] if x["kind"] == "shortage")
        check("自动生成少 2 盒差异单", short["short_qty"] == 2 and short["status"] == "open")

        # 前台不能改装箱
        code, _ = call("POST", "/batches", {"company_id": c1, "meal_date": "2026-10-04", "meal_type": "lunch"}, desk1)
        check("前台装箱被拒（403）", code == 403)

        # 复核 → 补送
        call("POST", f"/discrepancies/{short['id']}/review",
             {"approve": True, "resolution": "resupply", "note": "核实少餐，补送"}, reviewer, 200)
        _, rb = call("POST", f"/discrepancies/{short['id']}/resupply", {}, reviewer, 201)
        rid = rb["id"]
        check("补送批次 2 盒且挂回原批次",
              rb["packed_qty"] == 2 and rb["root_batch_id"] == bid and rb["kind"] == "resupply", rb)
        _, d = call("GET", f"/batches/{bid}")
        check("原批次已结清关闭", d["status"] == "closed" and d["lost_qty"] == 2,
              {"status": d["status"], "lost": d["lost_qty"]})

        # 补送批次走完全程
        call("POST", f"/batches/{rid}/seals",
             {"boxes": [{"box_no": "BOX-R", "seal_no": "SEAL-R1", "boxed_qty": 2}]}, kitchen, 200)
        call("POST", f"/batches/{rid}/handover", {"courier_id": courier2, "route_name": "补送专线"}, kitchen, 200)
        call("POST", f"/batches/{rid}/deliver", {}, courier2, 200)
        call("POST", f"/batches/{rid}/sign", {
            "receiver_name": "陈前台", "seal_intact": True,
            "lines": [{"dish_name": "红烧排骨饭", "received_qty": 2}],
        }, desk1, 201)
        _, rd = call("GET", f"/batches/{rid}")
        check("补送批次数量平衡并关闭", rd["conservation"]["balanced"] and rd["status"] == "closed")

        print("场景二：少餐 → 退款（含回执幂等）")
        _, b2 = call("POST", "/batches", {"company_id": c2, "meal_date": DATE, "meal_type": "lunch"}, kitchen, 201)
        b2id = b2["id"]
        check("公司2 装箱 12 盒", b2["packed_qty"] == 12)
        call("POST", f"/batches/{b2id}/seals",
             {"boxes": [{"box_no": "BOX-C", "seal_no": "SEAL-C1", "boxed_qty": 12}]}, kitchen, 200)
        call("POST", f"/batches/{b2id}/handover", {"courier_id": courier1, "route_name": "B座线"}, kitchen, 200)
        call("POST", f"/batches/{b2id}/deliver", {}, courier1, 200)
        # 多签必须被拒
        code, _ = call("POST", f"/batches/{b2id}/sign", {
            "receiver_name": "林前台", "seal_intact": True,
            "lines": [{"dish_name": "红烧排骨饭", "received_qty": 7},
                      {"dish_name": "番茄牛腩饭", "received_qty": 6}],
        }, desk2)
        check("实收超过应收被拒（餐盒不能凭空增加）", code == 422)
        # 正常签收：少 2 盒排骨饭
        call("POST", f"/batches/{b2id}/sign", {
            "receiver_name": "林前台", "seal_intact": True,
            "lines": [{"dish_name": "红烧排骨饭", "received_qty": 4},
                      {"dish_name": "番茄牛腩饭", "received_qty": 6}],
        }, desk2, 201)
        _, d2 = call("GET", f"/batches/{b2id}")
        disc2 = next(x for x in d2["discrepancies"] if x["kind"] == "shortage")
        call("POST", f"/discrepancies/{disc2['id']}/review",
             {"approve": True, "resolution": "refund"}, reviewer, 200)
        _, r1 = call("POST", f"/discrepancies/{disc2['id']}/refund",
                     {"receipt_no": "RCP-E2E-0001"}, reviewer, 200)
        check("首次退款入账", r1["deduplicated"] is False)
        _, r2 = call("POST", f"/discrepancies/{disc2['id']}/refund",
                     {"receipt_no": "RCP-E2E-0001"}, reviewer, 200)
        check("重复回执被识别为去重", r2["deduplicated"] is True)
        _, logs = call("GET", "/mock-logs")
        pr = [p for p in logs["payment"] if p["ReceiptNo"] == "RCP-E2E-0001"]
        check("支付侧只有一条回执，提交次数为 2", len(pr) == 1 and pr[0]["CallCount"] == 2, pr)
        _, d2 = call("GET", f"/batches/{b2id}")
        check("退款后原批次平衡并关闭", d2["conservation"]["balanced"] and d2["status"] == "closed",
              {"c": d2["conservation"], "s": d2["status"]})

        print("全局对账")
        _, audit = call("GET", "/audit")
        check("全部批次数量平衡", audit["all_balanced"], audit)
        check("无无依据补送批次", not audit["orphan_resupplies"], audit["orphan_resupplies"])
        check("退款回执全局唯一", audit["refund_receipt_unique"])
        check("总账 38 装箱 = 34 签收 + 4 核销，在途 0",
              audit["total_packed"] == 38 and audit["total_signed"] == 34
              and audit["total_lost"] == 4 and audit["total_outstanding"] == 0, audit)

        # 同一批餐不会在两条路线同时完成：调度轨迹每批 dispatch/reroute/complete 顺序
        dl = [l for l in logs["delivery"] if l["BatchNo"] in (d["batch_no"],)]
        actions = [l["Action"] for l in dl]
        check("原批次调度顺序 派车→改派→完成", actions == ["dispatch", "reroute", "complete"], actions)

        print(f"\n全部 {_passed} 项端到端检查通过 ✅")
    finally:
        os.killpg(os.getpgid(proc.pid), signal.SIGTERM)


if __name__ == "__main__":
    main()
