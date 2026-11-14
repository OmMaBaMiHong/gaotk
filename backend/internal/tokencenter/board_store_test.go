package tokencenter

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBoardReadKeepsPublishedScoresAndRevocation(t *testing.T) {
	c := fixture(t)
	c.Sites = append(c.Sites, Site{ID: "registered-manufacturer", Name: "New Lab", ProviderKind: "manufacturer"})
	rank := 1
	score := 83.25
	c.Rankings = map[string]Ranking{"value-general-balanced": {Entries: []RankedEntry{{EntityID: c.Models[0].ID, Rank: &rank, Score: &score}}, Excluded: []RankedEntry{}}}
	raw, err := materializedBoardViews(c)
	if err != nil {
		t.Fatal(err)
	}
	var views storedModelBoards
	if err = json.Unmarshal(raw, &views); err != nil {
		t.Fatal(err)
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	directory, _ := json.Marshal(views.Directory)
	board, _ := json.Marshal(views.Boards["value:value-general-balanced"])
	query := `SELECT board_views->\$1,board_views->'directory',board_views->'boards'->\$1,source_url,etag,last_modified,access_status FROM tokencenter_catalog WHERE id=1`
	columns := []string{"legacy", "directory", "board", "source", "etag", "modified", "access"}
	mock.ExpectQuery(query).WithArgs("value:value-general-balanced").WillReturnRows(sqlmock.NewRows(columns).AddRow(nil, directory, board, "https://source.test", "rev-1", "", 0))
	snap, err := NewSQLStore(db).LoadBoard(context.Background(), "value-general-balanced", "value")
	if err != nil || len(snap.Catalog.Models) != len(c.Models) || snap.Catalog.Models[0].Score == nil || *snap.Catalog.Models[0].Score != score || snap.Catalog.Summary.ModelRecords != len(c.Models) {
		t.Fatalf("published display was recomputed or lost: %+v %v", snap, err)
	}
	visibleManufacturer := false
	for _, site := range snap.Catalog.Sites {
		if site.ID == "registered-manufacturer" {
			visibleManufacturer = true
		}
	}
	if !visibleManufacturer {
		t.Fatal("manufacturer without models or offers was hidden")
	}
	wantOffers := make([]Offer, len(c.Offers))
	for i, offer := range c.Offers {
		wantOffers[i] = browseOffer(offer)
	}
	if !reflect.DeepEqual(snap.Catalog.Offers, wantOffers) {
		t.Fatal("shared publication lost channel offers")
	}
	for _, model := range views.Directory.Models {
		if model.Score != nil || model.Rank != nil || len(model.ValueScore) != 0 {
			t.Fatal("shared identity contains board-specific scores")
		}
	}
	// Existing snapshots remain readable before the next successful sync.
	legacy, _ := json.Marshal(snap.Catalog)
	mock.ExpectQuery(query).WithArgs("value:value-general-balanced").WillReturnRows(sqlmock.NewRows(columns).AddRow(legacy, nil, nil, "https://source.test", "rev-1", "", 0))
	old, err := NewSQLStore(db).LoadBoard(context.Background(), "value-general-balanced", "value")
	if err != nil || !reflect.DeepEqual(old.Catalog, snap.Catalog) {
		t.Fatalf("previous snapshot no longer readable: %v", err)
	}
	mock.ExpectQuery(query).WithArgs("value:value-general-balanced").WillReturnRows(sqlmock.NewRows(columns).AddRow(nil, directory, board, "https://source.test", "", "", 410))
	svc := testService(t, NewSQLStore(db), "")
	if _, err = svc.catalog(context.Background(), true, "", "value-general-balanced", "value"); !errors.Is(err, ErrSourceRevoked) {
		t.Fatalf("stored display bypassed revocation: %v", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSharedDirectoryRestoresEachBoardsOrderAndScores(t *testing.T) {
	c := fixture(t)
	first, second := c.Models[0].ID, c.Models[1].ID
	rank, ability, value := 1, 91.0, 73.5
	c.Rankings = map[string]Ranking{
		"intelligence":           {Entries: []RankedEntry{{EntityID: second, Rank: &rank, Score: &ability}}},
		"value-general-balanced": {Entries: []RankedEntry{{EntityID: first, Rank: &rank, Score: &value}}},
	}
	raw, err := materializedBoardViews(c)
	if err != nil {
		t.Fatal(err)
	}
	var views storedModelBoards
	if err := json.Unmarshal(raw, &views); err != nil {
		t.Fatal(err)
	}
	directory, _ := json.Marshal(views.Directory)
	db, mock, _ := sqlmock.New()
	defer db.Close()
	for _, test := range []struct {
		view, key, first string
		score            float64
	}{
		{"ability", "intelligence", second, ability},
		{"value", "value-general-balanced", first, value},
	} {
		board, _ := json.Marshal(views.Boards[boardProjectionKey(test.view, test.key)])
		mock.ExpectQuery(`SELECT board_views`).WithArgs(boardProjectionKey(test.view, test.key)).WillReturnRows(sqlmock.NewRows([]string{"legacy", "directory", "board", "source", "etag", "modified", "access"}).AddRow(nil, directory, board, "source", "rev-1", "", 0))
		snap, err := NewSQLStore(db).LoadBoard(context.Background(), test.key, test.view)
		if err != nil {
			t.Fatal(err)
		}
		if len(snap.Catalog.Models) != len(c.Models) || snap.Catalog.Models[0].ID != test.first || *snap.Catalog.Models[0].Score != test.score {
			t.Fatalf("board %s lost its own order/score: %+v", test.key, snap.Catalog.Models)
		}
		for _, model := range snap.Catalog.Models[1:] {
			if model.Rank != nil || model.Score != nil {
				t.Fatalf("unranked directory row inherited another board's score: %+v", model)
			}
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBrowseProjectionPreservesNativeTermsAndFullEvidence(t *testing.T) {
	c := fixture(t)
	c.Offers[0].Billing = json.RawMessage(`{"original":"native billing terms","currency":"CNY"}`)
	c.Offers[0].Condition = json.RawMessage(`{"group":"vip","discount":"source text"}`)
	c.Offers[0].ReferenceIdentity = &ReferenceOfferIdentity{SourceModelID: "original-model"}
	c.Offers[0].SourceMetadata = json.RawMessage(`{"audit":"full receipt"}`)
	before, _ := json.Marshal(c)
	raw, err := materializedBoardViews(c)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(c)
	if string(before) != string(after) {
		t.Fatal("browse projection mutated the admitted source snapshot")
	}
	var views storedModelBoards
	if err := json.Unmarshal(raw, &views); err != nil {
		t.Fatal(err)
	}
	offer := views.Directory.Offers[0]
	if !reflect.DeepEqual(offer.Billing, c.Offers[0].Billing) || !reflect.DeepEqual(offer.Condition, c.Offers[0].Condition) || offer.Input == nil || *offer.Input != *c.Offers[0].Input || offer.SourceURL != c.Offers[0].SourceURL {
		t.Fatal("compact list changed native price, terms or source")
	}
	if offer.ReferenceIdentity != nil || len(offer.SourceMetadata) > 0 {
		t.Fatal("list repeated internal matching evidence")
	}
	score := browseValueScore(json.RawMessage(`{"abilityScore":42,"missingEvidence":["original missing measurement"],"missingEvidenceCodes":["internal-code"],"unrankedReason":"original missing measurement"}`))
	var fields map[string]json.RawMessage
	json.Unmarshal(score, &fields)
	if string(fields["abilityScore"]) != "42" || string(fields["missingEvidence"]) != `["original missing measurement"]` || fields["missingEvidenceCodes"] != nil {
		t.Fatalf("list score/explanation changed: %s", score)
	}
}
