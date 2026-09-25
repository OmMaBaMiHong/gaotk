package repository

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestClaudeSubscriptionProfileFixedAuthority(t *testing.T) {
	client := &claudeOAuthService{baseURL: "https://attacker.invalid", tokenURL: "https://attacker.invalid"}
	client.clientFactory = func(proxy string) (*req.Client, error) {
		require.Empty(t, proxy)
		return newTestReqClient(newInProcessTransport(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "https://api.anthropic.com/api/oauth/profile", r.URL.String())
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "Bearer test-access", r.Header.Get("Authorization"))
			require.Equal(t, "no-cache", r.Header.Get("Cache-Control"))
			deadline, ok := r.Context().Deadline()
			require.True(t, ok)
			require.LessOrEqual(t, time.Until(deadline), 10*time.Second)
			_, _ = io.WriteString(w, `{"account":{"uuid":"account","email":"test@example.com"},"organization":{"uuid":"org","organization_type":"claude_pro"}}`)
		}, nil)), nil
	}
	profile, err := client.FetchSubscriptionProfile(context.Background(), "test-access", "")
	require.NoError(t, err)
	require.Equal(t, "claude_pro", profile.Organization.OrganizationType)
	require.Equal(t, "account", profile.Account.UUID)
}

func TestClaudeSubscriptionProfileRejectsRedirectAndInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		location string
	}{
		{"cross_host_redirect", 302, "", "https://attacker.invalid/profile"},
		{"same_host_redirect", 302, "", "https://api.anthropic.com/other"},
		{"unauthorized", 401, "secret-token", ""},
		{"forbidden", 403, "secret-token", ""},
		{"rate_limited", 429, "secret-token", ""},
		{"invalid_json", 200, "not json", ""},
		{"oversized", 200, strings.Repeat(" ", claudeSubscriptionProfileMaxBytes+1), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &claudeOAuthService{clientFactory: func(string) (*req.Client, error) {
				return newTestReqClient(newInProcessTransport(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if tc.location != "" {
						w.Header().Set("Location", tc.location)
					}
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.body)
				}, nil)), nil
			}}
			profile, err := client.FetchSubscriptionProfile(context.Background(), "secret-token", "")
			require.Error(t, err)
			require.Nil(t, profile)
			require.Equal(t, 1, calls)
			require.NotContains(t, err.Error(), "secret-token")
		})
	}
}
