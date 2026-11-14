package tokencenter

import (
	"bytes"
	"encoding/json"
	"time"
)

// Display projections preserve the publication's identities, order and scores.
// The browser chooses a board; it does not join or derive ranking records.
func projectModelBoard(c Catalog, key string) Catalog {
	offers := publishedReferenceOffers(c.Offers)
	models := make([]Model, len(c.Models))
	for i, model := range c.Models {
		model.ValueScore, model.ReferenceOfferID, model.ReferenceOffer = nil, "", nil
		models[i] = projectCatalogOffer(model, offers)
	}
	c.Models = models
	c.Relays = projectRelayBoard(c)
	key = selectedModelBoardKey(c, key)
	ranking, exists := c.Rankings[key]
	if !exists {
		c.OfficialModels = projectOfficialModels(c)
		c.Home = &HomeProjection{Models: []Model{}, Relays: firstRankedRelays(c.Relays)}
		return c
	}
	lookup := map[string]Model{}
	for _, m := range c.Models {
		lookup[m.ID] = m
	}
	rows := []Model{}
	seen := map[string]bool{}
	for _, entry := range ranking.Entries {
		m, ok := lookup[rankedModelID(entry)]
		if !ok {
			continue
		}
		m.Rank, m.Score = entry.Rank, entry.Score
		m = projectValueScore(m, entry.Metadata, offers)
		rows = append(rows, m)
		seen[m.ID] = true
	}
	for _, entry := range ranking.Excluded {
		if m, ok := lookup[rankedModelID(entry)]; ok {
			lookup[m.ID] = projectValueScore(m, entry.Metadata, offers)
		}
	}
	for _, m := range c.Models {
		if !seen[m.ID] {
			m = lookup[m.ID]
			m.Rank, m.Score = nil, nil
			rows = append(rows, m)
		}
	}
	c.Models = rows
	c.RankingKey = key
	c.OfficialModels = projectOfficialModels(c)
	c.Home = projectHome(c)
	return c
}

// Browse retains the admitted directory independently of rank/price coverage.
// Published scores annotate these records; missing measurements never hide a
// model's existing identity, specification or purchasing information.
func projectBrowseModelBoard(c Catalog, key, boardView string) Catalog {
	c = projectModelBoard(c, key)
	modelIDs := map[string]bool{}
	for _, model := range c.Models {
		modelIDs[model.ID] = true
		for _, quoteModelID := range model.QuoteModelIDs {
			modelIDs[quoteModelID] = true
		}
	}
	if boardView == "ability" || boardView == "value" || boardView == "price" {
		offers := make([]Offer, 0, len(c.Offers))
		for _, offer := range c.Offers {
			if modelIDs[offer.ModelID] {
				offers = append(offers, offer)
			}
		}
		c.Offers = offers
	}
	boards := make(map[string]Ranking, len(c.Rankings))
	for key, board := range c.Rankings {
		entries := make([]RankedEntry, len(board.Entries))
		for i, entry := range board.Entries {
			entry.Metadata = nil
			entries[i] = entry
		}
		board.Entries, board.Excluded = entries, nil
		boards[key] = board
	}
	c.Rankings = boards
	return c
}

func selectedModelBoardKey(c Catalog, key string) string {
	if key != "" {
		return key
	}
	if c.DefaultRanking != "" {
		return c.DefaultRanking
	}
	return "intelligence"
}

// The SQL store applies this projection before transferring JSON from the
// database. This shared fallback keeps other Store implementations equivalent.
func selectBrowseRankings(c Catalog, key string) Catalog {
	key = selectedModelBoardKey(c, key)
	if c.browseRelayEntries == nil {
		c.browseRelayEntries = c.Rankings["relays"].Entries
	}
	boards := make(map[string]Ranking, len(c.Rankings))
	for boardKey, board := range c.Rankings {
		if board.EntryCount == nil {
			count := len(board.Entries)
			board.EntryCount = &count
		}
		if board.ExcludedCount == nil {
			count := len(board.Excluded)
			board.ExcludedCount = &count
		}
		included := boardKey == key
		board.DetailsIncluded = &included
		if !included {
			board.Entries, board.Excluded = []RankedEntry{}, nil
		}
		boards[boardKey] = board
	}
	c.Rankings = boards
	return c
}

type referenceOffers struct {
	byID            map[string]Offer
	byIdentity      map[ReferenceOfferIdentity][]Offer
	byCatalogSource map[catalogOfferSource][]Offer
}

type catalogOfferSource struct {
	modelID, sourceURL, sourceModelID, sourceKey string
}

