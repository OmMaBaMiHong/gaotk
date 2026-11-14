package tokencenter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBoardReadKeepsPublishedScoresAndRevocation(t *testing.T) {
	c := fixture(t)
	rank := 1
	score := 83.25
	c.Rankings = map[string]Ranking{"value-general-balanced": {Entries: []RankedEntry{{EntityID: c.Models[0].ID, Rank: &rank, Score: &score}}, Excluded: []RankedEntry{}}}
	raw, err := materializedBoardViews(c)
	if err != nil {
		t.Fatal(err)
	}
	var views map[string]json.RawMessage
	if err = json.Unmarshal(raw, &views); err != nil {
		t.Fatal(err)
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	query := `SELECT board_views->\$1,source_url,etag,last_modified,access_status FROM tokencenter_catalog WHERE id=1`
	mock.ExpectQuery(query).WithArgs("value:value-general-balanced").WillReturnRows(sqlmock.NewRows([]string{"body", "source", "etag", "modified", "access"}).AddRow(views["value:value-general-balanced"], "https://source.test", "rev-1", "", 0))
	snap, err := NewSQLStore(db).LoadBoard(context.Background(), "value-general-balanced", "value")
	if err != nil || len(snap.Catalog.Models) != 1 || snap.Catalog.Models[0].Score == nil || *snap.Catalog.Models[0].Score != score || snap.Catalog.Summary.ModelRecords != len(c.Models) {
		t.Fatalf("published display was recomputed or lost: %+v %v", snap, err)
	}
	mock.ExpectQuery(query).WithArgs("value:value-general-balanced").WillReturnRows(sqlmock.NewRows([]string{"body", "source", "etag", "modified", "access"}).AddRow(views["value:value-general-balanced"], "https://source.test", "", "", 410))
	svc := testService(t, NewSQLStore(db), "")
	if _, err = svc.catalog(context.Background(), true, "", "value-general-balanced", "value"); !errors.Is(err, ErrSourceRevoked) {
		t.Fatalf("stored display bypassed revocation: %v", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
