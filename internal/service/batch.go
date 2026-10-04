package service

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"mealcheck/internal/models"
)

type Service struct {
	db  *gorm.DB
	Pay *PaymentService
}

func New(db *gorm.DB) *Service {
	return &Service{db: db, Pay: NewPaymentService(db)}
}

func (s *Service) DB() *gorm.DB { return s.db }

// ---------- 查询 ----------

type BatchDetail struct {
	models.Batch
	EnterpriseName string               `json:"enterprise_name"`
	Events         []models.RouteEvent  `json:"events"`
	Audits         []models.AuditLog    `json:"audits"`
	Discrepancies  []models.Discrepancy `json:"discrepancies"`
	Receipt        *models.Receipt      `json:"receipt,omitempty"`
}

func (s *Service) GetBatch(id uint) (*BatchDetail, error) {
	var b models.Batch
	if err := s.db.Preload("Items").Preload("Boxes").
		Preload("Assignments").Preload("Children").
		First(&b, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var ent models.Enterprise
	s.db.First(&ent, b.EnterpriseID)
	var events []models.RouteEvent
	s.db.Where("batch_id = ?", b.ID).Order("created_at asc, id asc").Find(&events)
	var audits []models.AuditLog
	s.db.Where("batch_id = ?", b.ID).Order("created_at asc, id asc").Find(&audits)
	var discs []models.Discrepancy
	s.db.Preload("Lines").Preload("Resolution").Preload("Resolution.Refund").
		Where("batch_id = ?", b.ID).Order("id asc").Find(&discs)
	d := &BatchDetail{Batch: b, EnterpriseName: ent.Name, Events: events, Audits: audits, Discrepancies: discs}
	var rcpt models.Receipt
	if err := s.db.Preload("Lines").Where("batch_id = ? AND status = 'signed'", b.ID).
		Order("id desc").First(&rcpt).Error; err == nil {
		d.Receipt = &rcpt
	}
	return d, nil
}

func (s *Service) TreeBatches(rootID uint) ([]models.Batch, error) {
	return s.treeBatchesDB(s.db, rootID)
}

func (s *Service) treeBatchesDB(db *gorm.DB, rootID uint) ([]models.Batch, error) {
	var bs []models.Batch
	err := db.Where("root_id = ?", rootID).Order("id asc").Find(&bs).Error
	return bs, err
}

func (s *Service) ListBatches(enterpriseID *uint, status string) ([]models.Batch, error) {
	q := s.db.Preload("Enterprise").Preload("Items").Preload("Boxes").Order("id desc")
	if enterpriseID != nil {
		q = q.Where("enterprise_id = ?", *enterpriseID)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var bs []models.Batch
	err := q.Find(&bs).Error
	return bs, err
}

func (s *Service) ActiveAssignment(batchID uint) (*models.RouteAssignment, error) {
	var a models.RouteAssignment
	err := s.db.Where("batch_id = ? AND active = ?", batchID, true).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNoActiveRoute
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Service) addAudit(tx *gorm.DB, batchID, actorID uint, actorName, action, detail string) {
	tx.Create(&models.AuditLog{
		BatchID: batchID, ActorID: actorID, ActorName: actorName,
		Action: action, Detail: detail, CreatedAt: clockNow(),
	})
}

func (s *Service) addEvent(tx *gorm.DB, ev models.RouteEvent) {
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = clockNow()
	}
	tx.Create(&ev)
}

func (s *Service) recomputeTotals(tx *gorm.DB, batchID uint) error {
	var items []models.BatchItem
	if err := tx.Where("batch_id = ?", batchID).Find(&items).Error; err != nil {
		return err
	}
	qty, amount := 0, 0
	for _, it := range items {
		qty += it.PackedQty
		amount += it.PackedQty * it.UnitPrice
	}
	return tx.Model(&models.Batch{}).Where("id = ?", batchID).
		Updates(map[string]any{"packed_total": qty, "price_total": amount}).Error
}

// ---------- 数量守恒核对 ----------
//
// 只核对“本批次树装箱清单上的正确餐盒”。错餐实物不属于本树产出，
// 单独用错餐平衡式跟踪，绝不允许计入正常实收而让正确餐盒数量虚增。
//
// 主恒等式（按物理餐盒、全树合计）：
//
//	装箱产出 = 正常实收 + 在途 + 待封箱补送 + 待复核差异 + 争议(驳回)
//	         + 损失核销 + 待入账退款 + 已入账退款 + 已补送核销
//
// 其中批准复核时对每条短少行强制 resupply+loss+refund == short：
//   - 原短少餐盒以“补送”核销的，同时授权子批次生产等量新餐盒（独立计入产出与实收）；
//   - 子批次实际装箱不得超过批准补送数，杜绝凭空多出的餐盒。
//
// 错餐平衡式：错餐签收 = 暂扣 + 退回厨房 + 驳回自留。

type ConservationReport struct {
	RootID uint `json:"root_id"`

	Produced int `json:"produced"` // 厨房装箱产出（原批次+补送批次）

	Received   int `json:"received"`    // 正常签收获准
	InFlight   int `json:"in_flight"`   // 已封箱未签收（含无人签收暂存）
	OpenClaim  int `json:"open_claim"`  // 少盒差异待复核
	Disputed   int `json:"disputed"`    // 差异驳回、数量仍有争议
	Lost       int `json:"lost"`        // 损失核销
	RefundWait int `json:"refund_wait"` // 退款已批准、通道未入账
	Refunded   int `json:"refunded"`    // 退款已入账
	Resupplied int `json:"resupplied"`  // 原短少餐盒以补送核销
	Accounted  int `json:"accounted"`   // 去向合计

	// 仅展示：draft 批次计划装箱数（未物理生产，不参与恒等式）
	PendingPackPlanned int `json:"pending_pack_planned"`

	// 错餐实物单独平衡（这些盒子不在装箱清单上）
	WrongReceived int `json:"wrong_received"`
	WrongHeld     int `json:"wrong_held"`
	WrongReturn   int `json:"wrong_return"`
	WrongKept     int `json:"wrong_kept"`

	// 补送产能依据
	ResupplyApproved int `json:"resupply_approved"`
	ResupplyPacked   int `json:"resupply_packed"`

	Balanced bool   `json:"balanced"`
	Detail   string `json:"detail,omitempty"`
}

func (s *Service) CheckConservation(rootID uint) (*ConservationReport, error) {
	return s.checkConservationDB(s.db, rootID)
}

func (s *Service) checkConservationDB(db *gorm.DB, rootID uint) (*ConservationReport, error) {
	batches, err := s.treeBatchesDB(db, rootID)
	if err != nil {
		return nil, err
	}
	if len(batches) == 0 {
		return nil, fmt.Errorf("%w: 批次树 %d", ErrNotFound, rootID)
	}
	ids := make([]uint, 0, len(batches))
	for _, b := range batches {
		ids = append(ids, b.ID)
	}
	rep := &ConservationReport{RootID: rootID}

	// 先查成功签收（分类以“有无签收回执”为准，而非批次状态——
	// 签收后带差异的批次状态为 resolving，但餐盒去向应由签收行决定）
	var receipts []models.Receipt
	if err := db.Where("batch_id IN ? AND status = 'signed'", ids).
		Preload("Lines").Find(&receipts).Error; err != nil {
		return nil, err
	}
	signedBatch := map[uint]bool{}
	for _, r := range receipts {
		signedBatch[r.BatchID] = true
		for _, l := range r.Lines {
			if l.Wrong {
				rep.WrongReceived += l.ReceivedQty
				rep.WrongHeld += l.ReceivedQty // 默认暂扣，等差异单结论
			} else {
				rep.Received += l.ReceivedQty
			}
		}
	}

	// 1) 装箱产出 / 在途
	//    draft 批次只是“装箱计划”，餐盒尚未物理成型：不计入产出，
	//    仅在 PendingPackPlanned 中展示；补送批次另做“不得超批准份数”校验。
	childPackedByName := map[string]int{}
	for _, b := range batches {
		var items []models.BatchItem
		db.Where("batch_id = ?", b.ID).Find(&items)
		packed := 0
		for _, it := range items {
			packed += it.PackedQty
			if b.SourceDiscrepancyID != nil {
				childPackedByName[it.Name] += it.PackedQty
			}
		}
		if b.Status == models.BatchDraft {
			// 草稿只是装箱计划：餐盒尚未物理成型，不计入产出/去向
			rep.PendingPackPlanned += packed
			continue
		}
		rep.Produced += packed
		if b.SourceDiscrepancyID != nil {
			rep.ResupplyPacked += packed
		}
		if !signedBatch[b.ID] {
			rep.InFlight += packed
		}
	}

	// 3) 差异单归类
	approvedResupplyByName := map[string]int{}
	var discs []models.Discrepancy
	if err := db.Preload("Lines").Preload("Resolution").Preload("Resolution.Refund").
		Where("root_id = ?", rootID).Find(&discs).Error; err != nil {
		return nil, err
	}
	for _, d := range discs {
		for _, l := range d.Lines {
			if l.Wrong {
				switch d.Status {
				case models.DiffApproved:
					rep.WrongHeld -= l.ShortQty
					rep.WrongReturn += l.ShortQty // 错餐确认，实物退回厨房
				case models.DiffRejected:
					rep.WrongHeld -= l.ShortQty
					rep.WrongKept += l.ShortQty // 差异不成立，企业自留
				}
				continue
			}
			switch d.Status {
			case models.DiffOpen:
				rep.OpenClaim += l.ShortQty
			case models.DiffRejected:
				rep.Disputed += l.ShortQty
			case models.DiffApproved:
				rep.ResupplyApproved += l.ResupplyQty
				rep.Resupplied += l.ResupplyQty
				approvedResupplyByName[l.Name] += l.ResupplyQty
				rep.Lost += l.LossQty
				switch {
				case l.RefundQty == 0:
				case d.Resolution != nil && d.Resolution.Refund != nil && d.Resolution.Refund.Status == "posted":
					rep.Refunded += l.RefundQty
				default:
					rep.RefundWait += l.RefundQty
				}
			}
		}
	}

	rep.Accounted = rep.Received + rep.InFlight + rep.OpenClaim + rep.Disputed +
		rep.Lost + rep.RefundWait + rep.Refunded + rep.Resupplied

	switch {
	case rep.Produced != rep.Accounted:
		rep.Balanced = false
		rep.Detail = fmt.Sprintf(
			"正确餐盒数量不守恒: 厨房装箱=%d，去向合计=%d（实收%d/在途%d/待复核%d/争议%d/损失%d/待退款%d/已退款%d/补送核销%d）",
			rep.Produced, rep.Accounted,
			rep.Received, rep.InFlight, rep.OpenClaim, rep.Disputed,
			rep.Lost, rep.RefundWait, rep.Refunded, rep.Resupplied)
		return rep, nil
	}
	// 补送批次按餐品核对：每种补送餐盒都必须有复核依据，且数量不得超过批准
	for name, qty := range childPackedByName {
		if qty <= 0 {
			continue
		}
		allowed := approvedResupplyByName[name]
		if qty > allowed {
			rep.Balanced = false
			rep.Detail = fmt.Sprintf(
				"补送餐品[%s]超出复核依据: 批准补送%d，补送批次已装箱%d，存在凭空增加的餐盒",
				name, allowed, qty)
			return rep, nil
		}
	}
	if rep.WrongReceived != rep.WrongHeld+rep.WrongReturn+rep.WrongKept {
		rep.Balanced = false
		rep.Detail = fmt.Sprintf(
			"错餐实物不平衡: 错餐签收=%d，暂扣%d+退回%d+自留%d",
			rep.WrongReceived, rep.WrongHeld, rep.WrongReturn, rep.WrongKept)
		return rep, nil
	}
	rep.Balanced = true
	return rep, nil
}
