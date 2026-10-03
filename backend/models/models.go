package models

import "time"

// 角色
const (
	RoleKitchen   = "kitchen"   // 厨房
	RoleCourier   = "courier"   // 配送员
	RoleFrontDesk = "frontdesk" // 企业前台
	RoleReviewer  = "reviewer"  // 差异复核员
)

// 批次状态
const (
	BatchPacked    = "packed" // 已装箱待交接
	BatchInTransit = "in_transit"
	BatchDelivered = "delivered" // 已送达待签收
	BatchSigned    = "signed"    // 已签收（可能有差异）
	BatchClosed    = "closed"    // 差异全部结清
)

// 餐次
const (
	MealBreakfast = "breakfast"
	MealLunch     = "lunch"
	MealDinner    = "dinner"
)

type Base struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Company 企业（前台归属企业）
type Company struct {
	Base
	Name    string `gorm:"not null" json:"name"`
	Address string `json:"address"`
}

// User 简化的登录主体：厨房/配送员/前台/复核员
type User struct {
	Base
	Name      string   `gorm:"not null" json:"name"`
	Role      string   `gorm:"not null" json:"role"`
	CompanyID *uint    `json:"company_id,omitempty"` // 前台归属企业
	Company   *Company `gorm:"-" json:"company,omitempty"`
	Phone     string   `json:"phone"`
}

// MealOrder 某企业某餐次的订餐汇总（应收数量依据，厨房据此装箱）
type MealOrder struct {
	Base
	CompanyID uint        `gorm:"uniqueIndex:idx_order_meal;not null" json:"company_id"`
	MealDate  string      `gorm:"uniqueIndex:idx_order_meal;not null" json:"meal_date"` // YYYY-MM-DD
	MealType  string      `gorm:"uniqueIndex:idx_order_meal;not null" json:"meal_type"`
	Company   Company     `gorm:"-" json:"company,omitempty"`
	Lines     []OrderLine `gorm:"constraint:OnDelete:CASCADE" json:"lines"`
}

type OrderLine struct {
	Base
	MealOrderID uint   `gorm:"index;not null" json:"meal_order_id"`
	DishName    string `gorm:"not null" json:"dish_name"`
	OrderedQty  int    `gorm:"not null" json:"ordered_qty"` // 应收份数
}

// Batch 配送批次。补送批次通过 root_batch_id 指回原始批次，不脱离原餐次。
type Batch struct {
	Base
	BatchNo     string `gorm:"uniqueIndex;not null" json:"batch_no"`
	CompanyID   uint   `gorm:"index;not null" json:"company_id"`
	MealDate    string `gorm:"index;not null" json:"meal_date"`
	MealType    string `gorm:"not null" json:"meal_type"`
	RootBatchID *uint  `gorm:"index" json:"root_batch_id,omitempty"`   // 补送批次指向原批次
	Kind        string `gorm:"not null default:'initial'" json:"kind"` // initial | resupply
	Status      string `gorm:"not null default:'packed'" json:"status"`

	PackedQty   int `gorm:"not null default:0" json:"packed_qty"`   // 装箱总盒数
	SignedQty   int `gorm:"not null default:0" json:"signed_qty"`   // 已签收盒数
	LostQty     int `gorm:"not null default:0" json:"lost_qty"`     // 确认损失盒数（含差异核销）
	RefundedQty int `gorm:"not null default:0" json:"refunded_qty"` // 已退款对应盒数（不改变物理盒数）

	Company Company `gorm:"-" json:"company,omitempty"`
}

// PackItem 装箱清单行（按菜品）。只有厨房可写。
type PackItem struct {
	Base
	BatchID  uint   `gorm:"uniqueIndex:idx_pack_batch_dish;not null" json:"batch_id"`
	DishName string `gorm:"uniqueIndex:idx_pack_batch_dish;not null" json:"dish_name"`
	Qty      int    `gorm:"not null" json:"qty"`
}

// CoolerBox 保温箱与封签
type CoolerBox struct {
	Base
	BatchID    uint   `gorm:"index;not null" json:"batch_id"`
	BoxNo      string `gorm:"not null" json:"box_no"`
	SealNo     string `gorm:"not null" json:"seal_no"`                      // 封签号
	SealStatus string `gorm:"not null default:'sealed'" json:"seal_status"` // sealed | broken | resealed
	BoxedQty   int    `gorm:"not null default:0" json:"boxed_qty"`
}

