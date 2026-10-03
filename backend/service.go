package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"campusmeal/models"
	"campusmeal/svcs"

	"gorm.io/gorm"
)

var (
	ErrForbidden        = errors.New("无权操作：该角色不能执行此动作")
	ErrBadState         = errors.New("批次状态不允许该操作")
	ErrQtyMismatch      = errors.New("数量核对不一致")
	ErrNotFound         = errors.New("记录不存在")
	ErrDuplicateReceipt = errors.New("退款回执已入账，不能重复入账")
)

type Service struct {
	db       *gorm.DB
	pay      *svcs.MockPayment
	delivery *svcs.MockDelivery
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db, pay: svcs.NewMockPayment(db), delivery: svcs.NewMockDelivery(db)}
}

func actorName(u *models.User) string {
	role := map[string]string{
		models.RoleKitchen: "厨房", models.RoleCourier: "配送员",
		models.RoleFrontDesk: "前台", models.RoleReviewer: "复核员",
	}[u.Role]
	return fmt.Sprintf("%s·%s", role, u.Name)
}

func (s *Service) logEvent(tx *gorm.DB, batchID uint, u *models.User, typ, detail string) {
	name := "系统"
	if u != nil {
		name = actorName(u)
	}
	tx.Create(&models.Event{BatchID: batchID, ActorID: func() uint {
		if u != nil {
			return u.ID
		}
		return 0
	}(), Actor: name, Type: typ, Detail: detail})
}

func requireRole(u *models.User, roles ...string) error {
	for _, r := range roles {
		if u.Role == r {
			return nil
		}
	}
	return ErrForbidden
}

// ---------- 厨房：按订单生成装箱批次 ----------

type BoxInput struct {
	BoxNo    string `json:"box_no"`
	SealNo   string `json:"seal_no"`
	BoxedQty int    `json:"boxed_qty"`
}

func (s *Service) nextBatchNo(tx *gorm.DB, date string, kind string) (string, error) {
	var n int64
	tx.Model(&models.Batch{}).Where("meal_date = ?", date).Count(&n)
	suffix := ""
	if kind == "resupply" {
		suffix = "R"
	}
	return fmt.Sprintf("B%s%s-%04d", strings.ReplaceAll(date, "-", ""), suffix, n+1), nil
}

