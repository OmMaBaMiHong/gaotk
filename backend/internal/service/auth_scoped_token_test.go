//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type codeOnceCacheStub struct {
	fail   bool
	exists map[string]bool
}

func newCodeOnceCacheStub() *codeOnceCacheStub {
	return &codeOnceCacheStub{exists: map[string]bool{}}
}

func (s *codeOnceCacheStub) PutIfAbsent(_ context.Context, key string, _ string, _ time.Duration) (bool, error) {
	if s.fail {
		return false, errors.New("redis down")
	}
	if s.exists[key] {
		return false, nil
	}
	s.exists[key] = true
	return true, nil
}

func newScopedTokenTestService() *AuthService {
	cfg := &config.Config{}
	cfg.JWT.Secret = "unit-test-secret"
	cfg.JWT.ExpireHour = 24
	cfg.JWT.RefreshTokenExpireDays = 30
	return &AuthService{cfg: cfg}
}

func TestConsumeOAuthCodeOnce(t *testing.T) {
	ctx := context.Background()

	t.Run("首次消费通过，重放拒绝", func(t *testing.T) {
		s := newScopedTokenTestService()
		cache := newCodeOnceCacheStub()
		s.SetOAuthCodeOnceCache(cache)

		if err := s.ConsumeOAuthCodeOnce(ctx, "nonce-1"); err != nil {
			t.Fatalf("first consume should pass, got %v", err)
		}
		if err := s.ConsumeOAuthCodeOnce(ctx, "nonce-1"); !errors.Is(err, ErrOAuthCodeReused) {
			t.Fatalf("replay should be ErrOAuthCodeReused, got %v", err)
		}
	})

	t.Run("缓存不可用 fail closed", func(t *testing.T) {
		s := newScopedTokenTestService()
		if err := s.ConsumeOAuthCodeOnce(ctx, "nonce-2"); !errors.Is(err, ErrServiceUnavailable) {
			t.Fatalf("nil cache should fail closed, got %v", err)
		}
		s.SetOAuthCodeOnceCache(&codeOnceCacheStub{fail: true})
		if err := s.ConsumeOAuthCodeOnce(ctx, "nonce-2"); !errors.Is(err, ErrServiceUnavailable) {
			t.Fatalf("cache error should fail closed, got %v", err)
		}
	})

	t.Run("空 nonce 视为重放", func(t *testing.T) {
		s := newScopedTokenTestService()
		s.SetOAuthCodeOnceCache(newCodeOnceCacheStub())
		if err := s.ConsumeOAuthCodeOnce(ctx, ""); !errors.Is(err, ErrOAuthCodeReused) {
			t.Fatalf("empty nonce should be ErrOAuthCodeReused, got %v", err)
		}
	})
}

func TestScopedAccessTokenCarriesScope(t *testing.T) {
	s := newScopedTokenTestService()
	user := &User{ID: 7, Email: "u@example.com", Username: "u7", Role: "user", Status: "active"}

	token, err := s.signAccessToken(user, "fam-1", "", []string{"profile", "membership"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("sign scoped token: %v", err)
	}
	claims, err := s.ValidateToken(token)
	if err != nil {
		t.Fatalf("validate scoped token: %v", err)
	}
	if claims.Scope != "profile,membership" {
		t.Fatalf("claims.Scope = %q, want %q", claims.Scope, "profile,membership")
	}
	if claims.SessionID != "fam-1" {
		t.Fatalf("claims.SessionID = %q, want fam-1", claims.SessionID)
	}

	// 全量令牌（登录路径）必须不带 scope，否则存量前端会被误判成受限令牌。
	full, err := s.generateAccessToken(user, "fam-2", "")
	if err != nil {
		t.Fatalf("sign full token: %v", err)
	}
	fullClaims, err := s.ValidateToken(full)
	if err != nil {
		t.Fatalf("validate full token: %v", err)
	}
	if fullClaims.Scope != "" {
		t.Fatalf("full token claims.Scope = %q, want empty", fullClaims.Scope)
	}
}
