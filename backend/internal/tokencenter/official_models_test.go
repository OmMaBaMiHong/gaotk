package tokencenter

import (
	"encoding/json"
	"testing"
)

func TestOfficialModelRelationKeepsChannelQuotesSeparate(t *testing.T) {
	a := chaosAccumulator{models: map[string]int{}, sites: map[string]int{}, offers: map[string]bool{}}
	for _, provider := range []string{"relay-a", "relay-b"} {
		raw, _ := json.Marshal(map[string]any{"schemaVersion": 1, "provider": map[string]any{"entityId": provider, "key": provider, "name": provider, "sourceUrl": "https://" + provider + ".test", "sourceAuthority": "first_party"}, "models": []any{map[string]any{"entityId": provider + "-model", "id": "native-id", "sourceKey": "native-key", "key": "provider:" + provider + ":native-id", "canonicalModelId": "native/model", "name": "Channel Label", "cost": map[string]any{"input": 1, "output": 2}, "officialModel": map[string]any{"entityId": "900", "canonicalModelId": "official/model", "name": "Official Model", "developerId": "lab", "developerName": "Official Lab", "sourceUrl": "https://lab.test/models", "logoUrl": "/public/v1/products/test/domains/models/materials/1/logo"}}}})
		if err := a.appendProvider(raw); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.catalog.Models) != 1 || len(a.catalog.Offers) != 2 || a.catalog.Models[0].ID != "900" || a.catalog.Models[0].Name != "Official Model" {
		t.Fatalf("published relation not projected: %+v", a.catalog)
	}
	for _, offer := range a.catalog.Offers {
		if offer.ModelID != "900" || offer.SourceModelID != "native-id" || offer.ReferenceIdentity.CanonicalModelID != "native/model" {
			t.Fatalf("source quote rewritten: %+v", offer)
		}
	}
	if a.catalog.Offers[0].ID == a.catalog.Offers[1].ID {
		t.Fatal("different channel quotes merged")
	}
	resolveCatalogLogoURLs(&a.catalog, "http://127.0.0.1:4000/public/v1/products/test/snapshot")
	if !webURL(a.catalog.Models[0].DeveloperLogoURL) {
		t.Fatal("public logo URL not resolved")
	}
}
