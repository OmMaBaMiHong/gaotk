-- TokenCenter is a directory snapshot, independent of account/gateway pricing.
CREATE TABLE IF NOT EXISTS tokencenter_catalog (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    catalog JSONB NOT NULL CHECK (jsonb_typeof(catalog) = 'object'),
    source_url TEXT NOT NULL DEFAULT '',
    etag TEXT NOT NULL DEFAULT '',
    last_modified TEXT NOT NULL DEFAULT '',
    saved_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE tokencenter_catalog ADD COLUMN IF NOT EXISTS access_status SMALLINT NOT NULL DEFAULT 0;
