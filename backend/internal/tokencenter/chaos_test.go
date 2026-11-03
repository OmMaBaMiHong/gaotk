package tokencenter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func chaosFixturePage(t *testing.T, page int) map[string]any {
	t.Helper()
	config := map[string]any{
		"product": "tokencenter", "domains": []any{map[string]any{"key": "models", "enabled": true, "topics": []any{map[string]any{"key": "intelligence", "rankingSlug": "models-intelligence"}, map[string]any{"key": "gpqa", "rankingSlug": "models-gpqa"}, map[string]any{"key": "providers", "rankingSlug": "providers-model-coverage"}}}},
		"delivery": map[string]any{"subscriptions": []any{map[string]any{"domainKey": "models", "enabled": true, "resources": []string{"materials", "rankings"}}}},
	}
	provider := func(id, authority string) map[string]any {
		kind := "unverified"
		if id == "10" {
			kind = "aggregator"
		}
		return map[string]any{"entityId": id, "key": "provider-" + id, "kind": kind, "name": "Provider " + id, "documentationUrl": "https://provider.example/docs", "apiUrl": "https://provider.example/v1", "sourceUrl": "https://source.example/models", "sourceName": "Source " + id, "sourceAuthority": authority, "observedAt": "2026-11-03T01:00:00Z"}
	}
	model := func(entity, id, key string, cost map[string]any) map[string]any {
		developerID, developerName := "openai", "OpenAI"
		if entity == "21" {
			developerID, developerName = "google", ""
		}
		return map[string]any{"entityId": entity, "id": id, "key": key, "sourceKey": "native-key-" + id, "developerId": developerID, "developerName": developerName, "name": "Model " + entity, "canonicalModelId": "vendor/version-" + entity, "contextWindow": nil, "maxInputTokens": 128000, "maxOutputTokens": 8192, "status": "deprecated", "lastUpdated": "2026-10-31", "modalities": map[string]any{"input": []string{"text", "image"}, "output": []string{"text"}}, "cost": cost, "benchmarks": map[string]any{"artificialAnalysis": map[string]any{"intelligenceIndex": 99.0, "codingIndex": nil, "agenticIndex": nil}}}
	}
	ranking := func(topic string, rows []any, total int) map[string]any {
		return map[string]any{"schemaVersion": 2, "source": "memory", "computedAt": "2026-11-03T02:00:00Z", "snapshot": map[string]any{"id": "ranking-700", "generatedAt": "2026-11-03T02:00:00Z"}, "list": map[string]any{"title": topic + " official title", "slug": "models-" + topic, "unit": "index points"}, "total": total, "items": rows}
	}
	row := func(id string, rank int, score float64) map[string]any {
		return map[string]any{"entityId": id, "name": "Ranked " + id, "rank": rank, "score": score, "factorsJson": map[string]any{"source": "native source measurement"}}
	}
	domain := map[string]any{"key": "models"}
	root := map[string]any{"schemaVersion": 1, "product": "tokencenter", "version": "51:config-abc", "publicationRevision": "51", "config": config, "domains": []any{domain}, "nextCursor": nil}
	if page == 1 {
		root["nextCursor"] = "opaque-page-2"
		domain["materials"] = map[string]any{"schemaVersion": 6, "total": 4, "nextCursor": "material-2", "items": []any{map[string]any{"id": "100", "modelCatalog": map[string]any{"schemaVersion": 1, "provider": provider("10", "first_party"), "models": []any{
			model("20", "vendor/shared", "canonical:vendor/version-20", map[string]any{"input": 0.0}),
			model("20", "vendor/shared-long", "canonical:vendor/version-20", map[string]any{"input": 2.0, "output": 4.0}),
			model("21", "vendor/second", "canonical:vendor/version-21", map[string]any{}),
		}}}}}
		domain["rankings"] = map[string]any{"intelligence": ranking("intelligence", []any{row("21", 7, 1)}, 2), "providers": ranking("providers", []any{row("10", 4, 3)}, 1)}
	} else {
		evaluation := func(provider string) map[string]any {
			return map[string]any{"schemaVersion": 1, "entityId": "20", "source": "openrouter", "benchmarkId": "gpqa_diamond", "canonicalModelId": "vendor/current-alias", "modelPermaslug": "vendor/version-20", "modelName": "Measured model", "providerName": provider, "metric": "accuracy", "score": 0.875, "scoreUnit": "fraction", "scoreStdDev": nil, "runCount": 2, "totalTasks": 396, "measuredAt": "2026-11-02T21:00:00Z", "observedAt": "2026-11-03T01:00:00Z", "sourceUrl": "https://openrouter.ai/benchmarks/gpqa-diamond", "publication": map[string]any{"snapshotDate": "2026-11-03", "dataset": "benchmark-leaderboard", "dataUrl": "https://source.example/2026-11-03.json", "sha256": strings.Repeat("a", 64), "license": map[string]any{"name": "CC BY 4.0", "url": "https://creativecommons.org/licenses/by/4.0/"}, "attribution": "Source OpenRouter", "methodology": "Task-weighted provider result"}}
		}
		community := model("20", "reseller/shared", "canonical:vendor/version-20", map[string]any{"input": 2.0, "output": 3.0, "tiers": []any{map[string]any{"input": 4.0, "output": 5.0, "condition": map[string]any{"context": "over 200k"}, "sourceMetadata": map[string]any{"input": 4, "output": 5, "context": "over 200k"}}}})
		domain["materials"] = map[string]any{"schemaVersion": 6, "total": 4, "nextCursor": nil, "items": []any{map[string]any{"id": "101", "modelBenchmark": evaluation("auto-routing")}, map[string]any{"id": "102", "modelBenchmark": evaluation("Provider B")}, map[string]any{"id": "103", "modelCatalog": map[string]any{"schemaVersion": 1, "provider": provider("11", "secondary"), "models": []any{community}}}}}
		domain["rankings"] = map[string]any{"intelligence": ranking("intelligence", []any{row("20", 11, 99)}, 2), "gpqa": ranking("gpqa", []any{row("20", 2, 87.5)}, 1)}
	}
	return root
}

