package tokencenter

import "testing"

func TestAuthoritativeCatalogPreservesSourceZeroContextForNonTextModels(t *testing.T) {
	c := fixture(t)
	c.RankingAuthority = "chaos"
	zero := 0.0
	c.Models[0].Context = &zero
	c.Models[0].Modalities = &ModelModalities{Output: []string{"image"}}
	got, err := Normalize(c)
	if err != nil || got.Models[0].Context == nil || *got.Models[0].Context != 0 {
		t.Fatalf("native non-text zero context was discarded: %v", err)
	}
	c.RankingAuthority = ""
	if _, err = Normalize(c); err == nil {
		t.Fatal("legacy required positive context contract changed")
	}
}

func TestAuthoritativeCatalogRetainsSourceReleaseDatePrecision(t *testing.T) {
	c := fixture(t)
	c.RankingAuthority = "chaos"
	month := "2025-09"
	c.Models[0].Released = &month
	got, err := Normalize(c)
	if err != nil || got.Models[0].Released == nil || *got.Models[0].Released != "2025-09" {
		t.Fatalf("source month precision was discarded/invented: %v", err)
	}
	c.RankingAuthority = ""
	if _, err = Normalize(c); err == nil {
		t.Fatal("legacy full-date requirement changed")
	}
}
