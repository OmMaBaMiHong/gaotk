package tokencenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

var errBoardProjectionMissing = errors.New("published board projection unavailable")

func boardProjectionKey(view, key string) string {
	if view == "price" {
		return "price"
	}
	return view + ":" + key
}

type storedModelBoard struct {
	RankingKey string             `json:"rankingKey"`
	Rankings   map[string]Ranking `json:"rankings"`
	Models     []storedModelScore `json:"models"`
}

type storedModelScore struct {
	ID               string          `json:"id"`
	Rank             *int            `json:"rank,omitempty"`
	Score            *float64        `json:"score,omitempty"`
	ValueScore       json.RawMessage `json:"valueScore,omitempty"`
	ReferenceOfferID string          `json:"referenceOfferId,omitempty"`
	ReferenceOffer   *Offer          `json:"referenceOffer,omitempty"`
}

type storedModelBoards struct {
	Directory Catalog                     `json:"directory"`
	Boards    map[string]storedModelBoard `json:"boards"`
}

// Persist presentation slices atomically with the source snapshot. This reuses
// the existing display projection; neither scoring nor source governance runs
// here. Requests read one row by its numeric PK, not the full fact catalog.
func materializedBoardViews(c Catalog) ([]byte, error) {
	views := storedModelBoards{Boards: map[string]storedModelBoard{}}
	build := func(key, view string) {
		out := projectBrowseModelBoard(selectBrowseRankings(c, key), key, view)
		out.Summary = summarizeCatalog(c)
		used := map[string]bool{}
		for _, offer := range out.Offers {
			used[offer.SiteID] = true
		}
		sites := []Site{}
		for _, site := range out.Sites {
			if used[site.ID] || site.ProviderKind == "manufacturer" {
				sites = append(sites, site)
			}
		}
		out.Sites = sites
		for i := range out.Models {
			out.Models[i].Evaluations = nil
			out.Models[i].Billing = nil
			out.Models[i].Admission = nil
			out.Models[i].ValueScore = browseValueScore(out.Models[i].ValueScore)
			if quote := out.Models[i].CatalogOffer; quote != nil {
				// The full quote and terms already exist once in Offers. These
				// fields locate it and support price sorting in the model list.
				out.Models[i].CatalogOffer = &Offer{ID: quote.ID, ModelID: quote.ModelID, SiteID: quote.SiteID, Input: quote.Input, Output: quote.Output, Currency: quote.Currency, Unit: quote.Unit}
			}
			if quote := out.Models[i].ReferenceOffer; quote != nil {
				copy := browseOffer(*quote)
				out.Models[i].ReferenceOffer = &copy
			}
		}
		for i := range out.Offers {
			out.Offers[i] = browseOffer(out.Offers[i])
		}
		out.Relays = nil
		out.OfficialModels = nil
		out.Home = nil
		out.Platforms = nil
		out.Resources = nil
		out.OfficialSubscriptions = nil
		out.RegionalSubscriptions = nil
		out.SubscriptionMerchants = nil
		board := storedModelBoard{RankingKey: out.RankingKey, Rankings: out.Rankings, Models: make([]storedModelScore, len(out.Models))}
		for i, model := range out.Models {
			board.Models[i] = storedModelScore{model.ID, model.Rank, model.Score, model.ValueScore, model.ReferenceOfferID, model.ReferenceOffer}
			out.Models[i].Rank, out.Models[i].Score = nil, nil
			out.Models[i].ValueScore, out.Models[i].ReferenceOfferID, out.Models[i].ReferenceOffer = nil, "", nil
		}
		// Identity, logos and channel offers are shared across every board in
		// this publication. Store them once, not once per ranking preference.
		if views.Directory.Models == nil {
			out.Rankings, out.RankingKey = nil, ""
			views.Directory = out
		}
		views.Boards[boardProjectionKey(view, key)] = board
	}
	for key := range c.Rankings {
		if strings.HasPrefix(key, "value-") {
			build(key, "value")
		} else if key != "relays" && key != "providers" {
			build(key, "ability")
		}
	}
	build(c.DefaultRanking, "price")
	return json.Marshal(views)
}

// The list carries native prices, terms, source links and published score
// explanations. Internal admission receipts and matching evidence stay in the
// full catalog/detail; they must not be downloaded for every visible row.
func browseOffer(offer Offer) Offer {
	offer.Admission, offer.ReferenceIdentity, offer.SourceMetadata = nil, nil, nil
	offer.SourceContentHash = ""
	return offer
}

func browseValueScore(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return raw
	}
	for _, key := range []string{"missingEvidenceCodes", "referenceOfferSnapshot", "referenceOfferIdentity"} {
		delete(fields, key)
	}
	// The long reason repeats the individual missing-evidence list. Retain
	// the individual source explanations in browse and the full audit in detail.
	var missing []string
	if json.Unmarshal(fields["missingEvidence"], &missing) == nil && len(missing) > 0 {
		delete(fields, "unrankedReason")
	}
	result, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return result
}

func (s *SQLStore) LoadBoard(ctx context.Context, rankingKey, view string) (Snapshot, error) {
	var out Snapshot
	if view != "value" && view != "ability" && view != "price" {
		return out, errBoardProjectionMissing
	}
	var legacy, directory, board []byte
	err := s.db.QueryRowContext(ctx, `SELECT board_views->$1,board_views->'directory',board_views->'boards'->$1,source_url,etag,last_modified,access_status FROM tokencenter_catalog WHERE id=1`, boardProjectionKey(view, rankingKey)).Scan(&legacy, &directory, &board, &out.SourceURL, &out.ETag, &out.LastModified, &out.AccessStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrEmpty
	}
	if err != nil {
		return out, err
	}
	if out.AccessStatus != 0 {
		return out, nil
	}
	// Only Save can populate this column, from a normalized admitted snapshot.
	// Re-normalizing a display projection would discard its frozen score DTO.
	if len(directory) > 0 && string(directory) != "null" {
		if len(board) == 0 || string(board) == "null" {
			return out, errBoardProjectionMissing
		}
		if err = json.Unmarshal(directory, &out.Catalog); err != nil {
			return out, err
		}
		var selected storedModelBoard
		if err = json.Unmarshal(board, &selected); err != nil {
			return out, err
		}
		models := make(map[string]Model, len(out.Catalog.Models))
		for _, model := range out.Catalog.Models {
			models[model.ID] = model
		}
		out.Catalog.Models = make([]Model, 0, len(selected.Models))
		for _, row := range selected.Models {
			model, ok := models[row.ID]
			if !ok {
				return out, errBoardProjectionMissing
			}
			model.Rank, model.Score, model.ValueScore = row.Rank, row.Score, row.ValueScore
			model.ReferenceOfferID, model.ReferenceOffer = row.ReferenceOfferID, row.ReferenceOffer
			out.Catalog.Models = append(out.Catalog.Models, model)
		}
		out.Catalog.RankingKey, out.Catalog.Rankings = selected.RankingKey, selected.Rankings
	} else {
		// Keep the last successful publication readable until the first sync
		// writes the shared layout; a rollout must not blank the directory.
		if len(legacy) == 0 || string(legacy) == "null" {
			return out, errBoardProjectionMissing
		}
		if err = json.Unmarshal(legacy, &out.Catalog); err != nil {
			return out, err
		}
	}
	out.Catalog.boardProjection = view
	return out, nil
}
