package tokencenter

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestHTTPImportAndPublicContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := testService(t, &memoryStore{}, "")
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), router.Group("/api/v1/admin", DevTokenAuth("test-admin")), s)
	request := func(method, path, token string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	if got := request("GET", "/api/v1/tokencenter/catalog", "", nil); got.Code != 503 {
		t.Fatal(got.Code)
	}
	if got := request("POST", "/api/v1/admin/tokencenter/import", "", demoJSON); got.Code != 401 {
		t.Fatal(got.Code)
	}
	if got := request("GET", "/api/v1/admin/tokencenter/catalog", "wrong", nil); got.Code != 401 {
		t.Fatal(got.Code)
	}
	if got := request("POST", "/api/v1/admin/tokencenter/import", "test-admin", demoJSON); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	}
	got := request("GET", "/api/v1/tokencenter/catalog", "", nil)
	var catalog Catalog
	if err := json.Unmarshal(got.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if got.Code != 200 || !catalog.Demo || len(catalog.Offers) != 5 || catalog.Source == "" || catalog.Sites[0].Models != 3 {
		t.Fatalf("bad contract: %s", got.Body)
	}
	if got.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing cache control")
	}
	if got := request("POST", "/api/v1/admin/tokencenter/import", "test-admin", []byte(`{}`)); got.Code != 400 {
		t.Fatal(got.Code)
	}
	if got := request("POST", "/api/v1/admin/tokencenter/sync", "test-admin", nil); got.Code != 503 {
		t.Fatal(got.Code)
	}
	if got := request("GET", "/api/v1/admin/tokencenter/catalog", "test-admin", nil); got.Code != 200 {
		t.Fatal(got.Code)
	}
}
func TestSyncConditionalMetadataOnlyAfterSuccess(t *testing.T) {
	var mode atomic.Int32
	var conditional atomic.Bool
	var requests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("If-None-Match") == `"good"` && r.Header.Get("If-Modified-Since") == "Mon, 19 Oct 2026 00:00:00 GMT" {
			conditional.Store(true)
		}
		switch mode.Load() {
		case 1:
			w.Header().Set("ETag", `"bad"`)
			_, _ = w.Write([]byte(`{"version":"bad"}`))
		case 2:
			w.WriteHeader(304)
		case 3:
			w.WriteHeader(500)
		default:
			w.Header().Set("ETag", `"good"`)
			w.Header().Set("Last-Modified", "Mon, 19 Oct 2026 00:00:00 GMT")
			_, _ = w.Write(demoJSON)
		}
	}))
	defer source.Close()
	store := &memoryStore{}
	s := testService(t, store, source.URL)
	if _, err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	// New instance must reuse persisted successful ETag/Last-Modified and data.
	s = testService(t, store, source.URL)
	mode.Store(1)
	if _, err := s.Sync(context.Background()); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	snap, _ := store.Load(context.Background())
	if snap.ETag != `"good"` || snap.Catalog.Version != "demo-2026-10-19" {
		t.Fatal("failed sync advanced metadata")
	}
	mode.Store(2)
	if _, err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !conditional.Load() {
		t.Fatal("missing successful conditional headers after restart")
	}
	mode.Store(3)
	if _, err := s.Sync(context.Background()); err == nil {
		t.Fatal("accepted HTTP 500")
	}
	c, _ := s.Catalog(context.Background())
	if c.Version != "demo-2026-10-19" {
		t.Fatal("failed sync lost snapshot")
	}
	// Import invalidates validators so a later source fetch is unconditional.
	if _, err := s.Import(context.Background(), fixture(t)); err != nil {
		t.Fatal(err)
	}
	mode.Store(2)
	if _, err := s.Sync(context.Background()); err == nil {
		t.Fatal("accepted 304 after manual import")
	}
	_ = requests.Load()
}
func TestSyncTimeoutAndAutomaticWorker(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer source.Close()
	s := testService(t, &memoryStore{}, source.URL)
	s.cfg.Timeout = 20 * time.Millisecond
	start := time.Now()
	if _, err := s.Sync(context.Background()); err == nil || time.Since(start) > time.Second {
		t.Fatal("timeout not enforced")
	}
	var calls atomic.Int32
	working := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = w.Write(demoJSON) }))
	defer working.Close()
	s = testService(t, &memoryStore{}, working.URL)
	s.cfg.Interval = 10 * time.Millisecond
	s.Start(context.Background())
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for calls.Load() < 2 {
		select {
		case <-deadline.C:
			s.Stop()
			t.Fatal("automatic sync did not run")
		case <-ticker.C:
		}
	}
	s.Stop()
}
func TestConfiguredSourceCannotRedirect(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	s := testService(t, &memoryStore{}, source.URL)
	if _, err := s.Sync(context.Background()); err == nil || leaked.Load() {
		t.Fatal("redirect followed")
	}
}
