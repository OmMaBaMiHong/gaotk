package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type ownedAdminStub struct {
	service.AdminService
	accounts map[int64]*service.Account
}

func (s ownedAdminStub) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	if account := s.accounts[id]; account != nil {
		return account, nil
	}
	return nil, service.ErrAccountNotFound
}
func TestOwnedAccountMiddlewareChecksPathAndEveryBatchMember(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mine := int64(42)
	other := int64(43)
	h := &AccountHandler{adminService: ownedAdminStub{accounts: map[int64]*service.Account{1: {ID: 1, OwnerUserID: &mine}, 2: {ID: 2, OwnerUserID: &other}, 3: {ID: 3}}}}
	for _, test := range []struct {
		path, body string
		owner      int64
		status     int
	}{
		{"/accounts/1", "{}", 42, 200}, {"/accounts/2", "{}", 42, 404}, {"/accounts/3", "{}", 42, 404}, {"/accounts/1", "{}", 0, 401},
		{"/accounts/batch", `{"account_ids":[1,2]}`, 42, 404}, {"/accounts/batch", `{"account_ids":[1]}`, 42, 200},
	} {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if test.owner > 0 {
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: test.owner})
			}
		})
		r.Use(h.OwnedAccountScope(nil))
		r.POST("/accounts/:id", func(c *gin.Context) { c.Status(200) })
		r.POST("/accounts/batch", func(c *gin.Context) { c.Status(200) })
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, test.status, w.Code, test.path+test.body)
	}
}
func TestOwnedImportSanitizesNestedAccountsAndNeverImportsProxies(t *testing.T) {
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"owner_user_id":99,"proxy_id":1,"group_ids":[9],"data":{"proxies":[{"host":"secret"}],"accounts":[{"name":"mine","platform":"openai","extra":{"quota_used":0},"proxy_key":"secret","priority":1,"credentials":{"access_token":"token","base_url":"http://internal","model_mapping":{"a":"b"}}}]}}`), &payload))
	sanitizeOwnedAccountPayload(payload)
	require.NotContains(t, payload, "owner_user_id")
	require.NotContains(t, payload, "proxy_id")
	require.NotContains(t, payload, "group_ids")
	data := payload["data"].(map[string]any)
	require.Equal(t, []any{}, data["proxies"])
	account := data["accounts"].([]any)[0].(map[string]any)
	require.NotContains(t, account, "extra")
	require.NotContains(t, account, "proxy_key")
	require.NotContains(t, account, "priority")
	require.Equal(t, map[string]any{"access_token": "token"}, account["credentials"])
}
func TestOwnedUsageDTODoesNotExposeConsumers(t *testing.T) {
	ip := "1.2.3.4"
	ua := "private agent"
	session := "private session"
	log := &service.UsageLog{ID: 1, AccountID: 4, UserID: 77, APIKeyID: 88, RequestID: "private-request", IPAddress: &ip, UserAgent: &ua, SessionID: &session, InputTokens: 13}
	out := ownedAccountUsageDTO(log)
	require.Equal(t, float64(13), out["input_tokens"])
	for _, key := range []string{"user_id", "api_key_id", "request_id", "user", "api_key", "ip_address", "user_agent", "session_id", "group", "subscription", "upstream_request_id"} {
		require.NotContains(t, out, key)
	}
}

func TestOwnedCodexImportDoesNotMatchAnotherOwnersCredentials(t *testing.T) {
	owner := int64(43)
	token := buildCodexAccessToken(t, "workspace", "user", time.Now().Add(time.Hour))
	svc := newCodexImportMemoryAdminService([]service.Account{{ID: 10, OwnerUserID: &owner, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": token, "chatgpt_account_id": "workspace", "chatgpt_user_id": "user"}}})
	h := &AccountHandler{adminService: svc}
	ctx := service.WithOwnedAccountScope(context.Background(), 42, nil)
	result, err := h.importCodexSessions(ctx, CodexSessionImportRequest{}, []codexImportEntry{{Index: 1, Value: map[string]any{"access_token": token}}})
	require.NoError(t, err)
	require.Equal(t, 1, result.Created)
	require.Zero(t, result.Updated)
	require.Empty(t, svc.updatedAccounts)
	require.Equal(t, token, svc.accounts[0].Credentials["access_token"])
}

type ownerListAdminStub struct {
	service.AdminService
	ownerID      int64
	scopedUserID int64
}

func (s *ownerListAdminStub) ListAccounts(ctx context.Context, page, pageSize int, platform, accountType, status, search string, groupID int64, privacyMode, sortBy, sortOrder string) ([]service.Account, int64, error) {
	s.ownerID = service.AccountListOwnerID(ctx)
	s.scopedUserID = service.OwnedAccountUserID(ctx)
	return []service.Account{}, 0, nil
}
func TestAdminAccountListOwnerFilterDoesNotEnableOwnerPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &ownerListAdminStub{}
	h := &AccountHandler{adminService: svc}
	r := gin.New()
	r.GET("/admin/accounts", h.List)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/accounts?owner_user_id=42", nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, int64(42), svc.ownerID)
	require.Zero(t, svc.scopedUserID)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/accounts?owner_user_id=invalid", nil))
	require.Equal(t, 400, w.Code)
}

func TestOwnedDataImportRunsOriginalValidationAndImportAfterSanitizing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, proxies string
		status        int
	}{
		{"empty proxies", `"proxies":[],`, http.StatusOK},
		{"discard supplied proxies", `"proxies":[{"name":"forbidden","host":"internal.invalid","port":8080,"protocol":"http"}],`, http.StatusOK},
		{"missing proxies keeps validation", ``, http.StatusBadRequest},
		{"null proxies keeps validation", `"proxies":null,`, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := newStubAdminService()
			h := &AccountHandler{adminService: svc}
			r := gin.New()
			r.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42}) })
			r.POST("/user/accounts/data", h.OwnedAccountScope(nil), h.ImportData)
			body := `{"data":{"type":"sub2api-data","version":1,` + test.proxies + `"accounts":[{"name":"mine","platform":"deepseek","type":"apikey","credentials":{"api_key":"test-only","base_url":"http://internal.invalid"},"proxy_key":"forbidden"}]}}`
			request := httptest.NewRequest(http.MethodPost, "/user/accounts/data", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, request)
			require.Equal(t, test.status, w.Code, w.Body.String())
			require.Empty(t, svc.createdProxies)
			require.Zero(t, svc.lastListProxies.calls)
			if test.status == http.StatusOK {
				require.Len(t, svc.createdAccounts, 1)
				require.Nil(t, svc.createdAccounts[0].ProxyID)
				require.Equal(t, map[string]any{"api_key": "test-only"}, svc.createdAccounts[0].Credentials)
				require.Contains(t, w.Body.String(), `"account_created":1`)
				require.Contains(t, w.Body.String(), `"proxy_created":0`)
			} else {
				require.Empty(t, svc.createdAccounts)
				require.Contains(t, w.Body.String(), "proxies is required")
			}
		})
	}
}