// RouteAssignment 路线交接。同一批次同时只有一条 active 路线（ enforced 业务层 + 唯一索引）。
type RouteAssignment struct {
	Base
	BatchID   uint       `gorm:"uniqueIndex:idx_route_active,priority:1,where:active" json:"batch_id"`
	RouteName string     `gorm:"not null" json:"route_name"`
	CourierID uint       `gorm:"not null" json:"courier_id"`
	Active    bool       `gorm:"uniqueIndex:idx_route_active,priority:2,where:active" json:"active"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Note      string     `json:"note"`
}

// Event 全程留痕：装箱/交接/改派/送达/封签破损/无人签收/签收/差异/复核/处理
type Event struct {
	Base
	BatchID uint   `gorm:"index;not null" json:"batch_id"`
	ActorID uint   `json:"actor_id"`
	Actor   string `json:"actor"` // 角色+姓名快照
	Type    string `gorm:"not null" json:"type"`
	Detail  string `json:"detail"`
}

// Receipt 签收记录（企业前台）
type Receipt struct {
	Base
	BatchID       uint          `gorm:"uniqueIndex;not null" json:"batch_id"`
	ReceiverName  string        `gorm:"not null" json:"receiver_name"`
	SealIntact    bool          `json:"seal_intact"`
	ReceivedQty   int           `gorm:"not null" json:"received_qty"` // 实收总盒数
	SignedQty     int           `gorm:"not null" json:"signed_qty"`   // 签收合格盒数
	NobodyPresent bool          `json:"nobody_present"`               // 无人签收
	Note          string        `json:"note"`
	Lines         []ReceiptLine `gorm:"constraint:OnDelete:CASCADE" json:"lines"`
}

type ReceiptLine struct {
	Base
	ReceiptID   uint   `gorm:"index;not null" json:"receipt_id"`
	DishName    string `gorm:"not null" json:"dish_name"`
	ExpectedQty int    `gorm:"not null" json:"expected_qty"` // 应收（装箱快照）
	ReceivedQty int    `gorm:"not null" json:"received_qty"` // 实收
}

// Discrepancy 差异单：前台上报，复核员处理。
type Discrepancy struct {
	Base
	BatchID  uint   `gorm:"index;not null" json:"batch_id"`
	Kind     string `gorm:"not null" json:"kind"` // shortage 少盒 | wrong 错餐 | damage 封签破损 | nobody 无人签收
	DishName string `json:"dish_name"`
	ShortQty int    `gorm:"not null" json:"short_qty"` // 差异数量（盒）
	Reason   string `json:"reason"`
	Status   string `gorm:"not null default:'open'" json:"status"` // open | approved | rejected | processed

	Resolution string `json:"resolution"` // resupply | loss | refund（复核决定）
	ResolveQty int    `json:"resolve_qty"`
	ReviewerID uint   `json:"reviewer_id"`
	ReviewNote string `json:"review_note"`

	// 处理结果引用
	ResupplyBatchID *uint `json:"resupply_batch_id,omitempty"`
	LossRecordID    *uint `json:"loss_record_id,omitempty"`
	RefundID        *uint `json:"refund_id,omitempty"`
}

// LossRecord 损失入账：餐盒从批次出账，数量守恒的一环。
type LossRecord struct {
	Base
	BatchID       uint   `gorm:"index;not null" json:"batch_id"`
	DiscrepancyID uint   `gorm:"not null" json:"discrepancy_id"`
	DishName      string `json:"dish_name"`
	Qty           int    `gorm:"not null" json:"qty"`
	Note          string `json:"note"`
}

// Refund 退款。回执号全局唯一：重复回执只入账一次。
type Refund struct {
	Base
	BatchID       uint   `gorm:"index;not null" json:"batch_id"`
	DiscrepancyID uint   `gorm:"not null" json:"discrepancy_id"`
	ReceiptNo     string `gorm:"uniqueIndex;not null" json:"receipt_no"` // 支付服务回执号（幂等键）
	Amount        int    `gorm:"not null" json:"amount"`                 // 分
	Qty           int    `gorm:"not null" json:"qty"`                    // 退款对应盒数
	Status        string `gorm:"not null default:'posted'" json:"status"`
}

// BoxLedger 餐盒数量流水。所有物理盒数变动必须在此留一条流水，是"不凭空增加"的审计依据。
//
//	packed  + 装箱进入批次（补送批次的每一盒都必须能对应一张已批准的差异单）
//	signed  - 签收离开在途
//	loss    - 确认损失/差异核销
//
// 恒等式：packed = signed + loss + outstanding（outstanding 为仍在途/待处理盒数，≥ 0）。
// 退款不改变物理盒数（退款针对的盒数已以损失核销或本就未签收），故不产生物理流水。
const (
	LedgerPacked = "packed"
	LedgerSigned = "signed"
	LedgerLoss   = "loss"
)

type BoxLedger struct {
	Base
	BatchID uint   `gorm:"index;not null" json:"batch_id"`
	Change  int    `gorm:"not null" json:"change"` // 正入负出
	Kind    string `gorm:"not null" json:"kind"`
	RefType string `json:"ref_type"` // receipt | discrepancy | loss
	RefID   uint   `json:"ref_id"`
	Note    string `json:"note"`
}
