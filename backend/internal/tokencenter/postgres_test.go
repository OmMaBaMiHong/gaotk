package tokencenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// Opt-in only: never discovers credentials or starts database containers.
// Uses a temporary schema in an explicitly supplied dedicated TokenCenter test DB.
func TestPostgresPersistenceAcrossReopen(t *testing.T) {
	dsn := os.Getenv("TOKENCENTER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TOKENCENTER_TEST_DATABASE_URL not configured; real PostgreSQL restart/persistence verification skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	open := func() *sql.DB {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		return db
	}
	db := open()
	var dbname string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&dbname); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if dbname != "tokencenter_test" {
		db.Close()
		t.Fatal("integration test requires dedicated database named tokencenter_test")
	}
	schema := "tokencenter_test_" + time.Now().UTC().Format("20060102150405_000000000")
	// schema contains only the fixed prefix, digits and underscores.
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	defer func() {
		cleanup := open()
		defer cleanup.Close()
		_, _ = cleanup.ExecContext(context.Background(), `DROP SCHEMA `+schema+` CASCADE`)
	}()
	setPath := func(db *sql.DB) {
		if _, err := db.ExecContext(ctx, `SET search_path TO `+schema); err != nil {
			t.Fatal(err)
		}
	}
	setPath(db)
	store := NewSQLStore(db)
	if err := store.Migrate(ctx); err != nil {
		db.Close()
		t.Fatal(err)
	}
	service := testService(t, store, "")
	c := fixture(t)
	c.Version = "persisted-import"
	if _, err := service.Import(ctx, c); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	// Close and reopen SQL pool as a process restart would.
	db = open()
	defer db.Close()
	setPath(db)
	restarted := testService(t, NewSQLStore(db), "")
	got, err := restarted.Catalog(ctx)
	if err != nil || got.Version != "persisted-import" {
		t.Fatalf("persisted catalog missing: %v", err)
	}
	before, _ := json.Marshal(got)
	bad := fixture(t)
	bad.Offers[0].Input = ptr(-10.0)
	if _, err := restarted.Import(ctx, bad); err == nil {
		t.Fatal("invalid import accepted")
	}
	restarted.cfg.SeedDemo = true
	if err := restarted.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := restarted.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(after)
	if string(before) != string(encoded) {
		t.Fatal("seed/invalid import changed prior snapshot")
	}
}