// CreateBatchFromOrder 厨房按企业+餐次的订单生成装箱批次。装箱数锁定为订单应收数。
func (s *Service) CreateBatchFromOrder(u *models.User, companyID uint, mealDate, mealType string) (*models.Batch, error) {
	if err := requireRole(u, models.RoleKitchen); err != nil {
		return nil, err
	}
	var order models.MealOrder
	if err := s.db.Preload("Lines").
		Where("company_id = ? AND meal_date = ? AND meal_type = ?", companyID, mealDate, mealType).
		First(&order).Error; err != nil {
		return nil, fmt.Errorf("%w: 该企业餐次无订单", ErrNotFound)
	}
	if len(order.Lines) == 0 {
		return nil, errors.New("订单没有菜品，无法装箱")
	}

	tx := s.db.Begin()
	defer func() { _ = tx.Rollback() }()

	// 同一企业同一餐次只允许一个初始批次（补送批次另计）
	var cnt int64
	tx.Model(&models.Batch{}).
		Where("company_id = ? AND meal_date = ? AND meal_type = ? AND kind = ?", companyID, mealDate, mealType, "initial").
		Count(&cnt)
	if cnt > 0 {
		return nil, errors.New("该企业餐次已存在初始批次")
	}

	no, _ := s.nextBatchNo(tx, mealDate, "initial")
	batch := models.Batch{BatchNo: no, CompanyID: companyID, MealDate: mealDate, MealType: mealType,
		Kind: "initial", Status: models.BatchPacked}
	if err := tx.Create(&batch).Error; err != nil {
		return nil, err
	}
	total := 0
	detail := []string{}
	for _, l := range order.Lines {
		pi := models.PackItem{BatchID: batch.ID, DishName: l.DishName, Qty: l.OrderedQty}
		if err := tx.Create(&pi).Error; err != nil {
			return nil, err
		}
		tx.Create(&models.BoxLedger{BatchID: batch.ID, Change: l.OrderedQty, Kind: models.LedgerPacked,
			RefType: "order_line", RefID: l.ID, Note: fmt.Sprintf("装箱 %s ×%d", l.DishName, l.OrderedQty)})
		total += l.OrderedQty
		detail = append(detail, fmt.Sprintf("%s×%d", l.DishName, l.OrderedQty))
	}
	batch.PackedQty = total
	if err := tx.Model(&batch).Update("packed_qty", total).Error; err != nil {
		return nil, err
	}
	s.logEvent(tx, batch.ID, u, "packed",
		fmt.Sprintf("按订单生成装箱清单，共 %d 盒：%s", total, strings.Join(detail, "、")))

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	var out models.Batch
	if err := s.db.First(&out, batch.ID).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

// RegisterSeals 厨房为保温箱登记封签号；箱内盒数合计必须等于装箱数。
func (s *Service) RegisterSeals(u *models.User, batchID uint, boxes []BoxInput) error {
	if err := requireRole(u, models.RoleKitchen); err != nil {
		return err
	}
	if len(boxes) == 0 {
		return errors.New("至少登记一个保温箱")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var batch models.Batch
		if err := tx.First(&batch, batchID).Error; err != nil {
			return ErrNotFound
		}
		if batch.Status != models.BatchPacked {
			return fmt.Errorf("%w: 只有待交接批次可登记封签", ErrBadState)
		}
		seals := map[string]bool{}
		sum := 0
		for _, b := range boxes {
			if b.SealNo == "" || b.BoxNo == "" || b.BoxedQty <= 0 {
				return errors.New("箱号、封签号、盒数都必须有效")
			}
			if seals[b.SealNo] {
				return fmt.Errorf("封签号重复: %s", b.SealNo)
			}
			seals[b.SealNo] = true
			sum += b.BoxedQty
		}
		if sum != batch.PackedQty {
			return fmt.Errorf("%w: 保温箱登记 %d 盒，装箱清单为 %d 盒", ErrQtyMismatch, sum, batch.PackedQty)
		}
		for _, b := range boxes {
			if err := tx.Create(&models.CoolerBox{BatchID: batchID, BoxNo: b.BoxNo, SealNo: b.SealNo,
				SealStatus: "sealed", BoxedQty: b.BoxedQty}).Error; err != nil {
				return err
			}
		}
		s.logEvent(tx, batchID, u, "seal_register",
			fmt.Sprintf("登记 %d 个保温箱封签，合计 %d 盒", len(boxes), sum))
		return nil
	})
}

// ---------- 配送：交接、改派、送达、异常 ----------

func (s *Service) Handover(u *models.User, batchID uint, courierID uint, routeName string) error {
	if err := requireRole(u, models.RoleKitchen, models.RoleCourier); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var batch models.Batch
		if err := tx.First(&batch, batchID).Error; err != nil {
			return ErrNotFound
		}
		if batch.Status != models.BatchPacked {
			return fmt.Errorf("%w: 批次已交接或已结束", ErrBadState)
		}
		var boxN int64
		tx.Model(&models.CoolerBox{}).Where("batch_id = ?", batchID).Count(&boxN)
		var boxedSum int64
		tx.Model(&models.CoolerBox{}).Where("batch_id = ?", batchID).Select("coalesce(sum(boxed_qty),0)").Scan(&boxedSum)
		if boxN == 0 || int(boxedSum) != batch.PackedQty {
			return fmt.Errorf("%w: 封签未登记或箱内数量与装箱数不符", ErrQtyMismatch)
		}
		var courier models.User
		if err := tx.First(&courier, courierID).Error; err != nil {
			return fmt.Errorf("配送员不存在: %w", ErrNotFound)
		}
		if courier.Role != models.RoleCourier {
			return errors.New("接收人不是配送员")
		}
		ra := models.RouteAssignment{BatchID: batchID, RouteName: routeName, CourierID: courierID, Active: true,
			Note: "首次交接"}
		if err := tx.Create(&ra).Error; err != nil {
			return errors.New("该批次已有进行中的路线")
		}
		tracking, err := s.delivery.Dispatch(tx, batch.BatchNo, routeName, courier.Name)
		if err != nil {
			return err
		}
		tx.Model(&batch).Update("status", models.BatchInTransit)
		s.logEvent(tx, batchID, u, "handover",
			fmt.Sprintf("配送员 %s 接货，路线「%s」，调度单号 %s", courier.Name, routeName, tracking))
		return nil
	})
}

