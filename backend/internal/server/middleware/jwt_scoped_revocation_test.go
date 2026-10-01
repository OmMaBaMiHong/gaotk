//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestScopedAccessRevocationOnlyClosesItsSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer := miniredis.RunT(t)
	cache := repository.NewRefreshTokenCache(redis.NewClient(&redis.Options{Addr: redisServer.Addr()}))
	user := &service.User{ID: 1, Email: "user@example.com", Role: "user", Status: service.StatusActive}
	users := &stubJWTUserRepo{users: map[int64]*service.User{1: user}}
	cfg := &config.Config{}
	cfg.JWT.Secret = "unit-test-secret"
	cfg.JWT.RefreshTokenExpireDays = 30
	auth := service.NewAuthService(nil, users, nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewJWTAuthMiddleware(auth, service.NewUserService(users, nil, nil, nil), nil, nil)))
	router.GET("/api/v1/auth/me", func(c *gin.Context) { c.Status(http.StatusOK) })

	first, err := auth.GenerateScopedTokenPair(context.Background(), user, "", []string{"profile"})
	require.NoError(t, err)
	second, err := auth.GenerateScopedTokenPair(context.Background(), user, "", []string{"profile"})
	require.NoError(t, err)
	request := func(token string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w.Code
	}
	require.Equal(t, http.StatusOK, request(first.AccessToken))
	claims, err := auth.ValidateToken(first.AccessToken)
	require.NoError(t, err)
	require.NoError(t, auth.RevokeSessionFamily(context.Background(), claims.SessionID))
	require.Equal(t, http.StatusUnauthorized, request(first.AccessToken))
	require.Equal(t, http.StatusOK, request(second.AccessToken))
	redisServer.Close()
	require.Equal(t, http.StatusServiceUnavailable, request(second.AccessToken))
}

func TestRevokeAllUserTokensRejectsOldScopedJWTsAndAllowsNewIssue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisServer := miniredis.RunT(t)
	cache := repository.NewRefreshTokenCache(redis.NewClient(&redis.Options{Addr: redisServer.Addr()}))
	user := &service.User{ID: 2, Email: "user@example.com", Role: "user", Status: service.StatusActive}
	users := &stubJWTUserRepo{users: map[int64]*service.User{2: user}}
	cfg := &config.Config{}
	cfg.JWT.Secret = "unit-test-secret"
	cfg.JWT.RefreshTokenExpireDays = 30
	auth := service.NewAuthService(nil, users, nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewJWTAuthMiddleware(auth, service.NewUserService(users, nil, nil, nil), nil, nil)))
	router.GET("/api/v1/auth/me", func(c *gin.Context) { c.Status(http.StatusOK) })
	request := func(token string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(w, req)
		return w.Code
	}

	first, err := auth.GenerateScopedTokenPair(context.Background(), user, "", []string{"profile"})
	require.NoError(t, err)
	second, err := auth.GenerateScopedTokenPair(context.Background(), user, "", []string{"profile"})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, request(first.AccessToken))
	require.Equal(t, http.StatusOK, request(second.AccessToken))

	require.NoError(t, auth.RevokeAllUserTokens(context.Background(), user.ID))
	require.Equal(t, http.StatusUnauthorized, request(first.AccessToken))
	require.Equal(t, http.StatusUnauthorized, request(second.AccessToken))

	third, err := auth.GenerateScopedTokenPair(context.Background(), user, "", []string{"profile"})
	require.NoError(t, err)
	claims, err := auth.ValidateToken(third.AccessToken)
	require.NoError(t, err)
	require.Equal(t, int64(1), claims.RevocationEpoch)
	require.Equal(t, http.StatusOK, request(third.AccessToken))
}

func TestScopedRefreshCannotCrossUserRevocationEpoch(t *testing.T) {
	redisServer := miniredis.RunT(t)
	cache := repository.NewRefreshTokenCache(redis.NewClient(&redis.Options{Addr: redisServer.Addr()}))
	user := &service.User{ID: 3, Email: "user@example.com", Role: "user", Status: service.StatusActive}
	users := &stubJWTUserRepo{users: map[int64]*service.User{3: user}}
	cfg := &config.Config{}
	cfg.JWT.Secret = "unit-test-secret"
	cfg.JWT.RefreshTokenExpireDays = 30
	auth := service.NewAuthService(nil, users, nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	pair, err := auth.GenerateScopedTokenPair(context.Background(), user, "", []string{"profile"})
	require.NoError(t, err)

	epochCache := cache.(service.UserTokenEpochCache)
	_, err = epochCache.IncrementUserTokenEpoch(context.Background(), user.ID)
	require.NoError(t, err)
	_, err = auth.RefreshTokenPair(context.Background(), pair.RefreshToken)
	require.ErrorIs(t, err, service.ErrTokenRevoked)
}
