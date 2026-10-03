package main

import (
	"campusmeal/models"
)

type BatchDetail struct {
	models.Batch
	CompanyName   string                   `json:"company_name"`
	PackItems     []models.PackItem        `json:"pack_items"`
	Boxes         []models.CoolerBox       `json:"boxes"`
	Routes        []models.RouteAssignment `json:"routes"`
	Events        []models.Event           `json:"events"`
	Receipt       *models.Receipt          `json:"receipt,omitempty"`
	Discrepancies []models.Discrepancy     `json:"discrepancies"`
	Losses        []models.LossRecord      `json:"losses"`
	Refunds       []models.Refund          `json:"refunds"`
	Children      []models.Batch           `json:"children"`
	Conservation  Conservation             `json:"conservation"`
}

type Conservation struct {
	Packed      int  `json:"packed"`
	Signed      int  `json:"signed"`
	Lost        int  `json:"lost"`
	Outstanding int  `json:"outstanding"` // 仍在途/待处理，必须 ≥ 0
	Balanced    bool `json:"balanced"`    // packed == signed+lost+outstanding
	LedgerSum   int  `json:"ledger_sum"`  // 流水余额（应等于 outstanding 的镜像口径核对）
}

func (s *Service) GetBatch(id uint) (*BatchDetail, error) {
	var b models.Batch
	if err := s.db.First(&b, id).Error; err != nil {
		return nil, ErrNotFound
	}
	return s.decorate(b)
}

func (s *Service) decorate(b models.Batch) (*BatchDetail, error) {
	d := &BatchDetail{Batch: b}
	var c models.Company
	if s.db.First(&c, b.CompanyID).Error == nil {
		d.CompanyName = c.Name
	}
	s.db.Where("batch_id = ?", b.ID).Find(&d.PackItems)
	s.db.Where("batch_id = ?", b.ID).Order("id asc").Find(&d.Boxes)
	s.db.Where("batch_id = ?", b.ID).Order("id asc").Find(&d.Routes)
	s.db.Where("batch_id = ?", b.ID).Order("id asc").Find(&d.Events)
	s.db.Where("batch_id = ?", b.ID).Order("id asc").Find(&d.Discrepancies)
	s.db.Where("batch_id = ?", b.ID).Find(&d.Losses)
	s.db.Where("batch_id = ?", b.ID).Find(&d.Refunds)
	s.db.Where("root_batch_id = ?", b.ID).Order("id asc").Find(&d.Children)

	var rc models.Receipt
	if s.db.Preload("Lines").Where("batch_id = ?", b.ID).First(&rc).Error == nil {
		d.Receipt = &rc
	}

	// 以流水为准重算守恒，同时核对汇总字段
	var ledger struct {
		In  int
		Out int
	}
	s.db.Model(&models.BoxLedger{}).Where("batch_id = ? AND change > 0", b.ID).
		Select("coalesce(sum(change),0)").Scan(&ledger.In)
	s.db.Model(&models.BoxLedger{}).Where("batch_id = ? AND change < 0", b.ID).
		Select("coalesce(sum(change),0)").Scan(&ledger.Out)
	ledgerSum := ledger.In + ledger.Out // 负数出账相加 => 在途余额
	outstanding := b.PackedQty - b.SignedQty - b.LostQty
	d.Conservation = Conservation{
		Packed:      b.PackedQty,
		Signed:      b.SignedQty,
		Lost:        b.LostQty,
		Outstanding: outstanding,
		Balanced:    b.PackedQty == b.SignedQty+b.LostQty+outstanding && outstanding >= 0 && ledgerSum == outstanding,
		LedgerSum:   ledgerSum,
	}
	return d, nil
}

func (s *Service) ListBatches(companyID *uint, kind string) ([]BatchDetail, error) {
	q := s.db.Model(&models.Batch{}).Order("id desc")
	if companyID != nil {
		q = q.Where("company_id = ?", *companyID)
	}
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	var bs []models.Batch
	q.Find(&bs)
	out := make([]BatchDetail, 0, len(bs))
	for _, b := range bs {
		d, err := s.decorate(b)
		if err == nil {
			out = append(out, *d)
		}
	}
	return out, nil
}

func (s *Service) ListOrders() ([]models.MealOrder, error) {
	var os []models.MealOrder
	s.db.Preload("Lines").Order("id desc").Find(&os)
	for i := range os {
		var c models.Company
		s.db.First(&c, os[i].CompanyID)
		os[i].Company = c
	}
	return os, nil
}

// GlobalAudit 全局数量守恒：所有批次都必须平衡；补送批次必须挂在已处理的原差异上。
type GlobalAudit struct {
	AllBalanced      bool     `json:"all_balanced"`
	TotalPacked      int      `json:"total_packed"`
	TotalSigned      int      `json:"total_signed"`
	TotalLost        int      `json:"total_lost"`
	TotalOutstanding int      `json:"total_outstanding"`
	Unbalanced       []string `json:"unbalanced"`
	OrphanResupplies []string `json:"orphan_resupplies"`
	RefundUnique     bool     `json:"refund_receipt_unique"`
	DuplicateRefunds []string `json:"duplicate_refund_receipts"`
}

func (s *Service) Audit() GlobalAudit {
	a := GlobalAudit{AllBalanced: true, RefundUnique: true}
	var bs []models.Batch
	s.db.Find(&bs)
	for _, b := range bs {
		out := b.PackedQty - b.SignedQty - b.LostQty
		a.TotalPacked += b.PackedQty
		a.TotalSigned += b.SignedQty
		a.TotalLost += b.LostQty
		a.TotalOutstanding += out
		var ledgerIn, ledgerOut int
		s.db.Model(&models.BoxLedger{}).Where("batch_id = ? AND change > 0", b.ID).Select("coalesce(sum(change),0)").Scan(&ledgerIn)
		s.db.Model(&models.BoxLedger{}).Where("batch_id = ? AND change < 0", b.ID).Select("coalesce(sum(change),0)").Scan(&ledgerOut)
		if out < 0 || b.PackedQty != b.SignedQty+b.LostQty+out || ledgerIn+ledgerOut != out {
			a.AllBalanced = false
			a.Unbalanced = append(a.Unbalanced, b.BatchNo)
		}
		if b.Kind == "resupply" {
			var n int64
			s.db.Model(&models.Discrepancy{}).
				Where("resupply_batch_id = ? AND status = ?", b.ID, "processed").Count(&n)
			if n == 0 {
				a.OrphanResupplies = append(a.OrphanResupplies, b.BatchNo)
			}
		}
	}
	type dup struct {
		ReceiptNo string
		N         int
	}
	var dups []dup
	s.db.Model(&models.Refund{}).Select("receipt_no, count(*) as n").Group("receipt_no").
		Having("count(*) > 1").Scan(&dups)
	for _, d := range dups {
		a.RefundUnique = false
		a.DuplicateRefunds = append(a.DuplicateRefunds, d.ReceiptNo)
	}
	return a
}