func chaosService(t *testing.T, store Store, server *httptest.Server) *Service {
	t.Helper()
	s, err := NewService(store, Config{SourceURL: server.URL + "/public/v1/products/tokencenter/snapshot?resource=materials&resource=rankings&limit=1", SourceFormat: "chaos", SourceToken: "fixture-source-token", Timeout: time.Second, Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	s.client = server.Client()
	return s
}

func TestChaosSyncConsumesEveryPageAndPreservesAuthoritativeBoards(t *testing.T) {
	store := &memoryStore{}
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer fixture-source-token" || r.Header.Get("Accept") != "application/json" || len(r.URL.Query()["resource"]) != 2 || r.URL.Query().Get("limit") != "1" {
			t.Errorf("source headers or query selection lost: %v %s", r.Header, r.URL.RawQuery)
		}
		page := 1
		if cursor := r.URL.Query().Get("cursor"); cursor != "" {
			if cursor != "opaque-page-2" || r.Header.Get("If-None-Match") != "" {
				t.Errorf("cursor or per-page validators incorrect: %q %v", cursor, r.Header)
			}
			if _, err := store.Load(context.Background()); err != ErrEmpty {
				t.Error("partial catalog saved before last page")
			}
			page = 2
		}
		w.Header().Set("ETag", `"publication-51"`)
		w.Header().Set("Last-Modified", "Tue, 03 Nov 2026 02:00:00 GMT")
		json.NewEncoder(w).Encode(chaosFixturePage(t, page))
	}))
	defer server.Close()
	c, err := chaosService(t, store, server).Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || c.RankingAuthority != "chaos" || c.Demo || c.Version != "51:config-abc" || c.SourceSnapshot == nil || c.SourceSnapshot.PublicationRevision != "51" || c.SourceSnapshot.ConfigRevision != "config-abc" || len(c.Models) != 2 || len(c.Sites) != 2 || len(c.Offers) != 5 {
		t.Fatalf("incomplete authoritative snapshot: requests=%d catalog=%+v", requests, c)
	}
	if c.Models[0].ID != "21" || c.Models[0].Rank == nil || *c.Models[0].Rank != 7 || c.Models[1].ID != "20" || *c.Models[1].Rank != 11 || *c.Models[1].Score != 99 {
		t.Fatalf("central rank/entry order overwritten by local score sort: %+v", c.Models)
	}
	m := c.Models[1]
	if m.Input == nil || *m.Input != 0 || m.Output != nil || m.Context != nil || m.Released != nil || m.SourceAuthority != "first_party" || len(m.Evaluations) != 2 || m.Evaluations[0].ModelPermaslug != "vendor/version-20" || m.Evaluations[0].Score == nil || *m.Evaluations[0].Score != 0.875 {
		t.Fatalf("unknown/zero/first-party source or provider evidence lost: %+v", m)
	}
	if m.Provider != "OpenAI" || m.DeveloperID != "openai" || m.DeveloperName != "OpenAI" || m.Family != "openai" || m.SourceKey != "native-key-vendor/shared" || m.Status != "deprecated" || m.LastUpdated == nil || *m.LastUpdated != "2026-10-31" || m.MaxInputTokens == nil || *m.MaxInputTokens != 128000 || m.MaxOutputTokens == nil || *m.MaxOutputTokens != 8192 || m.Modalities == nil || len(m.Modalities.Input) != 2 || m.Modalities.Input[1] != "image" || c.Models[0].Provider != "google" || c.Models[0].Family != "google" {
		t.Fatalf("source-declared developer/capabilities were replaced by supplier or fictional tags: %+v", c.Models)
	}
	intel := c.Rankings["intelligence"]
	if intel.Title != "intelligence official title" || intel.Slug != "models-intelligence" || intel.Version != "ranking-700" || intel.AsOf != "2026-11-03T02:00:00Z" || intel.Unit != "index points" || intel.Source != "memory" || len(intel.Entries) != 2 || intel.Entries[0].EntityID != "21" || *intel.Entries[0].Rank != 7 || !strings.Contains(string(intel.Entries[0].Metadata), "native source measurement") {
		t.Fatalf("published ranking metadata or full source entries lost: %+v", intel)
	}
	if c.Sites[0].ID != "10" || c.Sites[0].Rank == nil || *c.Sites[0].Rank != 4 || c.Sites[1].Rank != nil || c.Sites[1].Score != nil || c.Sites[1].Ratio != nil || c.Sites[0].ProviderKey != "provider-10" || c.Sites[0].ProviderKind != "aggregator" || c.Sites[1].ProviderKind != "unverified" {
		t.Fatalf("unknown provider metrics/rank fabricated: %+v", c.Sites)
	}
	var plan, tier bool
	for _, o := range c.Offers {
		if o.SourceModelID == "vendor/shared-long" {
			plan = true
		}
		if strings.Contains(string(o.Condition), "over 200k") {
			tier = true
		}
	}
	if !plan || !tier {
		t.Fatalf("actual provider model IDs/price conditions lost: %+v", c.Offers)
	}
	saved, err := store.Load(context.Background())
	if err != nil || saved.ETag != `"publication-51"` || saved.Catalog.Models[0].ID != "21" {
		t.Fatalf("atomic catalog/conditional metadata not saved: %+v %v", saved, err)
	}
}

