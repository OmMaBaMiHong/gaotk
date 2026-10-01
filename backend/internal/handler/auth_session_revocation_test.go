//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAuthHandlerRevokeAllSessionsInvalidatesAccessTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &userHandlerRepoStub{
		user: &service.User{
			ID:           29,
			Email:        "session@example.com",
			Username:     "session-user",
			Role:         service.RoleUser,
			Status:       service.StatusActive,
			TokenVersion: 7,
		},
	}
	refreshTokenCache := &userHandlerRefreshTokenCacheStub{}
	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:     "test-secret",
			ExpireHour: 1,
		},
	}
	authService := service.NewAuthService(nil, repo, nil, refreshTokenCache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	handler := &AuthHandler{authService: authService}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/revoke-all-sessions", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 29})

	handler.RevokeAllSessions(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []int64{29}, refreshTokenCache.revokedUserIDs)
	// users 表没有 token_version 列；scoped access 由 Redis 用户 epoch 撤销，
	// 同时清理 refresh session。TokenVersion 仍不应写回用户行。
	require.Equal(t, int64(7), repo.user.TokenVersion)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "All sessions have been revoked. Please log in again.", resp.Data.Message)
}

func TestLogoutRevokesOnlyPresentedScopedAccessSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer := miniredis.RunT(t)
	cache := repository.NewRefreshTokenCache(redis.NewClient(&redis.Options{Addr: redisServer.Addr()}))
	user := &service.User{ID: 29, Email: "session@example.com", Role: service.RoleUser, Status: service.StatusActive}
	cfg := &config.Config{}
	cfg.JWT.Secret = "unit-test-secret"
	cfg.JWT.RefreshTokenExpireDays = 30
	auth := service.NewAuthService(nil, &userHandlerRepoStub{user: user}, nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	first, err := auth.GenerateScopedTokenPair(context.Background(), user, "", []string{"profile"})
	require.NoError(t, err)
	second, err := auth.GenerateScopedTokenPair(context.Background(), user, "", []string{"profile"})
	require.NoError(t, err)
	firstClaims, err := auth.ValidateToken(first.AccessToken)
	require.NoError(t, err)
	secondClaims, err := auth.ValidateToken(second.AccessToken)
	require.NoError(t, err)
	revocationCache := cache.(service.AccessSessionRevocationCache)
	require.NoError(t, revocationCache.RevokeAccessSession(context.Background(), firstClaims.SessionID, time.Hour))
	_, err = auth.RefreshTokenPair(context.Background(), first.RefreshToken)
	require.ErrorIs(t, err, service.ErrTokenRevoked)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	c.Request.Header.Set("Authorization", "Bearer "+first.AccessToken)
	(&AuthHandler{authService: auth}).Logout(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	revoked, err := auth.IsAccessSessionRevoked(context.Background(), firstClaims.SessionID)
	require.NoError(t, err)
	require.True(t, revoked)
	revoked, err = auth.IsAccessSessionRevoked(context.Background(), secondClaims.SessionID)
	require.NoError(t, err)
	require.False(t, revoked)
}
