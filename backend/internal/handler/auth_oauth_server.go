package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// oauthServerCodeTTL 授权码有效期（一次性 code，无状态签名）。
const oauthServerCodeTTL = 5 * time.Minute

// oauthServerAuthCode 授权码载荷（HMAC-SHA256 签名，无状态；不依赖 Redis）。
// Scopes 非空 = 受限模式授权，换 token 时按此签发受限令牌；
// Nonce 兼作 jti，受限模式换 token 时用 Redis 原子占坑做一次性消费（防重放）。
type oauthServerAuthCode struct {
	UserID    int64    `json:"user_id"`
	ClientID  string   `json:"client_id"`
	ExpiresAt int64    `json:"expires_at"`
	Nonce     string   `json:"nonce"`
	Scopes    []string `json:"scopes,omitempty"`
}

func (h *AuthHandler) signOAuthServerCode(payload oauthServerAuthCode) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(h.cfg.JWT.Secret))
	mac.Write(data)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (h *AuthHandler) verifyOAuthServerCode(code string) (*oauthServerAuthCode, bool) {
	parts := strings.Split(code, ".")
	if len(parts) != 2 {
		return nil, false
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, []byte(h.cfg.JWT.Secret))
	mac.Write(data)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, false
	}
	var payload oauthServerAuthCode
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, false
	}
	if payload.ExpiresAt < time.Now().Unix() {
		return nil, false
	}
	return &payload, true
}

func oauthServerRandomNonce() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}

// OAuthAuthorize 官网作为 OAuth Server 的授权端点（无 consent，first-party）。
// GET /api/v1/oauth/authorize?client_id=skoob&redirect_uri=...&state=...
// 未登录 → 重定向官网登录页；已登录 → 签发一次性签名 code 重定向回 redirect_uri。
// OAuthAuthorizeWithAuth 先做认证(有效→设 subject;无效→清 cookie 匿名),再走 OAuthAuthorize。
func (h *AuthHandler) OAuthAuthorizeWithAuth(c *gin.Context) {
	h.oauthTryAuth(c)
	h.OAuthAuthorize(c)
}

// oauthTryAuth 尝试从 Authorization header 验证 token(有效设 subject,无效清 cookie)。
func (h *AuthHandler) oauthTryAuth(c *gin.Context) {
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return
	}
	tokenString := strings.TrimSpace(parts[1])
	if tokenString == "" {
		return
	}
	claims, err := h.authService.ValidateToken(tokenString)
	if err != nil {
		c.SetCookie("auth_token", "", -1, "/", "", false, true)
		c.SetCookie("access_token", "", -1, "/", "", false, true)
		return
	}
	// OAuth 受限令牌不能驱动 authorize：否则一个只带 profile scope 的令牌
	// 就能在这里换出任意（甚至全量的）新 code，构成越权升级通道。
	// 受限会话续期一律走 refresh token。
	if claims.Scope != "" {
		c.SetCookie("auth_token", "", -1, "/", "", false, true)
		c.SetCookie("access_token", "", -1, "/", "", false, true)
		return
	}
	user, err := h.userService.GetByID(c.Request.Context(), claims.UserID)
	if err != nil || !user.IsActive() || claims.TokenVersion != user.TokenVersion {
		c.SetCookie("auth_token", "", -1, "/", "", false, true)
		c.SetCookie("access_token", "", -1, "/", "", false, true)
		return
	}
	c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: user.ID, Concurrency: user.Concurrency})
}