// Reroute 途中改派：结束当前路线（留痕），新路线激活。批次不会同时存在两条 active 路线。
func (s *Service) Reroute(u *models.User, batchID uint, newCourierID uint, newRoute, note string) error {
	if err := requireRole(u, models.RoleKitchen, models.RoleCourier, models.RoleReviewer); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var batch models.Batch
		if err := tx.First(&batch, batchID).Error; err != nil {
			return ErrNotFound
		}
		if batch.Status != models.BatchInTransit {
			return fmt.Errorf("%w: 只有在途批次可以改派", ErrBadState)
		}
		var cur models.RouteAssignment
		if err := tx.Where("batch_id = ? AND active = ?", batchID, true).First(&cur).Error; err != nil {
			return errors.New("当前没有进行中的路线，无法改派")
		}
		var nc models.User
		if err := tx.First(&nc, newCourierID).Error; err != nil {
			return fmt.Errorf("新配送员不存在: %w", ErrNotFound)
		}
		if nc.Role != models.RoleCourier {
			return errors.New("改派对象不是配送员")
		}
		now := time.Now()
		cur.Active = false
		cur.EndedAt = &now
		cur.Note = fmt.Sprintf("改派结束: %s", note)
		tx.Save(&cur)

		nr := models.RouteAssignment{BatchID: batchID, RouteName: newRoute, CourierID: newCourierID, Active: true,
			Note: fmt.Sprintf("由路线 %s 改派: %s", cur.RouteName, note)}
		if err := tx.Create(&nr).Error; err != nil {
			return err
		}
		tracking, err := s.delivery.Reroute(tx, batch.BatchNo, newRoute, nc.Name)
		if err != nil {
			return err
		}
		s.logEvent(tx, batchID, u, "reroute",
			fmt.Sprintf("途中改派：%s → 配送员 %s / 路线「%s」（调度单 %s）。%s",
				cur.RouteName, nc.Name, newRoute, tracking, note))
		return nil
	})
}

func (s *Service) Deliver(u *models.User, batchID uint) error {
	if err := requireRole(u, models.RoleCourier); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var batch models.Batch
		if err := tx.First(&batch, batchID).Error; err != nil {
			return ErrNotFound
		}
		if batch.Status != models.BatchInTransit {
			return fmt.Errorf("%w: 只有在途批次可以送达", ErrBadState)
		}
		var ra models.RouteAssignment
		if err := tx.Where("batch_id = ? AND active = ?", batchID, true).First(&ra).Error; err != nil {
			return errors.New("没有进行中的路线，不能送达")
		}
		var courier models.User
		tx.First(&courier, ra.CourierID)
		// 送达只登记事件，路线保持 active 直到签收完成（避免两条路线都显示已完成）
		tx.Model(&batch).Update("status", models.BatchDelivered)
		s.logEvent(tx, batchID, u, "deliver",
			fmt.Sprintf("配送员 %s 按路线「%s」送达企业，等待前台签收", courier.Name, ra.RouteName))
		return nil
	})
}

