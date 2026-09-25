package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestOwnedAccountRoutesExposeOnlyExplicitAccountOperations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{
		Account: &admin.AccountHandler{}, OAuth: &admin.OAuthHandler{}, OpenAIOAuth: &admin.OpenAIOAuthHandler{}, GeminiOAuth: &admin.GeminiOAuthHandler{}, AntigravityOAuth: &admin.AntigravityOAuthHandler{}, GrokOAuth: &admin.GrokOAuthHandler{}, KimiOAuth: &admin.KimiOAuthHandler{}, CNProvider: &admin.CNProviderHandler{}, Usage: &admin.UsageHandler{},
	}}
	registerOwnedAccountRoutes(r.Group("/user"), h)
	routes := map[string]bool{}
	for _, route := range r.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, path := range []string{"GET /user/accounts", "POST /user/accounts", "POST /user/accounts/data", "POST /user/accounts/import/codex-session", "POST /user/accounts/generate-auth-url", "POST /user/openai/create-from-oauth", "POST /user/gemini/oauth/exchange-code", "POST /user/antigravity/oauth/exchange-code", "POST /user/grok/sso-to-oauth", "POST /user/kimi/create-from-oauth", "GET /user/accounts/:id/usage-logs"} {
		require.True(t, routes[path], path)
	}
	for _, path := range []string{"POST /user/accounts/sync/crs", "POST /user/accounts/:id/duplicate", "POST /user/accounts/:id/shadow", "POST /user/accounts/bulk-update", "GET /user/accounts/data", "PUT /user/accounts/upstream-billing-probe/settings", "POST /user/accounts/:id/reset-quota", "POST /user/grok/oauth/reconcile", "GET /user/grok/runtime-sanity", "POST /user/openai/accounts/:id/reset-quota", "GET /user/proxies/all", "GET /user/groups/all"} {
		require.False(t, routes[path], path)
	}
}

func TestTokenBankDisabledGuardCoversAllOwnedAccountRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &handler.Handlers{TokenBank: handler.NewTokenBankHandler(nil, nil, nil), Admin: &handler.AdminHandlers{
		Account: &admin.AccountHandler{}, OAuth: &admin.OAuthHandler{}, OpenAIOAuth: &admin.OpenAIOAuthHandler{}, GeminiOAuth: &admin.GeminiOAuthHandler{}, AntigravityOAuth: &admin.AntigravityOAuthHandler{}, GrokOAuth: &admin.GrokOAuthHandler{}, KimiOAuth: &admin.KimiOAuthHandler{}, CNProvider: &admin.CNProviderHandler{},
	}}
	registerOwnedAccountRoutes(r.Group("/user", h.TokenBank.UserGuard), h)
	for _, path := range []string{"/user/accounts", "/user/accounts/data", "/user/accounts/import/codex-session", "/user/accounts/generate-auth-url", "/user/accounts/generate-setup-token-url", "/user/openai/create-from-oauth", "/user/gemini/oauth/auth-url", "/user/kimi/oauth/start", "/user/grok/sso-to-oauth"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		require.Equal(t, 503, w.Code, path)
		require.Contains(t, w.Body.String(), "TOKEN_BANK_DISABLED")
	}
}
