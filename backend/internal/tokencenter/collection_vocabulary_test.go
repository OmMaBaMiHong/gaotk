package tokencenter

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCollectionVocabularyProjectsChaosContract(t *testing.T) {
	if bytes.Contains(collectionVocabularyContractJSON, []byte(`"id"`)) {
		t.Fatal("embedded contract hardcodes numeric Tag ids")
	}
	vocab, err := projectCollectionVocabulary(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := parseCollectionContract(collectionVocabularyContractJSON)
	if err != nil {
		t.Fatal(err)
	}
	if vocab.Dictionary != "Tag" || vocab.Dimension != "navigation" || vocab.StableIdentity != "slug" || vocab.Version != "2026-11-11" || vocab.Revision != contract.Version || vocab.ContractCommit != collectionVocabularyContractCommit || vocab.ContractFile != collectionVocabularyContractFile || vocab.ProposalFile != contract.ProposalFile {
		t.Fatalf("vocabulary identity drifted: %+v", vocab)
	}
	if !slices.Equal(vocab.BillingUnits, []string{"traffic", "ip", "gpu_hour"}) || !slices.Equal(vocab.PurchaseSurfaces, []string{"catalog", "none", "forbidden"}) || !slices.Equal(vocab.ApplicableKinds, []string{"product_surface"}) {
		t.Fatalf("contract enums drifted: %+v", vocab)
	}
	wantNav := []string{"tc-nav-home", "tc-nav-models", "tc-nav-relay", "tc-nav-official", "tc-nav-compute", "tc-nav-hardware", "tc-nav-vpn", "tc-nav-guide"}
	if !slices.Equal(vocab.TopNav, wantNav) || !slices.Equal(vocab.TopNav, contract.TopNav) {
		t.Fatalf("topNav = %v", vocab.TopNav)
	}
	public := make([]collectionContractRow, 0, len(contract.Entries))
	for _, entry := range contract.Entries {
		if publicGovernance(entry.GovernanceStatus) {
			public = append(public, entry)
			continue
		}
		for _, term := range vocab.Entries {
			if term.Slug == entry.Slug {
				t.Fatalf("draft %s is in the public dictionary", entry.Slug)
			}
		}
		if slices.Contains(vocab.TopNav, entry.Slug) {
			t.Fatalf("draft %s is in topNav", entry.Slug)
		}
	}
	if len(vocab.Entries) != len(public) {
		t.Fatalf("public entries = %d, contract public rows = %d", len(vocab.Entries), len(public))
	}
	encoded, err := json.Marshal(vocab)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"id"`)) {
		t.Fatal("unpublished vocabulary emitted numeric ids")
	}
	by := map[string]CollectionTerm{}
	for i, entry := range public {
		term := vocab.Entries[i]
		by[term.Slug] = term
		if term.ID != "" || term.Slug != entry.Slug || term.Name != entry.Name || term.Dimension != "navigation" || term.Revision != contract.Version || term.GovernanceStatus != entry.GovernanceStatus || term.Surface != entry.Surface || term.NavOrder != entry.NavOrder || term.BoardRule != entry.BoardRule || term.PurchaseSurface != entry.PurchaseSurface || !slices.Equal(term.MemberSlugs, entry.MemberSlugs) || !slices.Equal(term.ExternalFilterSlugs, entry.ExternalFilterSlugs) || !sameStringPtr(term.ParentSlug, entry.ParentSlug) || !sameStringPtr(term.BillingUnit, entry.BillingUnit) {
			t.Fatalf("projection diverged for %s: %+v", entry.Slug, term)
		}
		for _, unit := range vocab.BillingUnits {
			if term.Slug == unit {
				t.Fatalf("billing unit %s was projected as a tag", unit)
			}
		}
	}
	for _, slug := range []string{"text", "multimodal", "video", "ai-hub--relay", "ai-lab", "opensource", "ai-app--agent"} {
		if _, exists := by[slug]; exists {
			t.Fatalf("existing concept %s was rewritten into a navigation tag", slug)
		}
	}
	models := by["tc-nav-models"]
	if models.Name != "模型榜" || !slices.Equal(models.MemberSlugs, []string{"tc-modality-image", "tc-modality-video", "tc-modality-audio"}) || !slices.Equal(models.ExternalFilterSlugs, []string{"text", "multimodal"}) {
		t.Fatalf("model board filters drifted: %+v", models)
	}
	for _, slug := range []string{"tc-modality-image", "tc-modality-video", "tc-modality-audio"} {
		if by[slug].ParentSlug == nil || *by[slug].ParentSlug != "tc-nav-models" || by[slug].BoardRule != "modality_filter" {
			t.Fatalf("modality %s is not under the model board: %+v", slug, by[slug])
		}
	}
	if by["tc-relay-api"].Name != "API" || by["tc-official-api"].Name != "API" || by["tc-relay-api"].Slug == by["tc-official-api"].Slug {
		t.Fatal("API display names are no longer distinguishable by slug")
	}
	compute := by["tc-nav-compute"]
	if compute.Name != "算力" || !slices.Equal(compute.MemberSlugs, []string{"tc-compute-gpu-hour"}) {
		t.Fatalf("compute main board drifted: %+v", compute)
	}
	if by["tc-compute-gpu-hour"].BillingUnit == nil || *by["tc-compute-gpu-hour"].BillingUnit != "gpu_hour" || by["tc-compute-gpu-hour"].BoardRule != "compute_main_gpu_hour" {
		t.Fatalf("GPU hour leaf drifted: %+v", by["tc-compute-gpu-hour"])
	}
	exit := by["tc-compute-machine-exit"]
	if exit.Name != "租机/出口" || exit.ParentSlug == nil || *exit.ParentSlug != "tc-nav-compute" || slices.Contains(compute.MemberSlugs, exit.Slug) || exit.BoardRule != "compute_sub_board" {
		t.Fatalf("machine-exit sub-board drifted: %+v", exit)
	}
	for _, slug := range []string{"tc-compute-machine-plus-account", "tc-compute-account-only"} {
		if slices.Contains(compute.MemberSlugs, slug) || by[slug].BoardRule != "compute_excluded_from_main" || by[slug].PurchaseSurface != "none" {
			t.Fatalf("%s returned to the compute main board: %+v", slug, by[slug])
		}
	}
	vpn := by["tc-nav-vpn"]
	if !slices.Equal(vpn.MemberSlugs, []string{"tc-vpn-rotating-residential", "tc-vpn-static-isp", "tc-vpn-static-residential", "tc-vpn-datacenter"}) {
		t.Fatalf("vpn leaves drifted: %+v", vpn.MemberSlugs)
	}
	if by["tc-vpn-rotating-residential"].BillingUnit == nil || *by["tc-vpn-rotating-residential"].BillingUnit != "traffic" || by["tc-vpn-rotating-residential"].BoardRule != "vpn_traffic" {
		t.Fatalf("rotating residential drifted: %+v", by["tc-vpn-rotating-residential"])
	}
	for _, slug := range []string{"tc-vpn-static-isp", "tc-vpn-static-residential"} {
		if by[slug].BillingUnit == nil || *by[slug].BillingUnit != "ip" || by[slug].BoardRule != "vpn_ip" {
			t.Fatalf("%s drifted: %+v", slug, by[slug])
		}
	}
	datacenter := by["tc-vpn-datacenter"]
	if datacenter.BillingUnit != nil || datacenter.BoardRule != "vpn_datacenter_separate" || datacenter.BoardRule == by["tc-vpn-rotating-residential"].BoardRule || datacenter.BoardRule == by["tc-vpn-static-isp"].BoardRule {
		t.Fatalf("datacenter leaf was mixed into traffic or ip: %+v", datacenter)
	}
	for _, slug := range []string{"tc-excluded-finished-account", "tc-excluded-account-opening", "tc-excluded-sms-farming", "tc-excluded-social-account"} {
		if by[slug].PurchaseSurface != "forbidden" || len(by[slug].MemberSlugs) != 0 || slices.Contains(vocab.TopNav, slug) {
			t.Fatalf("forbidden purchase slug drifted: %+v", by[slug])
		}
	}
	for _, slug := range []string{"tc-reserved", "tc-reserved-open-weights", "tc-reserved-training", "tc-reserved-dataset", "tc-reserved-agent-deploy"} {
		if _, exists := by[slug]; exists || slices.Contains(vocab.TopNav, slug) || !bytes.Contains(collectionVocabularyContractJSON, []byte(`"`+slug+`"`)) {
			t.Fatalf("reserved slug %s left the contract or entered the public dictionary", slug)
		}
	}
}

func TestLegacyGovernanceStaysPublicAndDraftsStayOut(t *testing.T) {
	vocab, err := projectContractData([]byte(`{
		"version":"test","dictionary":"Tag","dimension":"navigation","stableIdentity":"slug",
		"numericId":"assigned by the database","proposalFile":"docs/standards/tokencenter-collection-vocabulary-v1.json",
		"applicableKinds":["product_surface"],"billingUnits":["traffic"],"purchaseSurfaces":["none"],
		"topNav":["keep-public"],
		"entries":[
			{"slug":"keep-public","name":"Public","parentSlug":null,"governanceStatus":"active","surface":"top_nav","navOrder":1,"boardRule":"route_only","billingUnit":null,"purchaseSurface":"none","memberSlugs":[],"externalFilterSlugs":[]},
			{"slug":"keep-legacy","name":"Legacy","parentSlug":"keep-public","governanceStatus":"legacy","surface":"in_board","navOrder":0,"boardRule":"group","billingUnit":null,"purchaseSurface":"none","memberSlugs":[],"externalFilterSlugs":[]},
			{"slug":"drop-draft","name":"Draft","parentSlug":null,"governanceStatus":"draft","surface":"reserved","navOrder":0,"boardRule":"reserved_no_nav","billingUnit":null,"purchaseSurface":"none","memberSlugs":[],"externalFilterSlugs":[]}
		]
	}`), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if vocab.TopNav[0] != "keep-public" || len(vocab.Entries) != 2 || vocab.Entries[1].Slug != "keep-legacy" || vocab.Entries[1].GovernanceStatus != "legacy" {
		t.Fatalf("legacy row dropped or draft published: %+v", vocab.Entries)
	}
}

func TestPublishedDictionaryUsesGovernedNamesAndDecimalIDs(t *testing.T) {
	revision, tags, ok := parsePublishedDictionary([]byte(`{
		"dictionary":"Tag","dimension":"navigation","revision":"9",
		"tags":[
			{"id":"42","slug":"tc-nav-home","name":"不是首页","dimension":"navigation","revision":7,"governanceStatus":"active"},
			{"tagId":"15","slug":"tc-nav-guide","revision":2,"status":"active"},
			{"id":99,"slug":"not-in-contract","name":"发明","dimension":"navigation","revision":1,"governanceStatus":"active"},
			{"id":"5","slug":"tc-reserved-training","revision":1,"governanceStatus":"draft"},
			{"id":8,"slug":"tc-nav-models","name":"模型榜被改写","dimension":"navigation","revision":3,"governanceStatus":"legacy"},
			{"id":"7","slug":"tc-nav-vpn","dimension":"subject_type","revision":1,"governanceStatus":"active"},
			{"id":"0","slug":"tc-nav-relay","revision":1,"governanceStatus":"active"},
			{"id":"042","slug":"tc-nav-official","revision":1,"governanceStatus":"active"}
		]
	}`))
	if !ok || revision != "9" || tags["tc-nav-home"].ID != "42" || tags["tc-nav-home"].Revision != "7" || tags["tc-nav-guide"].ID != "15" || tags["tc-nav-models"].ID != "8" {
		t.Fatalf("published ids not read: ok=%v rev=%s tags=%v", ok, revision, tags)
	}
	for _, slug := range []string{"not-in-contract", "tc-reserved-training", "tc-nav-vpn", "tc-nav-relay", "tc-nav-official"} {
		if _, exists := tags[slug]; exists {
			t.Fatalf("rejected published row kept: %s", slug)
		}
	}
	if _, _, ok = parsePublishedDictionary([]byte(`{"product":"tokencenter"}`)); ok {
		t.Fatal("snapshot payload recognized as a tag dictionary")
	}
	vocab, err := projectCollectionVocabulary(tags, revision)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]CollectionTerm{}
	for _, term := range vocab.Entries {
		by[term.Slug] = term
		if term.Name == "发明" || term.Slug == "not-in-contract" {
			t.Fatalf("published dictionary renamed or extended the contract: %+v", term)
		}
	}
	if by["tc-nav-home"].ID != "42" || by["tc-nav-home"].Name != "不是首页" || by["tc-nav-home"].Revision != "7" || by["tc-nav-models"].ID != "8" || by["tc-nav-models"].Name != "模型榜被改写" || by["tc-nav-models"].GovernanceStatus != "active" || by["tc-nav-guide"].ID != "15" || by["tc-nav-vpn"].ID != "" || by["tc-nav-relay"].Revision != "2026-11-11" || vocab.Revision != "9" || vocab.Version != "2026-11-11" {
		t.Fatalf("published name/id overlay mismatch: home=%+v models=%+v guide=%+v vpn=%+v", by["tc-nav-home"], by["tc-nav-models"], by["tc-nav-guide"], by["tc-nav-vpn"])
	}
}

func TestCatalogCarriesVocabularyWhenListingsAreEmpty(t *testing.T) {
	store := &memoryStore{}
	service := testService(t, store, "")
	empty := Catalog{Version: "empty-listings", UpdatedAt: "2026-11-11T00:00:00Z", Source: "fixture", Models: []Model{}, Sites: []Site{}, Offers: []Offer{}}
	got, err := service.Import(context.Background(), empty)
	if err != nil || len(got.Models) != 0 || len(got.Sites) != 0 || len(got.Offers) != 0 || got.CollectionVocabulary == nil || !slices.Equal(got.CollectionVocabulary.TopNav, []string{"tc-nav-home", "tc-nav-models", "tc-nav-relay", "tc-nav-official", "tc-nav-compute", "tc-nav-hardware", "tc-nav-vpn", "tc-nav-guide"}) {
		t.Fatalf("empty listings dropped the vocabulary: %+v %v", got.CollectionVocabulary, err)
	}
	if bytes.Contains(store.data, []byte("collectionVocabulary")) || bytes.Contains(store.data, []byte("tc-nav-home")) {
		t.Fatalf("vocabulary was stored in the catalog snapshot: %s", store.data)
	}
	reloaded, err := service.Catalog(context.Background())
	if err != nil || reloaded.CollectionVocabulary == nil || reloaded.CollectionVocabulary.Entries[0].Slug == "" {
		t.Fatal(err)
	}
}

func TestImportIgnoresClientVocabularyAndPreservesListingTags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &memoryStore{}
	service := testService(t, store, "")
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), router.Group("/api/v1/admin", DevTokenAuth("test-admin")), service)
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
	if got := request("POST", "/api/v1/admin/tokencenter/import", "test-admin", demoJSON); got.Code != 200 || !bytes.Contains(got.Body.Bytes(), []byte(`"collectionVocabulary"`)) {
		t.Fatalf("import response missing vocabulary: %d %s", got.Code, got.Body.Bytes())
	}
	var imported Catalog
	if err := json.Unmarshal(store.data, &imported); err != nil || imported.CollectionVocabulary != nil || imported.Models[0].Tags[1] != "chat" || imported.Sites[0].Tags[0] != "demo" {
		t.Fatalf("stored demo tags changed: %s", store.data)
	}
	got := request("GET", "/api/v1/tokencenter/catalog", "", nil)
	var catalog Catalog
	if err := json.Unmarshal(got.Body.Bytes(), &catalog); err != nil || !catalog.Demo || len(catalog.Offers) != 5 || catalog.CollectionVocabulary == nil || catalog.Models[0].Tags[1] != "chat" {
		t.Fatalf("public catalog lost listings or vocabulary: %s", got.Body.Bytes())
	}
	var wire map[string]any
	if err := json.Unmarshal(got.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	vocab := wire["collectionVocabulary"].(map[string]any)
	entries := vocab["entries"].([]any)
	entries[0].(map[string]any)["name"] = "篡改"
	entries[0].(map[string]any)["slug"] = "invented-slug"
	tampered, _ := json.Marshal(wire)
	again := request("POST", "/api/v1/admin/tokencenter/import", "test-admin", tampered)
	if again.Code != 200 || bytes.Contains(again.Body.Bytes(), []byte("篡改")) || bytes.Contains(again.Body.Bytes(), []byte("invented-slug")) || bytes.Contains(store.data, []byte("篡改")) {
		t.Fatalf("client vocabulary replaced the contract: %d %s", again.Code, again.Body.Bytes())
	}
}

func TestNormalizeDropsImportedVocabulary(t *testing.T) {
	catalog := fixture(t)
	catalog.CollectionVocabulary = &CollectionVocabulary{Entries: []CollectionTerm{{Slug: "invented", Name: "发明"}}}
	got, err := Normalize(catalog)
	if err != nil || got.CollectionVocabulary != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(got)
	if bytes.Contains(data, []byte("invented")) || bytes.Contains(data, []byte("collectionVocabulary")) {
		t.Fatalf("normalized snapshot kept a client word list: %s", data)
	}
}

func TestChaosSyncOverlaysPublishedIDsWithoutRewritingProviderFacts(t *testing.T) {
	store := &memoryStore{}
	var dictionaryRequests int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open/v1/tag-library" {
			dictionaryRequests++
			if r.Header.Get("Authorization") != "Bearer fixture-source-token" || r.Header.Get("If-None-Match") != "" || r.URL.Query().Get("dimension") != "navigation" {
				t.Errorf("dictionary request lost auth or reused catalog validators: %v %s", r.Header, r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"dictionary":"Tag","dimension":"navigation","revision":"9","tags":[{"id":"42","slug":"tc-nav-home","name":"不是首页","dimension":"navigation","revision":7,"governanceStatus":"active"}]}`))
			return
		}
		page := 1
		if r.URL.Query().Get("cursor") != "" {
			page = 2
		}
		data := chaosFixturePage(t, page)
		if page == 1 {
			domain := data["domains"].([]any)[0].(map[string]any)
			item := domain["materials"].(map[string]any)["items"].([]any)[0].(map[string]any)
			provider := item["modelCatalog"].(map[string]any)["provider"].(map[string]any)
			provider["kind"] = "manufacturer"
			provider["tags"] = []string{"厂商官方"}
		}
		json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()
	service := chaosService(t, store, server)
	service.cfg.CollectionDictionaryURL = server.URL + "/open/v1/tag-library?dimension=navigation"
	got, err := service.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if dictionaryRequests != 1 || got.CollectionVocabulary == nil {
		t.Fatalf("dictionary was not read: requests=%d", dictionaryRequests)
	}
	var home CollectionTerm
	for _, term := range got.CollectionVocabulary.Entries {
		if term.Slug == "tc-nav-home" {
			home = term
		}
		if term.Slug == "not-in-contract" {
			t.Fatal("sync added a slug outside the contract")
		}
	}
	if home.ID != "42" || home.Name != "不是首页" || home.Revision != "7" || got.CollectionVocabulary.Revision != "9" {
		t.Fatalf("published navigation name was not projected: %+v rev=%s", home, got.CollectionVocabulary.Revision)
	}
	found := false
	for _, site := range got.Sites {
		if site.ID != "10" {
			continue
		}
		found = true
		if site.ProviderKind != "manufacturer" || !slices.Equal(site.Tags, []string{"厂商官方"}) || len(site.TagRefs) != 0 {
			t.Fatalf("provider kind, tags, or tagRefs changed: %+v", site)
		}
	}
	if !found {
		t.Fatal("provider missing")
	}
	for _, model := range got.Models {
		for _, tag := range model.Tags {
			if strings.HasPrefix(tag, "tc-") {
				t.Fatalf("model tags gained a navigation slug: %+v", model.Tags)
			}
		}
	}
	if bytes.Contains(store.data, []byte("collectionVocabulary")) || bytes.Contains(store.data, []byte(`"id":"42"`)) {
		t.Fatalf("published vocabulary was written into the snapshot: %s", store.data)
	}
	if store.accessStatus != 0 {
		t.Fatal("dictionary read changed catalog access status")
	}
}

