package tokencenter

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/migrations"
)

type Snapshot struct {
	Catalog      Catalog
	SourceURL    string
	ETag         string
	LastModified string
}
type Store interface {
	Load(context.Context) (Snapshot, error)
	Save(context.Context, Snapshot, bool) error
}
type SQLStore struct{ db *sql.DB }

func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{db: db} }

// Migrate is for the independent launcher only. The main app uses its migration runner.
func (s *SQLStore) Migrate(ctx context.Context) error {
	ddl, err := migrations.FS.ReadFile("247_tokencenter_catalog.sql")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, string(ddl))
	return err
}
func (s *SQLStore) Load(ctx context.Context) (Snapshot, error) {
	var result Snapshot
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT catalog, source_url, etag, last_modified FROM tokencenter_catalog WHERE id = 1`).Scan(&data, &result.SourceURL, &result.ETag, &result.LastModified)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrEmpty
	}
	if err != nil {
		return result, err
	}
	result.Catalog, err = Decode(bytes.NewReader(data))
	return result, err
}

// Save atomically replaces both data and successful conditional-fetch metadata.
// seedOnly uses DO NOTHING to avoid overwriting concurrent imports or existing data.
func (s *SQLStore) Save(ctx context.Context, snapshot Snapshot, seedOnly bool) error {
	c, err := Normalize(snapshot.Catalog)
	if err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	query := `INSERT INTO tokencenter_catalog (id, catalog, source_url, etag, last_modified) VALUES (1, $1::jsonb, $2, $3, $4) ON CONFLICT (id) `
	if seedOnly {
		query += `DO NOTHING`
	} else {
		query += `DO UPDATE SET catalog = EXCLUDED.catalog, source_url = EXCLUDED.source_url, etag = EXCLUDED.etag, last_modified = EXCLUDED.last_modified, saved_at = NOW()`
	}
	_, err = s.db.ExecContext(ctx, query, string(data), snapshot.SourceURL, snapshot.ETag, snapshot.LastModified)
	return err
}
