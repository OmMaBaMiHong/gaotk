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

-- Product interaction totals, not unique-user analytics. Independent of catalog
-- refresh/revocation and application moderation. No personal data is collected.
CREATE TABLE IF NOT EXISTS tokencenter_service_usage (
    id SMALLINT PRIMARY KEY CHECK (id = 1),
    board_clicks BIGINT NOT NULL DEFAULT 0,
    model_clicks BIGINT NOT NULL DEFAULT 0,
    platform_clicks BIGINT NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO tokencenter_service_usage (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

-- Product-owned relationships. Account identity and catalog facts remain upstream.
CREATE TABLE IF NOT EXISTS tokencenter_merchants (
    id BIGSERIAL PRIMARY KEY,
    account_issuer TEXT NOT NULL,
    account_user_id BIGINT NOT NULL,
    account_email TEXT NOT NULL,
    profile JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    review_note TEXT NOT NULL DEFAULT '',
    revision BIGINT NOT NULL DEFAULT 1,
    history JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(account_issuer, account_user_id)
);
CREATE INDEX IF NOT EXISTS tokencenter_merchants_status_id ON tokencenter_merchants(status, id DESC);
CREATE TABLE IF NOT EXISTS tokencenter_platform_claims (
    id BIGSERIAL PRIMARY KEY,
    merchant_id BIGINT NOT NULL REFERENCES tokencenter_merchants(id),
    platform_id TEXT NOT NULL,
    platform JSONB NOT NULL,
    evidence JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    review_note TEXT NOT NULL DEFAULT '',
    revision BIGINT NOT NULL DEFAULT 1,
    history JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS tokencenter_claim_one_owner ON tokencenter_platform_claims(platform_id) WHERE status='approved';
CREATE UNIQUE INDEX IF NOT EXISTS tokencenter_claim_pending ON tokencenter_platform_claims(merchant_id, platform_id) WHERE status='pending';
CREATE INDEX IF NOT EXISTS tokencenter_claim_merchant_id ON tokencenter_platform_claims(merchant_id, id DESC);
CREATE INDEX IF NOT EXISTS tokencenter_claim_status_id ON tokencenter_platform_claims(status, id DESC);
CREATE TABLE IF NOT EXISTS tokencenter_ad_slots (
    id BIGSERIAL PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    revision BIGINT NOT NULL DEFAULT 1,
    history JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO tokencenter_ad_slots(code,name,description) VALUES('home-sponsor','首页品牌赞助位','首页领域海报下方；图片建议 16:9，支持品牌名称与落地页。') ON CONFLICT(code) DO NOTHING;
CREATE TABLE IF NOT EXISTS tokencenter_ad_campaigns (
    id BIGSERIAL PRIMARY KEY,
    merchant_id BIGINT NOT NULL REFERENCES tokencenter_merchants(id),
    claim_id BIGINT NOT NULL REFERENCES tokencenter_platform_claims(id),
    slot_id BIGINT NOT NULL REFERENCES tokencenter_ad_slots(id),
    creative JSONB NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    review_note TEXT NOT NULL DEFAULT '',
    revision BIGINT NOT NULL DEFAULT 1,
    history JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK(ends_at > starts_at)
);
CREATE INDEX IF NOT EXISTS tokencenter_ad_merchant_id ON tokencenter_ad_campaigns(merchant_id,id DESC);
CREATE INDEX IF NOT EXISTS tokencenter_ad_claim ON tokencenter_ad_campaigns(claim_id);
CREATE INDEX IF NOT EXISTS tokencenter_ad_status_id ON tokencenter_ad_campaigns(status,id DESC);
CREATE INDEX IF NOT EXISTS tokencenter_ad_schedule ON tokencenter_ad_campaigns(slot_id,starts_at,ends_at) WHERE status='approved';

-- User star reviews for catalog objects (relay sites, relay groups, merchants,
-- products). Post-style per product decision 2026-11-13: published by default,
-- no manual moderation; status reserves blocked/revoked for machine keyword
-- filtering and post-hoc takedown, pending for future anti-brigading. Contact
-- is private (dedupe key: one vote per contact per object, latest wins) and is
-- never served by the public API.
CREATE TABLE IF NOT EXISTS tokencenter_reviews (
    id BIGSERIAL PRIMARY KEY,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    stars INT NOT NULL CHECK (stars BETWEEN 1 AND 5),
    dimensions JSONB NOT NULL DEFAULT '[]'::jsonb,
    body TEXT NOT NULL DEFAULT '',
    contact TEXT NOT NULL,
    evidence TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'published',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (contact, object_type, object_id)
);
CREATE INDEX IF NOT EXISTS tokencenter_reviews_object_published
    ON tokencenter_reviews (object_type, object_id, id DESC) WHERE status = 'published';
