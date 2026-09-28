package domain

import (
	"errors"
	"testing"
)

func TestNormalizeOAuthScopes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"逗号+空格+大小写混合", "Profile, membership ,PROFILE", []string{"profile", "membership"}},
		{"换行分隔", "profile\nmembership", []string{"profile", "membership"}},
		{"去重保序", "membership,profile,membership", []string{"membership", "profile"}},
		{"空串", "  ", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeOAuthScopes(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("NormalizeOAuthScopes(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("NormalizeOAuthScopes(%q) = %v, want %v", tc.raw, got, tc.want)
				}
			}
		})
	}
}

func TestResolveRequestedScopes(t *testing.T) {
	legacy := &OAuthClientApp{ClientID: "legacy"} // 未登记 scope = 传统模式
	scoped := &OAuthClientApp{
		ClientID:      "app",
		AllowedScopes: []string{OAuthScopeProfile, OAuthScopeMembership},
	}

	t.Run("传统模式忽略请求参数", func(t *testing.T) {
		got, err := legacy.ResolveRequestedScopes("profile")
		if err != nil || got != nil {
			t.Fatalf("legacy ResolveRequestedScopes = %v, %v; want nil, nil", got, err)
		}
	})

	t.Run("受限模式缺省授予全部登记 scope", func(t *testing.T) {
		got, err := scoped.ResolveRequestedScopes("")
		if err != nil || len(got) != 2 {
			t.Fatalf("default scopes = %v, %v; want 2 scopes, nil", got, err)
		}
	})

	t.Run("子集放行", func(t *testing.T) {
		got, err := scoped.ResolveRequestedScopes("profile")
		if err != nil || len(got) != 1 || got[0] != OAuthScopeProfile {
			t.Fatalf("subset = %v, %v; want [profile], nil", got, err)
		}
	})

	t.Run("超出白名单拒绝", func(t *testing.T) {
		if _, err := scoped.ResolveRequestedScopes("profile admin"); !errors.Is(err, ErrOAuthClientScopeInvalid) {
			t.Fatalf("want ErrOAuthClientScopeInvalid, got %v", err)
		}
	})

	t.Run("未知 scope 拒绝", func(t *testing.T) {
		if _, err := scoped.ResolveRequestedScopes("email"); !errors.Is(err, ErrOAuthClientScopeInvalid) {
			t.Fatalf("want ErrOAuthClientScopeInvalid, got %v", err)
		}
	})
}

func TestValidateAllowedScopes(t *testing.T) {
	if err := ValidateAllowedScopes([]string{OAuthScopeProfile, OAuthScopeMembership}); err != nil {
		t.Fatalf("known scopes should pass, got %v", err)
	}
	if err := ValidateAllowedScopes([]string{OAuthScopeProfile, "wallet"}); !errors.Is(err, ErrOAuthClientScopeInvalid) {
		t.Fatalf("unknown scope should be rejected, got %v", err)
	}
}
