package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetOAuthServerSettings(c *gin.Context) {
	settings, err := h.settingService.GetOAuthServerSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *SettingHandler) UpdateOAuthServerSettings(c *gin.Context) {
	var body struct {
		RedirectURIs *[]string `json:"redirect_uris"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.RedirectURIs == nil {
		response.BadRequest(c, "redirect_uris 数组为必填项")
		return
	}
	settings, err := h.settingService.SetOAuthServerRedirectURIs(c.Request.Context(), *body.RedirectURIs)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, settings)
}
