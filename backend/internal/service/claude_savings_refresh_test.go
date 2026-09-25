//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/stretchr/testify/require"
)

type claudeSavingsRefreshClient struct {
	*claudeSavingsClient
	profileErr   error
	profileCalls int
}

func (c *claudeSavingsRefreshClient) FetchSubscriptionProfile(ctx context.Context, token, proxy string) (*ClaudeSubscriptionProfile, error) {
	c.profileCalls++
	if c.profileErr != nil {
		return nil, c.profileErr
	}
	return c.claudeSavingsClient.FetchSubscriptionProfile(ctx, token, proxy)
}

func claudeSavingsRefreshAccount() *Account {
	account := savingsScheduleAccount()
	account.Platform = PlatformAnthropic
	account.Rental.Platform = PlatformAnthropic
	account.Credentials["access_token"] = "old-access"
	account.Credentials["refresh_token"] = "old-refresh"
	account.Credentials["expires_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
	account.Credentials["savings_verified_at"] = int64(100)
	account.Credentials["account_uuid"] = "account-real"
	account.Credentials["org_uuid"] = "org-real"
	return account
}

func newClaudeSavingsRefreshClient(t *testing.T, orgType string) *claudeSavingsRefreshClient {
	return &claudeSavingsRefreshClient{claudeSavingsClient: &claudeSavingsClient{
		profile:   claudeSavingsProfile(t, orgType),
		refreshed: &oauth.TokenResponse{AccessToken: "rotated-access", RefreshToken: "rotated-refresh", ExpiresIn: 3600, TokenType: "Bearer", Scope: "user:profile user:inference"},
	}}
}

func TestClaudeSavingsRefreshPreservesRotationAndRechecksPlan(t *testing.T) {
	for _, tc := range []struct {
		name, orgType, wantPlan string
		profileErr              bool
	}{
		{"pro", "claude_pro", "pro", false},
		{"max", "claude_max", "max", false},
		{"team", "claude_team", "", false},
		{"expired_entitlement", "", "", false},
		{"invalid_profile_token", "claude_pro", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newClaudeSavingsRefreshClient(t, tc.orgType)
			if tc.profileErr {
				client.profileErr = errors.New("profile token invalid")
			}
			service := &OAuthService{oauthClient: client}
			credentials, err := NewClaudeTokenRefresher(service).Refresh(context.Background(), claudeSavingsRefreshAccount())
			require.NoError(t, err)
			require.Equal(t, "rotated-access", credentials["access_token"])
			require.Equal(t, "rotated-refresh", credentials["refresh_token"])
			require.Equal(t, tc.wantPlan, credentials["plan_type"])
			require.Equal(t, tc.wantPlan, credentials["subscription_type"])
			require.Equal(t, tc.wantPlan, credentials["savings_verified_plan_type"])
			if tc.wantPlan == "" {
				require.Equal(t, int64(0), credentials["savings_verified_at"])
			} else {
				require.Greater(t, credentials["savings_verified_at"].(int64), int64(100))
			}
			require.Equal(t, 1, client.profileCalls)
		})
	}
}

func TestClaudeSavingsTokenProviderRejectsPlanLostDuringRefresh(t *testing.T) {
	for _, orgType := range []string{"claude_team", "claude_max"} {
		t.Run(orgType, func(t *testing.T) {
			account := claudeSavingsRefreshAccount()
			repo := &refreshAPIAccountRepo{account: account}
			service := &OAuthService{oauthClient: newClaudeSavingsRefreshClient(t, orgType)}
			provider := NewClaudeTokenProvider(repo, nil, service)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), NewClaudeTokenRefresher(service))
			token, err := provider.GetAccessToken(context.Background(), account)
			require.Error(t, err)
			require.Empty(t, token)
			require.Equal(t, "rotated-refresh", repo.account.GetCredential("refresh_token"))
			require.Equal(t, "rotated-access", repo.account.GetCredential("access_token"))
		})
	}
}

func TestClaudeSavingsTokenProviderAcceptsUnchangedVerifiedPlan(t *testing.T) {
	account := claudeSavingsRefreshAccount()
	repo := &refreshAPIAccountRepo{account: account}
	service := &OAuthService{oauthClient: newClaudeSavingsRefreshClient(t, "claude_pro")}
	provider := NewClaudeTokenProvider(repo, nil, service)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), NewClaudeTokenRefresher(service))
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "rotated-access", token)
}

func TestClaudeSavingsRefreshLeavesAdminBehaviorUnchanged(t *testing.T) {
	account := claudeSavingsRefreshAccount()
	account.OwnerUserID = nil
	account.Rental = nil
	client := newClaudeSavingsRefreshClient(t, "claude_team")
	repo := &refreshAPIAccountRepo{account: account}
	service := &OAuthService{oauthClient: client}
	provider := NewClaudeTokenProvider(repo, nil, service)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), NewClaudeTokenRefresher(service))
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "rotated-access", token)
	require.Zero(t, client.profileCalls)
}

func TestClaudeSavingsTokenProviderRejectsCachedTokenAfterPlanLoss(t *testing.T) {
	account := claudeSavingsRefreshAccount()
	latest := claudeSavingsRefreshAccount()
	latest.Credentials["savings_verified_plan_type"] = ""
	cache := &claudeTokenCacheStub{tokens: map[string]string{ClaudeTokenCacheKey(account): "cached-access"}}
	provider := NewClaudeTokenProvider(&refreshAPIAccountRepo{account: latest}, cache, nil)
	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Empty(t, token)
}
