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

func TestBrowseKeepsRecordsAndLoadsFullModelOnDemand(t *testing.T) {
	store := &memoryStore{}
	catalog := fixture(t)
	catalog.Models[0].Evaluations = []Evaluation{{BenchmarkID: "published-history", Score: ptr(42.0)}}
	catalog.Models[0].Billing = json.RawMessage(`{"source":"published billing"}`)
	if err := store.Save(context.Background(), Snapshot{Catalog: catalog}, false); err != nil {
		t.Fatal(err)
	}
	service := testService(t, store, "")
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), router.Group("/api/v1/admin"), service)
	read := func(path string) *httptest.ResponseRecorder {
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest("GET", path, nil))
		return out
	}
	response := read("/api/v1/tokencenter/catalog?view=browse")
	var browse Catalog
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &browse) != nil {
		t.Fatal(response.Code)
	}
	if len(browse.Models) != len(catalog.Models) || len(browse.Offers) != len(catalog.Offers) || len(browse.Models[0].Evaluations) != 0 || len(browse.Models[0].Billing) != 0 {
		t.Fatal("browse lost records or eagerly loaded history")
	}
	response = read("/api/v1/tokencenter/models/" + catalog.Models[0].ID)
	var full Model
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &full) != nil || len(full.Evaluations) != 1 || len(full.Billing) == 0 {
		t.Fatal("detail lost source history")
	}
	if read("/api/v1/tokencenter/models/absent").Code != 404 {
		t.Fatal("missing model was not 404")
	}
	store.SetAccessStatus(context.Background(), "", 403)
	for _, path := range []string{"/api/v1/tokencenter/catalog?view=browse", "/api/v1/tokencenter/models/" + full.ID} {
		if read(path).Code != 503 {
			t.Fatal("read bypassed source withdrawal")
		}
	}
}

