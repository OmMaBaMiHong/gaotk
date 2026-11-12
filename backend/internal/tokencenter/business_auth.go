package tokencenter

// TokenCenter is an OAuth consumer of the existing account service. It neither
// stores passwords nor trusts account IDs or roles supplied by the browser.
import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const businessIdentityKey = "tokencenter.identity"
const accessCookie = "tc_access"
const refreshCookie = "tc_refresh"

type BusinessIdentity struct {
	ID       int64  `json:"id"`
	Issuer   string `json:"-"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func businessIdentity(c *gin.Context) BusinessIdentity {
	v, _ := c.Get(businessIdentityKey)
	u, _ := v.(BusinessIdentity)
	return u
}
func businessActor(c *gin.Context) string {
	u := businessIdentity(c)
	if u.ID > 0 {
		return u.Issuer + ":user:" + strconv.FormatInt(u.ID, 10)
	}
	return "admin"
}

type AccountBridge struct {
	BaseURL, PublicURL, WebURL, ClientID, ClientSecret string
	LegacyAdminToken                                   string
	Client                                             *http.Client
}

func AccountBridgeFromEnv() *AccountBridge {
	value := func(key, fallback string) string {
		if s := strings.TrimRight(os.Getenv(key), "/"); s != "" {
			return s
		}
		return fallback
	}
	base := value("TOKENCENTER_ACCOUNT_URL", "http://127.0.0.1:8080")
	return &AccountBridge{BaseURL: base, PublicURL: value("TOKENCENTER_ACCOUNT_PUBLIC_URL", base), WebURL: value("TOKENCENTER_WEB_URL", "http://127.0.0.1:3020"), ClientID: value("TOKENCENTER_OAUTH_CLIENT_ID", "tokencenter"), ClientSecret: os.Getenv("TOKENCENTER_OAUTH_CLIENT_SECRET"), LegacyAdminToken: os.Getenv("TOKENCENTER_ADMIN_TOKEN"), Client: &http.Client{Timeout: 5 * time.Second}}
}

type accountTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (a *AccountBridge) request(c *gin.Context, method, path, bearer string, input, output any) (int, error) {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return 503, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), method, a.BaseURL+path, body)
	if err != nil {
		return 503, err
	}
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := a.Client.Do(req)
	if err != nil {
		return 503, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return res.StatusCode, fmt.Errorf("account service status %d", res.StatusCode)
	}
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&envelope); err != nil || envelope.Code != 0 {
		return 503, fmt.Errorf("invalid account response")
	}
	if output != nil {
		err = json.Unmarshal(envelope.Data, output)
	}
	return res.StatusCode, err
}
func (a *AccountBridge) cookie(c *gin.Context, name, value string, seconds int) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, value, seconds, "/", "", strings.HasPrefix(a.WebURL, "https://"), true)
}
func (a *AccountBridge) setTokens(c *gin.Context, t accountTokens) {
	a.cookie(c, accessCookie, t.AccessToken, t.ExpiresIn)
	if t.RefreshToken != "" {
		a.cookie(c, refreshCookie, t.RefreshToken, 30*24*60*60)
	}
}
func (a *AccountBridge) sameOrigin(c *gin.Context) bool {
	origin := c.GetHeader("Origin")
	if origin == a.WebURL {
		return true
	}
	// Both existing local entry aliases are used by this product. No wildcard.
	return (a.WebURL == "http://127.0.0.1:3020" || a.WebURL == "http://localhost:3020") && (origin == "http://127.0.0.1:3020" || origin == "http://localhost:3020")
}
func (a *AccountBridge) authenticate(c *gin.Context, admin bool) bool {
	token := ""
	parts := strings.Fields(c.GetHeader("Authorization"))
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		token = parts[1]
	}
	if token == "" {
		token, _ = c.Cookie(accessCookie)
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" && !a.sameOrigin(c) {
			c.AbortWithStatusJSON(403, gin.H{"error": "请从本站页面提交操作"})
			return false
		}
	}
	if token == "" {
		c.AbortWithStatusJSON(401, gin.H{"error": "请先登录统一账号"})
		return false
	}
	var user BusinessIdentity
	status, err := a.request(c, "GET", "/api/v1/auth/me", token, nil, &user)
	if err != nil || user.ID <= 0 {
		if status == 401 || status == 403 {
			c.AbortWithStatusJSON(401, gin.H{"error": "登录已失效，请重新登录"})
		} else {
			c.AbortWithStatusJSON(503, gin.H{"error": "统一账号服务暂时不可用"})
		}
		return false
	}
	if admin && user.Role != "admin" {
		c.AbortWithStatusJSON(403, gin.H{"error": "此操作需要运营管理员账号"})
		return false
	}
	user.Issuer = a.BaseURL
	c.Set(businessIdentityKey, user)
	return true
}
func (a *AccountBridge) MemberAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if a.authenticate(c, false) {
			c.Next()
		}
	}
}
func (a *AccountBridge) AdminAuth(legacyToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Fields(c.GetHeader("Authorization"))
		if legacyToken != "" && len(parts) == 2 && parts[0] == "Bearer" && subtle.ConstantTimeCompare([]byte(parts[1]), []byte(legacyToken)) == 1 {
			c.Next()
			return
		}
		if a.legacySession(c) {
			if c.Request.Method != "GET" && !a.sameOrigin(c) {
				c.AbortWithStatusJSON(403, gin.H{"error": "请从本站页面提交操作"})
				return
			}
			c.Next()
			return
		}
		if a.authenticate(c, true) {
			c.Next()
		}
	}
}
func (a *AccountBridge) Register(public *gin.RouterGroup) {
	group := public.Group("/tokencenter/business/auth")
	group.GET("/start", a.start)
	group.GET("/callback", a.callback)
	group.GET("/session", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if a.legacySession(c) {
			c.JSON(200, BusinessIdentity{Role: "admin", Username: "运营管理令牌"})
			return
		}
		if a.authenticate(c, false) {
			c.JSON(200, businessIdentity(c))
		}
	})
	group.POST("/refresh", a.refresh)
	group.POST("/logout", a.logout)
	group.POST("/operator", a.operator)
}
func (a *AccountBridge) legacySession(c *gin.Context) bool {
	token, _ := c.Cookie("tc_operator")
	return a.LegacyAdminToken != "" && token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(a.LegacyAdminToken)) == 1
}

// Compatibility with the product's existing operator credential. It grants no
// shared-account identity and cannot submit merchant profiles or platform claims.
func (a *AccountBridge) operator(c *gin.Context) {
	if !a.sameOrigin(c) {
		c.JSON(403, gin.H{"error": "请从本站页面提交操作"})
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if !businessBody(c, &in) {
		return
	}
	if a.LegacyAdminToken == "" || subtle.ConstantTimeCompare([]byte(in.Token), []byte(a.LegacyAdminToken)) != 1 {
		c.JSON(401, gin.H{"error": "管理令牌不正确"})
		return
	}
	a.cookie(c, "tc_operator", in.Token, 3600)
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"ok": true})
}
func (a *AccountBridge) start(c *gin.Context) {
	if a.ClientSecret == "" {
		c.JSON(503, gin.H{"error": "统一账号授权应用尚未配置"})
		return
	}
	next := "/merchant"
	if c.Query("next") == "/admin/business" {
		next = "/admin/business"
	}
	// Establish the state cookie on the registered callback host. In particular,
	// localhost and 127.0.0.1 do not share cookies even on the same local port.
	web, _ := url.Parse(a.WebURL)
	if host := c.GetHeader("X-Forwarded-Host"); web != nil && host != "" && !strings.EqualFold(host, web.Host) {
		c.Header("Cache-Control", "no-store")
		c.Redirect(302, a.WebURL+"/api/business/auth/start?next="+url.QueryEscape(next))
		return
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		c.Status(503)
		return
	}
	state := hex.EncodeToString(b)
	a.cookie(c, "tc_oauth_state", state, 600)
	a.cookie(c, "tc_oauth_next", next, 600)
	query := url.Values{"client_id": {a.ClientID}, "redirect_uri": {a.WebURL + "/api/business/auth/callback"}, "state": {state}, "scope": {"profile"}}
	c.Header("Cache-Control", "no-store")
	c.Redirect(302, a.PublicURL+"/oauth/authorize?"+query.Encode())
}
func (a *AccountBridge) callback(c *gin.Context) {
	state, _ := c.Cookie("tc_oauth_state")
	next, _ := c.Cookie("tc_oauth_next")
	if next != "/admin/business" {
		next = "/merchant"
	}
	a.cookie(c, "tc_oauth_state", "", -1)
	a.cookie(c, "tc_oauth_next", "", -1)
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(c.Query("state"))) != 1 || c.Query("code") == "" {
		c.Redirect(303, a.WebURL+next+"?authError=1")
		return
	}
	var t accountTokens
	_, err := a.request(c, "POST", "/api/v1/oauth/token", "", gin.H{"grant_type": "authorization_code", "code": c.Query("code"), "client_id": a.ClientID, "client_secret": a.ClientSecret}, &t)
	if err != nil || t.AccessToken == "" || t.ExpiresIn <= 0 {
		c.Redirect(303, a.WebURL+next+"?authError=1")
		return
	}
	a.setTokens(c, t)
	a.cookie(c, "tc_operator", "", -1)
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Redirect(303, a.WebURL+next)
}
func (a *AccountBridge) refresh(c *gin.Context) {
	if !a.sameOrigin(c) {
		c.AbortWithStatusJSON(403, gin.H{"error": "请从本站页面提交操作"})
		return
	}
	rt, _ := c.Cookie(refreshCookie)
	if rt == "" {
		c.JSON(401, gin.H{"error": "请重新登录"})
		return
	}
	var t accountTokens
	status, err := a.request(c, "POST", "/api/v1/auth/refresh", "", gin.H{"refresh_token": rt}, &t)
	if err != nil || t.AccessToken == "" {
		if status >= 500 {
			c.JSON(503, gin.H{"error": "统一账号服务暂时不可用"})
		} else {
			c.JSON(401, gin.H{"error": "请重新登录"})
		}
		return
	}
	a.setTokens(c, t)
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"ok": true})
}
func (a *AccountBridge) logout(c *gin.Context) {
	if !a.sameOrigin(c) {
		c.AbortWithStatusJSON(403, gin.H{"error": "请从本站页面提交操作"})
		return
	}
	rt, _ := c.Cookie(refreshCookie)
	if rt != "" {
		_, _ = a.request(c, "POST", "/api/v1/auth/logout", "", gin.H{"refresh_token": rt}, nil)
	}
	a.cookie(c, accessCookie, "", -1)
	a.cookie(c, refreshCookie, "", -1)
	a.cookie(c, "tc_operator", "", -1)
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"ok": true})
}