// ReportSealBroken 封签破损必须留痕。
func (s *Service) ReportSealBroken(u *models.User, batchID uint, boxNo, note string) error {
	if err := requireRole(u, models.RoleCourier, models.RoleFrontDesk); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var batch models.Batch
		if err := tx.First(&batch, batchID).Error; err != nil {
			return ErrNotFound
		}
		if u.Role == models.RoleFrontDesk && (u.CompanyID == nil || *u.CompanyID != batch.CompanyID) {
			return ErrForbidden
		}
		q := tx.Model(&models.CoolerBox{}).Where("batch_id = ?", batchID)
		if boxNo != "" {
			q = q.Where("box_no = ?", boxNo)
		}
		res := q.Update("seal_status", "broken")
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: 保温箱不存在", ErrNotFound)
		}
		s.logEvent(tx, batchID, u, "seal_broken",
			fmt.Sprintf("封签破损（箱号 %s）：%s", boxNo, note))
		d := models.Discrepancy{BatchID: batchID, Kind: "damage", DishName: boxNo, ShortQty: 0,
			Reason: note, Status: "open"}
		tx.Create(&d)
		return nil
	})
}

// NobodyPresent 无人签收留痕。
func (s *Service) NobodyPresent(u *models.User, batchID uint, note string) error {
	if err := requireRole(u, models.RoleCourier); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var batch models.Batch
		if err := tx.First(&batch, batchID).Error; err != nil {
			return ErrNotFound
		}
		if batch.Status != models.BatchDelivered && batch.Status != models.BatchInTransit {
			return fmt.Errorf("%w: 当前状态不能登记无人签收", ErrBadState)
		}
		s.logEvent(tx, batchID, u, "nobody_present", fmt.Sprintf("到达后无人签收：%s", note))
		tx.Create(&models.Discrepancy{BatchID: batchID, Kind: "nobody", Reason: note, Status: "open"})
		return nil
	})
}

// ---------- 前台：签收核对 ----------

type SignLine struct {
	DishName    string `json:"dish_name"`
	ReceivedQty int    `json:"received_qty"`
}

type SignInput struct {
	ReceiverName string     `json:"receiver_name"`
	SealIntact   bool       `json:"seal_intact"`
	Lines        []SignLine `json:"lines"`
	Note         string     `json:"note"`
}

