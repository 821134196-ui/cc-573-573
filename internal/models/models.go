package models

import "time"

// 角色
const (
	RoleKitchen   = "kitchen"   // 厨房
	RoleCourier   = "courier"   // 配送员
	RoleFrontDesk = "frontdesk" // 企业前台
	RoleReviewer  = "reviewer"  // 差异复核
	RoleAdmin     = "admin"     // 调度/管理员
)

// 批次状态
const (
	BatchDraft     = "draft"      // 装箱编辑中
	BatchSealed    = "sealed"     // 已封箱
	BatchAssigned  = "assigned"   // 已指派路线
	BatchInTransit = "in_transit" // 配送途中
	BatchDelivered = "delivered"  // 已送达（等待签收）
	BatchSigned    = "signed"     // 已签收
	BatchResolving = "resolving"  // 差异处理中
	BatchClosed    = "closed"     // 已结清关闭
)

// 路线任务状态
const (
	AssignAssigned   = "assigned"
	AssignAccepted   = "accepted"
	AssignInTransit  = "in_transit"
	AssignDelivered  = "delivered"
	AssignReassigned = "reassigned" // 改派后原任务关闭
	AssignFailed     = "failed"     // 无人签收等异常
)

// 差异类型 / 状态
const (
	DiffShort  = "short"  // 少盒
	DiffWrong  = "wrong"  // 错餐
	DiffBroken = "broken" // 封签破损
	DiffNoOne  = "no_one" // 无人签收

	DiffOpen     = "open"
	DiffApproved = "approved"
	DiffRejected = "rejected"
)

// 复核动作
const (
	ActionResupply = "resupply" // 补送
	ActionLoss     = "loss"     // 损失核销
	ActionRefund   = "refund"   // 退款
)

type User struct {
	ID           uint   `gorm:"primarykey" json:"id"`
	Username     string `gorm:"uniqueIndex;not null" json:"username"`
	Name         string `gorm:"not null" json:"name"`
	Role         string `gorm:"not null" json:"role"`
	EnterpriseID *uint  `json:"enterprise_id,omitempty"`
}

type Enterprise struct {
	ID      uint   `gorm:"primarykey" json:"id"`
	Name    string `gorm:"not null" json:"name"`
	Address string `json:"address"`
}

type Route struct {
	ID        uint   `gorm:"primarykey" json:"id"`
	Name      string `gorm:"not null" json:"name"`
	CourierID uint   `gorm:"not null" json:"courier_id"`
	Courier   User   `gorm:"foreignKey:CourierID" json:"courier,omitempty"`
	Active    bool   `gorm:"default:true" json:"active"`
}

// Batch 一个企业一个餐次的装箱批次；补送批次以 ParentID/RootID 挂在原批次上
type Batch struct {
	ID           uint   `gorm:"primarykey" json:"id"`
	Code         string `gorm:"uniqueIndex;not null" json:"code"`
	Enterprise   Enterprise
	EnterpriseID uint   `gorm:"not null;index" json:"enterprise_id"`
	MealDate     string `gorm:"not null" json:"meal_date"` // YYYY-MM-DD
	MealType     string `gorm:"not null" json:"meal_type"` // breakfast/lunch/dinner

	RootID   uint  `gorm:"not null;index" json:"root_id"` // 根批次（最初餐次）
	ParentID *uint `gorm:"index" json:"parent_id,omitempty"`
	// 补送批次由哪张差异单发起 —— 保证“从原批次发起、保留数量依据”
	SourceDiscrepancyID *uint `gorm:"uniqueIndex" json:"source_discrepancy_id,omitempty"`
	ResupplySeq         int   `json:"resupply_seq"`

	Status string `gorm:"not null;default:draft;index" json:"status"`

	PackedTotal int `gorm:"not null;default:0" json:"packed_total"`
	PriceTotal  int `gorm:"not null;default:0" json:"price_total"` // 分

	CreatedBy uint `gorm:"not null" json:"created_by"`
	Note      string

	SealedAt    *time.Time `json:"sealed_at,omitempty"`
	AssignedAt  *time.Time `json:"assigned_at,omitempty"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	SignedAt    *time.Time `json:"signed_at,omitempty"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	Items       []BatchItem       `gorm:"foreignKey:BatchID" json:"items,omitempty"`
	Boxes       []Box             `gorm:"foreignKey:BatchID" json:"boxes,omitempty"`
	Assignments []RouteAssignment `gorm:"foreignKey:BatchID" json:"assignments,omitempty"`
	Children    []Batch           `gorm:"foreignKey:ParentID" json:"children,omitempty"`
}

