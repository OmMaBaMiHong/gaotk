package tokencenter

import (
	"encoding/json"
	"reflect"
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

func TestModelDialPublishedDeveloperDoesNotReplaceSourceIdentity(t *testing.T) {
	a := chaosAccumulator{catalog: Catalog{Models: []Model{}}, models: map[string]int{}}
	raw := json.RawMessage(`{"schemaVersion":1,"entityId":"123","source":"ModelDial","benchmarkId":"modeldial_five_axis","modelPermaslug":"native-model","modelName":"Native name","metric":"overall_score","score":95,"developer":{"id":"lab","name":"Lab","modelName":"Lab Model","canonicalModelId":"lab/model","logoUrl":"https://models.dev/logos/lab.svg","sourceUrl":"https://models.dev/catalog.json"}}`)
	if err := a.appendEvaluation(raw); err != nil {
		t.Fatal(err)
	}
	m := a.catalog.Models[0]
	if m.ID != "123" || m.Name != "Native name" || m.ModelPermaslug != "native-model" ||
		m.CanonicalModelID != "" || m.DisplayName != "Lab Model" || m.DeveloperName != "Lab" ||
		m.DeveloperLogoURL != "https://models.dev/logos/lab.svg" || *m.ModelDial.Score != 95 {
		t.Fatalf("unexpected display or source identity: %+v", m)
	}
}

func TestValueBoardProjectsPublishedEvidenceAndExactPairedQuote(t *testing.T) {
	rank, score, input, output := 2, 78.4, 1.25, 8.5
	identity := ReferenceOfferIdentity{SourceMaterialID: "material", ProviderKey: "provider", SourceModelID: "source-model", SourceKey: "native-key", CanonicalModelID: "lab/model", QuotePlanKey: "tier:1", ConfigurationKey: "reasoning:high", SourceURL: "https://source.example/catalog.json"}
	valueScore := json.RawMessage(`{"abilityScore":89,"priceScore":62.5,"referenceCost":9.75,"referenceOfferId":"paired","abilityBreakdown":{"source":88},"evaluationRefs":[{"id":"evaluation-1"}],"futureEvidence":{"kept":true}}`)
	var value map[string]any
	if err := json.Unmarshal(valueScore, &value); err != nil {
		t.Fatal(err)
	}
	value["referenceOfferIdentity"] = identity
	observedAt := "2026-11-12T01:00:00Z"
	quote := Offer{ID: "paired", ModelID: "quote-model", SiteID: "provider", Input: &input, Output: &output, Currency: "USD", Unit: "1M tokens", UpdatedAt: &observedAt, SourceContentHash: "source-revision", ReferenceIdentity: &identity, Condition: json.RawMessage(`{"context":"over 200k"}`), PricingURL: "https://source.example/pricing"}
	value["referenceOfferSnapshot"] = referenceSnapshotFixture(quote)
	metadata, _ := json.Marshal(map[string]any{"metadata": map[string]any{"valueScore": value}})
	c := Catalog{DefaultRanking: "modeldial", Models: []Model{{ID: "evaluation-model", Name: "Published evaluation model"}, {ID: "unranked", Name: "Unknown"}}, Offers: []Offer{quote}, Rankings: map[string]Ranking{
		"value-coding-balanced": {Entries: []RankedEntry{{EntityID: "evaluation-model", Rank: &rank, Score: &score, Metadata: metadata}}},
		"modeldial":             {Entries: []RankedEntry{{EntityID: "evaluation-model", Rank: &rank, Score: &score}}},
	}}
	got := projectModelBoard(c, "value-coding-balanced")
	m := got.Models[0]
	var published map[string]any
	if err := json.Unmarshal(m.ValueScore, &published); err != nil {
		t.Fatal(err)
	}
	if m.ReferenceOfferID != "paired" || m.ReferenceOffer == nil || !reflect.DeepEqual(*m.ReferenceOffer, quote) || *m.Rank != rank || *m.Score != score || published["futureEvidence"] == nil || m.Input != nil || m.Output != nil {
		t.Fatalf("published evidence or complete paired quote changed: %+v", m)
	}
	if len(got.Models[1].ValueScore) != 0 || got.Models[1].Score != nil || c.Models[0].ReferenceOffer != nil || len(c.Models[0].ValueScore) != 0 || c.DefaultRanking != "modeldial" {
		t.Fatal("projection fabricated scores, mutated facts or changed the default ranking")
	}
	switched := projectModelBoard(got, "modeldial")
	if len(switched.Models[0].ValueScore) != 0 || switched.Models[0].ReferenceOffer != nil || switched.Models[0].ReferenceOfferID != "" || *switched.Models[0].Score != score {
		t.Fatal("value evidence leaked into an ordinary source board")
	}
}

func TestValueReferenceQuoteDoesNotSubstituteMissingOrMismatchedEvidence(t *testing.T) {
	identity := ReferenceOfferIdentity{SourceMaterialID: "m", ProviderKey: "p", SourceModelID: "native", QuotePlanKey: "base", SourceURL: "https://source.example/models"}
	different := identity
	different.ConfigurationKey = "high"
	for _, tc := range []struct {
		name      string
		id        string
		identity  *ReferenceOfferIdentity
		offers    []Offer
		wantQuote string
	}{
		{"missing id", "missing", &identity, []Offer{{ID: "other", ReferenceIdentity: &identity}}, ""},
		{"configuration mismatch", "quote", &different, []Offer{{ID: "quote", ReferenceIdentity: &identity}}, ""},
		{"identity only", "", &identity, []Offer{{ID: "exact", ReferenceIdentity: &identity}}, "exact"},
		{"ambiguous identity", "", &identity, []Offer{{ID: "one", ReferenceIdentity: &identity}, {ID: "two", ReferenceIdentity: &identity}}, ""},
		{"no published quote", "", nil, []Offer{{ID: "available", ReferenceIdentity: &identity}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observedAt := "2026-11-12T01:00:00Z"
			for i := range tc.offers {
				tc.offers[i].UpdatedAt, tc.offers[i].SourceContentHash = &observedAt, "source-revision"
				tc.offers[i].Currency, tc.offers[i].Unit = "USD", "1M tokens"
			}
			value := map[string]any{"referenceOfferId": tc.id, "referenceOfferIdentity": tc.identity, "unrankedReason": "insufficient_evidence", "missingEvidence": []string{"configuration"}}
			if tc.id != "" || tc.identity != nil {
				value["referenceOfferSnapshot"] = referenceSnapshotFixture(tc.offers[0])
			}
			metadata, _ := json.Marshal(map[string]any{"valueScore": value})
			got := projectValueScore(Model{ID: "model"}, metadata, publishedReferenceOffers(tc.offers))
			if tc.wantQuote == "" && got.ReferenceOffer != nil || tc.wantQuote != "" && (got.ReferenceOffer == nil || got.ReferenceOffer.ID != tc.wantQuote) {
				t.Fatalf("quote substituted without exact published reference: %+v", got)
			}
			if got.Score != nil || got.Rank != nil || len(got.ValueScore) == 0 {
				t.Fatalf("unranked evidence changed into a local score: %+v", got)
			}
		})
	}
}

func TestModelDialPublishedMetadataSurvivesProviderPageOrder(t *testing.T) {
	provider := json.RawMessage(`{"schemaVersion":1,"provider":{"entityId":"10","key":"source","name":"Catalog supplier","sourceAuthority":"first_party"},"models":[{"entityId":"123","key":"canonical:lab/model","id":"native-model","sourceKey":"source-key","canonicalModelId":"lab/model","name":"Native catalog name","displayName":"Catalog display name","developerId":"lab","developerName":"Catalog lab","cost":{"input":1,"output":4}}]}`)
	evaluation := json.RawMessage(`{"schemaVersion":1,"entityId":"123","source":"ModelDial","benchmarkId":"modeldial_five_axis","modelPermaslug":"evaluation-model-high","modelName":"Native evaluation name","metric":"overall_score","score":89.35,"configurationId":"lab:model:high","reasoningEffort":"high","dimensions":{"backend":95,"frontend":94,"knowledge":95,"aesthetic":75,"maintainability":61},"released":"2026-11-01","configurations":[{"candidate_id":"lab:model:high","model_id":"evaluation-model","reasoning_effort":"high","overall_score":89.35}],"developer":{"id":"lab","name":"Published Lab","modelName":"Published display name","canonicalModelId":"lab/model","logoUrl":"https://source.example/lab.svg","sourceUrl":"https://source.example/catalog.json"},"quoteModelIds":["captured-price-model"],"quoteIdentityEvidence":{"canonicalModelId":"lab/model","sourceUrl":"https://source.example/catalog.json","sha256":"published-sha256","configurationMatched":false},"publication":{"metadata":{"sourceEvidence":"retained"}}}`)
	project := func(benchmarkFirst bool) Model {
		t.Helper()
		a := chaosAccumulator{catalog: Catalog{Models: []Model{}, Sites: []Site{}, Offers: []Offer{}}, models: map[string]int{}, sites: map[string]int{}, offers: map[string]bool{}}
		operations := []func() error{func() error { return a.appendProviderMaterial("material", provider, "published-content-hash") }, func() error { return a.appendEvaluation(evaluation) }}
		if benchmarkFirst {
			operations[0], operations[1] = operations[1], operations[0]
		}
		for _, operation := range operations {
			if err := operation(); err != nil {
				t.Fatal(err)
			}
		}
		return a.catalog.Models[0]
	}
	catalogFirst, benchmarkFirst := project(false), project(true)
	if !reflect.DeepEqual(catalogFirst, benchmarkFirst) || benchmarkFirst.ModelDial == nil || benchmarkFirst.ModelDial.Dimensions == nil || len(benchmarkFirst.ModelDial.Configurations) != 1 || len(benchmarkFirst.Evaluations) != 1 {
		t.Fatalf("provider-after-evaluation lost published metadata: catalogFirst=%+v benchmarkFirst=%+v", catalogFirst, benchmarkFirst)
	}
	if benchmarkFirst.Name != "Native catalog name" || benchmarkFirst.DisplayName != "Published display name" || benchmarkFirst.DeveloperName != "Published Lab" || benchmarkFirst.DeveloperLogoURL != "https://source.example/lab.svg" || benchmarkFirst.CanonicalModelID != "lab/model" || benchmarkFirst.ModelPermaslug != "evaluation-model-high" || string(benchmarkFirst.ModelDial.Publication.Metadata) != `{"sourceEvidence":"retained"}` {
		t.Fatalf("source identity or display evidence changed: %+v", benchmarkFirst)
	}
	if !reflect.DeepEqual(benchmarkFirst.QuoteModelIDs, []string{"captured-price-model"}) || benchmarkFirst.QuoteIdentityEvidence == nil || !reflect.DeepEqual(benchmarkFirst.QuoteIdentityEvidence, benchmarkFirst.ModelDial.QuoteIdentityEvidence) || benchmarkFirst.QuoteIdentityEvidence.ConfigurationMatched == nil || *benchmarkFirst.QuoteIdentityEvidence.ConfigurationMatched || benchmarkFirst.QuoteIdentityEvidence.SourceURL != "https://source.example/catalog.json" || benchmarkFirst.QuoteIdentityEvidence.SHA256 != "published-sha256" || benchmarkFirst.ReferenceOffer != nil || len(benchmarkFirst.ValueScore) != 0 {
		t.Fatalf("published quote relation lost or promoted to ranking evidence: %+v", benchmarkFirst)
	}
	encoded, _ := json.Marshal(benchmarkFirst)
	var reloaded Model
	if err := json.Unmarshal(encoded, &reloaded); err != nil || !reflect.DeepEqual(reloaded.QuoteIdentityEvidence, benchmarkFirst.QuoteIdentityEvidence) || !reflect.DeepEqual(reloaded.QuoteModelIDs, benchmarkFirst.QuoteModelIDs) {
		t.Fatalf("published quote relationship did not persist: %+v %v", reloaded, err)
	}
}

func TestModelDialAbsentQuoteRelationDoesNotInferOrMergeCatalogIdentity(t *testing.T) {
	a := chaosAccumulator{catalog: Catalog{Models: []Model{}, Sites: []Site{}, Offers: []Offer{}}, models: map[string]int{}, sites: map[string]int{}, offers: map[string]bool{}}
	provider := json.RawMessage(`{"schemaVersion":1,"provider":{"entityId":"10","key":"source","name":"Source","sourceAuthority":"first_party"},"models":[{"entityId":"price-model","key":"canonical:lab/model","id":"lab/model","canonicalModelId":"lab/model","name":"Same published name","developerId":"lab","cost":{"input":1,"output":4}}]}`)
	if err := a.appendProviderMaterial("material", provider, "published-content-hash"); err != nil {
		t.Fatal(err)
	}
	evaluation := json.RawMessage(`{"schemaVersion":1,"entityId":"evaluation-model","source":"ModelDial","benchmarkId":"modeldial_five_axis","modelPermaslug":"lab/model","modelName":"Same published name","metric":"overall_score","score":89,"developer":{"id":"lab","name":"Lab","canonicalModelId":"lab/model"}}`)
	if err := a.appendEvaluation(evaluation); err != nil {
		t.Fatal(err)
	}
	model := a.catalog.Models[a.models["evaluation-model"]]
	if len(a.catalog.Models) != 2 || len(a.catalog.Offers) != 1 || len(model.QuoteModelIDs) != 0 || model.QuoteIdentityEvidence != nil || model.Input != nil || model.Output != nil || model.ReferenceOffer != nil || model.CanonicalModelID != "" {
		t.Fatalf("missing central quote relationship was inferred or merged: %+v", model)
	}
	// Applying a later publication without a relation clears the earlier relation.
	model.QuoteModelIDs = []string{"old-price-model"}
	model.QuoteIdentityEvidence = &QuoteIdentityEvidence{CanonicalModelID: "old/model"}
	applyPublishedModelDial(&model, model.ModelDial)
	if len(model.QuoteModelIDs) != 0 || model.QuoteIdentityEvidence != nil {
		t.Fatal("absent published quote relationship retained stale evidence")
	}
}

func TestCatalogOfferRequiresUniqueExactOwnPublishedPricePair(t *testing.T) {
	input, output, zero := 2.0, 7.0, 0.0
	for _, name := range []string{"exact", "source URL", "source model", "source key", "tier", "other entity", "input", "output", "null is not zero", "missing identity", "duplicate", "missing source", "partial pair"} {
		t.Run(name, func(t *testing.T) {
			model := Model{ID: "model", SourceURL: "https://source.example/catalog.json", SourceModelID: "native-model", SourceKey: "native-key", Input: &input, Output: &output, QuoteModelIDs: []string{"other-model"}}
			identity := ReferenceOfferIdentity{SourceKey: "native-key", QuotePlanKey: "base"}
			offer := Offer{ID: "quote", ModelID: "model", SiteID: "provider", SourceURL: model.SourceURL, SourceModelID: model.SourceModelID, Input: &input, Output: &output, ReferenceIdentity: &identity}
			want := false
			switch name {
			case "exact":
				want = true
			case "source URL":
				offer.SourceURL = "https://other.example/catalog.json"
			case "source model":
				offer.SourceModelID = "other-native-model"
			case "source key":
				identity.SourceKey = "other-key"
			case "tier":
				identity.QuotePlanKey = "tier:0"
			case "other entity":
				offer.ModelID = "other-model"
			case "input":
				offer.Input = &zero
			case "output":
				offer.Output = &zero
			case "null is not zero":
				model.Input, offer.Input = nil, &zero
			case "missing identity":
				offer.ReferenceIdentity = nil
			case "missing source":
				model.SourceURL, offer.SourceURL = "", ""
			case "partial pair":
				model.Output, offer.Output, want = nil, nil, true
			}
			offers := []Offer{offer}
			if name == "duplicate" {
				duplicate := offer
				duplicate.ID = "another-quote"
				offers = append(offers, duplicate)
			}
			got := projectCatalogOffer(model, publishedReferenceOffers(offers))
			if (got.CatalogOffer != nil) != want || want && !reflect.DeepEqual(*got.CatalogOffer, offer) || got.ReferenceOffer != nil || got.ReferenceOfferID != "" || len(got.ValueScore) != 0 || !sameOptionalFloat64(got.Input, model.Input) || !sameOptionalFloat64(got.Output, model.Output) {
				t.Fatalf("catalog quote selected without a unique own source pair: %+v", got)
			}
		})
	}
}

func TestCatalogOfferAppearsOnAllBoardsAndStaysSeparateFromRankReference(t *testing.T) {
	input, output, cheaper, rankedInput := 2.0, 7.0, 0.1, 5.0
	observedAt := "2026-11-12T01:00:00Z"
	identity := ReferenceOfferIdentity{SourceKey: "native-key", QuotePlanKey: "base"}
	model := Model{ID: "model", Name: "Published model", SourceURL: "https://source.example/catalog.json", SourceModelID: "native-model", SourceKey: "native-key", Input: &input, Output: &output}
	base := Offer{ID: "own-base", ModelID: "model", SiteID: "official", SourceURL: model.SourceURL, SourceModelID: model.SourceModelID, SourceAuthority: "first_party", Input: &input, Output: &output, Currency: "USD", Unit: "1M tokens", UpdatedAt: &observedAt, SourceContentHash: "source-revision", ReferenceIdentity: &identity}
	other, ranked := base, base
	other.ID, other.Input, other.Output = "cheaper", &cheaper, &cheaper
	ranked.ID, ranked.Input = "rank-reference", &rankedInput
	metadata, _ := json.Marshal(map[string]any{"metadata": map[string]any{"valueScore": map[string]any{"referenceOfferId": ranked.ID, "referenceCost": 12, "referenceOfferSnapshot": referenceSnapshotFixture(ranked)}}})
	rank, score := 1, 82.0
	c := Catalog{Models: []Model{model}, Sites: []Site{{ID: "official", ProviderKind: "manufacturer"}}, Offers: []Offer{other, base, ranked}, Rankings: map[string]Ranking{
		"intelligence":           {Entries: []RankedEntry{{EntityID: "model", Rank: &rank, Score: &score}}},
		"value-general-balanced": {Entries: []RankedEntry{{EntityID: "model", Rank: &rank, Score: &score, Metadata: metadata}}},
		"empty":                  {Entries: []RankedEntry{}},
	}}
	for _, key := range []string{"intelligence", "value-general-balanced", "empty", "missing"} {
		got := projectModelBoard(c, key)
		if got.Models[0].CatalogOffer == nil || got.Models[0].CatalogOffer.ID != "own-base" || *got.Models[0].Input != input || *got.Models[0].Output != output || c.Models[0].CatalogOffer != nil {
			t.Fatalf("%s lost the own catalog source or chose a cheaper quote: %+v", key, got.Models)
		}
		if key == "value-general-balanced" && (got.Models[0].ReferenceOffer == nil || got.Models[0].ReferenceOffer.ID != "rank-reference") || key != "value-general-balanced" && got.Models[0].ReferenceOffer != nil {
			t.Fatalf("%s mixed catalog and value reference quotes: %+v", key, got.Models)
		}
		if len(got.OfficialModels) != 3 || got.OfficialModels[0].QuoteID != "cheaper" || *got.OfficialModels[0].Input != cheaper || got.OfficialModels[0].CatalogOffer != nil {
			t.Fatalf("%s changed the existing official per-offer projection: %+v", key, got.OfficialModels)
		}
	}
}

func referenceSnapshotFixture(offer Offer) ReferenceOfferSnapshot {
	return ReferenceOfferSnapshot{SchemaVersion: 1, ID: offer.ID, Identity: offer.ReferenceIdentity, Input: offer.Input, Output: offer.Output, CacheRead: offer.CacheRead, CacheWrite: offer.CacheWrite, Currency: offer.Currency, Unit: offer.Unit, Condition: offer.Condition, SourceMetadata: offer.SourceMetadata, Billing: offer.Billing, ObservedAt: offer.UpdatedAt, SourceContentHash: offer.SourceContentHash, QuoteContentHash: "published-quote-hash"}
}

func TestValueReferenceRequiresFrozenQuoteVersionAndCompletePrices(t *testing.T) {
	oldInput, oldOutput, newInput, newOutput, zero := 1.0, 4.0, 10.0, 40.0, 0.0
	observedAt, newerAt := "2026-11-12T01:00:00Z", "2026-11-12T02:00:00Z"
	identity := ReferenceOfferIdentity{SourceMaterialID: "unchanged-material", ProviderKey: "source", SourceModelID: "model", SourceKey: "source-key", QuotePlanKey: "base", SourceURL: "https://source.example/pricing"}
	for _, name := range []string{"same publication", "new prices same identity", "new source revision", "new observed time", "new condition", "new source metadata", "new billing", "new cache price", "missing source hash", "missing quote hash", "legacy reference", "partial snapshot", "zero price", "zero is not unknown"} {
		t.Run(name, func(t *testing.T) {
			old := Offer{ID: "stable-offer", ModelID: "model", SiteID: "source", Input: &oldInput, Output: &oldOutput, Currency: "USD", Unit: "1M tokens", UpdatedAt: &observedAt, SourceContentHash: "revision-before", ReferenceIdentity: &identity}
			current := old
			snapshot := referenceSnapshotFixture(old)
			cost, want := 2.0, false
			switch name {
			case "same publication":
				want = true
			case "new prices same identity":
				current.Input, current.Output = &newInput, &newOutput
			case "new source revision":
				current.SourceContentHash = "revision-after"
			case "new observed time":
				current.UpdatedAt = &newerAt
			case "new condition":
				current.Condition = json.RawMessage(`{"period":"OFF-PEAK"}`)
			case "new source metadata":
				current.SourceMetadata = json.RawMessage(`{"tier":"changed"}`)
			case "new billing":
				current.Billing = json.RawMessage(`{"unit":"request"}`)
			case "new cache price":
				current.CacheRead = &zero
			case "missing source hash":
				current.SourceContentHash, snapshot.SourceContentHash = "", ""
			case "missing quote hash":
				snapshot.QuoteContentHash = ""
			case "zero price":
				current.Input, current.Output, snapshot.Input, snapshot.Output, cost, want = &zero, &zero, &zero, &zero, zero, true
			case "zero is not unknown":
				current.Input, snapshot.Input = &zero, nil
			}
			value := map[string]any{"referenceOfferId": old.ID, "referenceOfferIdentity": identity, "referenceCost": cost}
			if name != "legacy reference" {
				encoded, _ := json.Marshal(snapshot)
				if name == "partial snapshot" {
					var fields map[string]json.RawMessage
					_ = json.Unmarshal(encoded, &fields)
					delete(fields, "cacheWrite")
					encoded, _ = json.Marshal(fields)
				}
				value["referenceOfferSnapshot"] = json.RawMessage(encoded)
			}
			metadata, _ := json.Marshal(map[string]any{"metadata": map[string]any{"valueScore": value}})
			got := projectValueScore(Model{ID: "evaluation-model"}, metadata, publishedReferenceOffers([]Offer{current}))
			if (got.ReferenceOffer != nil) != want {
				t.Fatalf("mixed old ranking cost with a different quote version: %+v", got)
			}
			var preserved map[string]json.RawMessage
			_ = json.Unmarshal(got.ValueScore, &preserved)
			var preservedCost float64
			_ = json.Unmarshal(preserved["referenceCost"], &preservedCost)
			if preservedCost != cost || name != "legacy reference" && len(preserved["referenceOfferSnapshot"]) == 0 || name == "zero price" && (*got.ReferenceOffer.Input != 0 || *got.ReferenceOffer.Output != 0) {
				t.Fatal("frozen evidence or valid zero prices were dropped or recomputed")
			}
		})
	}
}

func TestBrowseModelBoardKeepsUnrankedAndUnpricedDirectoryRows(t *testing.T) {
	rank, score, input, output := 1, 91.0, 1.0, 4.0
	catalog := Catalog{
		Models: []Model{
			{ID: "ranked", Name: "Ranked", QuoteModelIDs: []string{"quote-alias"}},
			{ID: "excluded", Name: "Excluded"},
			{ID: "priced", Name: "Priced", SourceURL: "https://source.example/catalog", SourceModelID: "priced", SourceKey: "priced-key"},
			{ID: "incomplete", Name: "Incomplete", CatalogOffer: &Offer{ID: "partial", Input: &input}},
		},
		Offers: []Offer{
			{ID: "ranked-quote", ModelID: "ranked"},
			{ID: "alias-quote", ModelID: "quote-alias"},
			{ID: "unrelated", ModelID: "unrelated"},
			{ID: "priced-quote", ModelID: "priced", Input: &input, Output: &output, SourceURL: "https://source.example/catalog", SourceModelID: "priced", ReferenceIdentity: &ReferenceOfferIdentity{SourceKey: "priced-key", QuotePlanKey: "base"}},
		},
		Rankings: map[string]Ranking{
			"ability": {Topic: "ability", Entries: []RankedEntry{{EntityID: "ranked", Rank: &rank, Score: &score}}, Excluded: []RankedEntry{{EntityID: "excluded"}}},
			"relays":  {},
		},
	}

	for _, view := range []string{"ability", "value", "price"} {
		result := projectBrowseModelBoard(catalog, "ability", view)
		if len(result.Models) != 4 || result.Models[0].ID != "ranked" || len(result.Offers) != 3 {
			t.Fatalf("%s hid directory data or lost exact quote relations: %+v", view, result)
		}
		if result.Models[0].Rank == nil || *result.Models[0].Score != score {
			t.Fatal("published ranking changed")
		}
		for _, model := range result.Models[1:] {
			if model.Rank != nil || model.Score != nil {
				t.Fatal("unranked directory model acquired a fake score")
			}
		}
	}
}