// Sign 企业前台签收：并列核对应收/实收/差额。实收任何一行都不得超过应收。
func (s *Service) Sign(u *models.User, batchID uint, in SignInput) (*models.Receipt, error) {
	if err := requireRole(u, models.RoleFrontDesk); err != nil {
		return nil, err
	}
	var receipt *models.Receipt
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var batch models.Batch
		if err := tx.First(&batch, batchID).Error; err != nil {
			return ErrNotFound
		}
		if u.CompanyID == nil || *u.CompanyID != batch.CompanyID {
			return ErrForbidden
		}
		if batch.Status != models.BatchDelivered && batch.Status != models.BatchInTransit {
			return fmt.Errorf("%w: 批次尚未送达，不能签收", ErrBadState)
		}
		var existed int64
		tx.Model(&models.Receipt{}).Where("batch_id = ?", batchID).Count(&existed)
		if existed > 0 {
			return errors.New("该批次已签收，不能重复签收")
		}
		var packs []models.PackItem
		tx.Where("batch_id = ?", batchID).Find(&packs)
		packMap := map[string]int{}
		for _, p := range packs {
			packMap[p.DishName] = p.Qty
		}
		recvMap := map[string]int{}
		for _, l := range in.Lines {
			exp, ok := packMap[l.DishName]
			if !ok {
				return fmt.Errorf("%w: 装箱清单中没有菜品「%s」，前台不能自行增加", ErrQtyMismatch, l.DishName)
			}
			if l.ReceivedQty < 0 {
				return errors.New("实收数量不能为负")
			}
			if l.ReceivedQty > exp {
				return fmt.Errorf("%w: 「%s」实收 %d 超过应收 %d，餐盒不能凭空增加",
					ErrQtyMismatch, l.DishName, l.ReceivedQty, exp)
			}
			recvMap[l.DishName] = l.ReceivedQty
		}

		rc := models.Receipt{BatchID: batchID, ReceiverName: in.ReceiverName, SealIntact: in.SealIntact,
			Note: in.Note}
		signedTotal := 0
		for _, p := range packs {
			got := recvMap[p.DishName]
			rc.Lines = append(rc.Lines, models.ReceiptLine{
				DishName: p.DishName, ExpectedQty: p.Qty, ReceivedQty: got})
			signedTotal += got
		}
		rc.ReceivedQty = signedTotal
		rc.SignedQty = signedTotal
		if err := tx.Create(&rc).Error; err != nil {
			return err
		}
		tx.Model(&batch).Updates(map[string]any{"status": models.BatchSigned, "signed_qty": signedTotal})
		tx.Create(&models.BoxLedger{BatchID: batchID, Change: -signedTotal, Kind: models.LedgerSigned,
			RefType: "receipt", RefID: rc.ID, Note: fmt.Sprintf("前台签收 %d 盒", signedTotal)})

		// 逐菜品生成少盒差异
		parts := []string{}
		for _, l := range rc.Lines {
			if l.ReceivedQty < l.ExpectedQty {
				diff := l.ExpectedQty - l.ReceivedQty
				tx.Create(&models.Discrepancy{BatchID: batchID, Kind: "shortage", DishName: l.DishName,
					ShortQty: diff, Reason: "签收核对少盒", Status: "open"})
				parts = append(parts, fmt.Sprintf("%s少%d", l.DishName, diff))
			}
		}
		if !in.SealIntact {
			tx.Create(&models.Discrepancy{BatchID: batchID, Kind: "damage",
				Reason: "签收时封签破损", Status: "open"})
			parts = append(parts, "封签破损")
		}
		detail := fmt.Sprintf("前台 %s 签收：应收 %d / 实收 %d / 差额 %d",
			in.ReceiverName, batch.PackedQty, signedTotal, batch.PackedQty-signedTotal)
		if len(parts) > 0 {
			detail += "；差异：" + strings.Join(parts, "、")
		}
		s.logEvent(tx, batchID, u, "sign", detail)

		// 签收路线收口
		tx.Model(&models.RouteAssignment{}).Where("batch_id = ? AND active = ?", batchID, true).
			Updates(map[string]any{"active": false, "ended_at": time.Now()})
		if err := s.delivery.Complete(tx, batch.BatchNo); err != nil {
			return err
		}
		var got models.Receipt
		tx.Preload("Lines").First(&got, rc.ID)
		receipt = &got
		return s.recomputeClose(tx, batchID)
	})
	return receipt, err
}

// ---------- 差异上报（前台）与复核 ----------

func (s *Service) ReportDiscrepancy(u *models.User, batchID uint, kind, dish string, qty int, reason string) error {
	if err := requireRole(u, models.RoleFrontDesk); err != nil {
		return err
	}
	// 前台只能提差异，任何装箱数量字段都不经过此接口（无权修改厨房装箱数）。
	valid := map[string]bool{"shortage": true, "wrong": true, "damage": true, "nobody": true}
	if !valid[kind] {
		return errors.New("差异类型非法")
	}
	if qty < 0 {
		return errors.New("差异数量不能为负")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var batch models.Batch
		if err := tx.First(&batch, batchID).Error; err != nil {
			return ErrNotFound
		}
		if u.CompanyID == nil || *u.CompanyID != batch.CompanyID {
			return ErrForbidden
		}
		d := models.Discrepancy{BatchID: batchID, Kind: kind, DishName: dish, ShortQty: qty,
			Reason: reason, Status: "open"}
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		s.logEvent(tx, batchID, u, "discrepancy_open",
			fmt.Sprintf("前台提出差异：%s %s×%d，%s", kindLabel(kind), dish, qty, reason))
		return nil
	})
}

