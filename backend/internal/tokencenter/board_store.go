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

// Persist presentation slices atomically with the source snapshot. This reuses
// the existing display projection; neither scoring nor source governance runs
// here. Requests read one row by its numeric PK, not the full fact catalog.
func materializedBoardViews(c Catalog) ([]byte, error) {
	views := map[string]Catalog{}
	build := func(key, view string) {
		out := projectBrowseModelBoard(selectBrowseRankings(c, key), key, view)
		out.Summary = summarizeCatalog(c)
		used := map[string]bool{}
		for _, offer := range out.Offers {
			used[offer.SiteID] = true
		}
		sites := []Site{}
		for _, site := range out.Sites {
			if used[site.ID] {
				sites = append(sites, site)
			}
		}
		out.Sites = sites
		for i := range out.Models {
			out.Models[i].Evaluations = nil
			out.Models[i].Billing = nil
		}
		out.Relays = nil
		out.OfficialModels = nil
		out.Home = nil
		out.Platforms = nil
		out.Resources = nil
		out.OfficialSubscriptions = nil
		out.RegionalSubscriptions = nil
		out.SubscriptionMerchants = nil
		views[boardProjectionKey(view, key)] = out
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

func (s *SQLStore) LoadBoard(ctx context.Context, rankingKey, view string) (Snapshot, error) {
	var out Snapshot
	if view != "value" && view != "ability" && view != "price" {
		return out, errBoardProjectionMissing
	}
	var body []byte
	err := s.db.QueryRowContext(ctx, `SELECT board_views->$1,source_url,etag,last_modified,access_status FROM tokencenter_catalog WHERE id=1`, boardProjectionKey(view, rankingKey)).Scan(&body, &out.SourceURL, &out.ETag, &out.LastModified, &out.AccessStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrEmpty
	}
	if err != nil {
		return out, err
	}
	if out.AccessStatus != 0 {
		return out, nil
	}
	if len(body) == 0 || string(body) == "null" {
		return out, errBoardProjectionMissing
	}
	// Only Save can populate this column, from a normalized admitted snapshot.
	// Re-normalizing a display projection would discard its frozen score DTO.
	if err = json.Unmarshal(body, &out.Catalog); err != nil {
		return out, err
	}
	out.Catalog.boardProjection = view
	return out, nil
}
