package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/emma31-dev/argus/internal/model"
)

const stateCols = `token_id, status, negative_streak, first_negative_block, open_incident_id, last_block,
	COALESCE(max_deficit::text, ''), max_deficit_block, red_block, stale, last_error, updated_at, stale_since`

// LoadStates returns stored alert states keyed by token id. With no ids it
// returns every state (used by the status endpoint).
func (r *Repo) LoadStates(ctx context.Context, tokenIDs []string) (map[string]model.TokenState, error) {
	q := `SELECT ` + stateCols + ` FROM token_states`
	var args []any
	if len(tokenIDs) > 0 {
		q += ` WHERE token_id = ANY($1::text[])`
		args = append(args, tokenIDs)
	}
	rows, err := r.q.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: load states: %w", err)
	}
	defer rows.Close()
	out := map[string]model.TokenState{}
	for rows.Next() {
		var s model.TokenState
		var status string
		var streak int32
		var first, inc, last, mdb, red int64
		var staleSince *time.Time
		if err := rows.Scan(&s.TokenID, &status, &streak, &first, &inc, &last, &s.MaxDeficit, &mdb, &red,
			&s.Stale, &s.LastError, &s.UpdatedAt, &staleSince); err != nil {
			return nil, fmt.Errorf("store: scan state: %w", err)
		}
		s.Status = model.Status(status)
		s.NegativeStreak = int(streak)
		s.FirstNegativeBlock, s.OpenIncidentID, s.LastBlock = uint64(first), inc, uint64(last)
		s.MaxDeficitBlock, s.RedBlock = uint64(mdb), uint64(red)
		if staleSince != nil {
			s.StaleSince = *staleSince
		}
		out[s.TokenID] = s
	}
	return out, rows.Err()
}

// SaveStates upserts the given states in one batch.
func (r *Repo) SaveStates(ctx context.Context, states []model.TokenState) error {
	b := &pgx.Batch{}
	for _, s := range states {
		var staleSince *time.Time
		if !s.StaleSince.IsZero() {
			ss := s.StaleSince
			staleSince = &ss
		}
		upd := s.UpdatedAt
		if upd.IsZero() {
			upd = time.Now().UTC()
		}
		b.Queue(`INSERT INTO token_states (token_id, status, negative_streak, first_negative_block, open_incident_id,
				last_block, max_deficit, max_deficit_block, red_block, stale, last_error, updated_at, stale_since)
			VALUES ($1,$2,$3,$4,$5,$6, NULLIF($7::text,'')::numeric, $8,$9,$10,$11,$12,$13)
			ON CONFLICT (token_id) DO UPDATE SET status = EXCLUDED.status, negative_streak = EXCLUDED.negative_streak,
				first_negative_block = EXCLUDED.first_negative_block, open_incident_id = EXCLUDED.open_incident_id,
				last_block = EXCLUDED.last_block, max_deficit = EXCLUDED.max_deficit,
				max_deficit_block = EXCLUDED.max_deficit_block, red_block = EXCLUDED.red_block,
				stale = EXCLUDED.stale, last_error = EXCLUDED.last_error, updated_at = EXCLUDED.updated_at,
				stale_since = EXCLUDED.stale_since`,
			s.TokenID, string(s.Status), int32(s.NegativeStreak), int64(s.FirstNegativeBlock), s.OpenIncidentID,
			int64(s.LastBlock), s.MaxDeficit, int64(s.MaxDeficitBlock), int64(s.RedBlock), s.Stale, s.LastError, upd, staleSince)
	}
	if err := r.sendBatch(ctx, b); err != nil {
		return fmt.Errorf("store: save states: %w", err)
	}
	return nil
}
