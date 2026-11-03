package tokencenter

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStrictTransportRejectsWrongDataTemplateAndUnqualifiedRanking(t *testing.T) {
	data := admittedFixturePage(t, 1, strings.Repeat("b", 64))
	contractJSON, _ := json.Marshal(data["admission"])
	var contract AdmissionContract
	json.Unmarshal(contractJSON, &contract)
	contract.Datasets[0].TemplateKey = "content"
	material := data["domains"].([]any)[0].(map[string]any)["materials"].(map[string]any)["items"].([]any)[0]
	raw, _ := json.Marshal(material)
	if validateAdmittedMaterial(raw, &contract) == nil {
		t.Fatal("content qualification admitted model catalog facts")
	}
	var acc chaosAccumulator
	acc.requireAdmission = true
	if err := acc.initialize(chaosPageFromMap(t, data)); err != nil {
		t.Fatal(err)
	}
	domain := data["domains"].([]any)[0].(map[string]any)
	board := domain["rankings"].(map[string]any)["intelligence"].(map[string]any)
	delete(board, "admission")
	boardJSON, _ := json.Marshal(board)
	if acc.appendRanking("models", "intelligence", "", "index points", boardJSON) == nil {
		t.Fatal("unqualified ranking accepted")
	}
}

func TestMaterialMayQualifyUnderAnotherPublishedContractForTheSameSource(t *testing.T) {
	data := admittedFixturePage(t, 1, strings.Repeat("b", 64))
	rawContract, _ := json.Marshal(data["admission"])
	var contract AdmissionContract
	json.Unmarshal(rawContract, &contract)
	first := contract.Datasets[0]
	first.ContractHash = strings.Repeat("e", 64)
	contract.Datasets = append([]AdmissionDataset{first}, contract.Datasets...)
	material := data["domains"].([]any)[0].(map[string]any)["materials"].(map[string]any)["items"].([]any)[0]
	raw, _ := json.Marshal(material)
	if err := validateAdmittedMaterial(raw, &contract); err != nil {
		t.Fatalf("one stricter channel hid another channel's qualified material: %v", err)
	}
}

func TestContractChangeOnALaterPageAlsoWithdrawsTheOldCatalog(t *testing.T) {
	changed := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if r.URL.Query().Get("cursor") != "" {
			page = 2
		}
		version := strings.Repeat("b", 64)
		if changed && page == 2 {
			version = strings.Repeat("c", 64)
		}
		json.NewEncoder(w).Encode(admittedFixturePage(t, page, version))
	}))
	defer server.Close()
	store := &memoryStore{}
	s := chaosService(t, store, server)
	s.cfg.RequireAdmission = true
	if _, err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	changed = true
	if _, err := s.Sync(context.Background()); err == nil {
		t.Fatal("mixed contract accepted")
	}
	restarted := chaosService(t, store, server)
	restarted.cfg.RequireAdmission = true
	if _, err := restarted.Catalog(context.Background()); err == nil {
		t.Fatal("known stronger contract on a later page revived the old catalog")
	}
}

func chaosPageFromMap(t *testing.T, data map[string]any) chaosPage {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeChaosPage(raw)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestPublicCatalogMarksPersistentAdmissionWithdrawal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := testService(t, &memoryStore{data: demoJSON, accessStatus: 403}, "")
	router := gin.New()
	RegisterRoutes(router.Group("/api/v1"), router.Group("/admin"), s)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/tokencenter/catalog", nil))
	var body map[string]any
	json.Unmarshal(response.Body.Bytes(), &body)
	if body["code"] != "source_revoked" {
		t.Fatalf("consumer cannot distinguish withdrawal: %s", response.Body)
	}
}

func admittedFixturePage(t *testing.T, page int, version string) map[string]any {
	data := chaosFixturePage(t, page)
	hash := strings.Repeat("a", 64)
	data["admission"] = map[string]any{"schemaVersion": 1, "validatorVersion": 1, "version": version, "tagRevision": strings.Repeat("d", 64), "planVersion": 1, "datasets": []any{map[string]any{"sourceId": "1", "templateKey": "model-catalog", "contractHash": hash, "purposes": []string{"catalog", "ranking"}}, map[string]any{"sourceId": "2", "templateKey": "model-benchmark", "contractHash": hash, "purposes": []string{"catalog", "ranking"}}}}
	meta := func() map[string]any {
		return map[string]any{"schemaVersion": 1, "validatorVersion": 1, "contracts": map[string]any{hash: map[string]any{"catalog": true, "ranking": true}}}
	}
	var visit func(any)
	visit = func(v any) {
		switch value := v.(type) {
		case map[string]any:
			if _, ok := value["entityId"]; ok {
				value["admission"] = meta()
				value["tagRefs"] = []any{map[string]any{"tagId": "1473", "slug": "subject-model", "name": "模型", "dimension": "subject_type", "relation": "classified_as", "revision": 1}}
			}
			for k, child := range value {
				if k != "admission" {
					visit(child)
				}
			}
		case []any:
			for _, child := range value {
				visit(child)
			}
		}
	}
	for _, domainValue := range data["domains"].([]any) {
		domain := domainValue.(map[string]any)
		if rankings, ok := domain["rankings"].(map[string]any); ok {
			for _, raw := range rankings {
				board := raw.(map[string]any)
				board["admission"] = map[string]any{"schemaVersion": 1, "contractVersion": version, "eligible": board["total"]}
			}
		}
		if materials, ok := domain["materials"].(map[string]any); ok {
			for _, itemValue := range materials["items"].([]any) {
				item := itemValue.(map[string]any)
				item["sourceId"] = "1"
				if item["modelBenchmark"] != nil {
					item["sourceId"] = "2"
				}
				item["admission"] = meta()
				visit(item)
			}
		}
	}
	return data
}

