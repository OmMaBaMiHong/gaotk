package tokencenter

import (
	"encoding/json"
	"testing"
)

func TestManufacturerAssetDoesNotReplaceExistingInventory(t *testing.T) {

	catalog := json.RawMessage(`{"schemaVersion":1,"provider":{"entityId":"10","name":"Old reference name","kind":"unverified","sourceAuthority":"secondary","logoUrl":"https://catalog.test/lab.svg"},"models":[{"entityId":"20","key":"provider:lab:model","id":"model","name":"Model","cost":{}}]}`)
	registry := json.RawMessage(`{"schemaVersion":1,"provider":{"entityId":"10","name":"Lab","kind":"manufacturer","key":"lab","websiteUrl":"https://lab.example","recordKind":"manufacturer_registry","sourceAuthority":"secondary","logoUrl":"/public/v1/products/test/domains/models/materials/42/logo?v=brand","logoSourceUrl":"https://catalog.test/lab"},"models":[]}`)
	for _, inputs := range [][]json.RawMessage{{catalog, registry}, {registry, catalog}} {
		a := chaosAccumulator{models: map[string]int{}, sites: map[string]int{}, offers: map[string]bool{}}
		for _, input := range inputs {
			if err := a.appendProvider(input); err != nil {
				t.Fatal(err)
			}
		}
		if len(a.catalog.Sites) != 1 || a.catalog.Sites[0].Models != 1 || len(a.catalog.Models) != 1 {
			t.Fatal("identity import changed catalog inventory", a.catalog.Sites)
		}
		if a.catalog.Sites[0].ProviderKind != "manufacturer" || a.catalog.Sites[0].Name != "Lab" || a.catalog.Sites[0].WebsiteURL != "https://lab.example" {
			t.Fatal("registered identity remained hidden behind reference classification", a.catalog.Sites[0])
		}
		resolveCatalogLogoURLs(&a.catalog, "https://chaos.test/public/v1/products/test/snapshot")
		if a.catalog.Sites[0].LogoURL != "https://chaos.test/public/v1/products/test/domains/models/materials/42/logo?v=brand" {
			t.Fatal(a.catalog.Sites[0].LogoURL)
		}
	}
}