func TestChaosSyncFailuresRetainPriorSnapshotAndValidators(t *testing.T) {
	for _, failure := range []string{"publication", "version", "config", "ranking-version", "ranking-total", "repeated-cursor", "schema", "material-schema", "304", "401", "404"} {
		t.Run(failure, func(t *testing.T) {
			store := &memoryStore{}
			previous := fixture(t)
			previous.Version = "last-success"
			store.Save(context.Background(), Snapshot{Catalog: previous, ETag: `"last-good"`, SourceURL: "https://prior.example/snapshot"}, false)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page := 1
				if r.URL.Query().Get("cursor") != "" {
					page = 2
				}
				p := chaosFixturePage(t, page)
				if page == 2 {
					switch failure {
					case "publication":
						p["publicationRevision"] = "52"
					case "version":
						p["version"] = "52:config-abc"
					case "config":
						p["config"].(map[string]any)["product"] = "different-product"
					case "ranking-version":
						p["domains"].([]any)[0].(map[string]any)["rankings"].(map[string]any)["intelligence"].(map[string]any)["snapshot"] = map[string]any{"id": "ranking-701"}
					case "ranking-total":
						p["domains"].([]any)[0].(map[string]any)["rankings"].(map[string]any)["intelligence"].(map[string]any)["total"] = 9
					case "repeated-cursor":
						p["nextCursor"] = "opaque-page-2"
					case "schema":
						p["schemaVersion"] = 9
					case "material-schema":
						p["domains"].([]any)[0].(map[string]any)["materials"].(map[string]any)["items"] = "changed shape"
					case "401":
						w.WriteHeader(http.StatusUnauthorized)
						return
					case "304":
						w.WriteHeader(http.StatusNotModified)
						return
					case "404":
						w.WriteHeader(http.StatusNotFound)
						return
					}
				}
				w.Header().Set("ETag", `"bad-page"`)
				json.NewEncoder(w).Encode(p)
			}))
			defer server.Close()
			s := chaosService(t, store, server)
			if _, err := s.Sync(context.Background()); err == nil {
				t.Fatal("accepted incomplete or mixed source publication")
			}
			saved, err := store.Load(context.Background())
			if err != nil || saved.Catalog.Version != "last-success" || saved.ETag != `"last-good"` {
				t.Fatalf("failed source fetch changed prior catalog/validators: %+v %v", saved, err)
			}
		})
	}
}

