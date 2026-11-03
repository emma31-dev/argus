package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// GetKV returns the value stored under key, or ErrNotFound.
func (r *Repo) GetKV(ctx context.Context, key string) (string, error) {
	var v string
	err := r.pool.QueryRow(ctx, `SELECT value FROM kv WHERE key = $1`, key).Scan(&v)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: get kv: %w", err)
	}
	return v, nil
}

// SetKV stores value under key, replacing any previous value.
func (r *Repo) SetKV(ctx context.Context, key, value string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO kv (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, key, value)
	if err != nil {
		return fmt.Errorf("store: set kv: %w", err)
	}
	return nil
}