func TestDictionaryFailureDoesNotRevokeOrBlockCatalogSync(t *testing.T) {
	store := &memoryStore{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open/v1/tag-library" {
			http.Error(w, "no dictionary", http.StatusUnauthorized)
			return
		}
		page := 1
		if r.URL.Query().Get("cursor") != "" {
			page = 2
		}
		json.NewEncoder(w).Encode(chaosFixturePage(t, page))
	}))
	defer server.Close()
	service := chaosService(t, store, server)
	service.cfg.CollectionDictionaryURL = server.URL + "/open/v1/tag-library?dimension=navigation"
	got, err := service.Sync(context.Background())
	if err != nil || got.CollectionVocabulary == nil || got.Sites[0].ProviderKind != "aggregator" {
		t.Fatalf("dictionary 401 blocked catalog sync: %v %+v", err, got.Sites)
	}
	for _, term := range got.CollectionVocabulary.Entries {
		if term.ID != "" {
			t.Fatalf("unauthorized dictionary supplied an id: %+v", term)
		}
	}
	if store.accessStatus != 0 {
		t.Fatal("dictionary 401 revoked the catalog source")
	}
}

func TestMissingDictionaryClearsPublishedIDsAndKeepsCatalog(t *testing.T) {
	store := &memoryStore{}
	var dictionaryStatus atomic.Int32
	dictionaryStatus.Store(http.StatusOK)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open/v1/tag-library" {
			if status := int(dictionaryStatus.Load()); status != http.StatusOK {
				w.WriteHeader(status)
				return
			}
			_, _ = w.Write([]byte(`{"dictionary":"Tag","dimension":"navigation","tags":[{"id":"42","slug":"tc-nav-home","revision":7,"governanceStatus":"active"}]}`))
			return
		}
		page := 1
		if r.URL.Query().Get("cursor") != "" {
			page = 2
		}
		json.NewEncoder(w).Encode(chaosFixturePage(t, page))
	}))
	defer server.Close()
	service := chaosService(t, store, server)
	service.cfg.CollectionDictionaryURL = server.URL + "/open/v1/tag-library?dimension=navigation"
	first, err := service.Sync(context.Background())
	if err != nil || termBySlug(t, first, "tc-nav-home").ID != "42" {
		t.Fatalf("published id missing before withdrawal: %v %+v", err, first.CollectionVocabulary)
	}
	dictionaryStatus.Store(http.StatusNotFound)
	second, err := service.Sync(context.Background())
	if err != nil || termBySlug(t, second, "tc-nav-home").ID != "" || termBySlug(t, second, "tc-nav-home").Name != "首页" || second.Sites[0].ProviderKind == "" {
		t.Fatalf("missing dictionary dropped the catalog or left a stale id: %v %+v", err, termBySlug(t, second, "tc-nav-home"))
	}
}

