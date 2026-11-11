package tokencenter

import (
	"encoding/json"
	"testing"
)

func TestModelBoardIsProjectedServerSideWithoutChangingFacts(t *testing.T) {
	rank, score := 3, 89.35
	c := Catalog{DefaultRanking: "modeldial", Models: []Model{{ID: "1", Name: "Vendor: A"}, {ID: "2", Name: "B"}}, Rankings: map[string]Ranking{"modeldial": {Topic: "modeldial", Entries: []RankedEntry{{EntityID: "2", Rank: &rank, Score: &score}}}}}
	got := projectModelBoard(c, "")
	if got.RankingKey != "modeldial" || got.Models[0].ID != "2" || *got.Models[0].Rank != 3 || *got.Models[0].Score != 89.35 || got.Models[1].Rank != nil || got.Models[1].Name != "Vendor: A" {
		t.Fatalf("wrong projection: %+v", got)
	}
	if c.Models[0].ID != "1" || c.Models[1].Rank != nil {
		t.Fatal("projection mutated stored facts")
	}
}

func TestModelDialProjectionKeepsDimensionsAndUnknownVendorPrice(t *testing.T) {
	a := chaosAccumulator{catalog: Catalog{Models: []Model{}}, models: map[string]int{}}
	raw := json.RawMessage(`{"schemaVersion":1,"entityId":"123","source":"ModelDial","benchmarkId":"modeldial_five_axis","modelPermaslug":"example-model","modelName":"Published name","metric":"overall_score","score":89.35,"scoreUnit":"points","configurationId":"source:example:high","reasoningEffort":"high","dimensions":{"backend":95,"frontend":94,"knowledge":95,"aesthetic":75,"maintainability":61},"released":"2026-11-01","configurations":[{"candidate_id":"source:example:high","model_id":"example-model","reasoning_effort":"high","overall_score":89.35}]}`)
	if err := a.appendEvaluation(raw); err != nil {
		t.Fatal(err)
	}
	m := a.catalog.Models[0]
	if m.Name != "Published name" || m.Provider != "" || m.Input != nil || m.Output != nil || m.ModelDial == nil || *m.ModelDial.Dimensions.Backend != 95 || len(m.ModelDial.Configurations) != 1 {
		t.Fatalf("fabricated or lost source fields: %+v", m)
	}
}

func TestRelayProjectionUsesPublishedRelayRankAndPreservesUnrankedSites(t *testing.T) {
	allRank, relayRank, score := 1, 7, 81.0
	c := Catalog{Sites: []Site{{ID: "official", ProviderKind: "manufacturer", Rank: &allRank}, {ID: "relay", ProviderKind: "relay", Rank: &allRank}, {ID: "unranked", ProviderKind: "relay", Rank: &allRank}}, Rankings: map[string]Ranking{"relays": {Entries: []RankedEntry{{EntityID: "official", Rank: &allRank}, {EntityID: "relay", Rank: &relayRank, Score: &score}}}}}
	got := projectModelBoard(c, "")
	if len(got.Relays) != 2 || *got.Relays[0].Rank != 7 || got.Relays[1].Rank != nil || got.Home == nil || len(got.Home.Relays) != 1 || *got.Home.Relays[0].Rank != 7 {
		t.Fatalf("%+v", got)
	}
	if *c.Sites[1].Rank != 1 {
		t.Fatal("stored provider ranks changed")
	}
}

func TestOfficialQuoteProjectionKeepsPairedPricesAndConditions(t *testing.T) {
	inputA, outputA, inputB, outputB := 0.15, 1.2, 0.3, 0.6
	c := Catalog{Models: []Model{{ID: "m", Name: "Published model"}}, Sites: []Site{{ID: "official", ProviderKind: "manufacturer"}, {ID: "relay", ProviderKind: "relay"}}, Offers: []Offer{
		{ID: "a", ModelID: "m", SiteID: "official", SourceAuthority: "first_party", Input: &inputA, Output: &outputA, Condition: []byte(`{"period":"PEAK","timeZone":"UTC"}`)},
		{ID: "b", ModelID: "m", SiteID: "official", SourceAuthority: "first_party", Input: &inputB, Output: &outputB, Condition: []byte(`{"period":"OFF-PEAK","timeZone":"UTC"}`)},
		{ID: "c", ModelID: "m", SiteID: "relay", SourceAuthority: "first_party", Input: &inputA, Output: &outputB},
	}}
	rows := projectOfficialModels(c)
	if len(rows) != 2 || rows[0].QuoteID != "a" || *rows[0].Input != inputA || *rows[0].Output != outputA || rows[1].QuoteID != "b" || *rows[1].Input != inputB || *rows[1].Output != outputB || rows[1].QuotePeriod != "OFF-PEAK" {
		t.Fatalf("mixed or missing published quotes: %+v", rows)
	}
	if c.Models[0].QuoteID != "" {
		t.Fatal("projection mutated source models")
	}
}
