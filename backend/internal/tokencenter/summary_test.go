package tokencenter

import "testing"

func TestSummaryUsesPublishedSnapshotNotImportedClaims(t *testing.T) {
	c := Catalog{
		Summary:  &CatalogSummary{PublishedBoards: 999, ModelRecords: 999, APISites: 999, APIQuotes: 999},
		Models:   []Model{{ID: "1"}, {ID: "2"}},
		Sites:    []Site{{ID: "3"}},
		Offers:   []Offer{{ID: "4"}, {ID: "5"}},
		Rankings: map[string]Ranking{"published": {Entries: []RankedEntry{{EntityID: "1"}}}, "empty": {}},
	}
	got := summarizeCatalog(c)
	if *got != (CatalogSummary{PublishedBoards: 1, ModelRecords: 2, APISites: 1, APIQuotes: 2}) {
		t.Fatalf("summary counted unprovided or unpublished facts: %+v", got)
	}
	empty := summarizeCatalog(Catalog{})
	if *empty != (CatalogSummary{}) {
		t.Fatalf("empty snapshot invented counts: %+v", empty)
	}
}
