package tokencenter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestChaosSyncPreservesCentralSiteTypesAndTags(t *testing.T) {
	for _, tc := range []struct{ kind, tag string }{
		{"manufacturer", "厂商官方"}, {"aggregator", "第三方聚合"}, {"relay", "第三方中转"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				page := 1
				if r.URL.Query().Get("cursor") != "" {
					page = 2
				}
				data := chaosFixturePage(t, page)
				if page == 1 {
					domain := data["domains"].([]any)[0].(map[string]any)
					item := domain["materials"].(map[string]any)["items"].([]any)[0].(map[string]any)
					provider := item["modelCatalog"].(map[string]any)["provider"].(map[string]any)
					provider["kind"], provider["tags"] = tc.kind, []string{tc.tag}
				}
				json.NewEncoder(w).Encode(data)
			}))
			defer server.Close()
			got, err := chaosService(t, &memoryStore{}, server).Sync(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, site := range got.Sites {
				if site.ID != "10" {
					continue
				}
				found = true
				if site.ProviderKind != tc.kind || !slices.Contains(site.Tags, tc.tag) {
					t.Fatalf("central classification/tag lost at consumer boundary: %+v", site)
				}
			}
			if !found {
				t.Fatal("central provider missing from synced catalog")
			}
		})
	}
}
