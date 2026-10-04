package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"mealcheck/internal/models"
	"mealcheck/internal/service"
)

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

func NewWithDB(db *gorm.DB) *Handler { return New(service.New(db)) }

func (h *Handler) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	registerStatic(r)

	api := r.Group("/api")
	api.GET("/bootstrap", h.bootstrap)
	api.POST("/login", h.login)

	auth := api.Group("", h.auth())
	{
		auth.GET("/batches", h.listBatches)
		auth.GET("/batches/:id", h.getBatch)
		auth.GET("/batches/:id/conservation", h.conservation)
		auth.GET("/batches/:id/timeline", h.timeline)
		auth.GET("/routes", h.listRoutes)
		auth.GET("/discrepancies", h.listDiscrepancies)
	}

	kitchen := api.Group("/kitchen", h.auth(), h.require(models.RoleKitchen))
	{
		kitchen.POST("/batches", h.createBatch)
		kitchen.PUT("/batches/:id/packing", h.updatePacking)
		kitchen.POST("/batches/:id/seal", h.seal)
	}

	admin := api.Group("/dispatch", h.auth(), h.require(models.RoleAdmin))
	{
		admin.POST("/batches/:id/assign", h.assign)
		admin.POST("/batches/:id/reassign", h.reassign)
	}

	courier := api.Group("/routes", h.auth(), h.require(models.RoleCourier))
	{
		courier.GET("/my", h.myRoute)
		courier.POST("/batches/:id/accept", h.accept)
		courier.POST("/batches/:id/depart", h.depart)
		courier.POST("/batches/:id/deliver", h.deliver)
		courier.POST("/batches/:id/no-one", h.noOne)
	}
	// 封签破损：配送员与前台均可登记
	r.POST("/api/batches/:id/seal-broken", h.auth(), h.require(models.RoleCourier, models.RoleFrontDesk), h.reportBrokenSeal)

	desk := api.Group("/frontdesk", h.auth(), h.require(models.RoleFrontDesk))
	{
		desk.POST("/batches/:id/sign", h.sign)
		desk.POST("/batches/:id/discrepancies", h.raiseDiff)
	}

	rev := api.Group("/review", h.auth(), h.require(models.RoleReviewer, models.RoleAdmin))
	{
		rev.POST("/discrepancies/:id/approve", h.approve)
		rev.POST("/discrepancies/:id/reject", h.reject)
		rev.POST("/resolutions/:id/refund-receipt", h.refundReceipt)
	}

	return r
}

// ---------- 鉴权 ----------

