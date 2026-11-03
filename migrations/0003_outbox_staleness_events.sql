-- 0003: transactional alert outbox, per-token staleness, collision-free
-- incident event identity.

-- Alerts are written in the same transaction as the incident and state
-- changes that produced them; a relay moves them to the queue afterwards.
CREATE TABLE alert_outbox (
    id          BIGSERIAL PRIMARY KEY,
    payload     JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    enqueued_at TIMESTAMPTZ
);
CREATE INDEX alert_outbox_pending_idx ON alert_outbox (created_at) WHERE enqueued_at IS NULL;

-- When reads of a token started failing (NULL while it reads fine).
ALTER TABLE token_states ADD COLUMN stale_since TIMESTAMPTZ;

-- The old key (incident, receipt, event, token, md5(raw)) collapsed distinct
-- items of one log line (token_diff alice -100 / bob +100 on the same token).
-- The position of the log line and of the item within it identify a row.
ALTER TABLE incident_events
    ADD COLUMN log_index  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN item_index INTEGER NOT NULL DEFAULT 0;
-- Rows stored before this migration that the old key kept apart must stay
-- apart under the new one: number them within the new key's other columns.
UPDATE incident_events e SET item_index = r.rn - 1
FROM (
    SELECT id, row_number() OVER (PARTITION BY incident_id, receipt_id, token_id ORDER BY id) AS rn
    FROM incident_events
) r
WHERE e.id = r.id;
DROP INDEX incident_events_dedupe;
CREATE UNIQUE INDEX incident_events_dedupe ON incident_events (incident_id, receipt_id, log_index, item_index, token_id);