// BatchItem 厨房装箱明细，装箱数一经封箱即不可被任何角色改动
type BatchItem struct {
	ID        uint   `gorm:"primarykey" json:"id"`
	BatchID   uint   `gorm:"not null;uniqueIndex:idx_batch_item_name" json:"batch_id"`
	Name      string `gorm:"not null;uniqueIndex:idx_batch_item_name" json:"name"`
	UnitPrice int    `gorm:"not null;default:0" json:"unit_price"` // 分
	PackedQty int    `gorm:"not null;default:0" json:"packed_qty"` // 装箱数量（厨房填写）
}

// Box 保温箱与封签
type Box struct {
	ID         uint       `gorm:"primarykey" json:"id"`
	BatchID    uint       `gorm:"not null;index" json:"batch_id"`
	Code       string     `gorm:"not null" json:"code"`
	SealNo     string     `gorm:"not null;index" json:"seal_no"`
	Status     string     `gorm:"not null;default:intact" json:"status"` // intact/broken
	SealedAt   time.Time  `gorm:"not null" json:"sealed_at"`
	BrokenAt   *time.Time `json:"broken_at,omitempty"`
	BrokenNote string     `json:"broken_note"`
}

// RouteAssignment 一批餐在某条路线上的一段任务。改派 = 旧段关闭 + 新段生效
type RouteAssignment struct {
	ID        uint   `gorm:"primarykey" json:"id"`
	BatchID   uint   `gorm:"not null;index" json:"batch_id"`
	RouteID   uint   `gorm:"not null" json:"route_id"`
	CourierID uint   `gorm:"not null" json:"courier_id"`
	Seq       int    `gorm:"not null;default:1" json:"seq"`
	Status    string `gorm:"not null;default:assigned" json:"status"`
	// 同一批次至多一条 Active 任务（部分唯一索引由 Migrate 显式创建）
	Active       bool       `gorm:"not null;default:false" json:"active"`
	HandoverNote string     `json:"handover_note"`
	AcceptedAt   *time.Time `json:"accepted_at,omitempty"`
	DepartedAt   *time.Time `json:"departed_at,omitempty"`
	DeliveredAt  *time.Time `json:"delivered_at,omitempty"`
	ReassignedAt *time.Time `json:"reassigned_at,omitempty"`
	FailedAt     *time.Time `json:"failed_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// RouteEvent 路线经过：接收、交接、改派、封签破损、无人签收、送达
type RouteEvent struct {
	ID           uint      `gorm:"primarykey" json:"id"`
	BatchID      uint      `gorm:"not null;index" json:"batch_id"`
	AssignmentID uint      `gorm:"not null;index" json:"assignment_id"`
	FromRouteID  *uint     `json:"from_route_id,omitempty"`
	ToRouteID    *uint     `json:"to_route_id,omitempty"`
	ActorID      uint      `gorm:"not null" json:"actor_id"`
	ActorName    string    `gorm:"not null" json:"actor_name"`
	Type         string    `gorm:"not null" json:"type"`
	Note         string    `json:"note"`
	CreatedAt    time.Time `gorm:"not null;index" json:"created_at"`
}

// Receipt 签收回执（含无人签收的失败记录）
type Receipt struct {
	ID           uint          `gorm:"primarykey" json:"id"`
	BatchID      uint          `gorm:"not null;index" json:"batch_id"`
	AssignmentID uint          `gorm:"not null" json:"assignment_id"`
	Status       string        `gorm:"not null;default:signed" json:"status"` // signed/no_one/refused
	ReceiverName string        `json:"receiver_name"`
	SealOK       bool          `gorm:"not null;default:true" json:"seal_ok"`
	SealNote     string        `json:"seal_note"`
	Note         string        `json:"note"`
	SignedAt     *time.Time    `json:"signed_at,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	Lines        []ReceiptLine `gorm:"foreignKey:ReceiptID" json:"lines,omitempty"`
}