func publishedReferenceOffers(offers []Offer) referenceOffers {
	result := referenceOffers{byID: map[string]Offer{}, byIdentity: map[ReferenceOfferIdentity][]Offer{}, byCatalogSource: map[catalogOfferSource][]Offer{}}
	for _, offer := range offers {
		result.byID[offer.ID] = offer
		if offer.ReferenceIdentity != nil {
			identity := *offer.ReferenceIdentity
			result.byIdentity[identity] = append(result.byIdentity[identity], offer)
			if identity.QuotePlanKey == "base" {
				source := catalogOfferSource{offer.ModelID, offer.SourceURL, offer.SourceModelID, identity.SourceKey}
				result.byCatalogSource[source] = append(result.byCatalogSource[source], offer)
			}
		}
	}
	return result
}

// Explain only the model's already-published directory price pair. This lookup
// does not choose a quote for a benchmark alias or a value-ranking workload.
func projectCatalogOffer(model Model, offers referenceOffers) Model {
	model.CatalogOffer = nil
	if model.SourceURL == "" || model.SourceModelID == "" {
		return model
	}
	source := catalogOfferSource{model.ID, model.SourceURL, model.SourceModelID, model.SourceKey}
	for _, offer := range offers.byCatalogSource[source] {
		if !sameOptionalFloat64(model.Input, offer.Input) || !sameOptionalFloat64(model.Output, offer.Output) {
			continue
		}
		if model.CatalogOffer != nil {
			model.CatalogOffer = nil
			return model
		}
		quote := offer
		model.CatalogOffer = &quote
	}
	return model
}