func TestChaosConfiguredProductCannotBeReplacedByAnotherProduct(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := chaosFixturePage(t, 1)
		page["product"] = "different-product"
		page["config"].(map[string]any)["product"] = "different-product"
		json.NewEncoder(w).Encode(page)
	}))
	defer server.Close()
	if _, err := chaosService(t, &memoryStore{}, server).Sync(context.Background()); err == nil || !strings.Contains(err.Error(), "product") {
		t.Fatalf("configured product endpoint accepted another product: %v", err)
	}
}

func TestChaosBenchmarkBeforeCatalogKeepsExactVersionAndEntityIdentity(t *testing.T) {
	first, second := chaosFixturePage(t, 1), chaosFixturePage(t, 2)
	data, _ := json.Marshal(first)
	page, err := decodeChaosPage(data)
	if err != nil {
		t.Fatal(err)
	}
	var accumulator chaosAccumulator
	if err := accumulator.initialize(page); err != nil {
		t.Fatal(err)
	}
	evaluation := second["domains"].([]any)[0].(map[string]any)["materials"].(map[string]any)["items"].([]any)[0].(map[string]any)["modelBenchmark"].(map[string]any)
	benchmarkJSON, _ := json.Marshal(evaluation)
	if err := accumulator.appendEvaluation(benchmarkJSON); err != nil {
		t.Fatal(err)
	}
	provider := first["domains"].([]any)[0].(map[string]any)["materials"].(map[string]any)["items"].([]any)[0].(map[string]any)["modelCatalog"]
	providerJSON, _ := json.Marshal(provider)
	if err := accumulator.appendProvider(providerJSON); err != nil {
		t.Fatal(err)
	}
	model := accumulator.catalog.Models[accumulator.models["20"]]
	if model.ModelPermaslug != "vendor/version-20" || len(model.Evaluations) != 1 || model.CanonicalModelID != "vendor/version-20" || model.SourceAuthority != "first_party" {
		t.Fatalf("later catalog lost measured version/evidence: %+v", model)
	}
	// A similar name/current alias cannot override the central owning entity ID.
	evaluation["entityId"] = "22"
	evaluation["modelName"] = "OpenAI: Historical model"
	evaluation["modelPermaslug"] = "openai/historical-20250101"
	evaluation["canonicalModelId"] = "openai/current-alias"
	benchmarkJSON, _ = json.Marshal(evaluation)
	if err := accumulator.appendEvaluation(benchmarkJSON); err != nil {
		t.Fatal(err)
	}
	if len(accumulator.catalog.Models) != 3 || accumulator.catalog.Models[accumulator.models["22"]].Input != nil {
		t.Fatal("benchmark-only central entity was fuzzy-merged or given a price")
	}
	if model := accumulator.catalog.Models[accumulator.models["22"]]; model.DeveloperID != "openai" || model.DeveloperName != "OpenAI" || model.Provider != "OpenAI" || model.Family != "openai" {
		t.Fatalf("benchmark-only source developer was lost from brand filters: %+v", model)
	}
}

