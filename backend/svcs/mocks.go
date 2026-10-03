// Package svcs 是外部服务的本地模拟：支付退款与配送调度。
// 所有写操作都接收当前业务事务的 *gorm.DB，保证模拟服务回执与业务记录在同一事务内落库。
package svcs

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

// ---------- 支付服务（本地模拟） ----------

type MockPayment struct {
	db *gorm.DB
	mu sync.Mutex
}

type PaymentReceipt struct {
	ID        uint   `gorm:"primarykey"`
	ReceiptNo string `gorm:"uniqueIndex;not null"`
	Amount    int    // 分
	Qty       int
	CallCount int // 同一回执被提交的次数（用于演示幂等）
	Status    string
	CreatedAt time.Time
}

func (*PaymentReceipt) TableName() string { return "mock_payment_receipts" }

func NewMockPayment(db *gorm.DB) *MockPayment {
	_ = db.AutoMigrate(&PaymentReceipt{})
	return &MockPayment{db: db}
}

// RefundResult 模拟支付网关退款结果。
type RefundResult struct {
	ReceiptNo    string
	Amount       int
	Deduplicated bool // true 表示该回执此前已入账，本次未重复扣款/入账
}

// Refund 以回执号为幂等键：同一回执号无论提交多少次，只入账一次。
func (p *MockPayment) Refund(tx *gorm.DB, receiptNo string, qty, amount int) (*RefundResult, error) {
	if receiptNo == "" {
		return nil, errors.New("支付服务: 回执号不能为空")
	}
	if amount < 0 || qty < 0 {
		return nil, errors.New("支付服务: 金额或数量非法")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	var r PaymentReceipt
	err := tx.Where("receipt_no = ?", receiptNo).First(&r).Error
	if err == nil {
		// 已存在：登记一次重复调用，返回原回执，绝不重复入账。
		if e := tx.Model(&PaymentReceipt{}).Where("id = ?", r.ID).
			UpdateColumn("call_count", gorm.Expr("call_count + 1")).Error; e != nil {
			return nil, e
		}
		return &RefundResult{ReceiptNo: receiptNo, Amount: r.Amount, Deduplicated: true}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	r = PaymentReceipt{ReceiptNo: receiptNo, Amount: amount, Qty: qty, CallCount: 1, Status: "success", CreatedAt: time.Now()}
	if err := tx.Create(&r).Error; err != nil {
		return nil, fmt.Errorf("支付服务入账失败: %w", err)
	}
	return &RefundResult{ReceiptNo: receiptNo, Amount: amount, Deduplicated: false}, nil
}

func (p *MockPayment) Receipts() []PaymentReceipt {
	var rs []PaymentReceipt
	p.db.Order("id desc").Find(&rs)
	return rs
}

// ---------- 配送调度服务（本地模拟） ----------

type MockDelivery struct {
	db *gorm.DB
	mu sync.Mutex
}

type DispatchLog struct {
	ID          uint   `gorm:"primarykey"`
	BatchNo     string `gorm:"index"`
	TrackingNo  string `gorm:"uniqueIndex"`
	RouteName   string
	CourierName string
	Action      string // dispatch | reroute | complete
	Seq         int
	CreatedAt   time.Time
}

func (*DispatchLog) TableName() string { return "mock_dispatch_logs" }

func NewMockDelivery(db *gorm.DB) *MockDelivery {
	_ = db.AutoMigrate(&DispatchLog{})
	return &MockDelivery{db: db}
}

func (d *MockDelivery) nextSeq(tx *gorm.DB, batchNo string) int {
	var max struct{ N int }
	tx.Model(&DispatchLog{}).Where("batch_no = ?", batchNo).Select("coalesce(max(seq),0) as n").Scan(&max)
	return max.N + 1
}

func (d *MockDelivery) write(tx *gorm.DB, batchNo, route, courier, action string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	seq := d.nextSeq(tx, batchNo)
	log := DispatchLog{
		BatchNo: batchNo, TrackingNo: fmt.Sprintf("TRK-%s-%d", batchNo, seq),
		RouteName: route, CourierName: courier, Action: action, Seq: seq, CreatedAt: time.Now(),
	}
	if err := tx.Create(&log).Error; err != nil {
		return "", err
	}
	return log.TrackingNo, nil
}

// Dispatch 模拟派车，返回轨迹单号。
func (d *MockDelivery) Dispatch(tx *gorm.DB, batchNo, route, courierName string) (string, error) {
	return d.write(tx, batchNo, route, courierName, "dispatch")
}

// Reroute 模拟途中改派调度。
func (d *MockDelivery) Reroute(tx *gorm.DB, batchNo, route, courierName string) (string, error) {
	return d.write(tx, batchNo, route, courierName, "reroute")
}

// Complete 模拟路线完成回执。
func (d *MockDelivery) Complete(tx *gorm.DB, batchNo string) error {
	_, err := d.write(tx, batchNo, "", "", "complete")
	return err
}

func (d *MockDelivery) Logs(batchNo string) []DispatchLog {
	var ls []DispatchLog
	q := d.db.Order("seq asc")
	if batchNo != "" {
		q = q.Where("batch_no = ?", batchNo)
	}
	q.Find(&ls)
	return ls
}