func TestStricterContractFailureCannotRestoreOldCatalog(t *testing.T) {
	version := strings.Repeat("b", 64)
	failPage := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if r.URL.Query().Get("cursor") != "" {
			page = 2
		}
		if page == 2 && failPage {
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(admittedFixturePage(t, page, version))
	}))
	defer server.Close()
	store := &memoryStore{}
	s := chaosService(t, store, server)
	s.cfg.RequireAdmission = true
	if _, err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	failPage = true
	if _, err := s.Sync(context.Background()); err == nil {
		t.Fatal("incomplete pagination accepted")
	}
	if _, err := s.Catalog(context.Background()); err != nil {
		t.Fatal("same-contract transient failure discarded qualified snapshot", err)
	}
	version = strings.Repeat("c", 64)
	if _, err := s.Sync(context.Background()); err == nil {
		t.Fatal("partial stronger contract accepted")
	}
	restarted := chaosService(t, store, server)
	restarted.cfg.RequireAdmission = true
	if _, err := restarted.Catalog(context.Background()); err == nil {
		t.Fatal("old contract resurrected after stronger policy and restart")
	}
	failPage = false
	if _, err := restarted.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStrictSyncRejectsGenericClassificationAndLegacySnapshot(t *testing.T) {
	corrupt := false
	legacy := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if r.URL.Query().Get("cursor") != "" {
			page = 2
		}
		data := admittedFixturePage(t, page, strings.Repeat("b", 64))
		if legacy {
			delete(data, "admission")
		}
		if corrupt && page == 1 {
			item := data["domains"].([]any)[0].(map[string]any)["materials"].(map[string]any)["items"].([]any)[0].(map[string]any)
			provider := item["modelCatalog"].(map[string]any)["provider"].(map[string]any)
			provider["tagRefs"].([]any)[0].(map[string]any)["slug"] = "subject-collected-record"
		}
		json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()
	s := chaosService(t, &memoryStore{}, server)
	s.cfg.RequireAdmission = true
	if _, err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	corrupt = true
	if _, err := s.Sync(context.Background()); err == nil {
		t.Fatal("generic capture tag entered strict catalog")
	}
	corrupt = false
	legacy = true
	if _, err := s.Sync(context.Background()); err == nil {
		t.Fatal("legacy unqualified catalog entered strict mode")
	}
	if _, err := s.Catalog(context.Background()); err == nil {
		t.Fatal("missing admission contract kept old catalog public")
	}
}

func TestSourceRevocationSurvivesConsumerRestart(t *testing.T) {
	status := 200
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		page := 1
		if r.URL.Query().Get("cursor") != "" {
			page = 2
		}
		json.NewEncoder(w).Encode(chaosFixturePage(t, page))
	}))
	defer server.Close()
	store := &memoryStore{}
	service := chaosService(t, store, server)
	if _, err := service.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	status = 403
	if _, err := service.Sync(context.Background()); err == nil {
		t.Fatal("revocation was accepted")
	}
	restarted := chaosService(t, store, server)
	if _, err := restarted.Catalog(context.Background()); err == nil {
		t.Fatal("revoked source remained public after consumer restart")
	}
	status = 200
	if _, err := restarted.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Catalog(context.Background()); err != nil {
		t.Fatal("successful authorized re-import did not restore catalog", err)
	}
}

func TestCanonicalReferencesSurviveCatalogProjection(t *testing.T) {
	refs := []any{map[string]any{"tagId": "55", "slug": "ai-hub--relay", "name": "模型中转站", "dimension": "subject_type", "relation": "classified_as", "revision": 2}}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if r.URL.Query().Get("cursor") != "" {
			page = 2
		}
		data := chaosFixturePage(t, page)
		if page == 1 {
			item := data["domains"].([]any)[0].(map[string]any)["materials"].(map[string]any)["items"].([]any)[0].(map[string]any)
			item["modelCatalog"].(map[string]any)["provider"].(map[string]any)["tagRefs"] = refs
		}
		json.NewEncoder(w).Encode(data)
	}))
	defer server.Close()
	got, err := chaosService(t, &memoryStore{}, server).Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	var catalog map[string]any
	json.Unmarshal(raw, &catalog)
	for _, value := range catalog["sites"].([]any) {
		site := value.(map[string]any)
		if site["id"] == "10" && site["tagRefs"] == nil {
			t.Fatal("canonical tag references were dropped by local projection")
		}
	}
}