type ReceiptLine struct {
	ID          uint   `gorm:"primarykey" json:"id"`
	ReceiptID   uint   `gorm:"not null;index" json:"receipt_id"`
	BatchItemID *uint  `json:"batch_item_id,omitempty"`
	Name        string `gorm:"not null" json:"name"`
	ExpectedQty int    `gorm:"not null" json:"expected_qty"` // 当时应收
	ReceivedQty int    `gorm:"not null" json:"received_qty"` // 实收
	Wrong       bool   `gorm:"not null;default:false" json:"wrong"`
}

// Discrepancy 企业前台提出的差异（前台无权改装箱数，只能提差异）
type Discrepancy struct {
	ID       uint   `gorm:"primarykey" json:"id"`
	BatchID  uint   `gorm:"not null;index" json:"batch_id"` // 观察到差异的批次（可能是补送批次）
	RootID   uint   `gorm:"not null;index" json:"root_id"`
	RaisedBy uint   `gorm:"not null" json:"raised_by"`
	Kind     string `gorm:"not null" json:"kind"`
	Status   string `gorm:"not null;default:open;index" json:"status"`
	Note     string `json:"note"`

	ReviewNote string     `json:"review_note"`
	ReviewedBy *uint      `json:"reviewed_by,omitempty"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`

	Lines      []DiscrepancyLine `gorm:"foreignKey:DiscrepancyID" json:"lines,omitempty"`
	Resolution *Resolution       `gorm:"foreignKey:DiscrepancyID" json:"resolution,omitempty"`
}

type DiscrepancyLine struct {
	ID            uint   `gorm:"primarykey" json:"id"`
	DiscrepancyID uint   `gorm:"not null;index" json:"discrepancy_id"`
	BatchItemID   *uint  `json:"batch_item_id,omitempty"`
	Name          string `gorm:"not null" json:"name"`
	ExpectedQty   int    `gorm:"not null" json:"expected_qty"`
	ReceivedQty   int    `gorm:"not null" json:"received_qty"`
	ShortQty      int    `gorm:"not null" json:"short_qty"` // 差额依据
	Wrong         bool   `gorm:"not null;default:false" json:"wrong"`

	// 复核时对 ShortQty 的去向分配，三者之和必须 == ShortQty（批准时）
	ResupplyQty int `gorm:"not null;default:0" json:"resupply_qty"`
	LossQty     int `gorm:"not null;default:0" json:"loss_qty"`
	RefundQty   int `gorm:"not null;default:0" json:"refund_qty"`
}

// Resolution 一次复核结论
type Resolution struct {
	ID            uint   `gorm:"primarykey" json:"id"`
	DiscrepancyID uint   `gorm:"uniqueIndex;not null" json:"discrepancy_id"`
	ReviewerID    uint   `gorm:"not null" json:"reviewer_id"`
	Note          string `json:"note"`
	// 补送批次在事务中创建后回填
	ResupplyBatchID *uint         `json:"resupply_batch_id,omitempty"`
	RefundAmount    int           `gorm:"not null;default:0" json:"refund_amount"` // 分
	CreatedAt       time.Time     `json:"created_at"`
	Refund          *RefundRecord `gorm:"foreignKey:ResolutionID" json:"refund,omitempty"`
}

// RefundRecord 模拟支付通道退款单；IdemKey 唯一，重复回执只入账一次
type RefundRecord struct {
	ID             uint       `gorm:"primarykey" json:"id"`
	ResolutionID   *uint      `gorm:"uniqueIndex" json:"resolution_id,omitempty"`
	BatchID        uint       `gorm:"not null;index" json:"batch_id"`
	IdemKey        string     `gorm:"uniqueIndex;not null" json:"idem_key"`
	Amount         int        `gorm:"not null" json:"amount"`
	Qty            int        `gorm:"not null" json:"qty"`
	Status         string     `gorm:"not null;default:pending" json:"status"` // pending/posted/failed
	GatewayReceipt string     `json:"gateway_receipt"`
	PostedAt       *time.Time `json:"posted_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// AuditLog 批次级经过（封箱、签收、提差异、复核等）
type AuditLog struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	BatchID   uint      `gorm:"not null;index" json:"batch_id"`
	ActorID   uint      `gorm:"not null" json:"actor_id"`
	ActorName string    `gorm:"not null" json:"actor_name"`
	Action    string    `gorm:"not null" json:"action"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `gorm:"not null;index" json:"created_at"`
}
