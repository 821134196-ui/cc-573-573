package service

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"mealcheck/internal/models"
)

// PaymentService 本地模拟支付通道：退款创建与回执入账均以幂等键去重，
// 同一张退款单的支付回执无论推送多少次，只入账一次。
type PaymentService struct{ db *gorm.DB }

func NewPaymentService(db *gorm.DB) *PaymentService { return &PaymentService{db: db} }

// CreateRefund 在模拟通道创建退款单。幂等：同一幂等键只产生一张单。
func (p *PaymentService) CreateRefund(idemKey string, batchID uint, resolutionID uint, amount, qty int) (*models.RefundRecord, bool, error) {
	if amount <= 0 {
		return nil, false, fmt.Errorf("%w: 退款金额必须为正", ErrValidation)
	}
	var rec models.RefundRecord
	err := p.db.Where("idem_key = ?", idemKey).First(&rec).Error
	if err == nil {
		// 已存在：直接返回原单，不重复扣款/建单
		return &rec, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	rec = models.RefundRecord{
		ResolutionID:   &resolutionID,
		BatchID:        batchID,
		IdemKey:        idemKey,
		Amount:         amount,
		Qty:            qty,
		Status:         "pending",
		GatewayReceipt: "",
	}
	if err := p.db.Create(&rec).Error; err != nil {
		// 并发下唯一索引冲突也视为已存在
		if err2 := p.db.Where("idem_key = ?", idemKey).First(&rec).Error; err2 == nil {
			return &rec, false, nil
		}
		return nil, false, err
	}
	return &rec, true, nil
}

// PostReceipt 接收入账回执。重复回执（同单或同 gateway 回执号）只入账一次。
func (p *PaymentService) PostReceipt(idemKey, gatewayReceipt string) (*models.RefundRecord, error) {
	var rec models.RefundRecord
	if err := p.db.Where("idem_key = ?", idemKey).First(&rec).Error; err != nil {
		return nil, err
	}
	if rec.Status == "posted" {
		// 重复回执：原样返回，金额不再次入账
		return &rec, ErrAlreadyPosted
	}
	if gatewayReceipt != "" {
		var dup int64
		p.db.Model(&models.RefundRecord{}).
			Where("gateway_receipt = ? AND id <> ?", gatewayReceipt, rec.ID).Count(&dup)
		if dup > 0 {
			return nil, fmt.Errorf("%w: 通道回执号重复", ErrValidation)
		}
	}
	now := clockNow()
	rec.Status = "posted"
	rec.GatewayReceipt = gatewayReceipt
	rec.PostedAt = &now
	if err := p.db.Save(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}

// PostedRefundTotal 返回批次树根下已实际入账的退款总额（分）与份数
func (p *PaymentService) PostedRefundTotal(rootID uint) (amount int, qty int, err error) {
	var rows []models.RefundRecord
	if err = p.db.Where("batch_id IN (SELECT id FROM batches WHERE root_id = ?) AND status = 'posted'", rootID).
		Find(&rows).Error; err != nil {
		return 0, 0, err
	}
	for _, r := range rows {
		amount += r.Amount
		qty += r.Qty
	}
	return amount, qty, nil
}