func kindLabel(k string) string {
	return map[string]string{"shortage": "少盒", "wrong": "错餐", "damage": "封签破损", "nobody": "无人签收"}[k]
}

type ReviewInput struct {
	Approve    bool   `json:"approve"`
	Resolution string `json:"resolution"` // resupply | loss | refund
	ResolveQty int    `json:"resolve_qty"`
	Note       string `json:"note"`
}

// ReviewDiscrepancy 复核员两阶段处理的第一阶段：审批并确定处置方式与数量。
func (s *Service) ReviewDiscrepancy(u *models.User, discID uint, in ReviewInput) error {
	if err := requireRole(u, models.RoleReviewer); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var d models.Discrepancy
		if err := tx.First(&d, discID).Error; err != nil {
			return ErrNotFound
		}
		if d.Status == "processed" {
			return errors.New("差异已处理完毕")
		}
		if !in.Approve {
			tx.Model(&d).Updates(map[string]any{"status": "rejected", "reviewer_id": u.ID, "review_note": in.Note})
			s.logEvent(tx, d.BatchID, u, "review", fmt.Sprintf("差异 #%d 复核驳回：%s", discID, in.Note))
			return nil
		}
		if in.Resolution != "resupply" && in.Resolution != "loss" && in.Resolution != "refund" {
			return errors.New("处置方式必须是 resupply / loss / refund")
		}
		qty := in.ResolveQty
		if qty == 0 {
			qty = d.ShortQty
		}
		// 数量必须与原差异对得上：整额处理，不允许多也不允许少
		if d.ShortQty > 0 && qty != d.ShortQty {
			return fmt.Errorf("%w: 处置数量 %d 与差异数量 %d 不一致", ErrQtyMismatch, qty, d.ShortQty)
		}
		if d.ShortQty == 0 && qty != 0 {
			return fmt.Errorf("%w: 该差异不涉及盒数", ErrQtyMismatch)
		}
		tx.Model(&d).Updates(map[string]any{
			"status": "approved", "resolution": in.Resolution, "resolve_qty": qty,
			"reviewer_id": u.ID, "review_note": in.Note,
		})
		s.logEvent(tx, d.BatchID, u, "review",
			fmt.Sprintf("差异 #%d 复核通过：处置=%s 数量=%d。%s", discID, in.Resolution, qty, in.Note))
		return nil
	})
}

