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

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
)

// oauthServerCodeTTL 授权码有效期（一次性 code，无状态签名）。
const oauthServerCodeTTL = 5 * time.Minute

// oauthServerAuthCode 授权码载荷（HMAC-SHA256 签名，无状态；不依赖 Redis）。
type oauthServerAuthCode struct {
	UserID    int64  `json:"user_id"`
	ClientID  string `json:"client_id"`
	ExpiresAt int64  `json:"expires_at"`
	Nonce     string `json:"nonce"`
}

func (h *AuthHandler) oauthServerClient() *config.OAuthServerConfig {
	return &h.cfg.OAuthServer
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
func (h *AuthHandler) OAuthAuthorize(c *gin.Context) {
	clientID := strings.TrimSpace(c.Query("client_id"))
	redirectURI := strings.TrimSpace(c.Query("redirect_uri"))
	state := c.Query("state")

	client := h.oauthServerClient()
	if client.ClientID == "" || clientID != client.ClientID {
		response.Error(c, http.StatusBadRequest, "unknown client_id")
		return
	}
	if client.RedirectURI == "" || redirectURI != client.RedirectURI {
		response.Error(c, http.StatusBadRequest, "redirect_uri not allowed")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		// 未登录：跳官网登录页，登录后回到当前 authorize URL。
		loginURL := "/login?redirect=" + url.QueryEscape(c.Request.URL.String())
		c.Redirect(http.StatusFound, loginURL)
		return
	}

	code, err := h.signOAuthServerCode(oauthServerAuthCode{
		UserID:    subject.UserID,
		ClientID:  clientID,
		ExpiresAt: time.Now().Add(oauthServerCodeTTL).Unix(),
		Nonce:     oauthServerRandomNonce(),
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

	client := h.oauthServerClient()
	if client.ClientID == "" || body.ClientID != client.ClientID || client.ClientSecret == "" || client.ClientSecret != body.ClientSecret {
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
	respondWithTokenPair(c, h.authService, user)
}
