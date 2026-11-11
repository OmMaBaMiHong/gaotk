package tokencenter

import "encoding/json"

// Display projections preserve the publication's identities, order and scores.
// The browser chooses a board; it does not join or derive ranking records.
func projectModelBoard(c Catalog, key string) Catalog {
	c.Relays = projectRelayBoard(c)
	if key == "" {
		key = c.DefaultRanking
	}
	if key == "" {
		key = "intelligence"
	}
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
		m, ok := lookup[entry.EntityID]
		if !ok {
			continue
		}
		m.Rank, m.Score = entry.Rank, entry.Score
		rows = append(rows, m)
		seen[m.ID] = true
	}
	for _, m := range c.Models {
		if !seen[m.ID] {
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
	for _, entry := range c.Rankings["relays"].Entries {
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
