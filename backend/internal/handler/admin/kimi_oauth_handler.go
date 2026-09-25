package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/kimi"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// KimiOAuthHandler Kimi Code 设备授权流 OAuth 管理端点。
type KimiOAuthHandler struct {
	kimiOAuthService *service.KimiOAuthService
	adminService     service.AdminService
}

// NewKimiOAuthHandler 创建 Kimi OAuth 管理端点。
func NewKimiOAuthHandler(kimiOAuthService *service.KimiOAuthService, adminService service.AdminService) *KimiOAuthHandler {
	return &KimiOAuthHandler{
		kimiOAuthService: kimiOAuthService,
		adminService:     adminService,
	}
}

// GetCapabilities 返回 Kimi OAuth 能力信息。
// GET /api/v1/admin/kimi/oauth/capabilities
func (h *KimiOAuthHandler) GetCapabilities(c *gin.Context) {
	response.Success(c, h.kimiOAuthService.GetCapabilities())
}

// StartDeviceFlow 发起 Kimi 设备授权。
// POST /api/v1/admin/kimi/oauth/start
func (h *KimiOAuthHandler) StartDeviceFlow(c *gin.Context) {
	result, err := h.kimiOAuthService.StartDeviceFlow(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// PollDeviceFlowRequest 轮询请求体。
type PollDeviceFlowRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

// PollDeviceFlow 轮询 Kimi 设备授权结果。
// POST /api/v1/admin/kimi/oauth/poll
func (h *KimiOAuthHandler) PollDeviceFlow(c *gin.Context) {
	var req PollDeviceFlowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.kimiOAuthService.PollDeviceFlow(c.Request.Context(), req.SessionID)
	if err != nil {
		if err == kimi.ErrAuthorizationPending {
			response.Success(c, service.KimiPollDeviceFlowResult{Pending: true})
			return
		}
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// RefreshKimiTokenRequest 手动刷新请求体。
type RefreshKimiTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// RefreshToken 使用 refresh_token 手动刷新。
// POST /api/v1/admin/kimi/oauth/refresh
func (h *KimiOAuthHandler) RefreshToken(c *gin.Context) {
	var req RefreshKimiTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	tokenInfo, err := h.kimiOAuthService.RefreshToken(c.Request.Context(), strings.TrimSpace(req.RefreshToken))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, tokenInfo)
}

// RefreshAccountToken 刷新指定 Kimi OAuth 账号的 token 并持久化。
// POST /api/v1/admin/kimi/accounts/:id/refresh
func (h *KimiOAuthHandler) RefreshAccountToken(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account.Platform != service.PlatformKimi {
		response.BadRequest(c, "Account platform does not match Kimi OAuth endpoint")
		return
	}
	if !account.IsOAuth() {
		response.BadRequest(c, "Cannot refresh non-OAuth account credentials")
		return
	}
	if account.IsCredentialShadow() {
		response.BadRequest(c, "Cannot refresh kimi shadow account; its credentials are managed by the parent account")
		return
	}

	tokenInfo, err := h.kimiOAuthService.RefreshAccountToken(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	newCredentials := h.kimiOAuthService.BuildAccountCredentials(tokenInfo)
	for k, v := range account.Credentials {
		if _, exists := newCredentials[k]; !exists {
			newCredentials[k] = v
		}
	}

	updatedAccount, err := h.adminService.UpdateAccount(c.Request.Context(), accountID, &service.UpdateAccountInput{
		Credentials: newCredentials,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, accountDTOForContext(c.Request.Context(), updatedAccount))
}

// CreateKimiAccountFromOAuthRequest 从设备授权 token 创建账号的请求体。
type CreateKimiAccountFromOAuthRequest struct {
	SessionID   string  `json:"session_id" binding:"required"`
	Name        string  `json:"name"`
	Concurrency int     `json:"concurrency"`
	Priority    int     `json:"priority"`
	GroupIDs    []int64 `json:"group_ids"`
}

// CreateAccountFromOAuth 轮询取回 token 并创建 Kimi 平台 OAuth 账号。
// POST /api/v1/admin/kimi/create-from-oauth
func (h *KimiOAuthHandler) CreateAccountFromOAuth(c *gin.Context) {
	var req CreateKimiAccountFromOAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// 消费设备授权会话（唯一消费方）：取回缓存的 token 并删除会话。
	tokenInfo, err := h.kimiOAuthService.ConsumeDeviceFlow(c.Request.Context(), req.SessionID)
	if err != nil {
		if err == kimi.ErrAuthorizationPending {
			response.BadRequest(c, "Authorization is still pending; retry after the user approves")
			return
		}
		response.ErrorFrom(c, err)
		return
	}

	credentials := h.kimiOAuthService.BuildAccountCredentials(tokenInfo)

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Kimi Code Seat"
	}

	account, err := h.adminService.CreateAccount(c.Request.Context(), &service.CreateAccountInput{
		Name:        name,
		Platform:    service.PlatformKimi,
		Type:        service.AccountTypeOAuth,
		Credentials: credentials,
		Extra: map[string]any{
			"import_source": "kimi_device_oauth",
			"auth_provider": "kimi_code",
		},
		Concurrency: req.Concurrency,
		Priority:    req.Priority,
		GroupIDs:    req.GroupIDs,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, accountDTOForContext(c.Request.Context(), account))
}