func sameOptionalFloat64(a, b *float64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// Copy the central publication's score evidence and exact reference quote. A
// missing or ambiguous quote remains unavailable; no price or name heuristic is
// used to substitute another offer.
func projectValueScore(model Model, metadata json.RawMessage, offers referenceOffers) Model {
	var row struct {
		ValueScore json.RawMessage `json:"valueScore"`
		Metadata   struct {
			ValueScore json.RawMessage `json:"valueScore"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(metadata, &row); err != nil {
		return model
	}
	valueScore := row.Metadata.ValueScore
	if len(valueScore) == 0 {
		valueScore = row.ValueScore
	}
	if len(valueScore) == 0 || string(valueScore) == "null" {
		return model
	}
	model.ValueScore = append(json.RawMessage(nil), valueScore...)
	var reference struct {
		ID       string                  `json:"referenceOfferId"`
		Identity *ReferenceOfferIdentity `json:"referenceOfferIdentity"`
		Snapshot json.RawMessage         `json:"referenceOfferSnapshot"`
	}
	if err := json.Unmarshal(valueScore, &reference); err != nil {
		return model
	}
	model.ReferenceOfferID = reference.ID
	snapshot, ok := publishedReferenceSnapshot(reference.Snapshot)
	if !ok || reference.Identity != nil && *reference.Identity != *snapshot.Identity {
		return model
	}
	if reference.ID != "" {
		if offer, ok := offers.byID[reference.ID]; ok && matchesPublishedReferenceSnapshot(offer, snapshot) {
			model.ReferenceOffer = &offer
		}
	} else {
		if matches := offers.byIdentity[*snapshot.Identity]; len(matches) == 1 && matchesPublishedReferenceSnapshot(matches[0], snapshot) {
			offer := matches[0]
			model.ReferenceOfferID, model.ReferenceOffer = offer.ID, &offer
		}
	}
	return model
}

func publishedReferenceSnapshot(raw json.RawMessage) (ReferenceOfferSnapshot, bool) {
	var snapshot ReferenceOfferSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.SchemaVersion != 1 || snapshot.ID == "" || snapshot.Identity == nil || snapshot.SourceContentHash == "" || snapshot.QuoteContentHash == "" || snapshot.ObservedAt == nil || *snapshot.ObservedAt == "" {
		return snapshot, false
	}
	// An omitted nullable field is not a frozen unknown value. Require the full
	// publisher contract so a partial old reference cannot match today's quote.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return snapshot, false
	}
	for _, field := range []string{"input", "output", "cacheRead", "cacheWrite", "currency", "unit", "condition", "sourceMetadata", "billing"} {
		if _, exists := fields[field]; !exists {
			return snapshot, false
		}
	}
	return snapshot, true
}

func matchesPublishedReferenceSnapshot(offer Offer, snapshot ReferenceOfferSnapshot) bool {
	if offer.ID != snapshot.ID || offer.ReferenceIdentity == nil || *offer.ReferenceIdentity != *snapshot.Identity || offer.SourceContentHash != snapshot.SourceContentHash || offer.UpdatedAt == nil || snapshot.ObservedAt == nil {
		return false
	}
	offerTime, offerErr := time.Parse(time.RFC3339Nano, *offer.UpdatedAt)
	snapshotTime, snapshotErr := time.Parse(time.RFC3339Nano, *snapshot.ObservedAt)
	return offerErr == nil && snapshotErr == nil && offerTime.Equal(snapshotTime) &&
		offer.Currency == snapshot.Currency && offer.Unit == snapshot.Unit &&
		sameOptionalFloat64(offer.Input, snapshot.Input) && sameOptionalFloat64(offer.Output, snapshot.Output) &&
		sameOptionalFloat64(offer.CacheRead, snapshot.CacheRead) && sameOptionalFloat64(offer.CacheWrite, snapshot.CacheWrite) &&
		samePublishedJSON(offer.Condition, snapshot.Condition) && samePublishedJSON(offer.SourceMetadata, snapshot.SourceMetadata) && samePublishedJSON(offer.Billing, snapshot.Billing)
}

func samePublishedJSON(left, right json.RawMessage) bool {
	if len(bytes.TrimSpace(left)) == 0 {
		left = json.RawMessage("null")
	}
	if len(bytes.TrimSpace(right)) == 0 {
		right = json.RawMessage("null")
	}
	a, aErr := canonicalValue(left)
	b, bErr := canonicalValue(right)
	return aErr == nil && bErr == nil && bytes.Equal(a, b)
}

// Join each published manufacturer quote to its model for display. Keep every
// price/condition pair intact; never combine input/output minima across offers.
func projectOfficialModels(c Catalog) []Model {
	if c.Demo {
		return c.Models
	}
	models := map[string]Model{}
	for _, model := range c.Models {
		models[model.ID] = model
	}
	manufacturers := map[string]bool{}
	for _, site := range c.Sites {
		manufacturers[site.ID] = site.ProviderKind == "manufacturer"
	}
	rows := []Model{}
	for _, offer := range c.Offers {
		if !manufacturers[offer.SiteID] || offer.SourceAuthority != "first_party" {
			continue
		}
		model, ok := models[offer.ModelID]
		if !ok {
			continue
		}
		model.Input, model.Output = offer.Input, offer.Output
		model.SourceURL, model.SourceName = offer.SourceURL, offer.SourceName
		// Official rows are already projected from this exact offer via QuoteID.
		model.CatalogOffer = nil
		model.QuoteID = offer.ID
		var condition struct {
			Period   string `json:"period"`
			TimeZone string `json:"timeZone"`
		}
		_ = json.Unmarshal(offer.Condition, &condition)
		model.QuotePeriod, model.QuoteTimeZone = condition.Period, condition.TimeZone
		rows = append(rows, model)
	}
	return rows
}

type HomeProjection struct {
	Models []Model `json:"models"`
	Relays []Site  `json:"relays"`
}

func projectHome(c Catalog) *HomeProjection {
	home := &HomeProjection{Models: []Model{}, Relays: []Site{}}
	for _, m := range c.Models {
		if m.Rank != nil {
			home.Models = append(home.Models, m)
			if len(home.Models) == 5 {
				break
			}
		}
	}
	home.Relays = firstRankedRelays(c.Relays)
	return home
}

func firstRankedRelays(rows []Site) []Site {
	result := []Site{}
	for _, row := range rows {
		if row.Rank != nil {
			result = append(result, row)
		}
		if len(result) == 5 {
			break
		}
	}
	return result
}

func projectRelayBoard(c Catalog) []Site {
	lookup, seen := map[string]Site{}, map[string]bool{}
	for _, site := range c.Sites {
		if site.ProviderKind == "relay" {
			lookup[site.ID] = site
		}
	}
	rows := []Site{}
	entries := c.Rankings["relays"].Entries
	if c.browseRelayEntries != nil {
		entries = c.browseRelayEntries
	}
	for _, entry := range entries {
		if site, ok := lookup[entry.EntityID]; ok {
			site.Rank, site.Score = entry.Rank, entry.Score
			rows = append(rows, site)
			seen[site.ID] = true
		}
	}
	for _, site := range c.Sites {
		if site.ProviderKind == "relay" && !seen[site.ID] {
			site.Rank, site.Score = nil, nil
			rows = append(rows, site)
		}
	}
	return rows
}

type EvaluationDimensions struct {
	Backend         *float64 `json:"backend"`
	Frontend        *float64 `json:"frontend"`
	Knowledge       *float64 `json:"knowledge"`
	Aesthetic       *float64 `json:"aesthetic"`
	Maintainability *float64 `json:"maintainability"`
}

type EvaluationConfiguration struct {
	CandidateID     string   `json:"candidate_id"`
	ModelID         string   `json:"model_id"`
	Effort          string   `json:"reasoning_effort"`
	Overall         *float64 `json:"overall_score"`
	Backend         *float64 `json:"backend_score"`
	Frontend        *float64 `json:"frontend_score"`
	Knowledge       *float64 `json:"knowledge_score"`
	Aesthetic       *float64 `json:"aesthetic_score"`
	Maintainability *float64 `json:"maintainability_score"`
}
