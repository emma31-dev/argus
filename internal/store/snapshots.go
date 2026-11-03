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

const snapCols = `token_id, block_height, block_hash, block_time, owed::text, held::text, gap::text, provider, taken_at`

func scanSnapshot(row pgx.Row) (model.Snapshot, error) {
	var s model.Snapshot
	var h int64
	err := row.Scan(&s.TokenID, &h, &s.BlockHash, &s.BlockTime, &s.Owed, &s.Held, &s.Gap, &s.Provider, &s.TakenAt)
	s.BlockHeight = uint64(h)
	return s, err
}

// InsertSnapshots stores snapshots in one batch (a single implicit
// transaction); rows that already exist for (token, block) are skipped. It
// returns the number of rows actually inserted.
func (r *Repo) InsertSnapshots(ctx context.Context, snaps []model.Snapshot) (int, error) {
	b := &pgx.Batch{}
	for _, s := range snaps {
		taken := s.TakenAt
		if taken.IsZero() {
			taken = time.Now().UTC()
		}
		b.Queue(`INSERT INTO snapshots (token_id, block_height, block_hash, block_time, owed, held, gap, provider, taken_at)
			VALUES ($1,$2,$3,$4,$5::text::numeric,$6::text::numeric,$7::text::numeric,$8,$9)
			ON CONFLICT (token_id, block_height) DO NOTHING`,
			s.TokenID, int64(s.BlockHeight), s.BlockHash, s.BlockTime, s.Owed, s.Held, s.Gap, s.Provider, taken)
	}
	if b.Len() == 0 {
		return 0, nil
	}
	br := r.pool.SendBatch(ctx, b)
	inserted := 0
	var first error
	for i := 0; i < b.Len(); i++ {
		tag, err := br.Exec()
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		inserted += int(tag.RowsAffected())
	}
	if err := br.Close(); err != nil && first == nil {
		first = err
	}
	if first != nil {
		return 0, fmt.Errorf("store: insert snapshots: %w", first)
	}
	return inserted, nil
}