func TestChaosRankingUnitUsesPublishedScoreColumn(t *testing.T) {
	root := chaosFixturePage(t, 1)
	domain := root["domains"].([]any)[0].(map[string]any)
	board := domain["rankings"].(map[string]any)["intelligence"].(map[string]any)
	list := board["list"].(map[string]any)
	delete(list, "unit")
	list["columns"] = []any{map[string]any{"key": "name", "label": "Model"}, map[string]any{"key": "score", "label": "AA Intelligence Index", "unit": "index", "metricCode": "model_aa_intelligence_index"}}
	data, _ := json.Marshal(root)
	page, err := decodeChaosPage(data)
	if err != nil {
		t.Fatal(err)
	}
	var accumulator chaosAccumulator
	if err := accumulator.initialize(page); err != nil {
		t.Fatal(err)
	}
	if err := accumulator.append(page); err != nil {
		t.Fatal(err)
	}
	if got := accumulator.boards["intelligence"].ranking; got.Unit != "index" || !strings.Contains(string(got.Metadata), "model_aa_intelligence_index") {
		t.Fatalf("central score unit/column provenance lost: %+v", got)
	}
}

func TestChaosOfficialSubscriptionsPreserveQuotedAmountsAndEvidence(t *testing.T) {
	root := chaosFixturePage(t, 1)
	root["nextCursor"] = nil
	domain := root["domains"].([]any)[0].(map[string]any)
	delete(domain, "rankings")
	plans := []any{}
	for i, price := range []any{0.0, nil, 240.0} {
		record := map[string]any{
			"schemaVersion": 1, "entityId": string(rune('1' + i)), "vendor": "Fixture Vendor", "planID": []string{"free", "enterprise", "annual"}[i], "planName": []string{"Free", "Enterprise", "Annual"}[i],
			"price": price, "currency": "USD", "billingInterval": []string{"none", "none", "year"}[i], "annualTotal": nil, "monthlyEquivalent": nil, "billingUnit": "seat", "region": "US", "conditions": []string{"Official source condition"},
			"evidence": map[string]any{"sourceURL": "https://vendor.example/pricing", "capturedAt": "2026-11-03T00:30:00Z", "bodySHA256": strings.Repeat("a", 64), "quote": "Fixture official quoted price", "quoteSHA256": strings.Repeat("b", 64)},
		}
		if i == 2 {
			record["annualTotal"], record["monthlyEquivalent"] = 240.0, 20.0
		}
		plans = append(plans, map[string]any{"id": []string{"s1", "s2", "s3"}[i], "officialSubscription": record})
	}
	domain["materials"] = map[string]any{"schemaVersion": 6, "total": 3, "items": plans}
	data, _ := json.Marshal(root)
	page, err := decodeChaosPage(data)
	if err != nil {
		t.Fatal(err)
	}
	var accumulator chaosAccumulator
	if err := accumulator.initialize(page); err != nil {
		t.Fatal(err)
	}
	if err := accumulator.append(page); err != nil {
		t.Fatal(err)
	}
	catalog, err := accumulator.finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.OfficialSubscriptions) != 3 {
		t.Fatalf("official subscription facts lost: %+v", catalog)
	}
	free, unknown, annual := catalog.OfficialSubscriptions[0], catalog.OfficialSubscriptions[1], catalog.OfficialSubscriptions[2]
	if free.Price == nil || *free.Price != 0 || unknown.Price != nil || unknown.AnnualTotal != nil || unknown.MonthlyEquivalent != nil || annual.Price == nil || *annual.Price != 240 || annual.AnnualTotal == nil || *annual.AnnualTotal != 240 || annual.MonthlyEquivalent == nil || *annual.MonthlyEquivalent != 20 || annual.BillingInterval != "year" || annual.BillingUnit != "seat" {
		t.Fatalf("quoted zero/unknown/annual amounts or billing conditions changed: %+v", catalog.OfficialSubscriptions)
	}
	if annual.Evidence.SourceURL != "https://vendor.example/pricing" || annual.Evidence.CapturedAt == nil || *annual.Evidence.CapturedAt != "2026-11-03T00:30:00Z" || annual.Evidence.Quote != "Fixture official quoted price" || annual.Evidence.QuoteSHA256 != strings.Repeat("b", 64) || catalog.UpdatedAt != "2026-11-03T00:30:00Z" {
		t.Fatalf("official quoted evidence or capture time changed: %+v", annual)
	}
	encoded, _ := json.Marshal(catalog)
	reloaded, err := DecodeStored(strings.NewReader(string(encoded)))
	if err != nil || len(reloaded.OfficialSubscriptions) != 3 || !strings.Contains(string(encoded), `"planId":"annual"`) || !strings.Contains(string(encoded), `"sourceUrl":"https://vendor.example/pricing"`) {
		t.Fatalf("official subscription persistence/API casing changed: %s %v", encoded, err)
	}
}