func (h *AuthHandler) OAuthAuthorize(c *gin.Context) {
	clientID := strings.TrimSpace(c.Query("client_id"))
	redirectURI := strings.TrimSpace(c.Query("redirect_uri"))
	state := c.Query("state")
	scope := c.Query("scope")

	app, err := h.oauthClientAppService.GetEnabledByClientID(c.Request.Context(), clientID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !app.IsAllowedRedirectURI(redirectURI) {
		response.Error(c, http.StatusBadRequest, "redirect_uri not allowed")
		return
	}
	scopes, err := app.ResolveRequestedScopes(scope)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		// 无 token 或无效 token：清除可能残留的无效凭证 cookie，让浏览器干净地重新登录。
		c.SetCookie("auth_token", "", -1, "/", "", false, true)
		c.SetCookie("access_token", "", -1, "/", "", false, true)
		// 跳登录页，登录后回到当前 authorize URL。
		loginURL := "/login?redirect=" + url.QueryEscape(c.Request.URL.String())
		c.Redirect(http.StatusFound, loginURL)
		return
	}

	code, err := h.signOAuthServerCode(oauthServerAuthCode{
		UserID:    subject.UserID,
		ClientID:  clientID,
		ExpiresAt: time.Now().Add(oauthServerCodeTTL).Unix(),
		Nonce:     oauthServerRandomNonce(),
		Scopes:    scopes,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to sign authorization code")
		return
	}

	sep := "?"
	if strings.Contains(redirectURI, "?") {
		sep = "&"
	}
	c.Redirect(http.StatusFound, redirectURI+sep+"code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state))
}

// oauthAuthorizeJSONRequest 前端授权页通过 XHR 调用 authorize 的请求体。
// 页面携带 Authorization: Bearer <JWT>,后端返回 JSON 的 redirectUrl。
type oauthAuthorizeJSONRequest struct {
	ClientID    string `json:"client_id"`
	RedirectURI string `json:"redirect_uri"`
	State       string `json:"state"`
	Scope       string `json:"scope"`
}

// OAuthAuthorizeJSON 授权页 JSON 端点：已登录用户点击「授权并登录」后调用。
// POST /api/v1/oauth/authorize
// 返回 { code: 0, data: { redirectUrl: "https://..." } }。
func (h *AuthHandler) OAuthAuthorizeJSON(c *gin.Context) {
	var body oauthAuthorizeJSONRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	clientID := strings.TrimSpace(body.ClientID)
	redirectURI := strings.TrimSpace(body.RedirectURI)
	state := body.State

	app, err := h.oauthClientAppService.GetEnabledByClientID(c.Request.Context(), clientID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !app.IsAllowedRedirectURI(redirectURI) {
		response.Error(c, http.StatusBadRequest, "redirect_uri not allowed")
		return
	}
	scopes, err := app.ResolveRequestedScopes(body.Scope)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, "not authenticated")
		return
	}

	code, err := h.signOAuthServerCode(oauthServerAuthCode{
		UserID:    subject.UserID,
		ClientID:  clientID,
		ExpiresAt: time.Now().Add(oauthServerCodeTTL).Unix(),
		Nonce:     oauthServerRandomNonce(),
		Scopes:    scopes,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to sign authorization code")
		return
	}

	sep := "?"
	if strings.Contains(redirectURI, "?") {
		sep = "&"
	}
	redirectURL := redirectURI + sep + "code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	response.Success(c, gin.H{"redirectUrl": redirectURL, "appName": app.Name})
}

// oauthTokenRequest OAuth token 交换请求体。
type oauthTokenRequest struct {
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// OAuthToken 官网 OAuth Server 的 token 端点：授权码换 access token。
// POST /api/v1/oauth/token
func (h *AuthHandler) OAuthToken(c *gin.Context) {
	var body oauthTokenRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.GrantType != "" && body.GrantType != "authorization_code" {
		response.Error(c, http.StatusBadRequest, "unsupported grant_type")
		return
	}

	app, err := h.oauthClientAppService.GetByClientID(c.Request.Context(), body.ClientID)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, "invalid client credentials")
		return
	}
	if !app.Enabled || app.ClientSecret != body.ClientSecret {
		response.Error(c, http.StatusUnauthorized, "invalid client credentials")
		return
	}

	payload, ok := h.verifyOAuthServerCode(body.Code)
	if !ok {
		response.Error(c, http.StatusBadRequest, "invalid or expired authorization code")
		return
	}
	if payload.ClientID != body.ClientID {
		response.Error(c, http.StatusBadRequest, "code/client mismatch")
		return
	}

	user, err := h.userService.GetByID(c.Request.Context(), payload.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	// 受限模式：一次性消费授权码（nonce 兼作 jti）后签发受限令牌对。
	// 传统模式保持原行为不动——存量客户端零影响；一次性消费随受限模式一并落地。
	if len(payload.Scopes) > 0 {
		if err := h.authService.ConsumeOAuthCodeOnce(c.Request.Context(), payload.Nonce); err != nil {
			response.ErrorFrom(c, err)
			return
		}
		respondWithScopedTokenPair(c, h.authService, user, payload.Scopes)
		return
	}
	respondWithTokenPair(c, h.authService, user)
}

// respondWithScopedTokenPair 受限模式令牌响应：access/refresh 均绑定 scope，
// user 只回身份字段（id/email/username/头像/注册来源），不回余额、配额等面板字段。
func respondWithScopedTokenPair(c *gin.Context, authService *service.AuthService, user *service.User, scopes []string) {
	if err := ensureLoginUserActive(user); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	tokenPair, err := authService.GenerateScopedTokenPair(c.Request.Context(), user, "", scopes)
	if err != nil {
		response.InternalError(c, "Failed to generate token")
		return
	}
	response.Success(c, gin.H{
		"access_token":  tokenPair.AccessToken,
		"refresh_token": tokenPair.RefreshToken,
		"expires_in":    tokenPair.ExpiresIn,
		"token_type":    "Bearer",
		"scope":         strings.Join(scopes, " "),
		"user": gin.H{
			"id":            user.ID,
			"email":         user.Email,
			"username":      user.Username,
			"avatar_url":    user.AvatarURL,
			"signup_source": user.SignupSource,
		},
	})
}
