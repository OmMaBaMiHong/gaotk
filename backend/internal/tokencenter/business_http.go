package tokencenter

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type businessHandler struct {
	store   *SQLStore
	service *Service
}

// The caller authenticates member/admin with the shared account service. Public
// responses deliberately expose only approved creatives, never merchant contacts.
func RegisterBusinessRoutes(public, admin, member *gin.RouterGroup, s *Service) {
	store, ok := s.store.(*SQLStore)
	h := &businessHandler{store: store, service: s}
	available := func(c *gin.Context) {
		if !ok {
			c.AbortWithStatusJSON(503, gin.H{"error": "业务数据库尚未配置"})
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
	p := public.Group("/tokencenter/business", available)
	p.GET("/ads/:code", h.publicAd)
	m := member.Group("/tokencenter/business", available)
	m.GET("/merchant", h.merchant)
	m.PUT("/merchant", h.saveMerchant)
	m.GET("/claims", h.claims(false))
	m.POST("/claims", h.submitClaim)
	m.GET("/slots", h.slots)
	m.GET("/campaigns", h.campaigns(false))
	m.POST("/campaigns", h.saveCampaign)
	m.PUT("/campaigns/:id", h.saveCampaign)
	m.POST("/campaigns/:id/pause", h.pauseCampaign)
	a := admin.Group("/tokencenter/business", available)
	applications := &httpHandler{service: s}
	a.GET("/applications", applications.listApplications)
	a.POST("/applications/:id/review", applications.reviewApplication)
	a.GET("/merchants", h.merchants)
	a.POST("/merchants/:id/review", h.reviewMerchant)
	a.GET("/claims", h.claims(true))
	a.POST("/claims/:id/review", h.reviewClaim)
	a.GET("/slots", h.slots)
	a.POST("/slots", h.saveSlot)
	a.PUT("/slots/:id", h.saveSlot)
	a.GET("/campaigns", h.campaigns(true))
	a.POST("/campaigns/:id/review", h.reviewCampaign)
}
func businessError(c *gin.Context, err error) {
	status, message := 500, "业务服务暂时不可用，请稍后重试"
	switch {
	case errors.Is(err, ErrBusinessNotFound):
		status = 404
		message = err.Error()
	case errors.Is(err, ErrBusinessConflict):
		status = 409
		message = err.Error()
	case errors.Is(err, ErrBusinessInvalid):
		status = 422
		message = err.Error()
	case errors.Is(err, ErrBusinessForbidden):
		status = 403
		message = err.Error()
	case errors.Is(err, ErrEmpty), errors.Is(err, ErrSourceRevoked), errors.Is(err, ErrAdmissionPending):
		status = 503
		message = "中台发布目录暂不可用，请稍后再试"
	}
	c.JSON(status, gin.H{"error": message})
}
func businessBody(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	if err := c.ShouldBindJSON(v); err != nil {
		c.JSON(400, gin.H{"error": "请检查表单内容与编号格式"})
		return false
	}
	return true
}
func businessID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "无效记录编号"})
		return 0, false
	}
	return id, true
}
func businessBefore(c *gin.Context) (int64, bool) {
	raw := c.Query("before")
	if raw == "" {
		return 0, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"error": "无效分页位置"})
		return 0, false
	}
	return id, true
}
func (h *businessHandler) ownMerchant(c *gin.Context) (BusinessMerchant, bool) {
	u := businessIdentity(c)
	if u.ID <= 0 {
		c.JSON(401, gin.H{"error": "请先登录统一账号"})
		return BusinessMerchant{}, false
	}
	m, err := h.store.MerchantForAccount(c.Request.Context(), u)
	if err != nil {
		if errors.Is(err, ErrBusinessNotFound) {
			err = invalidBusiness("请先提交商家入驻资料")
		}
		businessError(c, err)
		return m, false
	}
	return m, true
}
func (h *businessHandler) merchant(c *gin.Context) {
	m, err := h.store.MerchantForAccount(c.Request.Context(), businessIdentity(c))
	if errors.Is(err, ErrBusinessNotFound) {
		c.JSON(200, gin.H{"merchant": nil})
		return
	}
	if err != nil {
		businessError(c, err)
		return
	}
	c.JSON(200, gin.H{"merchant": m})
}
func (h *businessHandler) saveMerchant(c *gin.Context) {
	var in struct {
		Profile  MerchantProfile `json:"profile"`
		Revision int64           `json:"revision"`
	}
	if !businessBody(c, &in) {
		return
	}
	m, err := h.store.SaveMerchant(c.Request.Context(), businessIdentity(c), in.Profile, in.Revision)
	if err != nil {
		businessError(c, err)
		return
	}
	c.JSON(200, m)
}
func (h *businessHandler) merchants(c *gin.Context) {
	before, ok := businessBefore(c)
	if !ok {
		return
	}
	page, err := h.store.Merchants(c.Request.Context(), c.Query("status"), before)
	if err != nil {
		businessError(c, err)
		return
	}
	c.JSON(200, page)
}
func (h *businessHandler) reviewMerchant(c *gin.Context) {
	id, ok := businessID(c)
	if !ok {
		return
	}
	var in BusinessReview
	if !businessBody(c, &in) {
		return
	}
	row, err := h.store.ReviewMerchant(c.Request.Context(), id, in, businessActor(c))
	if err != nil {
		businessError(c, err)
		return
	}
	c.JSON(200, row)
}
func (h *businessHandler) claims(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		before, ok := businessBefore(c)
		if !ok {
			return
		}
		var merchantID int64
		if !admin {
			m, ok := h.ownMerchant(c)
			if !ok {
				return
			}
			merchantID = m.ID
		}
		page, err := h.store.Claims(c.Request.Context(), merchantID, c.Query("status"), before)
		if err != nil {
			businessError(c, err)
			return
		}
		c.JSON(200, page)
	}
}
func (h *businessHandler) submitClaim(c *gin.Context) {
	m, ok := h.ownMerchant(c)
	if !ok {
		return
	}
	var in struct {
		PlatformID string        `json:"platformId"`
		Evidence   ClaimEvidence `json:"evidence"`
	}
	if !businessBody(c, &in) {
		return
	}
	p, err := h.service.claimPlatform(c.Request.Context(), strings.TrimSpace(in.PlatformID))
	if err != nil {
		businessError(c, err)
		return
	}
	row, err := h.store.SubmitClaim(c.Request.Context(), m.ID, p, in.Evidence, businessActor(c))
	if err != nil {
		businessError(c, err)
		return
	}
	c.JSON(201, row)
}
func (h *businessHandler) reviewClaim(c *gin.Context) {
	id, ok := businessID(c)
	if !ok {
		return
	}
	var in BusinessReview
	if !businessBody(c, &in) {
		return
	}
	if in.Status == "approved" {
		row, err := h.store.Claim(c.Request.Context(), id)
		if err != nil {
			businessError(c, err)
			return
		}
		if _, err = h.service.claimPlatform(c.Request.Context(), row.PlatformID); err != nil {
			businessError(c, err)
			return
		}
	}
	row, err := h.store.ReviewClaim(c.Request.Context(), id, in, businessActor(c))
	if err != nil {
		businessError(c, err)
		return
	}
	c.JSON(200, row)
}
func (h *businessHandler) slots(c *gin.Context) {
	before, ok := businessBefore(c)
	if !ok {
		return
	}
	page, err := h.store.Slots(c.Request.Context(), before)
	if err != nil {
		businessError(c, err)
		return
	}
	c.JSON(200, page)
}
func (h *businessHandler) saveSlot(c *gin.Context) {
	var id int64
	if c.Param("id") != "" {
		var ok bool
		id, ok = businessID(c)
		if !ok {
			return
		}
	}
	var in AdSlot
	if !businessBody(c, &in) {
		return
	}
	row, err := h.store.SaveSlot(c.Request.Context(), id, in, businessActor(c))
	if err != nil {
		businessError(c, err)
		return
	}
	c.JSON(200, row)
}
func (h *businessHandler) campaigns(admin bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		before, ok := businessBefore(c)
		if !ok {
			return
		}
		var merchantID int64
		if !admin {
			m, ok := h.ownMerchant(c)
			if !ok {
				return
			}
			merchantID = m.ID
		}
		page, err := h.store.Campaigns(c.Request.Context(), merchantID, c.Query("status"), before)
		if err != nil {
			businessError(c, err)
			return
		}
		c.JSON(200, page)
	}
}
func (h *businessHandler) saveCampaign(c *gin.Context) {
	m, ok := h.ownMerchant(c)
	if !ok {
		return
	}
	var id int64
	if c.Param("id") != "" {
		id, ok = businessID(c)
		if !ok {
			return
		}
		row, err := h.store.Campaign(c.Request.Context(), id)
		if err != nil {
			businessError(c, err)
			return
		}
		if row.MerchantID != m.ID {
			businessError(c, ErrBusinessForbidden)
			return
		}
	}
	var in AdCampaignInput
	if !businessBody(c, &in) {
		return
	}
	row, err := h.store.SaveCampaign(c.Request.Context(), m.ID, id, in, businessActor(c))
	if err != nil {
		businessError(c, err)
		return
	}
	c.JSON(200, row)
}
func (h *businessHandler) reviewCampaign(c *gin.Context) {
	id, ok := businessID(c)
	if !ok {
		return
	}
	var in BusinessReview
	if !businessBody(c, &in) {
		return
	}
	row, err := h.store.ReviewCampaign(c.Request.Context(), id, in, businessActor(c))
	if err != nil {
		businessError(c, err)
		return
	}
	c.JSON(200, row)
}
func (h *businessHandler) pauseCampaign(c *gin.Context) {
	m, ok := h.ownMerchant(c)
	if !ok {
		return
	}
	id, ok := businessID(c)
	if !ok {
		return
	}
	row, err := h.store.Campaign(c.Request.Context(), id)
	if err != nil {
		businessError(c, err)
		return
	}
	if row.MerchantID != m.ID {
		businessError(c, ErrBusinessForbidden)
		return
	}
	var in struct {
		Revision int64 `json:"revision"`
	}
	if !businessBody(c, &in) {
		return
	}
	row, err = h.store.ReviewCampaign(c.Request.Context(), id, BusinessReview{Revision: in.Revision, Status: "paused", Note: "商家主动暂停投放"}, businessActor(c))
	if err != nil {
		businessError(c, err)
		return
	}
	c.JSON(200, row)
}
func (h *businessHandler) publicAd(c *gin.Context) {
	row, err := h.store.PublicCampaign(c.Request.Context(), c.Param("code"))
	if err != nil {
		businessError(c, err)
		return
	}
	c.JSON(200, gin.H{"campaign": row})
}
