package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/stretchr/testify/require"
)

func TestClaudeSavingsVerifierAvailable(t *testing.T) {
	_, ok := any(&OAuthService{}).(SavingsAccountVerifier)
	require.True(t, ok, "Claude OAuth must verify personal subscription eligibility")
}

type claudeSavingsClient struct {
	ClaudeOAuthClient
	profile      *ClaudeSubscriptionProfile
	err          error
	token        string
	refreshed    *oauth.TokenResponse
	refreshCalls int
}

func (c *claudeSavingsClient) FetchSubscriptionProfile(_ context.Context, token, proxy string) (*ClaudeSubscriptionProfile, error) {
	c.token = token
	return c.profile, c.err
}

func (c *claudeSavingsClient) RefreshToken(_ context.Context, refresh, proxy string) (*oauth.TokenResponse, error) {
	c.refreshCalls++
	return c.refreshed, c.err
}

func claudeSavingsProfile(t *testing.T, orgType string) *ClaudeSubscriptionProfile {
	t.Helper()
	var profile ClaudeSubscriptionProfile
	require.NoError(t, json.Unmarshal([]byte(`{"account":{"uuid":"account-real","email":"real@example.com","has_claude_max":true},"organization":{"uuid":"org-real","organization_type":"`+orgType+`","rate_limit_tier":"default_claude_max_5x"}}`), &profile))
	return &profile
}

func TestClaudeSavingsVerifierAuthoritativePersonalPlan(t *testing.T) {
	for _, plan := range []string{"pro", "max"} {
		t.Run(plan, func(t *testing.T) {
			client := &claudeSavingsClient{profile: claudeSavingsProfile(t, "claude_"+plan)}
			s := &OAuthService{oauthClient: client}
			input := map[string]any{"access_token": "real-token", "plan_type": "enterprise", "subscription_type": "enterprise", "savings_verified_plan_type": "enterprise", "savings_verified_at": int64(1), "email_address": "spoof@example.com", "email": "spoof@example.com", "base_url": "https://attacker.invalid"}
			got, err := s.VerifySavingsAccount(context.Background(), PlatformAnthropic, AccountTypeOAuth, input)
			require.NoError(t, err)
			require.Equal(t, "real-token", client.token)
			require.Equal(t, plan, got["savings_verified_plan_type"])
			require.Equal(t, plan, got["plan_type"])
			require.Equal(t, plan, got["subscription_type"])
			require.Equal(t, "account-real", got["account_uuid"])
			require.Equal(t, "org-real", got["org_uuid"])
			require.Equal(t, "real@example.com", got["email_address"])
			require.NotContains(t, got, "email")
			require.Greater(t, got["savings_verified_at"].(int64), int64(1))
			require.Equal(t, "enterprise", input["plan_type"])
		})
	}
}

func TestClaudeSavingsVerifierRejectsUntrustedEligibility(t *testing.T) {
	for _, org := range []string{"claude_team", "claude_enterprise", "free", "", "pro", "unknown"} {
		t.Run(org, func(t *testing.T) {
			s := &OAuthService{oauthClient: &claudeSavingsClient{profile: claudeSavingsProfile(t, org)}}
			got, err := s.VerifySavingsAccount(context.Background(), PlatformAnthropic, AccountTypeOAuth, map[string]any{"access_token": "token", "plan_type": "max", "savings_verified_plan_type": "max"})
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
	for _, key := range []string{"org_uuid", "organization_uuid", "organization_id", "account_uuid", "account_id"} {
		t.Run(key+"_mismatch", func(t *testing.T) {
			s := &OAuthService{oauthClient: &claudeSavingsClient{profile: claudeSavingsProfile(t, "claude_pro")}}
			_, err := s.VerifySavingsAccount(context.Background(), PlatformAnthropic, AccountTypeOAuth, map[string]any{"access_token": "token", key: "spoofed"})
			require.Error(t, err)
		})
	}
}

func TestClaudeSavingsVerifierRejectsMissingIdentityAndInvalidToken(t *testing.T) {
	for _, field := range []string{"account", "org", "email", "token_error", "nil_profile"} {
		t.Run(field, func(t *testing.T) {
			client := &claudeSavingsClient{profile: claudeSavingsProfile(t, "claude_max")}
			switch field {
			case "account":
				client.profile.Account.UUID = ""
			case "org":
				client.profile.Organization.UUID = ""
			case "email":
				client.profile.Account.Email = ""
			case "token_error":
				client.err = errors.New("upstream rejected secret-token")
			case "nil_profile":
				client.profile = nil
			}
			s := &OAuthService{oauthClient: client}
			_, err := s.VerifySavingsAccount(context.Background(), PlatformAnthropic, AccountTypeOAuth, map[string]any{"access_token": "secret-token"})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret-token")
		})
	}
}

func TestClaudeSavingsVerifierRefreshOnly(t *testing.T) {
	client := &claudeSavingsClient{profile: claudeSavingsProfile(t, "claude_pro"), refreshed: &oauth.TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 3600, TokenType: "Bearer", Scope: "user:profile user:inference"}}
	s := &OAuthService{oauthClient: client}
	got, err := s.VerifySavingsAccount(context.Background(), PlatformAnthropic, AccountTypeOAuth, map[string]any{"refresh_token": "old-refresh"})
	require.NoError(t, err)
	require.Equal(t, 1, client.refreshCalls)
	require.Equal(t, "new-access", client.token)
	require.Equal(t, "new-refresh", got["refresh_token"])
	require.NotEmpty(t, got["expires_at"])
	client.refreshed = &oauth.TokenResponse{}
	_, err = s.VerifySavingsAccount(context.Background(), PlatformAnthropic, AccountTypeOAuth, map[string]any{"refresh_token": "old-refresh"})
	require.Error(t, err)
}

func TestClaudeSavingsVerifierRequiresOAuthAndConfiguredClient(t *testing.T) {
	client := &claudeSavingsClient{profile: claudeSavingsProfile(t, "claude_pro")}
	s := &OAuthService{oauthClient: client}
	for _, kind := range []string{AccountTypeAPIKey, AccountTypeSetupToken, ""} {
		_, err := s.VerifySavingsAccount(context.Background(), PlatformAnthropic, kind, map[string]any{"access_token": "token"})
		require.Error(t, err)
	}
	require.Empty(t, client.token)
	_, err := s.VerifySavingsAccount(context.Background(), PlatformAnthropic, AccountTypeOAuth, nil)
	require.Error(t, err)
	_, err = (&OAuthService{}).VerifySavingsAccount(context.Background(), PlatformAnthropic, AccountTypeOAuth, map[string]any{"access_token": "token"})
	require.Error(t, err)
}
