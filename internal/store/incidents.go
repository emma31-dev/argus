package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/emma31-dev/argus/internal/model"
)

const incCols = `id, token_id, start_block, red_block, COALESCE(end_block, 0), opened_at, closed_at,
	max_deficit::text, max_deficit_block, summary`

func scanIncident(row pgx.Row) (model.Incident, error) {
	var i model.Incident
	var start, red, end, mdb int64
	err := row.Scan(&i.ID, &i.TokenID, &start, &red, &end, &i.OpenedAt, &i.ClosedAt, &i.MaxDeficit, &mdb, &i.Summary)
	i.StartBlock, i.RedBlock, i.EndBlock, i.MaxDeficitBlock = uint64(start), uint64(red), uint64(end), uint64(mdb)
	return i, err
}

// OpenIncident creates an incident and returns its id. It is idempotent per
// token: if the token already has an open incident that one is returned (its
// max deficit is raised if the new one is larger).
func (r *Repo) OpenIncident(ctx context.Context, inc model.Incident) (int64, error) {
	opened := inc.OpenedAt
	if opened.IsZero() {
		opened = time.Now().UTC()
	}
	if inc.MaxDeficit == "" {
		inc.MaxDeficit = "0"
	}
	var id int64
	err := r.q.QueryRow(ctx, `
		INSERT INTO incidents (token_id, start_block, red_block, opened_at, max_deficit, max_deficit_block)
		VALUES ($1,$2,$3,$4,$5::text::numeric,$6)
		ON CONFLICT (token_id) WHERE closed_at IS NULL DO UPDATE SET
			max_deficit_block = CASE WHEN EXCLUDED.max_deficit > incidents.max_deficit
				THEN EXCLUDED.max_deficit_block ELSE incidents.max_deficit_block END,
			max_deficit = GREATEST(incidents.max_deficit, EXCLUDED.max_deficit)
		RETURNING id`,
		inc.TokenID, int64(inc.StartBlock), int64(inc.RedBlock), opened, inc.MaxDeficit, int64(inc.MaxDeficitBlock)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: open incident: %w", err)
	}
	return id, nil
}

// UpdateIncident raises an open incident's max deficit (it never lowers it).
func (r *Repo) UpdateIncident(ctx context.Context, id int64, maxDeficit string, maxDeficitBlock uint64) error {
	_, err := r.q.Exec(ctx, `
		UPDATE incidents SET
			max_deficit_block = CASE WHEN $2::text::numeric > max_deficit THEN $3 ELSE max_deficit_block END,
			max_deficit = GREATEST(max_deficit, $2::text::numeric)
		WHERE id = $1 AND closed_at IS NULL`, id, maxDeficit, int64(maxDeficitBlock))
	if err != nil {
		return fmt.Errorf("store: update incident: %w", err)
	}
	return nil
}

// CloseIncident marks an open incident closed (a no-op if already closed).
func (r *Repo) CloseIncident(ctx context.Context, id int64, endBlock uint64, closedAt time.Time, maxDeficit string, maxDeficitBlock uint64) error {
	if maxDeficit == "" {
		maxDeficit = "0"
	}
	_, err := r.q.Exec(ctx, `
		UPDATE incidents SET end_block = $2, closed_at = $3,
			max_deficit_block = CASE WHEN $4::text::numeric > max_deficit THEN $5 ELSE max_deficit_block END,
			max_deficit = GREATEST(max_deficit, $4::text::numeric)
		WHERE id = $1 AND closed_at IS NULL`, id, int64(endBlock), closedAt, maxDeficit, int64(maxDeficitBlock))
	if err != nil {
		return fmt.Errorf("store: close incident: %w", err)
	}
	return nil
}

// SetIncidentSummary stores the optional AI summary text.
func (r *Repo) SetIncidentSummary(ctx context.Context, id int64, summary string) error {
	_, err := r.q.Exec(ctx, `UPDATE incidents SET summary = $2 WHERE id = $1`, id, summary)
	return err
}

// GetIncident returns one incident or ErrNotFound.
func (r *Repo) GetIncident(ctx context.Context, id int64) (model.Incident, error) {
	i, err := scanIncident(r.q.QueryRow(ctx, `SELECT `+incCols+` FROM incidents WHERE id = $1`, id))
	if err == pgx.ErrNoRows {
		return i, ErrNotFound
	}
	if err != nil {
		return i, fmt.Errorf("store: get incident: %w", err)
	}
	return i, nil
}

// IncidentFilter selects incidents for ListIncidents.
type IncidentFilter struct {
	Status  string // "open", "closed" or "all"/"" (all)
	TokenID string // optional
	Limit   int    // default 100, max 1000
}

// ListIncidents returns incidents newest first.
func (r *Repo) ListIncidents(ctx context.Context, f IncidentFilter) ([]model.Incident, error) {
	var clauses []string
	var args []any
	switch f.Status {
	case "open":
		clauses = append(clauses, "closed_at IS NULL")
	case "closed":
		clauses = append(clauses, "closed_at IS NOT NULL")
	case "", "all":
	default:
		return nil, fmt.Errorf("store: invalid incident status %q", f.Status)
	}
	if f.TokenID != "" {
		args = append(args, f.TokenID)
		clauses = append(clauses, "token_id = $"+strconv.Itoa(len(args)))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 1000)
	q := `SELECT ` + incCols + ` FROM incidents`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY opened_at DESC, id DESC LIMIT " + strconv.Itoa(limit)
	rows, err := r.q.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list incidents: %w", err)
	}
	defer rows.Close()
	var out []model.Incident
	for rows.Next() {
		i, err := scanIncident(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan incident: %w", err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// AddIncidentEvents attaches events to their incidents; duplicates are
// ignored.
func (r *Repo) AddIncidentEvents(ctx context.Context, events []model.IncidentEvent) error {
	b := &pgx.Batch{}
	for _, e := range events {
		b.Queue(`INSERT INTO incident_events (incident_id, block_height, receipt_id, tx_hash, standard, event, token_id, amount, raw, log_index, item_index)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`,
			e.IncidentID, int64(e.BlockHeight), e.ReceiptID, e.TxHash, e.Standard, e.Event, e.TokenID, e.Amount, e.Raw,
			int32(e.LogIndex), int32(e.ItemIndex))
	}
	if err := r.sendBatch(ctx, b); err != nil {
		return fmt.Errorf("store: add incident events: %w", err)
	}
	return nil
}

// IncidentEvents returns an incident's events ordered by block.
func (r *Repo) IncidentEvents(ctx context.Context, incidentID int64) ([]model.IncidentEvent, error) {
	rows, err := r.q.Query(ctx, `SELECT incident_id, block_height, receipt_id, tx_hash, standard, event, token_id, amount, raw, log_index, item_index
		FROM incident_events WHERE incident_id = $1 ORDER BY block_height, id`, incidentID)
	if err != nil {
		return nil, fmt.Errorf("store: incident events: %w", err)
	}
	defer rows.Close()
	var out []model.IncidentEvent
	for rows.Next() {
		var e model.IncidentEvent
		var h int64
		var li, ii int32
		if err := rows.Scan(&e.IncidentID, &h, &e.ReceiptID, &e.TxHash, &e.Standard, &e.Event, &e.TokenID, &e.Amount, &e.Raw, &li, &ii); err != nil {
			return nil, err
		}
		e.BlockHeight = uint64(h)
		e.LogIndex, e.ItemIndex = int(li), int(ii)
		out = append(out, e)
	}
	return out, rows.Err()
}