// LatestSnapshots returns the newest snapshot of every token that has one,
// ordered by token id. It is equivalent to DISTINCT ON (token_id) ... ORDER
// BY block_height DESC but driven from the tokens table with a LATERAL index
// probe per token, which stays O(tokens) however large snapshots grows.
func (r *Repo) LatestSnapshots(ctx context.Context) ([]model.Snapshot, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT s.token_id, s.block_height, s.block_hash, s.block_time, s.owed::text, s.held::text, s.gap::text, s.provider, s.taken_at
		FROM tokens t
		JOIN LATERAL (
			SELECT * FROM snapshots WHERE token_id = t.id ORDER BY block_height DESC LIMIT 1
		) s ON TRUE
		ORDER BY s.token_id`)
	if err != nil {
		return nil, fmt.Errorf("store: latest snapshots: %w", err)
	}
	defer rows.Close()
	var out []model.Snapshot
	for rows.Next() {
		s, err := scanSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan snapshot: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Range bounds a query by time and/or block height; zero values are open.
// Bounds are inclusive.
type Range struct {
	FromTime, ToTime   time.Time
	FromBlock, ToBlock uint64
}

// where appends the range predicates to clauses/args (placeholders continue
// from len(args)).
func (rg Range) where(alias string, clauses []string, args []any) ([]string, []any) {
	add := func(format string, v any) {
		args = append(args, v)
		clauses = append(clauses, fmt.Sprintf(format, alias, len(args)))
	}
	if !rg.FromTime.IsZero() {
		add("%sblock_time >= $%d", rg.FromTime)
	}
	if !rg.ToTime.IsZero() {
		add("%sblock_time <= $%d", rg.ToTime)
	}
	if rg.FromBlock > 0 {
		add("%sblock_height >= $%d", int64(rg.FromBlock))
	}
	if rg.ToBlock > 0 {
		add("%sblock_height <= $%d", int64(rg.ToBlock))
	}
	return clauses, args
}

// Default and maximum point counts for History.
const (
	DefaultHistoryLimit = 2000
	MaxHistoryLimit     = 100000
)

// History returns a token's snapshots in rg ordered by block height. When the
// range holds more than limit rows it is downsampled to roughly limit rows by
// keeping every k-th row (plus the first and last), and ALWAYS keeps every
// snapshot with gap < 0, so the result can exceed limit when a long deficit
// exists. limit <= 0 means DefaultHistoryLimit.
func (r *Repo) History(ctx context.Context, tokenID string, rg Range, limit int) ([]model.Snapshot, error) {
	if limit <= 0 {
		limit = DefaultHistoryLimit
	}
	limit = min(limit, MaxHistoryLimit)
	clauses, args := rg.where("", []string{"token_id = $1"}, []any{tokenID})
	args = append(args, int64(limit))
	q := fmt.Sprintf(`
		WITH r AS (
			SELECT *, row_number() OVER (ORDER BY block_height) AS rn, count(*) OVER () AS cnt
			FROM snapshots WHERE %s
		)
		SELECT %s FROM r
		WHERE gap < 0 OR rn = 1 OR rn = cnt OR (rn - 1) %% ((cnt + $%d - 1) / $%d) = 0
		ORDER BY block_height`, strings.Join(clauses, " AND "), snapCols, len(args), len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: history: %w", err)
	}
	defer rows.Close()
	var out []model.Snapshot
	for rows.Next() {
		s, err := scanSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan snapshot: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ExportRow is one CSV export row: a snapshot with its token's symbol and
// decimals.
type ExportRow struct {
	Symbol   string
	Decimals int
	model.Snapshot
}

// ExportRows streams snapshots (no downsampling) to fn, ordered by token then
// block. tokenID == "" exports all tokens. maxRows <= 0 means no limit. If fn
// returns an error the stream stops and that error is returned.
func (r *Repo) ExportRows(ctx context.Context, tokenID string, rg Range, maxRows int, fn func(ExportRow) error) error {
	var clauses []string
	var args []any
	if tokenID != "" {
		args = append(args, tokenID)
		clauses = append(clauses, "s.token_id = $1")
	}
	clauses, args = rg.where("s.", clauses, args)
	q := `SELECT s.token_id, s.block_height, s.block_hash, s.block_time, s.owed::text, s.held::text, s.gap::text,
		s.provider, s.taken_at, COALESCE(t.symbol, ''), COALESCE(t.decimals, 0)
		FROM snapshots s LEFT JOIN tokens t ON t.id = s.token_id`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY s.token_id, s.block_height"
	if maxRows > 0 {
		q += " LIMIT " + strconv.Itoa(maxRows)
	}
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("store: export: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e ExportRow
		var h int64
		s := &e.Snapshot
		if err := rows.Scan(&s.TokenID, &h, &s.BlockHash, &s.BlockTime, &s.Owed, &s.Held, &s.Gap,
			&s.Provider, &s.TakenAt, &e.Symbol, &e.Decimals); err != nil {
			return fmt.Errorf("store: scan export row: %w", err)
		}
		s.BlockHeight = uint64(h)
		if err := fn(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

// PruneSnapshots deletes snapshots taken before olderThan to bound table
// growth (about 350k rows/day at 30 s x 120 tokens). With keepNegative every
// row with gap < 0 is kept forever (they are the evidence). The newest
// snapshot of each token is always kept so "latest" keeps working. It returns
// the number of rows deleted. Run it from a daily job; for very large
// deployments partition by block_time instead.
func (r *Repo) PruneSnapshots(ctx context.Context, olderThan time.Time, keepNegative bool) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM snapshots s
		WHERE s.taken_at < $1
		  AND ($2 = FALSE OR s.gap >= 0)
		  AND s.block_height < (SELECT max(m.block_height) FROM snapshots m WHERE m.token_id = s.token_id)`,
		olderThan, keepNegative)
	if err != nil {
		return 0, fmt.Errorf("store: prune snapshots: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Round summarises one completed snapshot round (for /v1/status, /readyz).
type Round struct {
	BlockHeight  uint64
	StartedAt    time.Time
	FinishedAt   time.Time
	TokensOK     int
	TokensFailed int
	Provider     string
}

// RecordRound stores a completed round.
func (r *Repo) RecordRound(ctx context.Context, rd Round) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO rounds (block_height, started_at, finished_at, tokens_ok, tokens_failed, provider)
		VALUES ($1,$2,$3,$4,$5,$6)`, int64(rd.BlockHeight), rd.StartedAt, rd.FinishedAt, rd.TokensOK, rd.TokensFailed, rd.Provider)
	if err != nil {
		return fmt.Errorf("store: record round: %w", err)
	}
	return nil
}

// LastRound returns the most recent round, or ErrNotFound if none exists.
func (r *Repo) LastRound(ctx context.Context) (Round, error) {
	var rd Round
	var h int64
	err := r.pool.QueryRow(ctx, `SELECT block_height, started_at, finished_at, tokens_ok, tokens_failed, provider
		FROM rounds ORDER BY finished_at DESC, id DESC LIMIT 1`).
		Scan(&h, &rd.StartedAt, &rd.FinishedAt, &rd.TokensOK, &rd.TokensFailed, &rd.Provider)
	if err == pgx.ErrNoRows {
		return rd, ErrNotFound
	}
	rd.BlockHeight = uint64(h)
	return rd, err
}

// PruneRounds deletes rounds finished before olderThan.
func (r *Repo) PruneRounds(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM rounds WHERE finished_at < $1`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("store: prune rounds: %w", err)
	}
	return tag.RowsAffected(), nil
}
