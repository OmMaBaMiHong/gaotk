//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type rentalPoliciesStub struct {
	TokenBankRepository
	policies []RentalPolicy
}

func (r rentalPoliciesStub) Policies(context.Context) ([]RentalPolicy, error) { return r.policies, nil }

type rentalAccountsStub struct {
	AccountRepository
	account *Account
	groups  []AccountGroup
	updated bool
}

func (r *rentalAccountsStub) CreateWithAccountGroups(_ context.Context, a *Account, groups []AccountGroup) error {
	a.ID = 99
	r.account = a
	r.groups = groups
	return nil
}
func (r *rentalAccountsStub) GetByID(context.Context, int64) (*Account, error) { return r.account, nil }
func (r *rentalAccountsStub) Update(_ context.Context, a *Account) error {
	r.account = a
	r.updated = true
	return nil
}

type rentalRoundTripper func(*http.Request) (*http.Response, error)

func (f rentalRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTokenBankImportOwnsAndVerifiesOfficialPlatform(t *testing.T) {
	ctx := context.Background()
	accounts := &rentalAccountsStub{}
	s := NewTokenBankService(rentalPoliciesStub{policies: []RentalPolicy{{ID: 4, Platform: PlatformDeepseek, GroupID: 8, Enabled: true}}}, accounts, nil, nil, nil)
	requests := 0
	s.client.Transport = rentalRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, "api.deepseek.com", r.URL.Host)
		require.Equal(t, "https", r.URL.Scheme)
		require.Equal(t, "Bearer example-test-key", r.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"deepseek-chat"}]}`)), Header: make(http.Header)}, nil
	})
	id, err := s.Import(ctx, 12, RentalImportInput{Name: "my account", Platform: PlatformDeepseek, APIKey: "example-test-key"})
	require.NoError(t, err)
	require.Equal(t, int64(99), id)
	require.Equal(t, int64(12), *accounts.account.OwnerUserID)
	require.Equal(t, int64(4), *accounts.account.RentalPolicyID)
	require.Equal(t, int64(8), accounts.groups[0].GroupID)
	require.NotContains(t, *accounts.account.RentalIdentity, "example-test-key")
	_, err = s.Import(ctx, 12, RentalImportInput{Name: "wrong", Platform: PlatformOpenAI, APIKey: "example-test-key"})
	require.Error(t, err)
	require.Equal(t, 1, requests)
	s.client.Transport = rentalRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"https://other.invalid"}}}, nil
	})
	_, err = s.Import(ctx, 12, RentalImportInput{Name: "redirect", Platform: PlatformDeepseek, APIKey: "example-test-key"})
	require.Error(t, err)
}

func TestTokenBankReauthorizationRequiresSameOwnerAndIdentity(t *testing.T) {
	ctx := context.Background()
	owner := int64(12)
	identity := "verified-identity"
	accounts := &rentalAccountsStub{account: &Account{ID: 99, Name: "mine", OwnerUserID: &owner, RentalIdentity: &identity, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}
	s := NewTokenBankService(nil, accounts, nil, nil, nil)
	p := &RentalPolicy{ID: 1, Platform: PlatformOpenAI}
	_, err := s.saveVerified(ctx, 13, "name", p, AccountTypeOAuth, map[string]any{}, identity, 99)
	require.ErrorIs(t, err, ErrRentalNotFound)
	require.False(t, accounts.updated)
	_, err = s.saveVerified(ctx, 12, "name", p, AccountTypeOAuth, map[string]any{}, "different-identity", 99)
	require.ErrorIs(t, err, ErrRentalInvalid)
	require.False(t, accounts.updated)
	_, err = s.saveVerified(ctx, 12, "name", p, AccountTypeOAuth, map[string]any{"access_token": "new-test-token"}, identity, 99)
	require.NoError(t, err)
	require.True(t, accounts.updated)
}

func TestTokenBankOAuthSessionOwnerStateExpiryAndOneTime(t *testing.T) {
	m := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: m.Addr()})
	defer rdb.Close()
	s := NewTokenBankService(rentalPoliciesStub{}, nil, nil, nil, rdb)
	ctx := context.Background()
	key := "token-bank:oauth:test-session"
	require.NoError(t, rdb.Set(ctx, key, `{"user_id":12,"state":"expected","platform":"openai"}`, time.Minute).Err())
	_, err := s.FinishOAuth(ctx, 13, RentalOAuthFinish{SessionID: "test-session", Code: "code", State: "expected"})
	require.ErrorIs(t, err, ErrRentalInvalid)
	require.True(t, m.Exists(key))
	_, err = s.FinishOAuth(ctx, 12, RentalOAuthFinish{SessionID: "test-session", Code: "code", State: "wrong"})
	require.ErrorIs(t, err, ErrRentalInvalid)
	require.True(t, m.Exists(key))
	// Correct identity/state consumes once, even if the platform was disabled
	// while the authorization page was open.
	_, err = s.FinishOAuth(ctx, 12, RentalOAuthFinish{SessionID: "test-session", Code: "code", State: "expected"})
	require.ErrorIs(t, err, ErrRentalInvalid)
	require.False(t, m.Exists(key))
	_, err = s.FinishOAuth(ctx, 12, RentalOAuthFinish{SessionID: "test-session", Code: "code", State: "expected"})
	require.ErrorIs(t, err, ErrRentalInvalid)
	require.NoError(t, rdb.Set(ctx, key, `{"user_id":12,"state":"expected"}`, time.Minute).Err())
	m.FastForward(2 * time.Minute)
	_, err = s.FinishOAuth(ctx, 12, RentalOAuthFinish{SessionID: "test-session", Code: "code", State: "expected"})
	require.ErrorIs(t, err, ErrRentalInvalid)
}