func TestChaosConditionalValidatorsAreReusableOnlyAfterCompleteSuccess(t *testing.T) {
	store := &memoryStore{}
	conditional := false
	mode, requests := 0, 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if mode == 1 {
			conditional = r.Header.Get("If-None-Match") == `"complete-51"`
			w.WriteHeader(http.StatusNotModified)
			return
		}
		page := 1
		if r.URL.Query().Get("cursor") != "" {
			page = 2
		}
		w.Header().Set("ETag", `"complete-51"`)
		json.NewEncoder(w).Encode(chaosFixturePage(t, page))
	}))
	defer server.Close()
	s := chaosService(t, store, server)
	if _, err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	mode = 1
	s = chaosService(t, store, server)
	c, err := s.Sync(context.Background())
	if err != nil || !conditional || requests != 3 || c.Version != "51:config-abc" {
		t.Fatalf("complete persisted validators not reused: requests=%d conditional=%t catalog=%+v err=%v", requests, conditional, c, err)
	}
	if _, err := s.Import(context.Background(), fixture(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sync(context.Background()); err == nil {
		t.Fatal("304 accepted after manual import invalidated the source publication")
	}
}

func TestChaosDeveloperFallbackUsesOnlyPublishedNameOrCanonicalNamespace(t *testing.T) {
	for _, tc := range []struct{ providerKey, name, canonical, developerID, developerName, wantID, wantName, wantProvider string }{
		{"openrouter", "OpenAI: Legacy model", "openai/version-1", "", "", "openai", "OpenAI", "OpenAI"},
		{"openrouter", "Plain source model", "anthropic/version-1", "", "", "anthropic", "", "anthropic"},
		{"reseller", "Unrelated label: Source model", "", "", "", "", "", "Actual source provider"},
		{"openrouter", "Different prefix: Model", "openai/version-1", "declared-id", "Declared developer", "declared-id", "Declared developer", "Declared developer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := chaosFixturePage(t, 1)
			encoded, _ := json.Marshal(root)
			page, err := decodeChaosPage(encoded)
			if err != nil {
				t.Fatal(err)
			}
			var accumulator chaosAccumulator
			if err := accumulator.initialize(page); err != nil {
				t.Fatal(err)
			}
			provider := map[string]any{"schemaVersion": 1, "provider": map[string]any{"entityId": "10", "key": tc.providerKey, "name": "Actual source provider", "sourceAuthority": "first_party"}, "models": []any{map[string]any{"entityId": "20", "key": "stable-key", "id": "actual-id", "name": tc.name, "canonicalModelId": tc.canonical, "developerId": tc.developerID, "developerName": tc.developerName, "cost": map[string]any{}}}}
			data, _ := json.Marshal(provider)
			if err := accumulator.appendProvider(data); err != nil {
				t.Fatal(err)
			}
			model := accumulator.catalog.Models[0]
			if model.DeveloperID != tc.wantID || model.DeveloperName != tc.wantName || model.Provider != tc.wantProvider {
				t.Fatalf("developer identity guessed or explicit source fields lost: %+v", model)
			}
		})
	}
}

