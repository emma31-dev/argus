package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/emma31-dev/argus/internal/alert"
)

// AddOutbox implements alert.Outbox: it stores alerts in the alert_outbox
// table. Called on a transactional Repo (see InTx) the rows commit or roll
// back together with the incident and state writes of the same token.
func (r *Repo) AddOutbox(ctx context.Context, alerts []alert.Alert) error {
	if len(alerts) == 0 {
		return nil
	}
	for _, a := range alerts {
		raw, err := json.Marshal(a)
		if err != nil {
			return fmt.Errorf("store: encode outbox alert: %w", err)
		}
		if _, err := r.q.Exec(ctx, `INSERT INTO alert_outbox (payload) VALUES ($1::jsonb)`, string(raw)); err != nil {
			return fmt.Errorf("store: add outbox: %w", err)
		}
	}
	return nil
}

// PendingOutbox implements alert.OutboxStore: the oldest rows not yet moved
// to the queue, in id order.
func (r *Repo) PendingOutbox(ctx context.Context, limit int) ([]alert.OutboxEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.q.Query(ctx, `SELECT id, payload::text FROM alert_outbox
		WHERE enqueued_at IS NULL ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: pending outbox: %w", err)
	}
	defer rows.Close()
	var out []alert.OutboxEntry
	for rows.Next() {
		var e alert.OutboxEntry
		var raw string
		if err := rows.Scan(&e.ID, &raw); err != nil {
			return nil, fmt.Errorf("store: scan outbox: %w", err)
		}
		if err := json.Unmarshal([]byte(raw), &e.Alert); err != nil {
			return nil, fmt.Errorf("store: decode outbox row %d: %w", e.ID, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkOutboxEnqueued implements alert.OutboxStore.
func (r *Repo) MarkOutboxEnqueued(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := r.q.Exec(ctx, `UPDATE alert_outbox SET enqueued_at = now()
		WHERE id = ANY($1::bigint[]) AND enqueued_at IS NULL`, ids); err != nil {
		return fmt.Errorf("store: mark outbox: %w", err)
	}
	return nil
}

// PruneOutbox implements alert.OutboxStore: it deletes rows that were
// enqueued before olderThan.
func (r *Repo) PruneOutbox(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.q.Exec(ctx, `DELETE FROM alert_outbox WHERE enqueued_at IS NOT NULL AND enqueued_at < $1`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("store: prune outbox: %w", err)
	}
	return tag.RowsAffected(), nil
}