// 本地模拟登录：直接以用户名换取会话（演示系统，不设密码）
func (h *Handler) login(c *gin.Context) {
	var in struct {
		Username string `json:"username"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Username == "" {
		fail(c, http.StatusBadRequest, "用户名必填")
		return
	}
	var u models.User
	if err := h.svc.DB().Preload("Enterprise").First(&u, "username = ?", in.Username).Error; err != nil {
		fail(c, http.StatusUnauthorized, "用户不存在")
		return
	}
	c.Set("user", u)
	ok(c, gin.H{"token": u.Username, "user": u})
}

// 模拟 token：X-User 头携带用户名
func (h *Handler) auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.GetHeader("X-User")
		if name == "" {
			name = c.Query("user")
		}
		if name == "" {
			fail(c, http.StatusUnauthorized, "未登录")
			c.Abort()
			return
		}
		var u models.User
		if err := h.svc.DB().First(&u, "username = ?", name).Error; err != nil {
			fail(c, http.StatusUnauthorized, "会话无效")
			c.Abort()
			return
		}
		c.Set("user", u)
		c.Next()
	}
}

func (h *Handler) require(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		for _, r := range roles {
			if u.Role == r {
				c.Next()
				return
			}
		}
		fail(c, http.StatusForbidden, "角色无权操作："+u.Role)
		c.Abort()
	}
}

func currentUser(c *gin.Context) models.User {
	v, _ := c.Get("user")
	return v.(models.User)
}

func (h *Handler) bootstrap(c *gin.Context) {
	var users []models.User
	h.svc.DB().Find(&users)
	var ents []models.Enterprise
	h.svc.DB().Find(&ents)
	var routes []models.Route
	h.svc.DB().Preload("Courier").Find(&routes)
	ok(c, gin.H{"users": users, "enterprises": ents, "routes": routes})
}

// ---------- 通用查询 ----------

func (h *Handler) listBatches(c *gin.Context) {
	u := currentUser(c)
	var ent *uint
	if u.Role == models.RoleFrontDesk && u.EnterpriseID != nil {
		ent = u.EnterpriseID
	}
	if v := c.Query("enterprise_id"); v != "" {
		if id, err := strconv.Atoi(v); err == nil {
			e := uint(id)
			ent = &e
		}
	}
	bs, err := h.svc.ListBatches(ent, c.Query("status"))
	if err != nil {
		serveErr(c, err)
		return
	}
	ok(c, bs)
}

func (h *Handler) getBatch(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	d, err := h.svc.GetBatch(uint(id))
	if err != nil {
		serveErr(c, err)
		return
	}
	ok(c, d)
}

func (h *Handler) conservation(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	d, err := h.svc.GetBatch(uint(id))
	if err != nil {
		serveErr(c, err)
		return
	}
	rep, err := h.svc.CheckConservation(d.RootID)
	if err != nil {
		serveErr(c, err)
		return
	}
	ok(c, rep)
}

func (h *Handler) timeline(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	d, err := h.svc.GetBatch(uint(id))
	if err != nil {
		serveErr(c, err)
		return
	}
	var events []models.RouteEvent
	h.svc.DB().Where("batch_id IN (SELECT id FROM batches WHERE root_id = ?)", d.RootID).
		Order("created_at asc, id asc").Find(&events)
	var audits []models.AuditLog
	h.svc.DB().Where("batch_id IN (SELECT id FROM batches WHERE root_id = ?)", d.RootID).
		Order("created_at asc, id asc").Find(&audits)
	ok(c, gin.H{"route_events": events, "audits": audits})
}

func (h *Handler) listRoutes(c *gin.Context) {
	var routes []models.Route
	h.svc.DB().Preload("Courier").Find(&routes)
	ok(c, routes)
}

func (h *Handler) myRoute(c *gin.Context) {
	u := currentUser(c)
	var as []models.RouteAssignment
	h.svc.DB().Preload("Batch").Preload("Batch.Enterprise").Preload("Batch.Items").Preload("Batch.Boxes").
		Where("courier_id = ? AND active = ?", u.ID, true).Find(&as)
	ok(c, as)
}

func (h *Handler) listDiscrepancies(c *gin.Context) {
	q := h.svc.DB().Preload("Lines").Preload("Resolution").Preload("Resolution.Refund").
		Order("id desc")
	if st := c.Query("status"); st != "" {
		q = q.Where("status = ?", st)
	}
	var ds []models.Discrepancy
	q.Find(&ds)
	ok(c, ds)
}

// ---------- 厨房 ----------

func (h *Handler) createBatch(c *gin.Context) {
	var in service.CreateBatchInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	b, err := h.svc.CreateBatch(currentUser(c), in)
	if err != nil {
		serveErr(c, err)
		return
	}
	ok(c, b)
}

func (h *Handler) updatePacking(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in struct {
		Items []service.ItemInput `json:"items"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	b, err := h.svc.UpdatePacked(currentUser(c), uint(id), in.Items)
	if err != nil {
		serveErr(c, err)
		return
	}
	ok(c, b)
}

func (h *Handler) seal(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in service.SealInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	b, err := h.svc.Seal(currentUser(c), uint(id), in)
	if err != nil {
		serveErr(c, err)
		return
	}
	ok(c, b)
}

// ---------- 调度 / 配送 ----------

func (h *Handler) assign(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in struct {
		RouteID uint   `json:"route_id"`
		Note    string `json:"note"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	a, err := h.svc.Routes().Assign(currentUser(c), uint(id), in.RouteID, in.Note)
	if err != nil {
		serveErr(c, err)
		return
	}
	ok(c, a)
}

func (h *Handler) reassign(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in struct {
		RouteID uint   `json:"route_id"`
		Note    string `json:"note"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	a, err := h.svc.Routes().Reassign(currentUser(c), uint(id), in.RouteID, in.Note)
	if err != nil {
		serveErr(c, err)
		return
	}
	ok(c, a)
}

func (h *Handler) accept(c *gin.Context)  { h.simpleRoute(c, h.svc.Routes().Accept) }
func (h *Handler) depart(c *gin.Context)  { h.simpleRoute(c, h.svc.Routes().Depart) }
func (h *Handler) deliver(c *gin.Context) { h.simpleRoute(c, h.svc.Routes().Deliver) }
func (h *Handler) noOne(c *gin.Context)   { h.simpleRoute(c, h.svc.Routes().NoOne) }

func (h *Handler) simpleRoute(c *gin.Context, fn func(models.User, uint, string) error) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&in)
	if err := fn(currentUser(c), uint(id), in.Note); err != nil {
		serveErr(c, err)
		return
	}
	ok(c, gin.H{"ok": true})
}

