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

func TestModelRowsReuseManufacturerRegistryLogo(t *testing.T) {
	catalog := Catalog{
		Sites: []Site{
			{ID: "10", Name: "Anthropic", ProviderKey: "anthropic", ProviderKind: "manufacturer", LogoURL: "/public/v1/products/test/domains/models/materials/42/logo?v=brand"},
			{ID: "20", Name: "Some Relay", ProviderKey: "somerelay", ProviderKind: "unverified", LogoURL: "https://relay.test/icon.png"},
		},
		Models: []Model{
			{ID: "1", Name: "Mono from directory", DeveloperID: "anthropic", DeveloperName: "Anthropic", DeveloperLogoURL: "https://models.dev/logos/anthropic.svg"},
			{ID: "2", Name: "No logo at all", DeveloperID: "anthropic", DeveloperName: "Anthropic"},
			{ID: "3", Name: "Unknown developer", DeveloperID: "mystery", DeveloperName: "Mystery", DeveloperLogoURL: "https://models.dev/logos/mystery.svg"},
		},
	}
	applyManufacturerRegistryLogos(&catalog)
	if catalog.Models[0].DeveloperLogoURL != "/public/v1/products/test/domains/models/materials/42/logo?v=brand" {
		t.Fatal("directory icon was not replaced by the registry asset", catalog.Models[0].DeveloperLogoURL)
	}
	if catalog.Models[1].DeveloperLogoURL != "/public/v1/products/test/domains/models/materials/42/logo?v=brand" {
		t.Fatal("model without a logo did not inherit the registry asset", catalog.Models[1].DeveloperLogoURL)
	}
	if catalog.Models[2].DeveloperLogoURL != "https://models.dev/logos/mystery.svg" {
		t.Fatal("model without a registered developer must keep its published logo", catalog.Models[2].DeveloperLogoURL)
	}
	// Relative governed paths must still resolve against the source afterwards.
	resolveCatalogLogoURLs(&catalog, "https://chaos.test/public/v1/products/test/snapshot")
	if catalog.Models[1].DeveloperLogoURL != "https://chaos.test/public/v1/products/test/domains/models/materials/42/logo?v=brand" {
		t.Fatal("registry asset did not resolve to the governed absolute URL", catalog.Models[1].DeveloperLogoURL)
	}
}
