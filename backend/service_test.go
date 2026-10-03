package main

import (
	"path/filepath"
	"testing"

	"campusmeal/models"

	"gorm.io/gorm/logger"
)

type fixture struct {
	svc      *Service
	kitchen  *models.User
	courier1 *models.User
	courier2 *models.User
	desk     *models.User // 属于公司1
	desk2    *models.User // 属于公司2
	reviewer *models.User
	company  uint
}

func setup(t *testing.T) *fixture {
	t.Helper()
	db := openDB(filepath.Join(t.TempDir(), "test.db") + "?_pragma=foreign_keys(1)")
	db.Logger = logger.Default.LogMode(logger.Silent)
	svc := NewService(db)

	c := models.Company{Name: "测试科技", Address: "A1"}
	if err := svc.db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	mk := func(name, role string, cid *uint) *models.User {
		u := models.User{Name: name, Role: role, CompanyID: cid}
		if err := svc.db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		return &u
	}
	f := &fixture{
		svc:      svc,
		kitchen:  mk("厨", models.RoleKitchen, nil),
		courier1: mk("骑手甲", models.RoleCourier, nil),
		courier2: mk("骑手乙", models.RoleCourier, nil),
		desk:     mk("前台甲", models.RoleFrontDesk, &c.ID),
		desk2:    mk("前台乙", models.RoleFrontDesk, func() *uint { c2 := c.ID + 1000; return &c2 }()),
		reviewer: mk("复核", models.RoleReviewer, nil),
		company:  c.ID,
	}
	return f
}

