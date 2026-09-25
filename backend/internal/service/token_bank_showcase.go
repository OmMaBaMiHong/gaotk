package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
)

const SettingKeyTokenBankShowcaseEnabled = "token_bank_showcase_enabled"

type TokenBankLeader struct {
	Rank   int     `json:"rank"`
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
}

type TokenBankRecent struct {
	Name      string    `json:"name"`
	Platform  string    `json:"platform"`
	Amount    float64   `json:"amount"`
	CreatedAt time.Time `json:"created_at"`
}

type TokenBankShowcase struct {
	Enabled     bool              `json:"enabled"`
	Leaderboard []TokenBankLeader `json:"leaderboard"`
	Recent      []TokenBankRecent `json:"recent"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type TokenBankShowcaseService struct {
	bank     TokenBankRepository
	settings SettingRepository
	mu       sync.Mutex
	cached   *TokenBankShowcase
}

func NewTokenBankShowcaseService(bank TokenBankRepository, settings SettingRepository) *TokenBankShowcaseService {
	return &TokenBankShowcaseService{bank: bank, settings: settings}
}

func (s *TokenBankShowcaseService) Enabled(ctx context.Context) (bool, error) {
	value, err := s.settings.GetValue(ctx, SettingKeyTokenBankShowcaseEnabled)
	if errors.Is(err, ErrSettingNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "true", nil
}

func (s *TokenBankShowcaseService) SetEnabled(ctx context.Context, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.settings.Set(ctx, SettingKeyTokenBankShowcaseEnabled, strconv.FormatBool(enabled)); err != nil {
		return err
	}
	s.cached = nil
	return nil
}

func (s *TokenBankShowcaseService) Get(ctx context.Context) (*TokenBankShowcase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check the persisted switch on every request, including cache hits, so a
	// disabled switch never leaves a previous payload publicly readable.
	enabled, err := s.Enabled(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		s.cached = nil
		return &TokenBankShowcase{Leaderboard: []TokenBankLeader{}, Recent: []TokenBankRecent{}, UpdatedAt: time.Now().UTC()}, nil
	}
	if s.cached != nil && time.Since(s.cached.UpdatedAt) < time.Minute {
		return s.cached, nil
	}
	result, err := s.bank.Showcase(ctx)
	if err != nil {
		return nil, err
	}
	for i := range result.Leaderboard {
		result.Leaderboard[i].Name = maskTokenBankName(result.Leaderboard[i].Name)
	}
	for i := range result.Recent {
		result.Recent[i].Name = maskTokenBankName(result.Recent[i].Name)
	}
	result.Enabled = true
	result.UpdatedAt = time.Now().UTC()
	s.cached = result
	return result, nil
}

func maskTokenBankName(name string) string {
	runes := []rune(strings.TrimSpace(name))
	if len(runes) == 0 {
		return "用户***"
	}
	if len(runes) == 1 {
		return "***"
	}
	return string(runes[0]) + "***"
}
