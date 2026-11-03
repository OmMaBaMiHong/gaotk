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

-- Private contact/collection leads. They never write tokencenter_catalog.
CREATE TABLE IF NOT EXISTS tokencenter_applications (
    id BIGSERIAL PRIMARY KEY,
    receipt_hash TEXT NOT NULL UNIQUE,
    request JSONB NOT NULL,
    state JSONB NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS tokencenter_applications_status_id ON tokencenter_applications ((state->>'status'), id DESC);
