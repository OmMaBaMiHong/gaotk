package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type oauthSettingsRepo struct {
	service.SettingRepository
	value string
	err   error
}

func (r *oauthSettingsRepo) GetValue(context.Context, string) (string, error) { return r.value, r.err }

func TestOAuthServerBothAuthorizeEndpointsUseSavedSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{OAuthServer: config.OAuthServerConfig{ClientID: "skoob", RedirectURI: "https://old.example/callback"}}
	repo := &oauthSettingsRepo{value: `["https://skoob.cc/api/v1/account/oauth/callback"]`}
	h := &AuthHandler{cfg: cfg, settingSvc: service.NewSettingService(repo, cfg)}
	for _, jsonMode := range []bool{false, true} {
		for _, tc := range []struct {
			uri     string
			allowed bool
		}{{"https://skoob.cc/api/v1/account/oauth/callback", true}, {"https://old.example/callback", false}, {"http://localhost:12@evil.example/callback", false}} {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			if jsonMode {
				c.Request = httptest.NewRequest("POST", "/api/v1/oauth/authorize", strings.NewReader(`{"client_id":"skoob","redirect_uri":"`+tc.uri+`"}`))
				c.Request.Header.Set("Content-Type", "application/json")
				h.OAuthAuthorizeJSON(c)
			} else {
				c.Request = httptest.NewRequest("GET", "/api/v1/oauth/authorize?client_id=skoob&redirect_uri="+url.QueryEscape(tc.uri), nil)
				h.OAuthAuthorize(c)
			}
			if tc.allowed {
				if jsonMode {
					require.Equal(t, 401, w.Code)
				} else {
					require.Equal(t, 302, w.Code)
				}
			} else {
				require.Equal(t, 400, w.Code)
				require.Contains(t, w.Body.String(), "redirect_uri not allowed")
			}
		}
	}
	repo.err = errors.New("database down")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/oauth/authorize?client_id=skoob&redirect_uri=https://old.example/callback", nil)
	h.OAuthAuthorize(c)
	require.Equal(t, 503, w.Code)
	require.NotContains(t, w.Body.String(), "redirect_uri not allowed")
}
