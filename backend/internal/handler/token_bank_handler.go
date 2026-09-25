package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TokenBankHandler struct {
	bank     service.TokenBankRepository
	showcase *service.TokenBankShowcaseService
}

func NewTokenBankHandler(bank service.TokenBankRepository, settings service.SettingRepository) *TokenBankHandler {
	return &TokenBankHandler{bank: bank, showcase: service.NewTokenBankShowcaseService(bank, settings)}
}

func rentalUser(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	return subject.UserID, true
}
func rentalPage(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		page = 100000
	}
	return 20, (page - 1) * 20
}
func rentalReply(c *gin.Context, data any, err error) {
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, data)
}

func (h *TokenBankHandler) Overview(c *gin.Context) {
	id, ok := rentalUser(c)
	if !ok {
		return
	}
	limit, offset := rentalPage(c)
	data, err := h.bank.Overview(c.Request.Context(), id, c.Query("platform"), c.Query("status"), limit, offset)
	rentalReply(c, data, err)
}
func (h *TokenBankHandler) Revenue(c *gin.Context) {
	id, ok := rentalUser(c)
	if !ok {
		return
	}
	accountID, _ := strconv.ParseInt(c.Query("account_id"), 10, 64)
	limit, offset := rentalPage(c)
	data, err := h.bank.Revenue(c.Request.Context(), id, accountID, c.Query("platform"), limit, offset)
	rentalReply(c, data, err)
}
func (h *TokenBankHandler) AdminOverview(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Query("owner_user_id"), 10, 64)
	limit, offset := rentalPage(c)
	data, err := h.bank.Overview(c.Request.Context(), id, c.Query("platform"), c.Query("status"), limit, offset)
	rentalReply(c, data, err)
}
func (h *TokenBankHandler) AdminRevenue(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Query("owner_user_id"), 10, 64)
	accountID, _ := strconv.ParseInt(c.Query("account_id"), 10, 64)
	limit, offset := rentalPage(c)
	data, err := h.bank.Revenue(c.Request.Context(), id, accountID, c.Query("platform"), limit, offset)
	rentalReply(c, data, err)
}

func (h *TokenBankHandler) Showcase(c *gin.Context) {
	if _, ok := rentalUser(c); !ok {
		return
	}
	data, err := h.showcase.Get(c.Request.Context())
	rentalReply(c, data, err)
}

func tokenBankAdmin(c *gin.Context) bool {
	if _, ok := rentalUser(c); !ok {
		return false
	}
	if c.GetString(string(middleware.ContextKeyUserRole)) != service.RoleAdmin {
		response.Forbidden(c, "Admin access required")
		return false
	}
	return true
}

func (h *TokenBankHandler) AdminShowcase(c *gin.Context) {
	if !tokenBankAdmin(c) {
		return
	}
	enabled, err := h.showcase.Enabled(c.Request.Context())
	rentalReply(c, gin.H{"enabled": enabled}, err)
}

func (h *TokenBankHandler) SetAdminShowcase(c *gin.Context) {
	if !tokenBankAdmin(c) {
		return
	}
	var input struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "enabled must be a boolean")
		return
	}
	err := h.showcase.SetEnabled(c.Request.Context(), *input.Enabled)
	rentalReply(c, gin.H{"enabled": *input.Enabled}, err)
}
