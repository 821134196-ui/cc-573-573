package service

import (
	"errors"
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"mealcheck/internal/models"
	"mealcheck/internal/store"
)

type fixture struct {
	t      *testing.T
	db     *gorm.DB
	svc    *Service
	users  map[string]models.User
	ent    models.Enterprise
	routes []models.Route
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	f := &fixture{t: t, db: db, svc: New(db), users: map[string]models.User{}}
	var us []models.User
	db.Find(&us)
	for _, u := range us {
		f.users[u.Username] = u
	}
	db.First(&f.ent, 1)
	db.Order("id asc").Find(&f.routes)
	if len(f.routes) < 2 {
		t.Fatalf("需要至少两条路线，实际 %d", len(f.routes))
	}
	return f
}

func (f *fixture) user(name string) models.User { return f.users[name] }

func must[T any](t *testing.T, v T, err error) T {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return v
}

// mb / mrt 直接接收服务方法的多返回值（Go 仅允许多值作为全部实参）
func (f *fixture) mb(b *models.Batch, err error) *models.Batch {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("unexpected batch error: %v", err)
	}
	return b
}

func (f *fixture) mrt(a *models.RouteAssignment, err error) *models.RouteAssignment {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("unexpected assignment error: %v", err)
	}
	return a
}

func (f *fixture) ok(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("unexpected error: %v", err)
	}
}

func wantErr(t *testing.T, err error, target error, msg string) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("%s: 期望错误 %v，实际 %v", msg, target, err)
	}
}

