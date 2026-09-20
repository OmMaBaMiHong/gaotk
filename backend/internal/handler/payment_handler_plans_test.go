//go:build unit

package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestGetPlansIncludesConfiguredQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := sql.Open("sqlite", "file:plans_quota?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(entsql.OpenDB(dialect.SQLite, db))))
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	group, err := client.Group.Create().SetName("Monthly").SetMonthlyLimitUsd(40).SetDailyLimitUsd(5).SetWeeklyLimitUsd(20).Save(ctx)
	require.NoError(t, err)
	_, err = client.SubscriptionPlan.Create().SetGroupID(group.ID).SetName("CNY Monthly").SetCurrency("CNY").SetPrice(69).SetValidityDays(30).SetValidityUnit("day").SetForSale(true).Save(ctx)
	require.NoError(t, err)
	h := NewPaymentHandler(nil, service.NewPaymentConfigService(client, nil, nil))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/payment/plans", nil)
	h.GetPlans(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Data, 1)
	require.Equal(t, float64(40), response.Data[0]["monthly_limit_usd"])
	require.Equal(t, float64(5), response.Data[0]["daily_limit_usd"])
	require.Equal(t, float64(20), response.Data[0]["weekly_limit_usd"])
	require.Equal(t, "CNY", response.Data[0]["currency"])
}
