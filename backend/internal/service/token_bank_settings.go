package service

import (
	"context"
	"errors"
	"strconv"
)

const SettingKeyTokenBankEnabled = "token_bank_enabled"

// TokenBankEnabled is opt-in and read on every owner request so a stale page
// cannot keep using the account APIs after an administrator closes the feature.
func (s *SettingService) TokenBankEnabled(ctx context.Context) (bool, error) {
	if s == nil || s.settingRepo == nil {
		return false, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyTokenBankEnabled)
	if errors.Is(err, ErrSettingNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "true", nil
}

func (s *SettingService) SetTokenBankEnabled(ctx context.Context, enabled bool) error {
	if err := s.settingRepo.Set(ctx, SettingKeyTokenBankEnabled, strconv.FormatBool(enabled)); err != nil {
		return err
	}
	if s.onUpdate != nil {
		s.onUpdate()
	}
	return nil
}
