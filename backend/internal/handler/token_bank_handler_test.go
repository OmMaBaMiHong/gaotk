//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	h := NewTokenBankHandler(repo)
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
