package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TokenBankHandler struct{ bank *service.TokenBankService }

func NewTokenBankHandler(bank *service.TokenBankService) *TokenBankHandler {
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

func (h *TokenBankHandler) Policies(c *gin.Context) {
	if _, ok := rentalUser(c); !ok {
		return
	}
	policies, err := h.bank.Repo.Policies(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := []gin.H{}
	for _, p := range policies {
		if p.Enabled {
			out = append(out, gin.H{"platform": p.Platform, "owner_share_bps": p.OwnerShareBPS})
		}
	}
	response.Success(c, out)
}
func (h *TokenBankHandler) Overview(c *gin.Context) {
	id, ok := rentalUser(c)
	if !ok {
		return
	}
	limit, offset := rentalPage(c)
	data, err := h.bank.Repo.Overview(c.Request.Context(), id, c.Query("platform"), c.Query("rental_status"), limit, offset)
	rentalReply(c, data, err)
}
func (h *TokenBankHandler) Revenue(c *gin.Context) {
	id, ok := rentalUser(c)
	if !ok {
		return
	}
	accountID, _ := strconv.ParseInt(c.Query("account_id"), 10, 64)
	limit, offset := rentalPage(c)
	data, err := h.bank.Repo.Revenue(c.Request.Context(), id, accountID, c.Query("platform"), limit, offset)
	rentalReply(c, data, err)
}
func (h *TokenBankHandler) Import(c *gin.Context) {
	id, ok := rentalUser(c)
	if !ok {
		return
	}
	var in service.RentalImportInput
	if c.ShouldBindJSON(&in) != nil {
		response.ErrorFrom(c, service.ErrRentalInvalid)
		return
	}
	accountID, err := h.bank.Import(c.Request.Context(), id, in)
	rentalReply(c, gin.H{"id": accountID}, err)
}
func (h *TokenBankHandler) StartOAuth(c *gin.Context) {
	id, ok := rentalUser(c)
	if !ok {
		return
	}
	var in service.RentalImportInput
	if c.ShouldBindJSON(&in) != nil {
		response.ErrorFrom(c, service.ErrRentalInvalid)
		return
	}
	data, err := h.bank.StartOAuth(c.Request.Context(), id, in)
	rentalReply(c, data, err)
}
func (h *TokenBankHandler) FinishOAuth(c *gin.Context) {
	id, ok := rentalUser(c)
	if !ok {
		return
	}
	var in service.RentalOAuthFinish
	if c.ShouldBindJSON(&in) != nil {
		response.ErrorFrom(c, service.ErrRentalInvalid)
		return
	}
	accountID, err := h.bank.FinishOAuth(c.Request.Context(), id, in)
	rentalReply(c, gin.H{"id": accountID}, err)
}
func (h *TokenBankHandler) SetStatus(c *gin.Context) {
	id, ok := rentalUser(c)
	if !ok {
		return
	}
	accountID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var in struct {
		Status string `json:"status"`
	}
	if c.ShouldBindJSON(&in) != nil {
		response.ErrorFrom(c, service.ErrRentalInvalid)
		return
	}
	rentalReply(c, gin.H{}, h.bank.Repo.SetStatus(c.Request.Context(), id, accountID, in.Status))
}

// These methods are registered only under the existing admin middleware.
func (h *TokenBankHandler) AdminPolicies(c *gin.Context) {
	data, err := h.bank.Repo.Policies(c.Request.Context())
	rentalReply(c, data, err)
}
func (h *TokenBankHandler) SavePolicy(c *gin.Context) {
	var in service.RentalPolicy
	if c.ShouldBindJSON(&in) != nil {
		response.ErrorFrom(c, service.ErrRentalInvalid)
		return
	}
	rentalReply(c, gin.H{}, h.bank.Repo.SavePolicy(c.Request.Context(), in))
}
func (h *TokenBankHandler) AdminOverview(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Query("owner_user_id"), 10, 64)
	limit, offset := rentalPage(c)
	data, err := h.bank.Repo.Overview(c.Request.Context(), id, c.Query("platform"), c.Query("rental_status"), limit, offset)
	rentalReply(c, data, err)
}
func (h *TokenBankHandler) AdminRevenue(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Query("owner_user_id"), 10, 64)
	accountID, _ := strconv.ParseInt(c.Query("account_id"), 10, 64)
	limit, offset := rentalPage(c)
	data, err := h.bank.Repo.Revenue(c.Request.Context(), id, accountID, c.Query("platform"), limit, offset)
	rentalReply(c, data, err)
}