func (h *Handler) reportBrokenSeal(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in struct {
		SealNo string `json:"seal_no"`
		Note   string `json:"note"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.svc.Routes().ReportBrokenSeal(currentUser(c), uint(id), in.SealNo, in.Note); err != nil {
		serveErr(c, err)
		return
	}
	ok(c, gin.H{"ok": true})
}

// ---------- 前台 ----------

func (h *Handler) sign(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in service.SignInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	r, err := h.svc.Sign(currentUser(c), uint(id), in)
	if err != nil {
		serveErr(c, err)
		return
	}
	ok(c, r)
}

func (h *Handler) raiseDiff(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in struct {
		Kind  string                     `json:"kind"`
		Lines []service.ReceiptLineInput `json:"lines"`
		Note  string                     `json:"note"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	d, err := h.svc.RaiseDifference(currentUser(c), uint(id), in.Kind, in.Lines, in.Note)
	if err != nil {
		serveErr(c, err)
		return
	}
	ok(c, d)
}

// ---------- 复核 ----------

func (h *Handler) approve(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in service.ApproveInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	res, err := h.svc.Review().Approve(currentUser(c), uint(id), in)
	if err != nil {
		serveErr(c, err)
		return
	}
	ok(c, res)
}

func (h *Handler) reject(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in struct {
		Note string `json:"note"`
	}
	_ = c.ShouldBindJSON(&in)
	if err := h.svc.Review().Reject(currentUser(c), uint(id), in.Note); err != nil {
		serveErr(c, err)
		return
	}
	ok(c, gin.H{"ok": true})
}

func (h *Handler) refundReceipt(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in struct {
		GatewayReceipt string `json:"gateway_receipt"`
	}
	_ = c.ShouldBindJSON(&in)
	rec, err := h.svc.Review().PostRefundReceipt(uint(id), in.GatewayReceipt)
	if err != nil && !errors.Is(err, service.ErrAlreadyPosted) {
		serveErr(c, err)
		return
	}
	ok(c, gin.H{"refund": rec, "duplicate": errors.Is(err, service.ErrAlreadyPosted)})
}

// ---------- helpers ----------

func ok(c *gin.Context, data any) { c.JSON(http.StatusOK, gin.H{"ok": true, "data": data}) }

func fail(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"ok": false, "error": msg})
}

func serveErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrForbidden):
		fail(c, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrNotFound):
		fail(c, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrConflict), errors.Is(err, service.ErrSealed),
		errors.Is(err, service.ErrNoActiveRoute), errors.Is(err, service.ErrAlreadyPosted):
		fail(c, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrValidation):
		fail(c, http.StatusBadRequest, err.Error())
	default:
		fail(c, http.StatusInternalServerError, err.Error())
	}
}
