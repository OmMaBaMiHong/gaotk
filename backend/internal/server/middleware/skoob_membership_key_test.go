//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSkoobMembershipKeyAllowsModelsAndInference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/v1/models", "/v1/chat/completions", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			groupID := int64(12)
			repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
				return &service.APIKey{ID: 1, UserID: 1, Status: service.StatusActive, GroupID: &groupID,
					User:  &service.User{ID: 1, Status: service.StatusActive},
					Group: &service.Group{ID: groupID, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeSubscription}}, nil
			}}
			cfg := &config.Config{RunMode: config.RunModeStandard}
			svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
			r := gin.New()
			r.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, activeMembershipSubscription(cfg), cfg)))
			r.Any(path, func(c *gin.Context) { c.Status(http.StatusOK) })
			w := httptest.NewRecorder()
			method := http.MethodPost
			if path == "/v1/models" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, path, nil)
			req.Header.Set("Authorization", "Bearer test-key")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestSkoobMembershipPolicyPreservesModelGroupsAndBillingRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		path string
	}{
		{"/v1/models"},
		{"/v1/responses"},
		{"/v1/usage"},
		{"/v1/sub2api/billing"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			groupID := int64(16)
			repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
				return &service.APIKey{ID: 1, UserID: 1, Status: service.StatusActive, GroupID: &groupID,
					User:  &service.User{ID: 1, Status: service.StatusActive},
					Group: &service.Group{ID: groupID, Status: service.StatusActive, Hydrated: true}}, nil
			}}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
			r := gin.New()
			r.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
			r.GET(tc.path, func(c *gin.Context) { c.Status(http.StatusOK) })
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer test-key")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestSkoobMembershipKeyAllowsGoogleModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(12)
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
		return &service.APIKey{ID: 1, UserID: 1, Status: service.StatusActive, GroupID: &groupID,
			User:  &service.User{ID: 1, Status: service.StatusActive},
			Group: &service.Group{ID: groupID, Status: service.StatusActive, Hydrated: true, SubscriptionType: service.SubscriptionTypeSubscription}}, nil
	}}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	r := gin.New()
	r.Use(APIKeyAuthWithSubscriptionGoogle(svc, activeMembershipSubscription(cfg), cfg))
	r.GET("/v1beta/models", func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	req.Header.Set("x-goog-api-key", "test-key")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func activeMembershipSubscription(cfg *config.Config) *service.SubscriptionService {
	now := time.Now()
	repo := &stubUserSubscriptionRepo{
		getActive: func(_ context.Context, userID, groupID int64) (*service.UserSubscription, error) {
			return &service.UserSubscription{ID: 1, UserID: userID, GroupID: groupID,
				Status: service.SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour),
				DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}, nil
		},
	}
	return service.NewSubscriptionService(nil, repo, nil, nil, cfg)
}