// 装箱 -> 指派 -> 接收 -> 出发 -> 送达
func (f *fixture) packAndDeliver(routeIdx int, items []ItemInput) *models.Batch {
	t := f.t
	b := f.mb(f.svc.CreateBatch(f.user("kitchen"), CreateBatchInput{
		EnterpriseID: f.ent.ID, MealDate: "2026-10-03", MealType: "lunch", Items: items,
	}))
	seals := make([]string, 0, len(items))
	for i := range items {
		seals = append(seals, "SEAL-"+b.Code+"-"+string(rune('A'+i)))
	}
	b = f.mb(f.svc.Seal(f.user("kitchen"), b.ID, SealInput{SealNumbers: seals}))

	route := f.routes[routeIdx]
	courier := f.userByID(route.CourierID)
	f.mrt(f.svc.Routes().Assign(f.user("admin"), b.ID, route.ID, "交接保温箱"))
	if err := f.svc.Routes().Accept(courier, b.ID, "已接收"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := f.svc.Routes().Depart(courier, b.ID, "出发"); err != nil {
		t.Fatalf("depart: %v", err)
	}
	if err := f.svc.Routes().Deliver(courier, b.ID, "送达前台"); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	return b
}

func (f *fixture) userByID(id uint) models.User {
	var u models.User
	f.db.First(&u, id)
	return u
}

func (f *fixture) assertBalanced(rootID uint) *ConservationReport {
	f.t.Helper()
	rep, err := f.svc.CheckConservation(rootID)
	if err != nil {
		f.t.Fatalf("check conservation: %v", err)
	}
	if !rep.Balanced {
		f.t.Fatalf("数量守恒校验失败: %s", rep.Detail)
	}
	return rep
}

func (f *fixture) signLines(b *models.Batch, received map[string]int, wrong []ReceiptLineInput) []ReceiptLineInput {
	lines := []ReceiptLineInput{}
	for _, it := range b.Items {
		q, ok := received[it.Name]
		if !ok {
			q = it.PackedQty
		}
		id := it.ID
		lines = append(lines, ReceiptLineInput{BatchItemID: &id, Name: it.Name, ExpectedQty: it.PackedQty, ReceivedQty: q})
	}
	return append(lines, wrong...)
}

func (f *fixture) openDiscrepancy(batchID uint) models.Discrepancy {
	var d models.Discrepancy
	if err := f.db.Where("batch_id = ? AND status = ?", batchID, models.DiffOpen).
		Preload("Lines").Order("id desc").First(&d).Error; err != nil {
		f.t.Fatalf("查询待复核差异: %v", err)
	}
	return d
}

// 1) 装箱与签收数量核对：完全相符时整棵树守恒并关闭
func TestPackSignExactMatch(t *testing.T) {
	f := setup(t)
	items := []ItemInput{
		{Name: "红烧肉饭", UnitPrice: 2500, PackedQty: 6},
		{Name: "素食套餐", UnitPrice: 2200, PackedQty: 4},
	}
	b := f.packAndDeliver(0, items)
	rcpt, err := f.svc.Sign(f.user("desk1"), b.ID, SignInput{
		ReceiverName: "前台小周", SealOK: true,
		Lines: f.signLines(b, nil, nil),
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if len(rcpt.Lines) != 2 {
		t.Fatalf("签收行应为 2，实际 %d", len(rcpt.Lines))
	}
	rep := f.assertBalanced(b.RootID)
	if rep.Produced != 10 || rep.Received != 10 {
		t.Fatalf("产出/实收应为 10/10，实际 %d/%d", rep.Produced, rep.Received)
	}
	var closed models.Batch
	f.db.First(&closed, b.ID)
	if closed.Status != models.BatchClosed {
		t.Fatalf("全部相符后批次应关闭，实际 %s", closed.Status)
	}
}

// 2) 前台不能改厨房装箱数；实收不能超过装箱
func TestFrontDeskCannotModifyPacking(t *testing.T) {
	f := setup(t)
	b := f.packAndDeliver(0, []ItemInput{{Name: "鸡腿饭", UnitPrice: 2000, PackedQty: 10}})

	// 前台无权改装箱
	_, err := f.svc.UpdatePacked(f.user("desk1"), b.ID,
		[]ItemInput{{Name: "鸡腿饭", UnitPrice: 2000, PackedQty: 20}})
	wantErr(t, err, ErrForbidden, "前台改装箱")

	// 封箱后厨房也不能改
	_, err = f.svc.UpdatePacked(f.user("kitchen"), b.ID,
		[]ItemInput{{Name: "鸡腿饭", UnitPrice: 2000, PackedQty: 20}})
	wantErr(t, err, ErrSealed, "封箱后改装箱")

	// 签收获准超过装箱 -> 拒绝（餐盒不能凭空增加）
	_, err = f.svc.Sign(f.user("desk1"), b.ID, SignInput{
		ReceiverName: "前台", SealOK: true,
		Lines: f.signLines(b, map[string]int{"鸡腿饭": 11}, nil),
	})
	wantErr(t, err, ErrValidation, "实收超过装箱")
}

// 3) 少盒签收产生差异，数量守恒（装箱=实收+待复核）
func TestShortMealOpenDifference(t *testing.T) {
	f := setup(t)
	b := f.packAndDeliver(0, []ItemInput{
		{Name: "鸡腿饭", UnitPrice: 2500, PackedQty: 10},
	})
	_, err := f.svc.Sign(f.user("desk1"), b.ID, SignInput{
		ReceiverName: "前台", SealOK: true,
		Lines: f.signLines(b, map[string]int{"鸡腿饭": 8}, nil),
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	rep := f.assertBalanced(b.RootID)
	if rep.Produced != 10 || rep.Received != 8 || rep.OpenClaim != 2 {
		t.Fatalf("应为 产出10=实收8+待复核2，实际 %+v", rep)
	}
	d := f.openDiscrepancy(b.ID)
	if d.Lines[0].ShortQty != 2 {
		t.Fatalf("短少应为 2，实际 %d", d.Lines[0].ShortQty)
	}
}

// 4) 途中改派：旧路线关闭、新路线生效；同批次不可能两条路线同时完成
func TestReassignUniqueness(t *testing.T) {
	f := setup(t)
	b := f.packAndDeliver(0, []ItemInput{{Name: "鸡排饭", UnitPrice: 2300, PackedQty: 5}})

	// packAndDeliver 已送达；模拟签收前调度二次指派必须走改派
	_, err := f.svc.Routes().Assign(f.user("admin"), b.ID, f.routes[1].ID, "再指派")
	wantErr(t, err, ErrConflict, "已有在途路线时再次指派")

	// 为了覆盖途中改派，另起批次：出发后立即改派
	b2 := f.packAndDeliverSealed(1)
	c1 := f.userByID(f.routes[1].CourierID)
	if err := f.svc.Routes().Accept(c1, b2.ID, "接收"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := f.svc.Routes().Depart(c1, b2.ID, "出发"); err != nil {
		t.Fatalf("depart: %v", err)
	}
	if _, err := f.svc.Routes().Reassign(f.user("admin"), b2.ID, f.routes[0].ID, "车辆故障改派"); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	var as []models.RouteAssignment
	f.db.Where("batch_id = ?", b2.ID).Order("seq asc").Find(&as)
	if len(as) != 2 || as[0].Active || as[0].Status != models.AssignReassigned || !as[1].Active {
		t.Fatalf("改派后旧段应关闭/新段生效: %+v", as)
	}
	if as[0].Status == models.AssignDelivered {
		t.Fatal("旧路线不得显示已完成送达")
	}
	var active int64
	f.db.Model(&models.RouteAssignment{}).Where("batch_id = ? AND active = ?", b2.ID, true).Count(&active)
	if active != 1 {
		t.Fatalf("有效路线任务必须恰好 1 条，实际 %d", active)
	}
	// 数据库部分唯一索引兜底：手工插第二条 active 必须失败
	dup := models.RouteAssignment{BatchID: b2.ID, RouteID: f.routes[2].ID, CourierID: 999, Seq: 9, Status: models.AssignAssigned, Active: true}
	if err := f.db.Create(&dup).Error; err == nil {
		t.Fatal("数据库允许同一批次两条 active 路线任务，唯一索引缺失")
	}

	// 旧配送员不能继续操作改派后的批次
	err = f.svc.Routes().Deliver(c1, b2.ID, "旧配送员送达")
	wantErr(t, err, ErrForbidden, "旧配送员继续操作")
}

// packAndDeliverSealed 只装箱封箱并指派（不接收）
func (f *fixture) packAndDeliverSealed(routeIdx int) *models.Batch {
	b := f.mb(f.svc.CreateBatch(f.user("kitchen"), CreateBatchInput{
		EnterpriseID: f.ent.ID, MealDate: "2026-10-04", MealType: "dinner",
		Items: []ItemInput{{Name: "牛腩饭", UnitPrice: 2600, PackedQty: 5}},
	}))
	b = f.mb(f.svc.Seal(f.user("kitchen"), b.ID, SealInput{SealNumbers: []string{"SEAL-R2-1"}}))
	f.mrt(f.svc.Routes().Assign(f.user("admin"), b.ID, f.routes[routeIdx].ID, ""))
	return b
}

// 5) 差异复核：分配必须对得上短少数；补送沿原批次发起；最终守恒
func TestReviewResupplyRefundChain(t *testing.T) {
	f := setup(t)
	b := f.packAndDeliver(0, []ItemInput{{Name: "鸡腿饭", UnitPrice: 2500, PackedQty: 10}})
	if _, err := f.svc.Sign(f.user("desk1"), b.ID, SignInput{
		ReceiverName: "前台", SealOK: true,
		Lines: f.signLines(b, map[string]int{"鸡腿饭": 8}, nil),
	}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	d := f.openDiscrepancy(b.ID)

	// 分配之和 != 短少 -> 拒绝且差异保持 open
	bad := ApproveInput{Lines: []ReviewLineInput{
		{LineID: d.Lines[0].ID, ResupplyQty: 1, LossQty: 0, RefundQty: 0}, // 只覆盖 1/2
	}}
	_, err := f.svc.Review().Approve(f.user("reviewer"), d.ID, bad)
	wantErr(t, err, ErrValidation, "复核分配对不上短少数")
	f.db.First(&d, d.ID)
	if d.Status != models.DiffOpen {
		t.Fatalf("被拒绝的复核不得改变差异状态，实际 %s", d.Status)
	}

	// 正确分配：1 补送 + 0 损失 + 1 退款（退款不走自动入账，验证待入账桶）
	good := ApproveInput{Lines: []ReviewLineInput{
		{LineID: d.Lines[0].ID, ResupplyQty: 1, LossQty: 0, RefundQty: 1},
	}, Note: "1盒补送1盒退款"}
	res, err := f.svc.Review().Approve(f.user("reviewer"), d.ID, good)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if res.ResupplyBatchID == nil {
		t.Fatal("批准补送后必须创建补送子批次")
	}
	childID := *res.ResupplyBatchID
	var child models.Batch
	f.db.First(&child, childID)
	if child.ParentID == nil || *child.ParentID != b.ID || child.RootID != b.RootID {
		t.Fatal("补送批次必须挂在原批次树下")
	}
	if child.SourceDiscrepancyID == nil || *child.SourceDiscrepancyID != d.ID {
		t.Fatal("补送批次必须保留来源差异单作为数量依据")
	}
	if child.PackedTotal != 1 {
		t.Fatalf("补送批次计划数量应为 1，实际 %d", child.PackedTotal)
	}
	rep := f.assertBalanced(b.RootID)
	if rep.Produced != 10 || rep.Received != 8 || rep.Resupplied != 1 || rep.RefundWait != 1 {
		t.Fatalf("批准后应为 10=8+补送1+待退款1，实际 %+v", rep)
	}

	// 支付回执入账（幂等测试在另一用例，这里先入账）
	if res.Refund == nil || res.Refund.Status != "pending" {
		t.Fatalf("退款应已创建为 pending，实际 %+v", res.Refund)
	}
	if _, err := f.svc.Review().PostRefundReceipt(res.Resolution.ID, "GW-1"); err != nil {
		t.Fatalf("post receipt: %v", err)
	}

	// 厨房补餐封箱、沿路线送达、前台补签收
	childSealed, err := f.svc.Seal(f.user("kitchen"), childID, SealInput{SealNumbers: []string{"SEAL-CHILD-1"}})
	if err != nil {
		t.Fatalf("child seal: %v", err)
	}
	route := f.routes[0]
	courier := f.userByID(route.CourierID)
	if _, err := f.svc.Routes().Assign(f.user("admin"), childSealed.ID, route.ID, "补送"); err != nil {
		t.Fatalf("assign child: %v", err)
	}
	if err := f.svc.Routes().Accept(courier, childSealed.ID, ""); err != nil {
		t.Fatalf("accept child: %v", err)
	}
	if err := f.svc.Routes().Depart(courier, childSealed.ID, ""); err != nil {
		t.Fatalf("depart child: %v", err)
	}
	if err := f.svc.Routes().Deliver(courier, childSealed.ID, ""); err != nil {
		t.Fatalf("deliver child: %v", err)
	}
	if _, err := f.svc.Sign(f.user("desk1"), childSealed.ID, SignInput{
		ReceiverName: "前台", SealOK: true,
		Lines: f.signLines(childSealed, nil, nil),
	}); err != nil {
		t.Fatalf("sign child: %v", err)
	}

	// 最终核对：厨房共产出 11 盒（原 10 + 补 1），去向 实收9 + 补送核销1 + 退款1
	rep = f.assertBalanced(b.RootID)
	if rep.Produced != 11 || rep.Received != 9 || rep.Resupplied != 1 || rep.Refunded != 1 {
		t.Fatalf("最终应为 产出11=实收9+补送1+退款1，实际 %+v", rep)
	}
	if rep.ResupplyPacked != 1 || rep.ResupplyApproved != 1 {
		t.Fatalf("补送依据应为批准1/装箱1，实际 %d/%d", rep.ResupplyApproved, rep.ResupplyPacked)
	}
	var all []models.Batch
	f.db.Where("root_id = ?", b.RootID).Find(&all)
	for _, x := range all {
		if x.Status != models.BatchClosed {
			t.Fatalf("结清后批次#%d 应为 closed，实际 %s", x.ID, x.Status)
		}
	}
}

// 6) 重复退款回执只入账一次
func TestRefundReceiptIdempotent(t *testing.T) {
	f := setup(t)
	b := f.packAndDeliver(0, []ItemInput{{Name: "鸡腿饭", UnitPrice: 2500, PackedQty: 3}})
	if _, err := f.svc.Sign(f.user("desk1"), b.ID, SignInput{
		ReceiverName: "前台", SealOK: true,
		Lines: f.signLines(b, map[string]int{"鸡腿饭": 1}, nil),
	}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	d := f.openDiscrepancy(b.ID)
	res, err := f.svc.Review().Approve(f.user("reviewer"), d.ID, ApproveInput{
		Lines: []ReviewLineInput{{LineID: d.Lines[0].ID, RefundQty: 2}},
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if res.Refund.Amount != 2*2500 || res.Refund.Qty != 2 {
		t.Fatalf("退款金额/份数应为 5000/2，实际 %d/%d", res.Refund.Amount, res.Refund.Qty)
	}
	// 第一次入账
	if _, err := f.svc.Review().PostRefundReceipt(res.Resolution.ID, "GW-DUP-1"); err != nil {
		t.Fatalf("first receipt: %v", err)
	}
	// 重复回执：返回 ErrAlreadyPosted，不重复入账
	_, err = f.svc.Review().PostRefundReceipt(res.Resolution.ID, "GW-DUP-1")
	wantErr(t, err, ErrAlreadyPosted, "重复退款回执")
	// 再推一次不同回执号也只认原单
	_, err = f.svc.Review().PostRefundReceipt(res.Resolution.ID, "GW-DUP-2")
	wantErr(t, err, ErrAlreadyPosted, "第三次退款回执")

	var n int64
	var postedSum int
	var rows []models.RefundRecord
	f.db.Where("batch_id = ?", b.RootID).Find(&rows)
	for _, r := range rows {
		if r.Status == "posted" {
			n++
			postedSum += r.Amount
		}
	}
	if n != 1 || postedSum != 5000 {
		t.Fatalf("已入账退款应仅 1 笔 5000 分，实际 %d 笔 %d 分", n, postedSum)
	}
}

// 7) 错餐：错餐盒不计入正常实收，批准后实物退回，正确餐盒仍守恒
func TestWrongMealHandling(t *testing.T) {
	f := setup(t)
	b := f.packAndDeliver(0, []ItemInput{{Name: "鸡腿饭", UnitPrice: 2500, PackedQty: 10}})
	wrong := []ReceiptLineInput{{Name: "鱼香肉丝饭(错送)", ReceivedQty: 1, Wrong: true}}
	if _, err := f.svc.Sign(f.user("desk1"), b.ID, SignInput{
		ReceiverName: "前台", SealOK: true,
		Lines: f.signLines(b, map[string]int{"鸡腿饭": 9}, wrong),
	}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	rep := f.assertBalanced(b.RootID)
	if rep.Received != 9 || rep.OpenClaim != 1 || rep.WrongReceived != 1 || rep.WrongHeld != 1 {
		t.Fatalf("少1+错1状态不符: %+v", rep)
	}
	d := f.openDiscrepancy(b.ID)
	// 找到短少行与错餐行
	var shortLine, wrongLine *models.DiscrepancyLine
	for i := range d.Lines {
		if d.Lines[i].Wrong {
			wrongLine = &d.Lines[i]
		} else {
			shortLine = &d.Lines[i]
		}
	}
	if shortLine == nil || wrongLine == nil {
		t.Fatal("差异单应同时包含短少行与错餐行")
	}
	// 错餐行不得产生补送/退款
	_, err := f.svc.Review().Approve(f.user("reviewer"), d.ID, ApproveInput{
		Lines: []ReviewLineInput{
			{LineID: shortLine.ID, LossQty: 1},
			{LineID: wrongLine.ID, RefundQty: 1}, // 非法
		},
	})
	wantErr(t, err, ErrValidation, "错餐行退款")

	if _, err := f.svc.Review().Approve(f.user("reviewer"), d.ID, ApproveInput{
		Lines: []ReviewLineInput{
			{LineID: shortLine.ID, LossQty: 1},
			{LineID: wrongLine.ID}, // 错餐默认退回厨房
		},
		Note: "少1核销，错餐退回",
	}); err != nil {
		t.Fatalf("approve: %v", err)
	}
	rep = f.assertBalanced(b.RootID)
	if rep.Produced != 10 || rep.Received != 9 || rep.Lost != 1 || rep.WrongReturn != 1 || rep.WrongHeld != 0 {
		t.Fatalf("复核后状态不符: %+v", rep)
	}
}

// 8) 封签破损与无人签收必须留经过
func TestSealBrokenAndNoOne(t *testing.T) {
	f := setup(t)
	b := f.packAndDeliver(0, []ItemInput{{Name: "卤肉饭", UnitPrice: 2400, PackedQty: 4}})
	route := f.routes[0]
	courier := f.userByID(route.CourierID)

	// 无人签收
	if err := f.svc.Routes().NoOne(courier, b.ID, "12:30 前台无人"); err != nil {
		t.Fatalf("no one: %v", err)
	}
	var noOneReceipt int64
	f.db.Model(&models.Receipt{}).Where("batch_id = ? AND status = 'no_one'", b.ID).Count(&noOneReceipt)
	if noOneReceipt != 1 {
		t.Fatal("无人签收必须留下回执记录")
	}
	rep := f.assertBalanced(b.RootID)
	if rep.InFlight != 4 || rep.Produced != 4 {
		t.Fatalf("无人签收后餐盒应仍在途: %+v", rep)
	}

	// 改派后新配送员送达
	if _, err := f.svc.Routes().Reassign(f.user("admin"), b.ID, f.routes[1].ID, "无人签收改派机动"); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	c2 := f.userByID(f.routes[1].CourierID)
	if err := f.svc.Routes().Accept(c2, b.ID, ""); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := f.svc.Routes().Depart(c2, b.ID, ""); err != nil {
		t.Fatalf("depart: %v", err)
	}
	if err := f.svc.Routes().Deliver(c2, b.ID, "二次送达"); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	// 封签破损登记
	if err := f.svc.Routes().ReportBrokenSeal(c2, b.ID, b.Boxes[0].SealNo, "外封签断裂"); err != nil {
		t.Fatalf("broken seal: %v", err)
	}
	var box models.Box
	f.db.First(&box, b.Boxes[0].ID)
	if box.Status != "broken" || box.BrokenAt == nil {
		t.Fatal("封签必须登记为破损并记录时间")
	}
	var brokenDisc int64
	f.db.Model(&models.Discrepancy{}).
		Where("batch_id = ? AND kind = ?", b.ID, models.DiffBroken).Count(&brokenDisc)
	if brokenDisc != 1 {
		t.Fatal("封签破损必须生成差异单待复核")
	}
	var evs []models.RouteEvent
	f.db.Where("batch_id = ? AND type IN ?", b.ID, []string{"no_one", "reassign", "seal_broken"}).Find(&evs)
	if len(evs) != 3 {
		t.Fatalf("无人签收/改派/破损三类经过都应留痕，实际 %d 条: %+v", len(evs), evs)
	}
}

// 9) 复核驳回：短少成为争议数量，不允许退款/损失
func TestReviewReject(t *testing.T) {
	f := setup(t)
	b := f.packAndDeliver(0, []ItemInput{{Name: "排骨饭", UnitPrice: 2500, PackedQty: 5}})
	if _, err := f.svc.Sign(f.user("desk1"), b.ID, SignInput{
		ReceiverName: "前台", SealOK: true,
		Lines: f.signLines(b, map[string]int{"排骨饭": 4}, nil),
	}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	d := f.openDiscrepancy(b.ID)
	if err := f.svc.Review().Reject(f.user("reviewer"), d.ID, "监控显示装箱足量"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	rep := f.assertBalanced(b.RootID)
	if rep.Disputed != 1 || rep.Produced != 5 || rep.Received != 4 {
		t.Fatalf("驳回后应为 产出5=实收4+争议1，实际 %+v", rep)
	}
	var refunds int64
	f.db.Model(&models.RefundRecord{}).Count(&refunds)
	if refunds != 0 {
		t.Fatalf("驳回不得产生退款，实际 %d 笔", refunds)
	}
}

// 10) 补送餐盒凭空增加：厨房把补送批次加成超过批准份数，封箱必须被守恒闸门拦截
func TestResupplyOverpackBlocked(t *testing.T) {
	f := setup(t)
	b := f.packAndDeliver(0, []ItemInput{{Name: "鸡腿饭", UnitPrice: 2500, PackedQty: 10}})
	if _, err := f.svc.Sign(f.user("desk1"), b.ID, SignInput{
		ReceiverName: "前台", SealOK: true,
		Lines: f.signLines(b, map[string]int{"鸡腿饭": 9}, nil),
	}); err != nil {
		t.Fatalf("sign: %v", err)
	}
	d := f.openDiscrepancy(b.ID)
	res, err := f.svc.Review().Approve(f.user("reviewer"), d.ID, ApproveInput{
		Lines: []ReviewLineInput{{LineID: d.Lines[0].ID, ResupplyQty: 1}},
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	childID := *res.ResupplyBatchID
	// 厨房试图在补送批次里装 2 盒（批准仅 1 盒）
	_, err = f.svc.UpdatePacked(f.user("kitchen"), childID,
		[]ItemInput{{Name: "鸡腿饭", UnitPrice: 2500, PackedQty: 2}})
	f.ok(err)
	_, err = f.svc.Seal(f.user("kitchen"), childID, SealInput{SealNumbers: []string{"SEAL-OVER-1"}})
	wantErr(t, err, ErrConflict, "补送超批封箱")

	// 改回批准数量即可正常封箱
	_, err = f.svc.UpdatePacked(f.user("kitchen"), childID,
		[]ItemInput{{Name: "鸡腿饭", UnitPrice: 2500, PackedQty: 1}})
	f.ok(err)
	f.mb(f.svc.Seal(f.user("kitchen"), childID, SealInput{SealNumbers: []string{"SEAL-OK-1"}}))
	f.assertBalanced(b.RootID)
}

// 11) 同企业同餐次不得另建与原餐次无关的订单，少餐只能沿批次树补送
func TestNoDuplicateRootBatch(t *testing.T) {
	f := setup(t)
	f.packAndDeliver(0, []ItemInput{{Name: "鸡腿饭", UnitPrice: 2500, PackedQty: 10}})
	_, err := f.svc.CreateBatch(f.user("kitchen"), CreateBatchInput{
		EnterpriseID: f.ent.ID, MealDate: "2026-10-03", MealType: "lunch",
		Items: []ItemInput{{Name: "鸡腿饭", UnitPrice: 2500, PackedQty: 1}},
	})
	wantErr(t, err, ErrConflict, "重复创建同餐次原批次")
}

// 12) 角色权限：配送员不能封箱、复核员不能签收
func TestRoleBoundaries(t *testing.T) {
	f := setup(t)
	b, err := f.svc.CreateBatch(f.user("kitchen"), CreateBatchInput{
		EnterpriseID: f.ent.ID, MealDate: "2026-10-05", MealType: "lunch",
		Items: []ItemInput{{Name: "鸡腿饭", UnitPrice: 2500, PackedQty: 3}},
	})
	f.ok(err)
	if _, err := f.svc.Seal(f.user("c1"), b.ID, SealInput{SealNumbers: []string{"X"}}); err == nil {
		t.Fatal("配送员封箱必须被拒绝")
	}
	if err := f.svc.Routes().Accept(f.user("c2"), b.ID, ""); err == nil {
		t.Fatal("非本路线配送员接收必须被拒绝")
	}
	// 复核员无权签收
	f.mb(f.svc.Seal(f.user("kitchen"), b.ID, SealInput{SealNumbers: []string{"SEAL-PERM-1"}}))
	f.mrt(f.svc.Routes().Assign(f.user("admin"), b.ID, f.routes[0].ID, ""))
	c := f.userByID(f.routes[0].CourierID)
	f.ok(f.svc.Routes().Accept(c, b.ID, ""))
	f.ok(f.svc.Routes().Depart(c, b.ID, ""))
	f.ok(f.svc.Routes().Deliver(c, b.ID, ""))
	_, err = f.svc.Sign(f.user("reviewer"), b.ID, SignInput{
		ReceiverName: "复核员", SealOK: true,
		Lines: f.signLines(b, nil, nil),
	})
	wantErr(t, err, ErrForbidden, "复核员签收")
}
