//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestSavingsRefreshRechecksUpstreamAndInvalidatesMissingPlan(t *testing.T) {
	for _, plan := range []string{"pro", "prolite", "free", ""} {
		t.Run(plan, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"account_id":"acct-1","plan_type":%q}`, plan)
			}))
			defer server.Close()
			svc := &OpenAIOAuthService{privacyClientFactory: func(string) (*req.Client, error) {
				return req.C().OnBeforeRequest(func(_ *req.Client, r *req.Request) error { r.SetURL(server.URL); return nil }), nil
			}}
			account := savingsScheduleAccount()
			account.Credentials["access_token"] = "existing-access-token"
			account.Credentials["chatgpt_account_id"] = "acct-1"
			account.Credentials["savings_verified_at"] = int64(10)
			credentials, err := NewOpenAITokenRefresher(svc, nil).Refresh(context.Background(), account)
			require.NoError(t, err)
			expected := plan
			if plan == "free" {
				expected = ""
			}
			require.Equal(t, expected, credentials["plan_type"])
			require.Equal(t, expected, credentials["savings_verified_plan_type"])
			if expected == "" {
				require.Equal(t, int64(0), credentials["savings_verified_at"])
			}
			require.Equal(t, "existing-access-token", credentials["access_token"])
		})
	}
}

func TestSavingsTokenProviderRejectsPlanChangedDuringRefresh(t *testing.T) {
	account := savingsScheduleAccount()
	account.Credentials["access_token"] = "old-token"
	account.Credentials["refresh_token"] = "old-refresh"
	account.Credentials["expires_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
	repo := &refreshAPIAccountRepo{account: account}
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{
		"access_token": "new-token", "refresh_token": "rotated-refresh", "plan_type": "prolite", "savings_verified_plan_type": "prolite", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
	}}
	provider := NewOpenAITokenProvider(repo, nil, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), executor)
	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Empty(t, token)
	require.Equal(t, "rotated-refresh", repo.account.GetCredential("refresh_token"), "rotation persists even when the current request loses plan eligibility")
}

func TestSavingsTokenProviderRejectsShadowParentPlanChangedBeforeTokenReturn(t *testing.T) {
	account := savingsScheduleAccount()
	parentID := int64(20)
	account.ParentAccountID = &parentID
	account.Credentials = map[string]any{"access_token": "shadow-token"}
	latest := savingsScheduleAccount()
	latest.ParentAccountID = &parentID
	latest.Credentials = map[string]any{"access_token": "shadow-token"}
	latest.Rental.OwnerVerifiedPlan = "prolite"
	latest.Rental.OwnerCurrentPlan = "prolite"
	provider := NewOpenAITokenProvider(&refreshAPIAccountRepo{account: latest}, nil, nil)
	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Empty(t, token)
}

func TestSavingsGatewayRejectsShadowParentDowngradeDuringCredentialResolution(t *testing.T) {
	parent := savingsScheduleAccount()
	parent.Credentials["access_token"] = "parent-token"
	parent.Credentials["plan_type"] = "prolite"
	parent.Credentials["savings_verified_plan_type"] = "prolite"
	parent.Rental.OwnerCurrentPlan = "prolite"
	parent.Rental.OwnerVerifiedPlan = "prolite"
	shadow := savingsScheduleAccount()
	shadow.ID = 30
	shadow.ParentAccountID = &parent.ID
	shadow.QuotaDimension = "spark"
	shadow.Credentials = map[string]any{}
	svc := &OpenAIGatewayService{accountRepo: &stubOpenAIAccountRepo{accounts: []Account{*parent}}}
	token, _, err := svc.GetAccessToken(context.Background(), shadow)
	require.Error(t, err)
	require.Empty(t, token)
}
