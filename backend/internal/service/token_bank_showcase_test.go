//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type showcaseSettings struct {
	SettingRepository
	value string
	err   error
}

func (s *showcaseSettings) GetValue(context.Context, string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if s.value == "" {
		return "", ErrSettingNotFound
	}
	return s.value, nil
}
func (s *showcaseSettings) Set(_ context.Context, key, value string) error {
	if key != "token_bank_showcase_enabled" {
		panic("unexpected setting key")
	}
	if s.err != nil {
		return s.err
	}
	s.value = value
	return nil
}

type showcaseBank struct {
	TokenBankRepository
	calls int
}

func (b *showcaseBank) Showcase(context.Context) (*TokenBankShowcase, error) {
	b.calls++
	return &TokenBankShowcase{
		Leaderboard: []TokenBankLeader{{Rank: 1, Name: "王小明", Amount: 1.25}, {Rank: 2, Name: "", Amount: 0.5}},
		Recent:      []TokenBankRecent{{Name: "alice@example.com", Platform: "deepseek", Amount: 0.25, CreatedAt: time.Now()}},
	}, nil
}
func TestTokenBankShowcaseDefaultClosedAndImmediateToggle(t *testing.T) {
	ctx := context.Background()
	settings, bank := &showcaseSettings{}, &showcaseBank{}
	s := NewTokenBankShowcaseService(bank, settings)
	result, err := s.Get(ctx)
	require.NoError(t, err)
	require.False(t, result.Enabled)
	require.NotNil(t, result.Leaderboard)
	require.NotNil(t, result.Recent)
	require.Zero(t, bank.calls)
	require.NoError(t, s.SetEnabled(ctx, true))
	result, err = s.Get(ctx)
	require.NoError(t, err)
	require.True(t, result.Enabled)
	require.Equal(t, "王***", result.Leaderboard[0].Name)
	require.Equal(t, "用户***", result.Leaderboard[1].Name)
	require.Equal(t, "a***", result.Recent[0].Name)
	require.Equal(t, 1.25, result.Leaderboard[0].Amount)
	require.False(t, result.UpdatedAt.IsZero())
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	for _, secret := range []string{"王小明", "alice@example.com", "owner_user_id", "email", "balance", "account_id"} {
		require.NotContains(t, string(encoded), secret)
	}
	_, err = s.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, bank.calls)
	require.NoError(t, s.SetEnabled(ctx, false))
	result, err = s.Get(ctx)
	require.NoError(t, err)
	require.False(t, result.Enabled)
	require.Empty(t, result.Leaderboard)
	require.Empty(t, result.Recent)
	require.NoError(t, s.SetEnabled(ctx, true))
	_, err = s.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, bank.calls, "toggle invalidates cached earnings")
	settings.err = errors.New("settings unavailable")
	result, err = s.Get(ctx)
	require.Error(t, err)
	require.Nil(t, result, "settings failure must not serve cached public data")
}
