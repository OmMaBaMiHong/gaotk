//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type oauthCodeOnceStub struct {
	calls  int
	fail   bool
	exists map[string]bool
}

func (s *oauthCodeOnceStub) PutIfAbsent(_ context.Context, key string, _ string, _ time.Duration) (bool, error) {
	s.calls++
	if s.fail {
		return false, errors.New("redis down")
	}
	if s.exists[key] {
		return false, nil
	}
	s.exists[key] = true
	return true, nil
}

func newOAuthServerTestHandler(t *testing.T) (*AuthHandler, *service.OAuthClientAppService, *oauthCodeOnceStub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.JWT.Secret = "unit-test-secret"
	cfg.JWT.ExpireHour = 24
	cfg.JWT.RefreshTokenExpireDays = 30

	clients := service.NewOAuthClientAppService(&oauthAppsRepo{}, cfg, nil)

	codeOnce := &oauthCodeOnceStub{exists: map[string]bool{}}
	authService := service.NewAuthService(nil, nil, nil, &wechatOAuthRefreshTokenCacheStub{}, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	authService.SetOAuthCodeOnceCache(codeOnce)

	user := &service.User{ID: 31, Email: "me@example.com", Username: "u31", Role: service.RoleUser, Status: service.StatusActive, SignupSource: "email"}
	h := &AuthHandler{
		cfg:                   cfg,
		authService:           authService,
		userService:           service.NewUserService(&userHandlerRepoStub{user: user}, nil, nil, nil),
		oauthClientAppService: clients,
	}
	return h, clients, codeOnce
}

func postOAuthToken(h *AuthHandler, code string, clientID string, secret string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := `{"grant_type":"authorization_code","code":"` + code + `","client_id":"` + clientID + `","client_secret":"` + secret + `"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/oauth/token", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.OAuthToken(c)
	return recorder
}

func TestOAuthTokenScopedGrantIssuesScopedTokensOnce(t *testing.T) {
	h, clients, codeOnce := newOAuthServerTestHandler(t)
	ctx := context.Background()
	_, err := clients.Create(ctx, &service.CreateOAuthClientAppInput{
		Name: "App", ClientID: "scoped-app", ClientSecret: "secret-scoped-123456",
		RedirectURIs: "https://app.example/cb", AllowedScopes: "profile,membership",
	})
	require.NoError(t, err)

	code, err := h.signOAuthServerCode(oauthServerAuthCode{
		UserID: 31, ClientID: "scoped-app",
		ExpiresAt: time.Now().Add(time.Minute).Unix(),
		Nonce:     "nonce-scoped-1", Scopes: []string{"profile", "membership"},
	})
	require.NoError(t, err)

	recorder := postOAuthToken(h, code, "scoped-app", "secret-scoped-123456")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var resp struct {
		Code int `json:"code"`
		Data struct {
			AccessToken  string         `json:"access_token"`
			RefreshToken string         `json:"refresh_token"`
			Scope        string         `json:"scope"`
			ExpiresIn    int            `json:"expires_in"`
			User         map[string]any `json:"user"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.NotEmpty(t, resp.Data.AccessToken)
	require.NotEmpty(t, resp.Data.RefreshToken)
	require.Equal(t, "profile membership", resp.Data.Scope)
	require.LessOrEqual(t, resp.Data.ExpiresIn, 3600)
	// 裁剪视图：只回身份字段，不回余额
	require.Equal(t, "me@example.com", resp.Data.User["email"])
	require.NotContains(t, resp.Data.User, "balance")
	require.Equal(t, 1, codeOnce.calls, "scoped grant must consume the code exactly once")

	// 重放同一 code：拒绝
	replay := postOAuthToken(h, code, "scoped-app", "secret-scoped-123456")
	require.GreaterOrEqual(t, replay.Code, http.StatusBadRequest, "replayed code must be rejected")
	require.Equal(t, 2, codeOnce.calls)
}

func TestOAuthTokenLegacyGrantUnchanged(t *testing.T) {
	h, clients, codeOnce := newOAuthServerTestHandler(t)
	ctx := context.Background()
	// 未登记 scope = 传统模式：换到的仍是全量面板令牌对，code 消费逻辑不介入。
	_, err := clients.Create(ctx, &service.CreateOAuthClientAppInput{
		Name: "Legacy", ClientID: "skoob", ClientSecret: "secret-legacy-123456",
		RedirectURIs: "https://skoob.cc/api/v1/account/oauth/callback",
	})
	require.NoError(t, err)

	code, err := h.signOAuthServerCode(oauthServerAuthCode{
		UserID: 31, ClientID: "skoob",
		ExpiresAt: time.Now().Add(time.Minute).Unix(),
		Nonce:     "nonce-legacy-1",
	})
	require.NoError(t, err)

	recorder := postOAuthToken(h, code, "skoob", "secret-legacy-123456")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var resp struct {
		Code int `json:"code"`
		Data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			Scope        string `json:"scope"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.NotEmpty(t, resp.Data.AccessToken)
	require.NotEmpty(t, resp.Data.RefreshToken, "legacy grant keeps refresh token")
	require.Empty(t, resp.Data.Scope)
	require.Equal(t, 0, codeOnce.calls, "legacy grant must not touch code-once cache")
}

func TestGetCurrentUserScopedTokenTrimsProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user := &service.User{ID: 31, Email: "me@example.com", Username: "u31", Role: service.RoleUser, Status: service.StatusActive, SignupSource: "email"}
	h := &AuthHandler{
		cfg:         &config.Config{},
		userService: service.NewUserService(&userHandlerRepoStub{user: user}, nil, nil, nil),
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 31})
	c.Set(string(middleware2.ContextKeyOAuthScope), "profile,membership")
	h.GetCurrentUser(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "me@example.com", resp.Data["email"])
	require.Equal(t, "u31", resp.Data["username"])
	require.NotContains(t, resp.Data, "balance")
	require.NotContains(t, resp.Data, "auth_bindings")
	require.NotContains(t, resp.Data, "run_mode")
}
