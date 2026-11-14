package tokencenter

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
)

//go:embed catalog_schema.sql
var catalogSchema string

type Snapshot struct {
	AccessStatus int
	Catalog      Catalog
	SourceURL    string
	ETag         string
	LastModified string
}
type Store interface {
	Load(context.Context) (Snapshot, error)
	Save(context.Context, Snapshot, bool) error
	SetAccessStatus(context.Context, string, int) error
}
type SQLStore struct{ db *sql.DB }

func NewSQLStore(db *sql.DB) *SQLStore { return &SQLStore{db: db} }

// Migrate applies only TokenCenter's schema to its dedicated database.
// Both launchers use it; account migrations never include this module's DDL.
func (s *SQLStore) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, catalogSchema)
	return err
}
func (s *SQLStore) Load(ctx context.Context) (Snapshot, error) {
	return s.load(ctx, false, "", "")
}

// Browse retains every record and its published display fields. Full historical
// evaluations and model billing are fetched only by the model detail request.
// This is a read projection; the stored publication is never rewritten.
func (s *SQLStore) LoadBrowse(ctx context.Context) (Snapshot, error) {
	return s.LoadBrowseForRanking(ctx, "")
}

func (s *SQLStore) LoadBrowseForRanking(ctx context.Context, rankingKey string) (Snapshot, error) {
	return s.load(ctx, true, "", rankingKey)
}

func (s *SQLStore) LoadForModel(ctx context.Context, modelID string) (Snapshot, error) {
	return s.load(ctx, true, modelID, "")
}

func (s *SQLStore) load(ctx context.Context, browse bool, modelID, rankingKey string) (Snapshot, error) {
	var result Snapshot
	var data, relayEntries []byte
	projection := `catalog`
	var args []any
	if browse {
		projection = `jsonb_set(catalog, '{models}', COALESCE((SELECT jsonb_agg(CASE WHEN item->>'id'=$1 AND $1<>'' THEN item ELSE item - 'evaluations' - 'billing' END ORDER BY ordinal) FROM jsonb_array_elements(catalog->'models') WITH ORDINALITY AS entries(item, ordinal)), '[]'::jsonb))`
		args = []any{modelID}
		if modelID == "" {
			projection = `jsonb_set(` + projection + `, '{rankings}', COALESCE((SELECT jsonb_object_agg(key,
				(CASE WHEN key = COALESCE(NULLIF($2, ''), NULLIF(catalog->>'defaultRanking', ''), 'intelligence')
					THEN value ELSE (value - 'entries' - 'excluded') || jsonb_build_object('entries', '[]'::jsonb) END)
				|| jsonb_build_object(
					'entryCount', COALESCE(jsonb_array_length(NULLIF(value->'entries', 'null'::jsonb)), 0),
					'excludedCount', COALESCE(jsonb_array_length(NULLIF(value->'excluded', 'null'::jsonb)), 0),
					'detailsIncluded', key = COALESCE(NULLIF($2, ''), NULLIF(catalog->>'defaultRanking', ''), 'intelligence')))
				FROM jsonb_each(COALESCE(catalog->'rankings', '{}'::jsonb))), '{}'::jsonb))`
			args = append(args, rankingKey)
		}
	}
	query := `SELECT ` + projection + `, source_url, etag, last_modified, access_status`
	destinations := []any{&data, &result.SourceURL, &result.ETag, &result.LastModified, &result.AccessStatus}
	if browse && modelID == "" {
		// Relay display ranks come from this same row/revision, independently of
		// the model board selection. They are never included in board overviews.
		query += `, catalog #> '{rankings,relays,entries}'`
		destinations = append(destinations, &relayEntries)
	}
	query += ` FROM tokencenter_catalog WHERE id = 1`
	err := s.db.QueryRowContext(ctx, query, args...).Scan(destinations...)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrEmpty
	}
	if err != nil {
		return result, err
	}
	if result.AccessStatus != 0 {
		return result, nil
	}
	result.Catalog, err = DecodeStored(bytes.NewReader(data))
	if err == nil && len(relayEntries) > 0 {
		err = json.Unmarshal(relayEntries, &result.Catalog.browseRelayEntries)
	}
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
	views, err := materializedBoardViews(c)
	if err != nil {
		return err
	}
	query := `INSERT INTO tokencenter_catalog (id, catalog, source_url, etag, last_modified,board_views) VALUES (1, $1::jsonb, $2, $3, $4,$5::jsonb) ON CONFLICT (id) `
	if seedOnly {
		query += `DO NOTHING`
	} else {
		query += `DO UPDATE SET catalog = EXCLUDED.catalog, board_views=EXCLUDED.board_views, source_url = EXCLUDED.source_url, etag = EXCLUDED.etag, last_modified = EXCLUDED.last_modified, access_status = 0, saved_at = NOW()`
	}
	_, err = s.db.ExecContext(ctx, query, string(data), snapshot.SourceURL, snapshot.ETag, snapshot.LastModified, string(views))
	return err
}

func (s *SQLStore) SetAccessStatus(ctx context.Context, source string, status int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO tokencenter_catalog(id,catalog,source_url,access_status) VALUES(1,'{"version":"pending","models":[],"sites":[],"offers":[]}'::jsonb,$1,$2) ON CONFLICT(id) DO UPDATE SET access_status=EXCLUDED.access_status,source_url=EXCLUDED.source_url,etag='',last_modified=''`, source, status)
	return err
}
