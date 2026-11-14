package tokencenter

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSQLStoreAtomicSnapshotAndRestartRead(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := fixture(t)
	data, _ := json.Marshal(c)
	snap := Snapshot{Catalog: c, SourceURL: "https://catalog.example.com/data", ETag: `"v1"`, LastModified: "Mon, 19 Oct 2026 00:00:00 GMT"}
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO tokencenter_catalog (id, catalog, source_url, etag, last_modified) VALUES (1, $1::jsonb, $2, $3, $4) ON CONFLICT (id) DO UPDATE SET catalog = EXCLUDED.catalog, source_url = EXCLUDED.source_url, etag = EXCLUDED.etag, last_modified = EXCLUDED.last_modified, access_status = 0, saved_at = NOW()`)).WithArgs(string(data), snap.SourceURL, snap.ETag, snap.LastModified).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := NewSQLStore(db).Save(context.Background(), snap, false); err != nil {
		t.Fatal(err)
	}
	// Recreate service and repository: no in-process cache may hide the persisted row.
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT catalog, source_url, etag, last_modified, access_status FROM tokencenter_catalog WHERE id = 1`)).WillReturnRows(sqlmock.NewRows([]string{"catalog", "source_url", "etag", "last_modified", "access_status"}).AddRow(data, snap.SourceURL, snap.ETag, snap.LastModified, 0))
	restarted := testService(t, NewSQLStore(db), "")
	got, err := restarted.Catalog(context.Background())
	if err != nil || got.Version != c.Version || len(got.Offers) != 5 {
		t.Fatalf("restart read failed: %v", err)
	}
	mock.ExpectExec(`ON CONFLICT \(id\) DO NOTHING`).WithArgs(string(data), "", "", "").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := NewSQLStore(db).Save(context.Background(), Snapshot{Catalog: c}, true); err != nil {
		t.Fatal(err)
	}
	c.Offers[0].Output = ptr(-1.0)
	if err := NewSQLStore(db).Save(context.Background(), Snapshot{Catalog: c}, false); err == nil {
		t.Fatal("invalid catalog reached SQL")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLBrowsePassesBoardSelectionAndKeepsRelayProjectionFromSameRead(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := fixture(t)
	rank, score := 7, 81.0
	c.Sites[0].ProviderKind = "relay"
	count, included := 1, false
	c.Rankings = map[string]Ranking{"relays": {Entries: []RankedEntry{}, EntryCount: &count, DetailsIncluded: &included}}
	data, _ := json.Marshal(c)
	relayEntries, _ := json.Marshal([]RankedEntry{{EntityID: c.Sites[0].ID, Rank: &rank, Score: &score}})
	mock.ExpectQuery(`SELECT jsonb_set\(jsonb_set\(catalog,.*rankings.*jsonb_object_agg.*FROM tokencenter_catalog WHERE id = 1`).WithArgs("", "value-general-balanced").WillReturnRows(sqlmock.NewRows([]string{"catalog", "source_url", "etag", "last_modified", "access_status", "relay_entries"}).AddRow(data, "https://source.example/snapshot", "publication-etag", "", 0, relayEntries))
	snapshot, err := NewSQLStore(db).LoadBrowseForRanking(context.Background(), "value-general-balanced")
	if err != nil {
		t.Fatal(err)
	}
	projected := projectBrowseModelBoard(snapshot.Catalog, "value-general-balanced", "")
	if len(projected.Relays) != 1 || projected.Relays[0].Rank == nil || *projected.Relays[0].Rank != rank || *projected.Relays[0].Score != score || snapshot.ETag != "publication-etag" || len(projected.Rankings["relays"].Entries) != 0 {
		t.Fatalf("same-snapshot relay ranks lost or leaked into board overview: %+v", projected)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
