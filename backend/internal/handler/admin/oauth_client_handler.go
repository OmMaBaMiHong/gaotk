package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// OAuthClientAppHandler 管理端「OAuth 授权应用」CRUD（超管专属路由组内）。
type OAuthClientAppHandler struct {
	oauthClientAppService *service.OAuthClientAppService
}

func NewOAuthClientAppHandler(oauthClientAppService *service.OAuthClientAppService) *OAuthClientAppHandler {
	return &OAuthClientAppHandler{oauthClientAppService: oauthClientAppService}
}

type CreateOAuthClientAppRequest struct {
	Name           string   `json:"name" binding:"required"`
	ClientID       string   `json:"client_id" binding:"required"`
	ClientSecret   string   `json:"client_secret"`
	RedirectURIs   []string `json:"redirect_uris" binding:"required,min=1"`
	AllowLocalhost bool     `json:"allow_localhost"`
	Remark         string   `json:"remark"`
}

type UpdateOAuthClientAppRequest struct {
	Name             *string   `json:"name"`
	ClientSecret     *string   `json:"client_secret"`
	RedirectURIs     *[]string `json:"redirect_uris"`
	AllowLocalhost   *bool     `json:"allow_localhost"`
	Enabled          *bool     `json:"enabled"`
	Remark           *string   `json:"remark"`
	RegenerateSecret *bool     `json:"regenerate_secret"`
}

type oauthClientAppView struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	ClientID       string   `json:"client_id"`
	SecretLast4    string   `json:"secret_last4"`
	RedirectURIs   []string `json:"redirect_uris"`
	AllowLocalhost bool     `json:"allow_localhost"`
	Enabled        bool     `json:"enabled"`
	Remark         string   `json:"remark"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

// view 列表/更新后的常规展示：密钥只回显尾 4 位。
func (h *OAuthClientAppHandler) view(app *service.OAuthClientApp) oauthClientAppView {
	secretLast4 := ""
	if len(app.ClientSecret) >= 4 {
		secretLast4 = app.ClientSecret[len(app.ClientSecret)-4:]
	}
	return oauthClientAppView{
		ID:             app.ID,
		Name:           app.Name,
		ClientID:       app.ClientID,
		SecretLast4:    secretLast4,
		RedirectURIs:   app.RedirectURIs,
		AllowLocalhost: app.AllowLocalhost,
		Enabled:        app.Enabled,
		Remark:         app.Remark,
		CreatedAt:      app.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:      app.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

// List 全量列表（客户端数量级极小，无需分页）。
// GET /api/v1/admin/oauth-clients
func (h *OAuthClientAppHandler) List(c *gin.Context) {
	apps, err := h.oauthClientAppService.List(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]oauthClientAppView, 0, len(apps))
	for _, app := range apps {
		items = append(items, h.view(app))
	}
	response.Success(c, gin.H{"items": items})
}

// Create 新建授权应用：secret 留空则服务端生成，仅在本次响应中完整返回一次。
// POST /api/v1/admin/oauth-clients
func (h *OAuthClientAppHandler) Create(c *gin.Context) {
	var req CreateOAuthClientAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	app, err := h.oauthClientAppService.Create(c.Request.Context(), &service.CreateOAuthClientAppInput{
		Name:           req.Name,
		ClientID:       req.ClientID,
		ClientSecret:   req.ClientSecret,
		RedirectURIs:   strings.Join(req.RedirectURIs, "\n"),
		AllowLocalhost: req.AllowLocalhost,
		Remark:         req.Remark,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	view := h.view(app)
	response.Success(c, gin.H{
		"client":        view,
		"client_secret": app.ClientSecret, // 仅创建响应返回一次
	})
}

// Update 编辑授权应用；regenerate_secret=true 时重新生成密钥并完整返回一次。
// PUT /api/v1/admin/oauth-clients/:id
func (h *OAuthClientAppHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, 400, "invalid id")
		return
	}
	var req UpdateOAuthClientAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	input := &service.UpdateOAuthClientAppInput{
		Name:             req.Name,
		ClientSecret:     req.ClientSecret,
		AllowLocalhost:   req.AllowLocalhost,
		Enabled:          req.Enabled,
		Remark:           req.Remark,
		RegenerateSecret: req.RegenerateSecret != nil && *req.RegenerateSecret,
	}
	if req.RedirectURIs != nil {
		uris := strings.Join(*req.RedirectURIs, "\n")
		input.RedirectURIs = &uris
	}
	app, err := h.oauthClientAppService.Update(c.Request.Context(), id, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	view := h.view(app)
	resp := gin.H{"client": view}
	if input.RegenerateSecret {
		resp["client_secret"] = app.ClientSecret // 仅重新生成时返回一次
	}
	response.Success(c, resp)
}

// Delete 删除授权应用（硬删除）。
// DELETE /api/v1/admin/oauth-clients/:id
func (h *OAuthClientAppHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, 400, "invalid id")
		return
	}
	if err := h.oauthClientAppService.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "deleted"})
}
