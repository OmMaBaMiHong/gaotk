package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestSavingsVerificationUsesUpstreamUsageNotClientPlan(t *testing.T) {
	for _, plan := range []string{"free", "plus", "pro", "prolite", "chatgpt_pro"} {
		t.Run(plan, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer real-access", r.Header.Get("Authorization"))
				require.Equal(t, "actual-account", r.Header.Get("ChatGPT-Account-ID"))
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"plan_type":%q,"account_id":"actual-account","user_id":"actual-user"}`, plan)
			}))
			defer server.Close()
			s := &OpenAIOAuthService{privacyClientFactory: func(proxy string) (*req.Client, error) {
				require.Empty(t, proxy)
				return req.C().OnBeforeRequest(func(_ *req.Client, r *req.Request) error {
					require.Equal(t, chatGPTUsageURL, r.RawURL)
					r.SetURL(server.URL)
					return nil
				}), nil
			}}
			input := map[string]any{"access_token": "real-access", "chatgpt_account_id": "actual-account", "plan_type": "pro", "savings_verified_plan_type": "pro", "base_url": "https://attacker.invalid", "id_token": "unverified.jwt.payload"}
			got, err := s.VerifySavingsAccount(context.Background(), PlatformOpenAI, AccountTypeOAuth, input)
			require.NoError(t, err)
			require.Equal(t, NormalizeSavingsPlan(plan), got["plan_type"])
			require.Equal(t, NormalizeSavingsPlan(plan), got["savings_verified_plan_type"])
			require.NotZero(t, got["savings_verified_at"])
			require.Equal(t, "pro", input["plan_type"], "verification must not mutate caller input on failure or success")
		})
	}
}

type savingsOAuthClient struct {
	OpenAIOAuthClient
	response *openai.TokenResponse
}

func (c savingsOAuthClient) RefreshTokenWithClientID(_ context.Context, refresh, proxy, clientID string) (*openai.TokenResponse, error) {
	return c.response, nil
}

func TestSavingsVerificationRefreshPersistsRotationAndIgnoresSubmittedJWT(t *testing.T) {
	payload := fmt.Sprintf(`{"exp":%d,"https://api.openai.com/auth":{"chatgpt_plan_type":"plus","chatgpt_account_id":"provider-account"}}`, time.Now().Add(time.Hour).Unix())
	providerJWT := "header." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".signature"
	s := &OpenAIOAuthService{oauthClient: savingsOAuthClient{response: &openai.TokenResponse{AccessToken: "rotated-access", RefreshToken: "rotated-refresh", IDToken: providerJWT, ExpiresIn: 3600}}}
	got, err := s.VerifySavingsAccount(context.Background(), PlatformOpenAI, AccountTypeOAuth, map[string]any{"refresh_token": "old-refresh", "id_token": "forged-pro", "plan_type": "pro", "chatgpt_account_id": "forged-account"})
	require.NoError(t, err)
	require.Equal(t, "plus", got["savings_verified_plan_type"])
	require.Equal(t, "rotated-access", got["access_token"])
	require.Equal(t, "rotated-refresh", got["refresh_token"])
	require.Equal(t, "provider-account", got["chatgpt_account_id"])
}

func TestSavingsVerificationRefreshCannotUseWorkspaceEnrichmentForPersonalPlan(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// This valid workspace response must never establish the personal tier.
		_, _ = w.Write([]byte(`{"accounts":{"workspace":{"account":{"plan_type":"pro","is_default":true}}}}`))
	}))
	defer server.Close()
	s := &OpenAIOAuthService{oauthClient: savingsOAuthClient{response: &openai.TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh"}}, privacyClientFactory: func(string) (*req.Client, error) {
		return req.C().OnBeforeRequest(func(_ *req.Client, r *req.Request) error { r.SetURL(server.URL); return nil }), nil
	}}
	got, err := s.VerifySavingsAccount(context.Background(), PlatformOpenAI, AccountTypeOAuth, map[string]any{"refresh_token": "refresh", "plan_type": "pro"})
	require.Error(t, err)
	require.Nil(t, got)
}

func TestSavingsVerificationFailsClosedOnUnknownOrMismatchedUpstream(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"unknown", `{"plan_type":"future-pro","account_id":"account"}`, 200},
		{"missing", `{"account_id":"account"}`, 200},
		{"other account", `{"plan_type":"pro","account_id":"other"}`, 200},
		{"rejected", `{"plan_type":"pro","account_id":"account"}`, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			s := &OpenAIOAuthService{privacyClientFactory: func(string) (*req.Client, error) {
				return req.C().OnBeforeRequest(func(_ *req.Client, r *req.Request) error { r.SetURL(server.URL); return nil }), nil
			}}
			got, err := s.VerifySavingsAccount(context.Background(), PlatformOpenAI, AccountTypeOAuth, map[string]any{"access_token": "access", "chatgpt_account_id": "account", "plan_type": "pro"})
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
}

func TestSavingsVerificationNonOAuthCannotCarryTrustedPlan(t *testing.T) {
	s := &OpenAIOAuthService{}
	got, err := s.VerifySavingsAccount(context.Background(), PlatformOpenAI, AccountTypeAPIKey, map[string]any{"api_key": "key", "plan_type": "pro", "savings_verified_plan_type": "pro", "savings_verified_at": 123})
	require.NoError(t, err)
	require.NotContains(t, got, "plan_type")
	require.NotContains(t, got, "savings_verified_plan_type")
	require.NotContains(t, got, "savings_verified_at")
}

func TestSavingsVerificationPATUsesWhoamiAndClearsOAuthCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer at-real-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"actual@example.com","chatgpt_user_id":"actual-user","chatgpt_account_id":"actual-account","chatgpt_plan_type":"prolite","chatgpt_account_is_fedramp":false}`))
	}))
	defer server.Close()
	old := openAICodexPATWhoamiURL
	openAICodexPATWhoamiURL = server.URL
	t.Cleanup(func() { openAICodexPATWhoamiURL = old })
	s := &OpenAIOAuthService{}
	got, err := s.VerifySavingsAccount(context.Background(), PlatformOpenAI, AccountTypeOAuth, map[string]any{"access_token": "at-real-token", "refresh_token": "old-refresh", "id_token": "forged-pro", "plan_type": "pro"})
	require.NoError(t, err)
	require.Equal(t, "prolite", got["savings_verified_plan_type"])
	require.Equal(t, "actual-account", got["chatgpt_account_id"])
	require.NotContains(t, got, "refresh_token")
	require.NotContains(t, got, "id_token")
}

func TestSavingsVerificationCannotTrustJWTWithoutUpstreamCredentials(t *testing.T) {
	s := &OpenAIOAuthService{}
	got, err := s.VerifySavingsAccount(context.Background(), PlatformOpenAI, AccountTypeOAuth, map[string]any{"id_token": "forged.jwt.signature", "plan_type": "pro"})
	require.Error(t, err)
	require.Nil(t, got)
}
