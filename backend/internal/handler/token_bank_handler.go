package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TokenBankHandler struct{ bank service.TokenBankRepository }

func NewTokenBankHandler(bank service.TokenBankRepository) *TokenBankHandler {
	return &TokenBankHandler{bank: bank}
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
