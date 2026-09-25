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

func (r *oauthSettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != service.SettingKeyOAuthServerRedirectURIs {
		return "", service.ErrSettingNotFound
	}
	return r.value, r.err
}

func (r *oauthSettingsRepo) Set(context.Context, string, string) error { return nil }

type oauthAppsRepo struct {
	service.OAuthClientAppRepository
	app *service.OAuthClientApp
	err error
}

func (r *oauthAppsRepo) Count(context.Context) (int, error) {
	if r.app != nil {
		return 1, nil
	}
	return 0, nil
}
func (r *oauthAppsRepo) Create(_ context.Context, app *service.OAuthClientApp) error {
	r.app = app
	return nil
}
func (r *oauthAppsRepo) Update(_ context.Context, app *service.OAuthClientApp) error {
	r.app = app
	return nil
}
func (r *oauthAppsRepo) GetByClientID(_ context.Context, id string) (*service.OAuthClientApp, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.app == nil || r.app.ClientID != id {
		return nil, service.ErrOAuthClientNotFound
	}
	copy := *r.app
	return &copy, nil
}

func TestOAuthServerBothAuthorizeEndpointsUseSavedSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{OAuthServer: config.OAuthServerConfig{ClientID: "skoob", ClientSecret: "existing-private-secret", RedirectURI: "https://old.example/callback"}}
	repo := &oauthSettingsRepo{value: `["https://skoob.cc/api/v1/account/oauth/callback"]`}
	settings := service.NewSettingService(repo, cfg)
	apps := &oauthAppsRepo{}
	settings.SetOAuthClientAppRepository(apps)
	clients := service.NewOAuthClientAppService(apps, cfg, settings)
	h := &AuthHandler{cfg: cfg, settingSvc: settings, oauthClientAppService: clients}
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
	apps.err = errors.New("database down")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/oauth/authorize?client_id=skoob&redirect_uri=https://old.example/callback", nil)
	h.OAuthAuthorize(c)
	require.Equal(t, 500, w.Code)
	require.NotContains(t, w.Body.String(), "redirect_uri not allowed")
}
