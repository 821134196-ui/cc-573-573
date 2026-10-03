package main

import (
	"errors"
	"net/http"
	"strconv"

	"campusmeal/models"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *Service
}

func (h *Handler) currentUser(c *gin.Context) *models.User {
	uid, err := strconv.Atoi(c.GetHeader("X-User-Id"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录（缺少 X-User-Id）"})
		return nil
	}
	var u models.User
	if err := h.svc.db.First(&u, uid).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户不存在"})
		return nil
	}
	return &u
}

func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, ErrBadState):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, ErrQtyMismatch):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	case errors.Is(err, ErrDuplicateReceipt):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "deduplicated": true})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}

func idParam(c *gin.Context) uint {
	n, _ := strconv.Atoi(c.Param("id"))
	return uint(n)
}

func (h *Handler) Login(c *gin.Context) {
	var in struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供 name"})
		return
	}
	var u models.User
	if err := h.svc.db.Where("name = ?", in.Name).First(&u).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	c.JSON(200, u)
}

func (h *Handler) Me(c *gin.Context) { c.JSON(200, h.currentUser(c)) }

func (h *Handler) ListUsers(c *gin.Context) {
	var us []models.User
	h.svc.db.Find(&us)
	c.JSON(200, us)
}

func (h *Handler) ListCompanies(c *gin.Context) {
	var cs []models.Company
	h.svc.db.Find(&cs)
	c.JSON(200, cs)
}

func (h *Handler) ListOrders(c *gin.Context) {
	os, err := h.svc.ListOrders()
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, os)
}

func (h *Handler) ListBatches(c *gin.Context) {
	var companyID *uint
	if v := c.Query("company_id"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			u2 := uint(n)
			companyID = &u2
		}
	}
	bs, err := h.svc.ListBatches(companyID, c.Query("kind"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, bs)
}

func (h *Handler) GetBatch(c *gin.Context) {
	b, err := h.svc.GetBatch(idParam(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, b)
}

func (h *Handler) CreateBatch(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	var in struct {
		CompanyID uint   `json:"company_id"`
		MealDate  string `json:"meal_date"`
		MealType  string `json:"meal_type"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	b, err := h.svc.CreateBatchFromOrder(u, in.CompanyID, in.MealDate, in.MealType)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(201, b)
}

func (h *Handler) RegisterSeals(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	var in struct {
		Boxes []BoxInput `json:"boxes"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.RegisterSeals(u, idParam(c), in.Boxes); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) Handover(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	var in struct {
		CourierID uint   `json:"courier_id"`
		RouteName string `json:"route_name"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.RouteName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "需要 courier_id 和 route_name"})
		return
	}
	if err := h.svc.Handover(u, idParam(c), in.CourierID, in.RouteName); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) Reroute(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	var in struct {
		CourierID uint   `json:"courier_id"`
		RouteName string `json:"route_name"`
		Note      string `json:"note"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.Reroute(u, idParam(c), in.CourierID, in.RouteName, in.Note); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) Deliver(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	if err := h.svc.Deliver(u, idParam(c)); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) SealBroken(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	var in struct {
		BoxNo string `json:"box_no"`
		Note  string `json:"note"`
	}
	_ = c.ShouldBindJSON(&in)
	if err := h.svc.ReportSealBroken(u, idParam(c), in.BoxNo, in.Note); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) Nobody(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&in)
	if err := h.svc.NobodyPresent(u, idParam(c), in.Note); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) Sign(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	var in SignInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rc, err := h.svc.Sign(u, idParam(c), in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(201, rc)
}

func (h *Handler) ReportDiscrepancy(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	var in struct {
		Kind     string `json:"kind"`
		DishName string `json:"dish_name"`
		ShortQty int    `json:"short_qty"`
		Reason   string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.ReportDiscrepancy(u, idParam(c), in.Kind, in.DishName, in.ShortQty, in.Reason); err != nil {
		fail(c, err)
		return
	}
	c.JSON(201, gin.H{"ok": true})
}

func (h *Handler) Review(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	var in ReviewInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.ReviewDiscrepancy(u, idDisc(c), in); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func idDisc(c *gin.Context) uint {
	n, _ := strconv.Atoi(c.Param("did"))
	return uint(n)
}

func (h *Handler) Resupply(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	b, err := h.svc.ProcessResupply(u, idDisc(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(201, b)
}

func (h *Handler) Loss(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	if err := h.svc.ProcessLoss(u, idDisc(c)); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *Handler) Refund(c *gin.Context) {
	u := h.currentUser(c)
	if u == nil {
		return
	}
	var in RefundInput
	if err := c.ShouldBindJSON(&in); err != nil || in.ReceiptNo == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "需要 receipt_no"})
		return
	}
	dedup, err := h.svc.ProcessRefund(u, idDisc(c), in.ReceiptNo)
	if err != nil && !errors.Is(err, ErrDuplicateReceipt) {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true, "deduplicated": dedup || errors.Is(err, ErrDuplicateReceipt)})
}

func (h *Handler) Audit(c *gin.Context) { c.JSON(200, h.svc.Audit()) }

func (h *Handler) MockLogs(c *gin.Context) {
	c.JSON(200, gin.H{
		"payment":  h.svc.pay.Receipts(),
		"delivery": h.svc.delivery.Logs(c.Query("batch_no")),
	})
}

func router(h *Handler) *gin.Engine {
	r := gin.Default()
	api := r.Group("/api")
	{
		api.POST("/login", h.Login)
		api.GET("/users", h.ListUsers)
		api.GET("/companies", h.ListCompanies)
		api.GET("/orders", h.ListOrders)
		api.GET("/batches", h.ListBatches)
		api.GET("/batches/:id", h.GetBatch)
		api.POST("/batches", h.CreateBatch)
		api.POST("/batches/:id/seals", h.RegisterSeals)
		api.POST("/batches/:id/handover", h.Handover)
		api.POST("/batches/:id/reroute", h.Reroute)
		api.POST("/batches/:id/deliver", h.Deliver)
		api.POST("/batches/:id/seal-broken", h.SealBroken)
		api.POST("/batches/:id/nobody", h.Nobody)
		api.POST("/batches/:id/sign", h.Sign)
		api.POST("/batches/:id/discrepancies", h.ReportDiscrepancy)
		api.POST("/discrepancies/:did/review", h.Review)
		api.POST("/discrepancies/:did/resupply", h.Resupply)
		api.POST("/discrepancies/:did/loss", h.Loss)
		api.POST("/discrepancies/:did/refund", h.Refund)
		api.GET("/audit", h.Audit)
		api.GET("/mock-logs", h.MockLogs)
	}
	return r
}