// ProcessResupply 第二阶段：从原批次的已批准差异发起补送批次（不另建无关餐次）。
func (s *Service) ProcessResupply(u *models.User, discID uint) (*models.Batch, error) {
	if err := requireRole(u, models.RoleReviewer, models.RoleKitchen); err != nil {
		return nil, err
	}
	var out *models.Batch
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var d models.Discrepancy
		if err := tx.First(&d, discID).Error; err != nil {
			return ErrNotFound
		}
		if d.Status != "approved" || d.Resolution != "resupply" {
			return fmt.Errorf("%w: 差异未批准为补送", ErrBadState)
		}
		if d.ResupplyBatchID != nil {
			return errors.New("该差异已发起补送")
		}
		var root models.Batch
		if err := tx.First(&root, d.BatchID).Error; err != nil {
			return ErrNotFound
		}
		no, _ := s.nextBatchNo(tx, root.MealDate, "resupply")
		rb := models.Batch{
			BatchNo: no, CompanyID: root.CompanyID, MealDate: root.MealDate, MealType: root.MealType,
			RootBatchID: &root.ID, Kind: "resupply", Status: models.BatchPacked,
			PackedQty: d.ResolveQty,
		}
		if err := tx.Create(&rb).Error; err != nil {
			return err
		}
		tx.Create(&models.PackItem{BatchID: rb.ID, DishName: d.DishName, Qty: d.ResolveQty})
		// 补送的每一盒都挂在已批准差异上：数量依据明确，不是凭空多出的餐。
		tx.Create(&models.BoxLedger{BatchID: rb.ID, Change: d.ResolveQty, Kind: models.LedgerPacked,
			RefType: "discrepancy", RefID: d.ID,
			Note: fmt.Sprintf("补送 %s ×%d（原批次 %s，差异 #%d）", d.DishName, d.ResolveQty, root.BatchNo, d.ID)})
		// 原批次少掉的盒数已由厨房重新制作补送：原批次必须核销（计入损失），
		// 否则这几盒永远挂"在途"，账不平。物理总量：原批核销的 + 补送新装箱 = 守恒。
		if err := writeOff(tx, &d, "resupply",
			fmt.Sprintf("差异 #%d 补送 %d 盒，原批次少餐核销（补送批次 %s）", d.ID, d.ResolveQty, rb.BatchNo), u); err != nil {
			return err
		}
		tx.Model(&d).Updates(map[string]any{"status": "processed", "resupply_batch_id": rb.ID})
		s.logEvent(tx, root.ID, u, "resupply",
			fmt.Sprintf("依据差异 #%d 发起补送批次 %s：%s×%d（与原餐次同企业同餐次）",
				d.ID, rb.BatchNo, d.DishName, d.ResolveQty))
		s.logEvent(tx, rb.ID, u, "packed",
			fmt.Sprintf("补送装箱 %s×%d，数量依据为原批次差异 #%d", d.DishName, d.ResolveQty, d.ID))
		var b models.Batch
		tx.First(&b, rb.ID)
		out = &b
		return s.recomputeClose(tx, root.ID)
	})
	return out, err
}

// writeOff 把差异数量在原批次上记为物理损失（退款同样需要核销未交付盒数）。
func writeOff(tx *gorm.DB, d *models.Discrepancy, tag, note string, u *models.User) error {
	var batch models.Batch
	if err := tx.First(&batch, d.BatchID).Error; err != nil {
		return ErrNotFound
	}
	// 不能超过尚未核销的盒数：packed - signed - lost 必须足够
	outstanding := batch.PackedQty - batch.SignedQty - batch.LostQty
	if d.ResolveQty > outstanding {
		return fmt.Errorf("%w: 核销 %d 盒超过在途未结 %d 盒", ErrQtyMismatch, d.ResolveQty, outstanding)
	}
	lr := models.LossRecord{BatchID: d.BatchID, DiscrepancyID: d.ID, DishName: d.DishName,
		Qty: d.ResolveQty, Note: note}
	if err := tx.Create(&lr).Error; err != nil {
		return err
	}
	tx.Model(&batch).UpdateColumn("lost_qty", gorm.Expr("lost_qty + ?", d.ResolveQty))
	tx.Create(&models.BoxLedger{BatchID: d.BatchID, Change: -d.ResolveQty, Kind: models.LedgerLoss,
		RefType: "discrepancy", RefID: d.ID, Note: note})
	return nil
}

func (s *Service) ProcessLoss(u *models.User, discID uint) error {
	if err := requireRole(u, models.RoleReviewer); err != nil {
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var d models.Discrepancy
		if err := tx.First(&d, discID).Error; err != nil {
			return ErrNotFound
		}
		if d.Status != "approved" || d.Resolution != "loss" {
			return fmt.Errorf("%w: 差异未批准为损失核销", ErrBadState)
		}
		if err := writeOff(tx, &d, "loss",
			fmt.Sprintf("差异 #%d 确认损失核销 %d 盒", d.ID, d.ResolveQty), u); err != nil {
			return err
		}
		tx.Model(&d).Update("status", "processed")
		s.logEvent(tx, d.BatchID, u, "loss",
			fmt.Sprintf("差异 #%d 处理完成：损失 %d 盒（%s）", d.ID, d.ResolveQty, d.DishName))
		return s.recomputeClose(tx, d.BatchID)
	})
}

