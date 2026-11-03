-- Argus initial schema.
--
-- Amounts are NUMERIC(78,0): wide enough for 2^256 (78 digits) and exact. The
-- Go side always passes and reads them as text (::text casts), never floats.

CREATE TABLE tokens (
    id         TEXT PRIMARY KEY,                 -- full Intents id, e.g. nep141:wrap.near
    standard   TEXT NOT NULL,                    -- nep141 | nep245 | nep171
    contract   TEXT NOT NULL,
    inner_id   TEXT NOT NULL DEFAULT '',
    symbol     TEXT NOT NULL,
    decimals   INTEGER NOT NULL,
    source     TEXT NOT NULL DEFAULT '',         -- enumeration | 1click | config
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    active     BOOLEAN NOT NULL DEFAULT TRUE
);

-- One row per (token, block). Retention: at the default 30 s interval and ~120
-- tokens this is ~350k rows/day (~130M/year). Use store.PruneSnapshots to drop
-- old healthy rows (negative-gap rows are kept forever); a further option is
-- time partitioning by block_time if the table outgrows a single node.
CREATE TABLE snapshots (
    token_id     TEXT        NOT NULL,           -- no FK: history outlives token rows
    block_height BIGINT      NOT NULL,
    block_hash   TEXT        NOT NULL DEFAULT '',
    block_time   TIMESTAMPTZ NOT NULL,
    owed         NUMERIC(78,0) NOT NULL,
    held         NUMERIC(78,0) NOT NULL,
    gap          NUMERIC(78,0) NOT NULL,         -- held - owed, may be negative
    provider     TEXT        NOT NULL DEFAULT '',
    taken_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (token_id, block_height)
);
CREATE INDEX snapshots_token_time_idx ON snapshots (token_id, block_time);
-- Cheap range scans for pruning and CSV export across all tokens (append-only,
-- so block_time correlates with physical order).
CREATE INDEX snapshots_block_time_brin ON snapshots USING BRIN (block_time);
-- Tiny partial index: every negative-gap row, for history downsampling,
-- incident ranges and prune-keep-negative.
CREATE INDEX snapshots_negative_idx ON snapshots (token_id, block_height) WHERE gap < 0;

CREATE TABLE token_states (
    token_id             TEXT PRIMARY KEY,
    status               TEXT NOT NULL,
    negative_streak      INTEGER NOT NULL DEFAULT 0,
    first_negative_block BIGINT NOT NULL DEFAULT 0,
    open_incident_id     BIGINT NOT NULL DEFAULT 0,
    last_block           BIGINT NOT NULL DEFAULT 0,
    max_deficit          NUMERIC(78,0),
    max_deficit_block    BIGINT NOT NULL DEFAULT 0,
    red_block            BIGINT NOT NULL DEFAULT 0,
    stale                BOOLEAN NOT NULL DEFAULT FALSE,
    last_error           TEXT NOT NULL DEFAULT '',
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE incidents (
    id                BIGSERIAL PRIMARY KEY,
    token_id          TEXT NOT NULL,
    start_block       BIGINT NOT NULL,
    red_block         BIGINT NOT NULL,
    end_block         BIGINT,
    opened_at         TIMESTAMPTZ NOT NULL,
    closed_at         TIMESTAMPTZ,
    max_deficit       NUMERIC(78,0) NOT NULL,
    max_deficit_block BIGINT NOT NULL,
    summary           TEXT NOT NULL DEFAULT ''
);
-- At most one open incident per token; makes OpenIncident idempotent.
CREATE UNIQUE INDEX incidents_one_open_per_token ON incidents (token_id) WHERE closed_at IS NULL;
CREATE INDEX incidents_opened_at_idx ON incidents (opened_at DESC);
CREATE INDEX incidents_token_idx ON incidents (token_id, opened_at DESC);

CREATE TABLE incident_events (
    id           BIGSERIAL PRIMARY KEY,
    incident_id  BIGINT NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    block_height BIGINT NOT NULL,
    receipt_id   TEXT NOT NULL,
    tx_hash      TEXT NOT NULL DEFAULT '',
    standard     TEXT NOT NULL,
    event        TEXT NOT NULL,
    token_id     TEXT NOT NULL DEFAULT '',
    amount       TEXT NOT NULL DEFAULT '',
    raw          TEXT NOT NULL
);
CREATE INDEX incident_events_incident_idx ON incident_events (incident_id, block_height);
-- Re-attaching the same event is a no-op.
CREATE UNIQUE INDEX incident_events_dedupe ON incident_events (incident_id, receipt_id, event, token_id, md5(raw));

CREATE TABLE subscriptions (
    id         BIGSERIAL PRIMARY KEY,
    channel    TEXT NOT NULL CHECK (channel IN ('telegram', 'webhook')),
    target     TEXT NOT NULL,                    -- telegram chat id or webhook URL
    token_id   TEXT NOT NULL DEFAULT '*',        -- '*' = every token
    secret     TEXT NOT NULL DEFAULT '',         -- webhook HMAC secret
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (channel, target, token_id)
);
CREATE INDEX subscriptions_lookup_idx ON subscriptions (channel, token_id);

-- One row per completed snapshot round, for /v1/status and /readyz.
CREATE TABLE rounds (
    id           BIGSERIAL PRIMARY KEY,
    block_height BIGINT NOT NULL,
    started_at   TIMESTAMPTZ NOT NULL,
    finished_at  TIMESTAMPTZ NOT NULL,
    tokens_ok    INTEGER NOT NULL,
    tokens_failed INTEGER NOT NULL,
    provider     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX rounds_finished_idx ON rounds (finished_at DESC);
