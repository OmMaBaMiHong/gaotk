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