func termBySlug(t *testing.T, catalog Catalog, slug string) CollectionTerm {
	t.Helper()
	if catalog.CollectionVocabulary == nil {
		t.Fatal("missing vocabulary")
	}
	for _, term := range catalog.CollectionVocabulary.Entries {
		if term.Slug == slug {
			return term
		}
	}
	t.Fatalf("slug %s missing from vocabulary", slug)
	return CollectionTerm{}
}

func TestChaosSourceConfigDerivesNavigationDictionaryURL(t *testing.T) {
	t.Setenv("TOKENCENTER_SOURCE_FORMAT", "chaos")
	t.Setenv("TOKENCENTER_SOURCE_URL", "https://chaos.example/public/v1/products/tokencenter/snapshot?resource=materials&resource=rankings")
	t.Setenv("TOKENCENTER_SOURCE_TOKEN", "")
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.CollectionDictionaryURL != "https://chaos.example/public/v1/products/tokencenter/navigation" {
		t.Fatalf("dictionary URL = %q (%v)", cfg.CollectionDictionaryURL, err)
	}
	t.Setenv("TOKENCENTER_SOURCE_FORMAT", "catalog")
	t.Setenv("TOKENCENTER_SOURCE_URL", "https://catalog.example/catalog.json")
	cfg, err = ConfigFromEnv()
	if err != nil || cfg.CollectionDictionaryURL != "" {
		t.Fatalf("catalog format grew a dictionary URL: %+v %v", cfg, err)
	}
}