func TestBrowseSelectsOneBoardAndPreservesOverviewsRelayRanksAndFullDetails(t *testing.T) {
	store := &memoryStore{}
	rank, relayRank, score := 1, 7, 84.0
	c := Catalog{Version: "published", Source: "Chaos", RankingAuthority: "chaos", DefaultRanking: "value-general-balanced", Models: []Model{
		{ID: "one", Name: "One", Evaluations: []Evaluation{{BenchmarkID: "full-history"}}},
		{ID: "two", Name: "Two", Evaluations: []Evaluation{{BenchmarkID: "full-history"}}},
		{ID: "three", Name: "Three"},
	}, Sites: []Site{{ID: "relay", Name: "Relay", ProviderKind: "relay"}}, Offers: []Offer{}, Rankings: map[string]Ranking{
		"value-general-balanced": {Topic: "value-general-balanced", Title: "General value", Source: "published-source", Metadata: json.RawMessage(`{"list":{"entityType":"ai_model"},"valuePolicy":{"abilityWeight":0.6,"priceWeight":0.4}}`), Entries: []RankedEntry{{EntityID: "one", Rank: &rank, Score: &score, Metadata: json.RawMessage(`{"metadata":{"valueScore":{"abilityScore":88,"referenceCost":2}}}`)}}, Excluded: []RankedEntry{{EntityID: "two", Metadata: json.RawMessage(`{"metadata":{"valueScore":{"unrankedReason":"missing_reference_quote","missingEvidence":["quote"]}}}`)}}},
		"value-coding-balanced":  {Topic: "value-coding-balanced", Title: "Coding value", Source: "other-source", Metadata: json.RawMessage(`{"list":{"entityType":"ai_model"},"valuePolicy":{"abilityWeight":0.7,"priceWeight":0.3}}`), Entries: []RankedEntry{{EntityID: "three", Rank: &rank, Score: &score, Metadata: json.RawMessage(`{"metadata":{"valueScore":{"abilityScore":91}}}`)}}, Excluded: []RankedEntry{{EntityID: "one", Metadata: json.RawMessage(`{"metadata":{"valueScore":{"unrankedReason":"coding_evidence_missing"}}}`)}}},
		"empty":                  {Topic: "empty", Title: "Empty published board", Entries: []RankedEntry{}, Metadata: json.RawMessage(`{"list":{"entityType":"ai_model"}}`)},
		"relays":                 {Topic: "relays", Title: "Relay board", Entries: []RankedEntry{{EntityID: "relay", Rank: &relayRank, Score: &score}}},
	}}
	if err := store.Save(context.Background(), Snapshot{Catalog: c}, false); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), router.Group("/api/v1/admin"), testService(t, store, ""))
	read := func(path string, target any) {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), target) != nil {
			t.Fatalf("invalid response: %d %s", response.Code, response.Body.String())
		}
	}
	var browse Catalog
	read("/api/v1/tokencenter/catalog?view=browse", &browse)
	if browse.RankingKey != c.DefaultRanking || len(browse.Models) != 3 || len(browse.Rankings) != 4 || browse.Summary == nil || browse.Summary.PublishedBoards != 3 {
		t.Fatalf("browse lost records, default board or board counts: %+v", browse)
	}
	for key, board := range browse.Rankings {
		if board.EntryCount == nil || *board.EntryCount != len(c.Rankings[key].Entries) || board.ExcludedCount == nil || *board.ExcludedCount != len(c.Rankings[key].Excluded) || board.DetailsIncluded == nil || *board.DetailsIncluded != (key == browse.RankingKey) || len(board.Excluded) != 0 {
			t.Fatalf("wrong board overview: %s %+v", key, board)
		}
		if key != browse.RankingKey && len(board.Entries) != 0 || key == browse.RankingKey && (len(board.Entries) != 1 || len(board.Entries[0].Metadata) > 0 && string(board.Entries[0].Metadata) != "null") {
			t.Fatalf("browse duplicated per-model evidence or returned another board's entries: %+v", board)
		}
	}
	if browse.Rankings["value-coding-balanced"].Title != "Coding value" || browse.Rankings["value-coding-balanced"].Source != "other-source" || !bytes.Contains(browse.Rankings["value-coding-balanced"].Metadata, []byte(`"abilityWeight":0.7`)) || !bytes.Contains(browse.Models[0].ValueScore, []byte(`"abilityScore":88`)) || !bytes.Contains(browse.Models[1].ValueScore, []byte("missing_reference_quote")) {
		t.Fatal("board overview policy or selected model evidence was dropped")
	}
	if len(browse.Relays) != 1 || browse.Relays[0].Rank == nil || *browse.Relays[0].Rank != relayRank || browse.Home == nil || len(browse.Home.Relays) != 1 || *browse.Home.Relays[0].Rank != relayRank {
		t.Fatal("summary-only relay board removed the existing homepage/relay ranks")
	}
	read("/api/v1/tokencenter/catalog?view=browse&ranking=value-coding-balanced", &browse)
	if browse.RankingKey != "value-coding-balanced" || browse.Models[0].ID != "three" || !bytes.Contains(browse.Models[0].ValueScore, []byte(`"abilityScore":91`)) {
		t.Fatal("changing the selected board did not load its own evidence")
	}
	var full Catalog
	read("/api/v1/tokencenter/catalog?ranking=value-general-balanced", &full)
	if len(full.Rankings["value-coding-balanced"].Entries) != 1 || len(full.Rankings["value-coding-balanced"].Excluded) != 1 || len(full.Models[0].Evaluations) != 1 {
		t.Fatal("browse projection changed the full catalog publication")
	}
	var detail Model
	read("/api/v1/tokencenter/models/two?ranking=value-general-balanced", &detail)
	if len(detail.Evaluations) != 1 || !bytes.Contains(detail.ValueScore, []byte("missing_reference_quote")) {
		t.Fatal("model detail lost full evidence")
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
