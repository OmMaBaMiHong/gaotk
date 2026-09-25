//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type tokenBankHandlerRepo struct {
	service.TokenBankRepository
	ownerID int64
}

func (r *tokenBankHandlerRepo) Overview(_ context.Context, ownerID int64, _, _ string, _, _ int) (*service.RentalOverview, error) {
	r.ownerID = ownerID
	return &service.RentalOverview{Accounts: []service.RentalAccount{}}, nil
}
func TestTokenBankUserHandlersScopeAndPublicPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &tokenBankHandlerRepo{}
	h := NewTokenBankHandler(repo, nil)
	request := func(path string, userID int64, fn gin.HandlerFunc) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, path, nil)
		if userID > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
		}
		fn(c)
		return w
	}
	response := request("/api/v1/user/token-bank/accounts", 0, h.Overview)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	response = request("/api/v1/user/token-bank/accounts?owner_user_id=999", 12, h.Overview)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, int64(12), repo.ownerID)
}

type tokenBankHandlerSettings struct {
	service.SettingRepository
	value string
}

func (s *tokenBankHandlerSettings) GetValue(context.Context, string) (string, error) {
	if s.value == "" {
		return "", service.ErrSettingNotFound
	}
	return s.value, nil
}
func (s *tokenBankHandlerSettings) Set(_ context.Context, _, value string) error {
	s.value = value
	return nil
}

func TestTokenBankShowcaseHandlersRequireUserAndAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &tokenBankHandlerSettings{}
	h := NewTokenBankHandler(&tokenBankHandlerRepo{}, settings)
	request := func(method, body, role string, id int64, fn gin.HandlerFunc) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(method, "/api/v1/token-bank/showcase", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		if id > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: id})
		}
		c.Set(string(middleware.ContextKeyUserRole), role)
		fn(c)
		return w
	}
	require.Equal(t, http.StatusUnauthorized, request("GET", "", "", 0, h.Showcase).Code)
	w := request("GET", "", service.RoleUser, 1, h.Showcase)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"enabled":false`)
	require.Contains(t, w.Body.String(), `"leaderboard":[]`)
	require.Contains(t, w.Body.String(), `"recent":[]`)
	require.Equal(t, http.StatusUnauthorized, request("GET", "", "", 0, h.AdminShowcase).Code)
	require.Equal(t, http.StatusForbidden, request("PUT", `{"enabled":true}`, service.RoleUser, 1, h.SetAdminShowcase).Code)
	require.Empty(t, settings.value)
	require.Equal(t, http.StatusBadRequest, request("PUT", `{}`, service.RoleAdmin, 1, h.SetAdminShowcase).Code)
	require.Equal(t, http.StatusBadRequest, request("PUT", `{"enabled":"true"}`, service.RoleAdmin, 1, h.SetAdminShowcase).Code)
	w = request("PUT", `{"enabled":true}`, service.RoleAdmin, 1, h.SetAdminShowcase)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "true", settings.value)
	w = request("GET", "", service.RoleAdmin, 1, h.AdminShowcase)
	require.Contains(t, w.Body.String(), `"enabled":true`)
	w = request("PUT", `{"enabled":false}`, service.RoleAdmin, 1, h.SetAdminShowcase)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "false", settings.value)
}
