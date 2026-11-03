package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/emma31-dev/argus/internal/model"
)

const tokenCols = `id, standard, contract, inner_id, symbol, decimals, source, updated_at`

func scanToken(row pgx.Row) (model.Token, error) {
	var t model.Token
	var std string
	err := row.Scan(&t.TokenID, &std, &t.Contract, &t.InnerID, &t.Symbol, &t.Decimals, &t.Source, &t.UpdatedAt)
	t.Standard = model.Standard(std)
	return t, err
}

// UpsertTokens inserts or updates tokens and marks them active. A zero
// UpdatedAt means now.
func (r *Repo) UpsertTokens(ctx context.Context, tokens []model.Token) error {
	b := &pgx.Batch{}
	for _, t := range tokens {
		if t.UpdatedAt.IsZero() {
			t.UpdatedAt = time.Now().UTC()
		}
		b.Queue(`INSERT INTO tokens (id, standard, contract, inner_id, symbol, decimals, source, updated_at, active)
			VALUES ($1,$2,$3,$4,$5,$6,$7, $8, TRUE)
			ON CONFLICT (id) DO UPDATE SET standard = EXCLUDED.standard, contract = EXCLUDED.contract,
				inner_id = EXCLUDED.inner_id, symbol = EXCLUDED.symbol, decimals = EXCLUDED.decimals,
				source = EXCLUDED.source, updated_at = EXCLUDED.updated_at, active = TRUE`,
			t.TokenID, string(t.Standard), t.Contract, t.InnerID, t.Symbol, t.Decimals, t.Source, t.UpdatedAt)
	}
	if err := r.sendBatch(ctx, b); err != nil {
		return fmt.Errorf("store: upsert tokens: %w", err)
	}
	return nil
}

// DeactivateTokensExcept marks every active token not in keep as inactive
// (a token dropped from discovery) and returns how many were changed. Call it
// only after a complete discovery run.
func (r *Repo) DeactivateTokensExcept(ctx context.Context, keep []string) (int64, error) {
	if keep == nil {
		keep = []string{}
	}
	tag, err := r.pool.Exec(ctx, `UPDATE tokens SET active = FALSE WHERE active AND id <> ALL($1::text[])`, keep)
	if err != nil {
		return 0, fmt.Errorf("store: deactivate tokens: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ListTokens returns the active tokens ordered by symbol then id.
func (r *Repo) ListTokens(ctx context.Context) ([]model.Token, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+tokenCols+` FROM tokens WHERE active ORDER BY symbol, id`)
	if err != nil {
		return nil, fmt.Errorf("store: list tokens: %w", err)
	}
	defer rows.Close()
	var out []model.Token
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan token: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetToken returns one token (active or not) or ErrNotFound.
func (r *Repo) GetToken(ctx context.Context, id string) (model.Token, error) {
	t, err := scanToken(r.pool.QueryRow(ctx, `SELECT `+tokenCols+` FROM tokens WHERE id = $1`, id))
	if err == pgx.ErrNoRows {
		return t, ErrNotFound
	}
	return t, err
}