type RefundInput struct {
	ReceiptNo string `json:"receipt_no"`
}

// ProcessRefund 第二阶段：退款。回执号为幂等键，重复回执只入账一次。
func (s *Service) ProcessRefund(u *models.User, discID uint, receiptNo string) (deduplicated bool, err error) {
	if e := requireRole(u, models.RoleReviewer); e != nil {
		return false, e
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var d models.Discrepancy
		if e := tx.First(&d, discID).Error; e != nil {
			return ErrNotFound
		}
		// 同一回执号此前已用于任意退款：拒绝重复入账（DB 唯一索引兜底）
		var dup models.Refund
		if e := tx.Where("receipt_no = ?", receiptNo).First(&dup).Error; e == nil {
			if dup.DiscrepancyID == d.ID && d.Status == "processed" {
				// 同一差异重复提交：仍向支付网关确认一次（网关侧幂等，不重复扣款），
				// 业务库不产生第二条记录。
				if _, e := s.pay.Refund(tx, receiptNo, dup.Qty, dup.Amount); e != nil {
					return e
				}
				deduplicated = true
				return nil
			}
			return ErrDuplicateReceipt
		}
		if d.Status == "processed" && d.RefundID != nil {
			return errors.New("该差异已退款处理完毕")
		}
		if d.Status != "approved" || d.Resolution != "refund" {
			return fmt.Errorf("%w: 差异未批准为退款", ErrBadState)
		}
		amount := d.ResolveQty * 2500 // 模拟 25 元/盒
		res, e := s.pay.Refund(tx, receiptNo, d.ResolveQty, amount)
		if e != nil {
			return e
		}
		deduplicated = res.Deduplicated
		if res.Deduplicated {
			// 支付侧识别为重复回执：业务侧绝不第二次入账
			return ErrDuplicateReceipt
		}
		rf := models.Refund{BatchID: d.BatchID, DiscrepancyID: d.ID, ReceiptNo: receiptNo,
			Amount: amount, Qty: d.ResolveQty, Status: "posted"}
		if e := tx.Create(&rf).Error; e != nil {
			return e
		}
		// 物理账：未交付盒数核销；财务账：记退款盒数与金额
		if e := writeOff(tx, &d, "refund",
			fmt.Sprintf("差异 #%d 退款 %d 盒并核销未交付盒数，回执 %s", d.ID, d.ResolveQty, receiptNo), u); e != nil {
			return e
		}
		tx.Model(&models.Batch{}).Where("id = ?", d.BatchID).
			UpdateColumn("refunded_qty", gorm.Expr("refunded_qty + ?", d.ResolveQty))
		tx.Model(&d).Updates(map[string]any{"status": "processed", "refund_id": rf.ID})
		s.logEvent(tx, d.BatchID, u, "refund",
			fmt.Sprintf("差异 #%d 处理完成：退款 %d 盒 ¥%.2f，回执 %s", d.ID, d.ResolveQty,
				float64(amount)/100, receiptNo))
		return s.recomputeClose(tx, d.BatchID)
	})
	return deduplicated, err
}

// recomputeClose 当批次所有差异结清且无在途未结盒数时关闭。
func (s *Service) recomputeClose(tx *gorm.DB, batchID uint) error {
	var batch models.Batch
	if err := tx.First(&batch, batchID).Error; err != nil {
		return err
	}
	if batch.Status != models.BatchSigned {
		return nil
	}
	var open int64
	tx.Model(&models.Discrepancy{}).Where("batch_id = ? AND status IN ?", batchID, []string{"open", "approved"}).Count(&open)
	outstanding := batch.PackedQty - batch.SignedQty - batch.LostQty
	if open == 0 && outstanding == 0 {
		tx.Model(&batch).Update("status", models.BatchClosed)
		s.logEvent(tx, batchID, nil, "closed", "差异全部结清，数量核对平衡，批次关闭")
	}
	return nil
}
