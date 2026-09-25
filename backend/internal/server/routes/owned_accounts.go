package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

// registerOwnedAccountRoutes deliberately lists the allowed operations rather than exposing admin route groups.
func registerOwnedAccountRoutes(user *gin.RouterGroup, h *handler.Handlers) {
	if h.Admin == nil || h.Admin.Account == nil {
		return
	}
	owned := user.Group("")
	owned.Use(h.Admin.Account.OwnedAccountScope(h.Admin.Channel))
	accounts := owned.Group("/accounts")
	accounts.GET("", h.Admin.Account.List)
	accounts.GET("/:id", h.Admin.Account.GetByID)
	accounts.POST("", h.Admin.Account.Create)
	accounts.POST("/batch", h.Admin.Account.BatchCreate)
	accounts.POST("/data", h.Admin.Account.ImportData)
	accounts.POST("/import/codex-session", h.Admin.Account.ImportCodexSession)
	accounts.PUT("/:id", h.Admin.Account.Update)
	accounts.DELETE("/:id", h.Admin.Account.Delete)
	accounts.POST("/:id/test", h.Admin.Account.Test)
	accounts.POST("/:id/refresh", h.Admin.Account.Refresh)
	accounts.POST("/:id/apply-oauth-credentials", h.Admin.Account.ApplyOAuthCredentials)
	accounts.POST("/:id/clear-error", h.Admin.Account.ClearError)
	accounts.POST("/:id/schedulable", h.Admin.Account.SetSchedulable)
	accounts.GET("/:id/stats", h.Admin.Account.GetStats)
	accounts.GET("/:id/usage", h.Admin.Account.GetUsage)
	accounts.GET("/:id/today-stats", h.Admin.Account.GetTodayStats)
	accounts.GET("/:id/models", h.Admin.Account.GetAvailableModels)
	accounts.GET("/:id/ollama-cloud-usage", h.Admin.Account.GetOllamaCloudUsage)
	accounts.POST("/:id/ollama-cloud-usage/refresh", h.Admin.Account.RefreshOllamaCloudUsage)
	accounts.GET("/:id/opencode-go-usage", h.Admin.Account.GetOpenCodeGoUsage)
	accounts.POST("/:id/opencode-go-usage/refresh", h.Admin.Account.RefreshOpenCodeGoUsage)
	accounts.POST("/usage/batch", h.Admin.Account.GetBatchUsage)
	accounts.POST("/today-stats/batch", h.Admin.Account.GetBatchTodayStats)
	if h.Admin.Usage != nil {
		accounts.GET("/:id/usage-logs", h.Admin.Usage.List)
	}
	if h.Admin.OAuth != nil {
		accounts.POST("/generate-auth-url", h.Admin.OAuth.GenerateAuthURL)
		accounts.POST("/generate-setup-token-url", h.Admin.OAuth.GenerateSetupTokenURL)
		accounts.POST("/exchange-code", h.Admin.OAuth.ExchangeCode)
		accounts.POST("/exchange-setup-token-code", h.Admin.OAuth.ExchangeSetupTokenCode)
		accounts.POST("/cookie-auth", h.Admin.OAuth.CookieAuth)
		accounts.POST("/setup-token-cookie-auth", h.Admin.OAuth.SetupTokenCookieAuth)
	}
	if h.Admin.OpenAIOAuth != nil {
		openai := owned.Group("/openai")
		openai.POST("/generate-auth-url", h.Admin.OpenAIOAuth.GenerateAuthURL)
		openai.POST("/exchange-code", h.Admin.OpenAIOAuth.ExchangeCode)
		openai.POST("/refresh-token", h.Admin.OpenAIOAuth.RefreshToken)
		openai.POST("/accounts/:id/refresh", h.Admin.OpenAIOAuth.RefreshAccountToken)
		openai.POST("/create-from-oauth", h.Admin.OpenAIOAuth.CreateAccountFromOAuth)
		openai.POST("/create-from-codex-pat", h.Admin.OpenAIOAuth.CreateAccountFromCodexPAT)
		openai.GET("/accounts/:id/quota", h.Admin.OpenAIOAuth.QueryQuota)
		openai.POST("/accounts/:id/quota/refresh", h.Admin.OpenAIOAuth.RefreshQuota)
	}
	if h.Admin.GeminiOAuth != nil {
		gemini := owned.Group("/gemini")
		gemini.POST("/oauth/auth-url", h.Admin.GeminiOAuth.GenerateAuthURL)
		gemini.POST("/oauth/exchange-code", h.Admin.GeminiOAuth.ExchangeCode)
		gemini.GET("/oauth/capabilities", h.Admin.GeminiOAuth.GetCapabilities)
	}
	if h.Admin.KimiOAuth != nil {
		kimi := owned.Group("/kimi")
		kimi.GET("/oauth/capabilities", h.Admin.KimiOAuth.GetCapabilities)
		kimi.POST("/oauth/start", h.Admin.KimiOAuth.StartDeviceFlow)
		kimi.POST("/oauth/poll", h.Admin.KimiOAuth.PollDeviceFlow)
		kimi.POST("/oauth/refresh", h.Admin.KimiOAuth.RefreshToken)
		kimi.POST("/accounts/:id/refresh", h.Admin.KimiOAuth.RefreshAccountToken)
		kimi.POST("/create-from-oauth", h.Admin.KimiOAuth.CreateAccountFromOAuth)
	}
	if h.Admin.AntigravityOAuth != nil {
		ag := owned.Group("/antigravity")
		ag.POST("/oauth/auth-url", h.Admin.AntigravityOAuth.GenerateAuthURL)
		ag.POST("/oauth/exchange-code", h.Admin.AntigravityOAuth.ExchangeCode)
		ag.POST("/oauth/refresh-token", h.Admin.AntigravityOAuth.RefreshToken)
	}
	if h.Admin.GrokOAuth != nil {
		grok := owned.Group("/grok")
		grok.GET("/oauth/capabilities", h.Admin.GrokOAuth.GetCapabilities)
		grok.POST("/oauth/auth-url", h.Admin.GrokOAuth.GenerateAuthURL)
		grok.POST("/oauth/exchange-code", h.Admin.GrokOAuth.ExchangeCode)
		grok.POST("/oauth/refresh-token", h.Admin.GrokOAuth.RefreshToken)
		grok.POST("/oauth/sso-token", h.Admin.GrokOAuth.ValidateSSOToken)
		grok.POST("/oauth/password", h.Admin.GrokOAuth.AuthorizePassword)
		grok.POST("/oauth/create-from-oauth", h.Admin.GrokOAuth.CreateAccountFromOAuth)
		grok.POST("/sso-to-oauth", h.Admin.GrokOAuth.CreateAccountsFromSSO)
		grok.POST("/accounts/:id/refresh", h.Admin.GrokOAuth.RefreshAccountToken)
		grok.GET("/accounts/:id/quota", h.Admin.GrokOAuth.QueryQuota)
	}
	if h.Admin.CNProvider != nil {
		cn := owned.Group("/cn-providers")
		cn.GET("/accounts/:id/quota", h.Admin.CNProvider.QueryQuota)
		cn.GET("/accounts/:id/balance", h.Admin.CNProvider.QueryBalance)
	}
}
