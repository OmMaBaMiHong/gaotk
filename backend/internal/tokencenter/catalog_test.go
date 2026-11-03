package tokencenter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu                     sync.Mutex
	data                   []byte
	source, etag, modified string
}

func (s *memoryStore) Load(_ context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		return Snapshot{}, ErrEmpty
	}
	var c Catalog
	if err := json.Unmarshal(s.data, &c); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Catalog: c, SourceURL: s.source, ETag: s.etag, LastModified: s.modified}, nil
}
func (s *memoryStore) Save(_ context.Context, snap Snapshot, seed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seed && s.data != nil {
		return nil
	}
	var err error
	s.data, err = json.Marshal(snap.Catalog)
	s.source, s.etag, s.modified = snap.SourceURL, snap.ETag, snap.LastModified
	return err
}
func fixture(t *testing.T) Catalog {
	t.Helper()
	c, err := DemoCatalog()
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func testService(t *testing.T, store Store, source string) *Service {
	t.Helper()
	s, err := NewService(store, Config{SourceURL: source, Timeout: time.Second, Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func ptr[T any](v T) *T { return &v }

func TestNormalizeRanksAndCoverage(t *testing.T) {
	c := fixture(t)
	c.Models[0].Score = ptr(80.0)
	c.Models[1].Score = ptr(80.0)
	c.Models[2].Score = ptr(99.0)
	c.Sites[0].Models = 999
	c, err := Normalize(c)
	if err != nil {
		t.Fatal(err)
	}
	if c.Models[0].ID != "demo-fast" || c.Models[1].ID != "demo-chat" || c.Models[2].Rank == nil || *c.Models[2].Rank != 3 {
		t.Fatalf("incorrect stable rank: %+v", c.Models)
	}
	if c.Sites[0].Models != 3 || c.Sites[1].Models != 2 {
		t.Fatalf("incorrect coverage: %+v", c.Sites)
	}
}
func TestRejectInvalidSnapshotKeepsPrevious(t *testing.T) {
	cases := map[string]func(*Catalog){
		"negative quote":     func(c *Catalog) { c.Offers[0].Input = ptr(-1.0) },
		"NaN":                func(c *Catalog) { c.Models[0].Score = ptr(math.NaN()) },
		"infinity":           func(c *Catalog) { c.Sites[0].Ratio = ptr(math.Inf(1)) },
		"fractional context": func(c *Catalog) { c.Models[0].Context = ptr(1.5) },
		"dangling reference": func(c *Catalog) { c.Offers[0].ModelID = "missing" },
		"duplicate id":       func(c *Catalog) { c.Models[1].ID = c.Models[0].ID },
		"duplicate offer id": func(c *Catalog) { c.Offers[1].ID = c.Offers[0].ID },
		"duplicate pair":     func(c *Catalog) { c.Offers[1].ModelID = c.Offers[0].ModelID; c.Offers[1].SiteID = c.Offers[0].SiteID },
		"currency":           func(c *Catalog) { c.Offers[0].Currency = "CNY" },
		"timestamp":          func(c *Catalog) { c.UpdatedAt = "bad" },
		"uptime":             func(c *Catalog) { v := 101.0; c.Sites[0].Uptime = &v },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			store := &memoryStore{}
			s := testService(t, store, "")
			old := fixture(t)
			if _, err := s.Import(context.Background(), old); err != nil {
				t.Fatal(err)
			}
			bad := fixture(t)
			bad.Version = "invalid"
			mutate(&bad)
			if _, err := s.Import(context.Background(), bad); err == nil {
				t.Fatal("accepted invalid snapshot")
			}
			saved, err := s.Catalog(context.Background())
			if err != nil || saved.Version != old.Version {
				t.Fatal("previous snapshot lost")
			}
		})
	}
}

func TestNormalizeChaosPreservesCentralOrderRanksAndMissingValues(t *testing.T) {
	c := Catalog{
		Version: "51:config", UpdatedAt: "2026-11-03T01:00:00Z", Source: "Chaos", RankingAuthority: "chaos",
		Models: []Model{{ID: "21", Name: "First central result", Rank: ptr(7), Score: ptr(1.0)}, {ID: "20", Name: "Second central result", Input: ptr(0.0), Rank: ptr(11), Score: ptr(99.0)}},
		Sites:  []Site{{ID: "10", Name: "First provider", Rank: ptr(4), Score: ptr(2.0)}, {ID: "11", Name: "Second provider", Rank: ptr(8), Score: ptr(200.0)}},
		Offers: []Offer{{ID: "base", ModelID: "20", SiteID: "10", Input: ptr(0.0), Currency: "USD", Unit: "1M tokens"}, {ID: "long-context", ModelID: "20", SiteID: "10", Input: ptr(2.0), Currency: "USD", Unit: "1M tokens", Condition: json.RawMessage(`{"contextWindow":200000}`)}},
	}
	got, err := Normalize(c)
	if err != nil {
		t.Fatal(err)
	}
	if got.Models[0].ID != "21" || *got.Models[0].Rank != 7 || *got.Models[1].Rank != 11 || *got.Models[0].Score != 1 || got.Sites[0].ID != "10" || *got.Sites[0].Rank != 4 || len(got.Offers) != 2 || got.Offers[0].ID != "base" {
		t.Fatalf("central results locally sorted/renumbered or offer plans collapsed: %+v", got)
	}
	if got.Models[0].Input != nil || got.Models[0].Context != nil || got.Models[0].Released != nil || got.Models[1].Input == nil || *got.Models[1].Input != 0 || got.Offers[0].Output != nil || got.Sites[0].Ratio != nil {
		t.Fatalf("unknown observation became a false zero: %+v", got)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := DecodeStored(bytes.NewReader(data))
	if err != nil || reloaded.Models[0].ID != "21" || *reloaded.Models[0].Rank != 7 || reloaded.Models[0].Input != nil {
		t.Fatalf("stored authoritative catalog changed during decode: %+v %v", reloaded, err)
	}
}

func TestStoredChaosCatalogCanExceedSingleImportRequestBudget(t *testing.T) {
	c := Catalog{Version: "1:config", Source: "Chaos", RankingAuthority: "chaos", Models: []Model{{ID: "1", Name: "Complete source model", Description: strings.Repeat("x", MaxCatalogBytes)}}, Sites: []Site{}, Offers: []Offer{}}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(bytes.NewReader(data)); err == nil {
		t.Fatal("admin/import request byte limit changed")
	}
	got, err := DecodeStored(bytes.NewReader(data))
	if err != nil || len(got.Models) != 1 || len(got.Models[0].Description) != MaxCatalogBytes {
		t.Fatalf("complete trusted snapshot was truncated: models=%d err=%v", len(got.Models), err)
	}
}
func TestDecodeStrictAndBounded(t *testing.T) {
	for name, data := range map[string][]byte{
		"trailing":       append(append([]byte{}, demoJSON...), []byte(" {}")...),
		"missing amount": bytes.Replace(demoJSON, []byte(`"input":2,`), nil, 1),
		"null amount":    bytes.Replace(demoJSON, []byte(`"input":2,`), []byte(`"input":null,`), 1),
		"unknown":        bytes.Replace(demoJSON, []byte(`"version":`), []byte(`"extra":1,"version":`), 1),
		"large":          []byte(strings.Repeat(" ", MaxCatalogBytes+1)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(bytes.NewReader(data)); err == nil {
				t.Fatal("accepted malformed snapshot")
			}
		})
	}
}
func TestSeedIsExplicitAndNeverOverwrites(t *testing.T) {
	store := &memoryStore{}
	s := testService(t, store, "")
	if err := s.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Catalog(context.Background()); !errors.Is(err, ErrEmpty) {
		t.Fatal("implicit seed")
	}
	s.cfg.SeedDemo = true
	if err := s.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := fixture(t)
	c.Version = "imported"
	c.Demo = false
	if _, err := s.Import(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if err := s.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted := testService(t, store, "")
	got, err := restarted.Catalog(context.Background())
	if err != nil || got.Version != "imported" || got.Demo {
		t.Fatal("seed overwrote persisted catalog")
	}
}
