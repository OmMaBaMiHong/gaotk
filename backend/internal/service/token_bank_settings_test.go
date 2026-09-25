//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

type bankSwitchSettings struct {
	SettingRepository
	value string
	err   error
}

func (s *bankSwitchSettings) GetValue(context.Context, string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if s.value == "" {
		return "", ErrSettingNotFound
	}
	return s.value, nil
}
func (s *bankSwitchSettings) Set(_ context.Context, key, value string) error {
	if key != SettingKeyTokenBankEnabled {
		panic("wrong key")
	}
	if s.err != nil {
		return s.err
	}
	s.value = value
	return nil
}
func TestTokenBankSwitchDefaultsClosedAndInvalidatesPublicSettings(t *testing.T) {
	ctx := context.Background()
	repo := &bankSwitchSettings{}
	s := NewSettingService(repo, nil)
	updates := 0
	s.SetOnUpdateCallback(func() { updates++ })
	enabled, err := s.TokenBankEnabled(ctx)
	require.NoError(t, err)
	require.False(t, enabled)
	require.NoError(t, s.SetTokenBankEnabled(ctx, true))
	enabled, err = s.TokenBankEnabled(ctx)
	require.NoError(t, err)
	require.True(t, enabled)
	require.NoError(t, s.SetTokenBankEnabled(ctx, false))
	enabled, err = s.TokenBankEnabled(ctx)
	require.NoError(t, err)
	require.False(t, enabled)
	require.Equal(t, 2, updates)
	repo.err = errors.New("unavailable")
	enabled, err = s.TokenBankEnabled(ctx)
	require.Error(t, err)
	require.False(t, enabled)
}
