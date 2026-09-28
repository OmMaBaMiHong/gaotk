//go:build unit

package middleware

import (
	"net/http"
	"testing"
)

func TestOAuthScopeAllowsEndpoint(t *testing.T) {
	cases := []struct {
		name   string
		scopes string
		method string
		path   string
		want   bool
	}{
		{"profile 放行 /auth/me", "profile", http.MethodGet, "/api/v1/auth/me", true},
		{"profile 拒绝面板 API", "profile", http.MethodGet, "/api/v1/keys", false},
		{"profile 拒绝改资料", "profile", http.MethodPut, "/api/v1/user", false},
		{"membership 放行订阅只读", "membership", http.MethodGet, "/api/v1/subscriptions/active", true},
		{"membership 拒绝写操作", "membership", http.MethodPost, "/api/v1/subscriptions", false},
		{"membership 拒绝兑换", "membership", http.MethodPost, "/api/v1/redeem", false},
		{"多 scope 取并集", "profile,membership", http.MethodGet, "/api/v1/subscriptions/summary", true},
		{"多 scope 仍拒绝面板", "profile,membership", http.MethodDelete, "/api/v1/keys/1", false},
		{"未知 scope 一律拒绝（fail closed）", "wallet", http.MethodGet, "/api/v1/auth/me", false},
		{"FullPath 缺省按 /auth/me", "profile", http.MethodGet, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := oauthScopeAllowsEndpoint(tc.scopes, tc.method, tc.path)
			if got != tc.want {
				t.Fatalf("oauthScopeAllowsEndpoint(%q, %s, %q) = %v, want %v", tc.scopes, tc.method, tc.path, got, tc.want)
			}
		})
	}
}