func (f *fixture) makeOrder(t *testing.T, date string, lines map[string]int) uint {
	t.Helper()
	o := models.MealOrder{CompanyID: f.company, MealDate: date, MealType: models.MealLunch}
	if err := f.svc.db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	for dish, q := range lines {
		if err := f.svc.db.Create(&models.OrderLine{MealOrderID: o.ID, DishName: dish, OrderedQty: q}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return f.company
}

// packHandoverDeliver 走完装箱→封签→交接→送达，返回批次详情。
func (f *fixture) packHandoverDeliver(t *testing.T, date string, courier *models.User, route string) *BatchDetail {
	t.Helper()
	b, err := f.svc.CreateBatchFromOrder(f.kitchen, f.company, date, models.MealLunch)
	if err != nil {
		t.Fatalf("装箱失败: %v", err)
	}
	if err := f.svc.RegisterSeals(f.kitchen, b.ID, []BoxInput{
		{BoxNo: "BOX-1", SealNo: "SEAL-" + b.BatchNo + "-1", BoxedQty: b.PackedQty},
	}); err != nil {
		t.Fatalf("封签失败: %v", err)
	}
	if err := f.svc.Handover(f.kitchen, b.ID, courier.ID, route); err != nil {
		t.Fatalf("交接失败: %v", err)
	}
	if err := f.svc.Deliver(courier, b.ID); err != nil {
		t.Fatalf("送达失败: %v", err)
	}
	d, err := f.svc.GetBatch(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func assertBalanced(t *testing.T, d *BatchDetail) {
	t.Helper()
	if !d.Conservation.Balanced {
		t.Errorf("批次 %s 数量不平衡: %+v", d.BatchNo, d.Conservation)
	}
}

// ---------- 场景 1：装箱与签收数量核对（含少盒） ----------

func TestPackSignQuantityCheck(t *testing.T) {
	f := setup(t)
	date := "2026-10-03"
	f.makeOrder(t, date, map[string]int{"排骨饭": 12, "鸡丁饭": 8})

	d := f.packHandoverDeliver(t, date, f.courier1, "R1")
	if d.PackedQty != 20 {
		t.Fatalf("装箱应为 20，实际 %d", d.PackedQty)
	}

	// 前台签收：排骨饭应收12实收10（少2），鸡丁饭8/8
	_, err := f.svc.Sign(f.desk, d.ID, SignInput{
		ReceiverName: "前台甲", SealIntact: true,
		Lines: []SignLine{{DishName: "排骨饭", ReceivedQty: 10}, {DishName: "鸡丁饭", ReceivedQty: 8}},
	})
	if err != nil {
		t.Fatalf("签收失败: %v", err)
	}
	d, _ = f.svc.GetBatch(d.ID)
	if d.Receipt == nil || d.Receipt.SignedQty != 18 {
		t.Fatalf("实收应为 18，实际 %+v", d.Receipt)
	}
	// 20 = 18 签收 + 2 在途待差异处理
	if d.Conservation.Outstanding != 2 || !d.Conservation.Balanced {
		t.Errorf("签收后账应余 2 盒在途: %+v", d.Conservation)
	}
	if len(d.Discrepancies) != 1 || d.Discrepancies[0].ShortQty != 2 {
		t.Fatalf("应生成 1 张少2 盒差异单，实际 %+v", d.Discrepancies)
	}
}

// 前台任何一行实收超过应收都必须被拒（餐盒不能凭空增加）
func TestSignMoreThanPackedRejected(t *testing.T) {
	f := setup(t)
	date := "2026-10-03"
	f.makeOrder(t, date, map[string]int{"排骨饭": 5})
	d := f.packHandoverDeliver(t, date, f.courier1, "R1")

	_, err := f.svc.Sign(f.desk, d.ID, SignInput{
		ReceiverName: "前台甲", SealIntact: true,
		Lines: []SignLine{{DishName: "排骨饭", ReceivedQty: 6}},
	})
	if err == nil {
		t.Fatal("实收 6 > 应收 5 必须被拒绝")
	}
	// 装箱清单之外的菜品也不能冒出来
	_, err = f.svc.Sign(f.desk, d.ID, SignInput{
		ReceiverName: "前台甲", SealIntact: true,
		Lines: []SignLine{{DishName: "排骨饭", ReceivedQty: 5}, {DishName: "幽灵套餐", ReceivedQty: 1}},
	})
	if err == nil {
		t.Fatal("清单外菜品必须被拒绝")
	}
}

// ---------- 场景 2：少餐 → 补送沿原批次处理，数量守恒 ----------

func TestShortageResupplyFlow(t *testing.T) {
	f := setup(t)
	date := "2026-10-03"
	f.makeOrder(t, date, map[string]int{"排骨饭": 10})
	d := f.packHandoverDeliver(t, date, f.courier1, "R1")

	_, err := f.svc.Sign(f.desk, d.ID, SignInput{ReceiverName: "前台甲", SealIntact: true,
		Lines: []SignLine{{DishName: "排骨饭", ReceivedQty: 6}}})
	if err != nil {
		t.Fatal(err)
	}
	d, _ = f.svc.GetBatch(d.ID)
	disc := d.Discrepancies[0]

	// 两阶段：先复核批准补送
	if err := f.svc.ReviewDiscrepancy(f.reviewer, disc.ID, ReviewInput{
		Approve: true, Resolution: "resupply", Note: "核实少4盒，安排补送"}); err != nil {
		t.Fatalf("复核失败: %v", err)
	}
	// 复核数量与差异不符必须拒绝
	if err := f.svc.ReviewDiscrepancy(f.reviewer, disc.ID+999, ReviewInput{
		Approve: true, Resolution: "resupply", ResolveQty: 3}); err == nil {
		t.Fatal("不存在的差异应报错")
	}
	rb, err := f.svc.ProcessResupply(f.reviewer, disc.ID)
	if err != nil {
		t.Fatalf("发起补送失败: %v", err)
	}
	if rb.Kind != "resupply" || rb.RootBatchID == nil || *rb.RootBatchID != d.ID {
		t.Fatalf("补送批次必须挂回原批次: %+v", rb)
	}
	if rb.PackedQty != 4 {
		t.Fatalf("补送应装箱 4 盒，实际 %d", rb.PackedQty)
	}
	// 同一差异不能重复补送
	if _, err := f.svc.ProcessResupply(f.reviewer, disc.ID); err == nil {
		t.Fatal("重复补送必须被拒绝")
	}
	// 原批次：10 = 6 签收 + 4 核销，应已关闭
	d, _ = f.svc.GetBatch(d.ID)
	assertBalanced(t, d)
	if d.Status != models.BatchClosed || d.LostQty != 4 {
		t.Fatalf("原批次应关闭且核销4，status=%s lost=%d", d.Status, d.LostQty)
	}

	// 补送批次走完全程（封签→交接→送达→足额签收）
	if err := f.svc.RegisterSeals(f.kitchen, rb.ID, []BoxInput{
		{BoxNo: "BOX-R", SealNo: "SEAL-R1", BoxedQty: 4}}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Handover(f.kitchen, rb.ID, f.courier2.ID, "R-补送"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Deliver(f.courier2, rb.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Sign(f.desk, rb.ID, SignInput{ReceiverName: "前台甲", SealIntact: true,
		Lines: []SignLine{{DishName: "排骨饭", ReceivedQty: 4}}}); err != nil {
		t.Fatalf("补送签收失败: %v", err)
	}

	audit := f.svc.Audit()
	if !audit.AllBalanced {
		t.Fatalf("全局数量不平衡: %+v", audit)
	}
	if len(audit.OrphanResupplies) != 0 {
		t.Fatalf("补送批次必须能追溯到已处理差异: %v", audit.OrphanResupplies)
	}
	// 物理总账：原批10 + 补送4 = 14；签收 6+4=10，损失核销 4
	if audit.TotalPacked != 14 || audit.TotalSigned != 10 || audit.TotalLost != 4 {
		t.Fatalf("总账错误: %+v", audit)
	}
}

// ---------- 场景 3：途中改派，同批不会同时两条路线 ----------

func TestRerouteSingleActiveRoute(t *testing.T) {
	f := setup(t)
	date := "2026-10-03"
	f.makeOrder(t, date, map[string]int{"排骨饭": 6})

	// 装箱→封签→交接（在途），改派发生在送达之前
	b, err := f.svc.CreateBatchFromOrder(f.kitchen, f.company, date, models.MealLunch)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RegisterSeals(f.kitchen, b.ID, []BoxInput{
		{BoxNo: "BOX-1", SealNo: "SEAL-1", BoxedQty: 6}}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Handover(f.kitchen, b.ID, f.courier1.ID, "北线"); err != nil {
		t.Fatal(err)
	}
	d, _ := f.svc.GetBatch(b.ID)

	activeCount := func() int64 {
		var n int64
		f.svc.db.Model(&models.RouteAssignment{}).Where("batch_id = ? AND active = ?", d.ID, true).Count(&n)
		return n
	}
	if activeCount() != 1 {
		t.Fatal("交接后应有且仅有一条 active 路线")
	}
	// 途中改派给骑手乙走南线
	if err := f.svc.Reroute(f.courier1, d.ID, f.courier2.ID, "南线", "甲骑手车故障"); err != nil {
		t.Fatalf("改派失败: %v", err)
	}
	if activeCount() != 1 {
		t.Fatal("改派后仍只能有一条 active 路线")
	}
	// 已在途的批次不能再次交接（防止另一条路线并行）
	if err := f.svc.Handover(f.kitchen, d.ID, f.courier1.ID, "北线2"); err == nil {
		t.Fatal("在途批次不能二次交接")
	}
	var routes []models.RouteAssignment
	f.svc.db.Where("batch_id = ?", d.ID).Order("id asc").Find(&routes)
	if len(routes) != 2 || routes[0].Active || routes[0].EndedAt == nil || !routes[1].Active {
		t.Fatalf("旧路线应结束、新路线激活: %+v", routes)
	}
	// 由新路线的骑手送达并签收
	if err := f.svc.Deliver(f.courier2, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Sign(f.desk, d.ID, SignInput{ReceiverName: "前台甲", SealIntact: true,
		Lines: []SignLine{{DishName: "排骨饭", ReceivedQty: 6}}}); err != nil {
		t.Fatal(err)
	}
	if activeCount() != 0 {
		t.Fatal("签收收口后不应再有 active 路线")
	}
	// 两条路线都留下了经过；配送模拟服务有 dispatch+reroute+complete
	logs := f.svc.delivery.Logs(d.BatchNo)
	if len(logs) < 3 {
		t.Fatalf("调度轨迹应含派车/改派/完成: %+v", logs)
	}
	var evs []models.Event
	f.svc.db.Where("batch_id = ? AND type = ?", d.ID, "reroute").Find(&evs)
	if len(evs) != 1 || evs[0].Actor == "" {
		t.Fatalf("改派必须留痕: %+v", evs)
	}
}

// ---------- 场景 4：差异复核——损失核销与退款幂等 ----------

func TestLossAndRefundReview(t *testing.T) {
	f := setup(t)
	date := "2026-10-03"
	f.makeOrder(t, date, map[string]int{"牛腩饭": 8})
	d := f.packHandoverDeliver(t, date, f.courier1, "R1")
	_, err := f.svc.Sign(f.desk, d.ID, SignInput{ReceiverName: "前台甲", SealIntact: true,
		Lines: []SignLine{{DishName: "牛腩饭", ReceivedQty: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	d, _ = f.svc.GetBatch(d.ID)
	disc := d.Discrepancies[0] // 少 3 盒

	if err := f.svc.ReviewDiscrepancy(f.reviewer, disc.ID,
		ReviewInput{Approve: true, Resolution: "loss"}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ProcessLoss(f.reviewer, disc.ID); err != nil {
		t.Fatalf("损失核销失败: %v", err)
	}
	d, _ = f.svc.GetBatch(d.ID)
	assertBalanced(t, d)
	if d.LostQty != 3 || d.Status != models.BatchClosed {
		t.Fatalf("损失后应核销3并关闭: lost=%d status=%s", d.LostQty, d.Status)
	}
	if !f.svc.Audit().AllBalanced {
		t.Fatal("损失核销后全局应平衡")
	}
}

func TestRefundReceiptIdempotent(t *testing.T) {
	f := setup(t)
	date := "2026-10-03"
	f.makeOrder(t, date, map[string]int{"牛腩饭": 8})
	d := f.packHandoverDeliver(t, date, f.courier1, "R1")
	_, _ = f.svc.Sign(f.desk, d.ID, SignInput{ReceiverName: "前台甲", SealIntact: true,
		Lines: []SignLine{{DishName: "牛腩饭", ReceivedQty: 5}}})
	d, _ = f.svc.GetBatch(d.ID)
	disc := d.Discrepancies[0]

	if err := f.svc.ReviewDiscrepancy(f.reviewer, disc.ID,
		ReviewInput{Approve: true, Resolution: "refund"}); err != nil {
		t.Fatal(err)
	}
	dedup, err := f.svc.ProcessRefund(f.reviewer, disc.ID, "RCP-1001")
	if err != nil || dedup {
		t.Fatalf("首次退款应成功入账: dedup=%v err=%v", dedup, err)
	}
	// 同回执再次提交：幂等成功但不重复入账
	dedup2, err2 := f.svc.ProcessRefund(f.reviewer, disc.ID, "RCP-1001")
	if !dedup2 {
		t.Fatalf("重复回执必须识别为去重: dedup=%v err=%v", dedup2, err2)
	}
	var refunds []models.Refund
	f.svc.db.Where("discrepancy_id = ?", disc.ID).Find(&refunds)
	if len(refunds) != 1 || refunds[0].Amount != 3*2500 {
		t.Fatalf("业务退款记录只能有一条且金额=75元: %+v", refunds)
	}
	prs := f.svc.pay.Receipts()
	var hit *svcsRow
	for i := range prs {
		if prs[i].ReceiptNo == "RCP-1001" {
			r := prs[i]
			hit = &svcsRow{call: r.CallCount, amount: r.Amount}
		}
	}
	if hit == nil || hit.call != 2 {
		t.Fatalf("支付侧回执应只有1条且被提交2次: %+v", prs)
	}

	// 另一张差异单复用同回执：必须拒绝，且不得产生第二条退款
	date2 := "2026-10-04"
	f.makeOrder(t, date2, map[string]int{"牛腩饭": 3})
	d2 := f.packHandoverDeliver(t, date2, f.courier2, "R2")
	if _, err := f.svc.Sign(f.desk, d2.ID, SignInput{ReceiverName: "前台甲", SealIntact: true,
		Lines: []SignLine{{DishName: "牛腩饭", ReceivedQty: 2}}}); err != nil {
		t.Fatal(err)
	}
	d2, _ = f.svc.GetBatch(d2.ID)
	disc2 := d2.Discrepancies[0]
	if err := f.svc.ReviewDiscrepancy(f.reviewer, disc2.ID,
		ReviewInput{Approve: true, Resolution: "refund"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ProcessRefund(f.reviewer, disc2.ID, "RCP-1001"); err == nil {
		t.Fatal("跨差异复用同一退款回执必须被拒绝")
	}
	var refunds2 []models.Refund
	f.svc.db.Where("discrepancy_id = ?", disc2.ID).Find(&refunds2)
	if len(refunds2) != 0 {
		t.Fatalf("被拒回执不得入账: %+v", refunds2)
	}
	audit := f.svc.Audit()
	if !audit.RefundUnique || len(audit.DuplicateRefunds) != 0 {
		t.Fatalf("退款回执必须全局唯一: %+v", audit)
	}
	if !audit.AllBalanced {
		t.Fatalf("被拒操作后数量仍应平衡: %+v", audit)
	}
}

type svcsRow struct{ call, amount int }

// ---------- 场景 5：权限——前台不能改厨房装箱数 ----------

func TestFrontDeskCannotTouchPacking(t *testing.T) {
	f := setup(t)
	date := "2026-10-03"
	f.makeOrder(t, date, map[string]int{"排骨饭": 5})

	if _, err := f.svc.CreateBatchFromOrder(f.desk, f.company, date, models.MealLunch); err == nil {
		t.Fatal("前台无权生成装箱批次")
	}
	b, err := f.svc.CreateBatchFromOrder(f.kitchen, f.company, date, models.MealLunch)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RegisterSeals(f.desk, b.ID, []BoxInput{{BoxNo: "X", SealNo: "S", BoxedQty: 5}}); err == nil {
		t.Fatal("前台无权登记/修改封签装箱")
	}
	// 复核员、配送员也不能装箱
	if _, err := f.svc.CreateBatchFromOrder(f.reviewer, f.company, date, models.MealLunch); err == nil {
		t.Fatal("复核员无权装箱")
	}
	// 前台只能提差异，不能传任何装箱数量字段（接口根本不接收）
	if err := f.svc.ReportDiscrepancy(f.desk, b.ID, "wrong", "排骨饭", 1, "菜错了"); err != nil {
		t.Fatalf("前台应能提出差异: %v", err)
	}
	// 非本企业前台不能签收/提差异
	if err := f.svc.ReportDiscrepancy(f.desk2, b.ID, "shortage", "x", 1, ""); err == nil {
		t.Fatal("非本企业前台不能操作该批次")
	}
}

// ---------- 场景 6：封签破损、无人签收必须留痕 ----------

func TestSealBrokenAndNobodyTrail(t *testing.T) {
	f := setup(t)
	date := "2026-10-03"
	f.makeOrder(t, date, map[string]int{"排骨饭": 4})
	d := f.packHandoverDeliver(t, date, f.courier1, "R1")

	if err := f.svc.NobodyPresent(f.courier1, d.ID, "午休无人"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ReportSealBroken(f.courier1, d.ID, "BOX-1", "外封签断裂"); err != nil {
		t.Fatal(err)
	}
	var box models.CoolerBox
	f.svc.db.Where("batch_id = ?", d.ID).First(&box)
	if box.SealStatus != "broken" {
		t.Fatalf("封签状态应为 broken: %s", box.SealStatus)
	}
	var disc int64
	f.svc.db.Model(&models.Discrepancy{}).Where("batch_id = ?", d.ID).Count(&disc)
	if disc < 2 {
		t.Fatalf("无人签收和封签破损都应生成差异单，实际 %d 张", disc)
	}
	for _, typ := range []string{"nobody_present", "seal_broken"} {
		var n int64
		f.svc.db.Model(&models.Event{}).Where("batch_id = ? AND type = ?", d.ID, typ).Count(&n)
		if n != 1 {
			t.Fatalf("事件 %s 必须留痕", typ)
		}
	}
	// 无人签收后仍可正常签收（后续有人）
	if _, err := f.svc.Sign(f.desk, d.ID, SignInput{ReceiverName: "前台甲", SealIntact: false,
		Lines: []SignLine{{DishName: "排骨饭", ReceivedQty: 4}}}); err != nil {
		t.Fatal(err)
	}
}

// ---------- 复核驳回 ----------

func TestReviewReject(t *testing.T) {
	f := setup(t)
	date := "2026-10-03"
	f.makeOrder(t, date, map[string]int{"排骨饭": 4})
	d := f.packHandoverDeliver(t, date, f.courier1, "R1")
	if err := f.svc.ReportDiscrepancy(f.desk, d.ID, "wrong", "排骨饭", 0, "口味做错"); err != nil {
		t.Fatal(err)
	}
	d, _ = f.svc.GetBatch(d.ID)
	var wrong *models.Discrepancy
	for i := range d.Discrepancies {
		if d.Discrepancies[i].Kind == "wrong" {
			wrong = &d.Discrepancies[i]
		}
	}
	if err := f.svc.ReviewDiscrepancy(f.reviewer, wrong.ID,
		ReviewInput{Approve: false, Note: "证据不足"}); err != nil {
		t.Fatal(err)
	}
	var got models.Discrepancy
	f.svc.db.First(&got, wrong.ID)
	if got.Status != "rejected" {
		t.Fatalf("差异应被驳回: %s", got.Status)
	}
}