func sameStringPtr(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func TestPublishedNavigationFollowsAllPagesAndKeepsLastConsistentRevision(t *testing.T) {
	changed := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("after") == "100" {
			revision := "v1"
			if changed {
				revision = "v2"
			}
			json.NewEncoder(w).Encode(map[string]any{"dictionary": "Tag", "dimension": "navigation", "tagRevision": revision, "items": []map[string]any{{"id": "101", "slug": "tc-nav-vpn"}}})
		} else {
			w.Write([]byte(`{"dictionary":"Tag","dimension":"navigation","tagRevision":"v1","items":[{"id":"42","slug":"tc-nav-home"}],"nextCursor":"100"}`))
		}
	}))
	defer server.Close()
	service := chaosService(t, &memoryStore{}, server)
	service.cfg.CollectionDictionaryURL = server.URL + "/navigation"
	service.refreshCollectionDictionary(context.Background())
	if len(service.publishedTags) != 2 || service.publishedTags["tc-nav-vpn"].ID != "101" {
		t.Fatal("later navigation page lost")
	}
	changed = true
	service.refreshCollectionDictionary(context.Background())
	if service.publishedRevision != "v1" || len(service.publishedTags) != 2 {
		t.Fatal("mixed revision replaced last accepted vocabulary")
	}
}