func TestChaosNativeBillingDoesNotBecomeAnInventedTokenPrice(t *testing.T) {
	root := chaosFixturePage(t, 1)
	encoded, _ := json.Marshal(root)
	page, err := decodeChaosPage(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var accumulator chaosAccumulator
	if err := accumulator.initialize(page); err != nil {
		t.Fatal(err)
	}
	billing := []map[string]any{{"unit": "request", "requestPriceUsd": 0.002, "expressions": map[string]any{"input": "basePrice * groupMultiplier"}}, {"unit": "request", "requestPriceUsd": 0.0, "expressions": map[string]any{"input": "unknown token expression"}}}
	models := []any{}
	for i, native := range billing {
		models = append(models, map[string]any{"entityId": []string{"20", "21"}[i], "key": []string{"request-paid", "request-zero"}[i], "id": []string{"native-paid", "native-zero"}[i], "name": "Native per-request offering", "cost": map[string]any{}, "billing": native})
	}
	provider := map[string]any{"schemaVersion": 1, "provider": map[string]any{"entityId": "10", "key": "newapi", "kind": "aggregator", "name": "Published API source", "sourceAuthority": "first_party"}, "models": models}
	data, _ := json.Marshal(provider)
	if err := accumulator.appendProvider(data); err != nil {
		t.Fatal(err)
	}
	catalog, err := accumulator.finish()
	if err != nil {
		t.Fatal(err)
	}
	for i, model := range catalog.Models {
		offer := catalog.Offers[i]
		want, _ := json.Marshal(billing[i])
		if model.Input != nil || model.Output != nil || offer.Input != nil || offer.Output != nil || string(model.Billing) != string(want) || string(offer.Billing) != string(want) || offer.Currency != "USD" || offer.Unit != "1M tokens" {
			t.Fatalf("native billing was dropped or converted into token price: model=%+v offer=%+v", model, offer)
		}
	}
	encoded, _ = json.Marshal(catalog)
	reloaded, err := DecodeStored(strings.NewReader(string(encoded)))
	if err != nil || string(reloaded.Models[0].Billing) != string(catalog.Models[0].Billing) || string(reloaded.Offers[1].Billing) != string(catalog.Offers[1].Billing) {
		t.Fatalf("native billing did not persist verbatim: %v", err)
	}
}

func TestChaosSourceFormatConfigurationIsExplicit(t *testing.T) {
	t.Setenv("TOKENCENTER_SOURCE_FORMAT", "chaos")
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.SourceFormat != "chaos" {
		t.Fatalf("chaos source format not read: %+v %v", cfg, err)
	}
	t.Setenv("TOKENCENTER_SOURCE_FORMAT", "")
	cfg, err = ConfigFromEnv()
	if err != nil || cfg.SourceFormat != "catalog" {
		t.Fatalf("legacy default changed: %+v %v", cfg, err)
	}
	t.Setenv("TOKENCENTER_SOURCE_FORMAT", "unknown-source-format")
	if _, err = ConfigFromEnv(); err == nil {
		t.Fatal("unknown format silently fell back to legacy catalog")
	}
}
