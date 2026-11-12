package tokencenter

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBusinessAccountBridgeUsesVerifiedIdentityAndRoles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/me" {
			t.Error(r.URL.Path)
		}
		role := "user"
		if r.Header.Get("Authorization") == "Bearer admin" {
			role = "admin"
		} else if r.Header.Get("Authorization") != "Bearer member" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(gin.H{"code": 0, "data": gin.H{"id": 42, "email": "verified@example.test", "role": role}})
	}))
	defer upstream.Close()
	a := &AccountBridge{BaseURL: upstream.URL, WebURL: "https://tc.example", Client: upstream.Client(), LegacyAdminToken: "legacy-operator"}
	r := gin.New()
	a.Register(r.Group("/api/v1"))
	r.GET("/private", a.MemberAuth(), func(c *gin.Context) { c.JSON(200, businessIdentity(c)) })
	r.POST("/private", a.MemberAuth(), func(c *gin.Context) { c.JSON(200, businessIdentity(c)) })
	r.GET("/admin", a.AdminAuth("legacy-operator"), func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.POST("/admin", a.AdminAuth("legacy-operator"), func(c *gin.Context) { c.Status(200) })
	for _, tc := range []struct {
		method, path, bearer, cookie, origin string
		want                                 int
	}{
		{"GET", "/private", "", "", "", 401}, {"GET", "/private", "forged", "", "", 401}, {"GET", "/private", "member", "", "", 200},
		{"GET", "/admin", "member", "", "", 403}, {"GET", "/admin", "admin", "", "", 200}, {"GET", "/admin", "legacy-operator", "", "", 200},
		{"POST", "/private", "", "tc_access=member", "https://other.example", 403}, {"POST", "/private", "", "tc_access=member", "https://tc.example", 200},
		{"POST", "/admin", "", "tc_operator=legacy-operator", "https://other.example", 403}, {"GET", "/private", "", "tc_operator=legacy-operator", "", 401},
	} {
		t.Run(tc.method+tc.path+tc.bearer+tc.origin, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			req.Header.Set("Cookie", tc.cookie)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-User-Id", "999")
			out := httptest.NewRecorder()
			r.ServeHTTP(out, req)
			if out.Code != tc.want {
				t.Fatal(out.Code, out.Body)
			}
			if out.Code == 200 && tc.path == "/private" && !strings.Contains(out.Body.String(), `"id":42`) {
				t.Fatal("unverified identity", out.Body)
			}
		})
	}
}
func TestBusinessOAuthCallbackBindsStateAndKeepsTokensHttpOnly(t *testing.T) {
	exchanges := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/oauth/token" {
			t.Fatal(r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["client_id"] != "tokencenter" || body["client_secret"] != "server-secret" || body["code"] != "once" {
			t.Error("bad exchange")
		}
		exchanges++
		json.NewEncoder(w).Encode(gin.H{"code": 0, "data": gin.H{"access_token": "access-only", "refresh_token": "refresh-only", "expires_in": 3600}})
	}))
	defer upstream.Close()
	a := &AccountBridge{BaseURL: upstream.URL, PublicURL: "https://accounts.example", WebURL: "https://tc.example", ClientID: "tokencenter", ClientSecret: "server-secret", Client: upstream.Client()}
	r := gin.New()
	a.Register(r.Group("/api/v1"))
	alias := httptest.NewRecorder()
	aliasRequest := httptest.NewRequest("GET", "/api/v1/tokencenter/business/auth/start?next=/admin/business", nil)
	aliasRequest.Header.Set("X-Forwarded-Host", "localhost:3020")
	r.ServeHTTP(alias, aliasRequest)
	if alias.Code != 302 || alias.Header().Get("Location") != "https://tc.example/api/business/auth/start?next=%2Fadmin%2Fbusiness" || len(alias.Result().Cookies()) != 0 {
		t.Fatal("OAuth state was set on the wrong callback host", alias.Code, alias.Header().Get("Location"))
	}
	start := httptest.NewRecorder()
	r.ServeHTTP(start, httptest.NewRequest("GET", "/api/v1/tokencenter/business/auth/start?next=/admin/business", nil))
	location, _ := url.Parse(start.Header().Get("Location"))
	if location.Host != "accounts.example" || location.Query().Get("scope") != "profile" || strings.Contains(location.String(), "server-secret") {
		t.Fatal(location)
	}
	cookies := start.Result().Cookies()
	bad := httptest.NewRequest("GET", "/api/v1/tokencenter/business/auth/callback?state=forged&code=once", nil)
	for _, c := range cookies {
		bad.AddCookie(c)
	}
	out := httptest.NewRecorder()
	r.ServeHTTP(out, bad)
	if exchanges != 0 || !strings.Contains(out.Header().Get("Location"), "authError=1") {
		t.Fatal("state forgery accepted")
	}
	req := httptest.NewRequest("GET", "/api/v1/tokencenter/business/auth/callback?state="+location.Query().Get("state")+"&code=once", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	out = httptest.NewRecorder()
	r.ServeHTTP(out, req)
	if exchanges != 1 || out.Code != 303 || out.Header().Get("Location") != "https://tc.example/admin/business" {
		t.Fatal(out.Code, out.Header())
	}
	n := 0
	for _, c := range out.Result().Cookies() {
		if c.Name == accessCookie || c.Name == refreshCookie {
			n++
			if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
				t.Fatal("unsafe cookie", c.Name)
			}
		}
	}
	if n != 2 {
		t.Fatal("tokens missing")
	}
	if bytes.Contains(out.Body.Bytes(), []byte("access-only")) {
		t.Fatal("token leaked to page")
	}
}
