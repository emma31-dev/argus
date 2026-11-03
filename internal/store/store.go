// Package store is Argus's Postgres repository (pgx/v5). It owns the schema
// (embedded migrations in /migrations), the snapshot history and everything
// the read API and the alert engine persist.
//
// Amounts are NUMERIC(78,0) in the database and base-10 strings in Go: they
// are sent as text and read back with ::text so no precision is ever lost.
// Repo satisfies the alert package's StateStore, IncidentStore, TokenLister
// and SubscriptionStore interfaces (store imports alert, never the reverse).
package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/migrations"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// Repo is the Postgres repository.
type Repo struct {
	pool *pgxpool.Pool
}

var (
	_ alert.StateStore        = (*Repo)(nil)
	_ alert.IncidentStore     = (*Repo)(nil)
	_ alert.TokenLister       = (*Repo)(nil)
	_ alert.SubscriptionStore = (*Repo)(nil)
)

// New wraps an existing pool.
func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Open creates a pool from a connection URL and verifies connectivity.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("store: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return pool, nil
}

// Pool exposes the underlying pool (health checks, tests).
func (r *Repo) Pool() *pgxpool.Pool { return r.pool }

const migrationLockID = 0x41524755 // "ARGU"

// Migrate applies every embedded migration that is not yet recorded in
// schema_migrations, in file-name order, each in its own transaction. It is
// safe to call concurrently from several processes (advisory lock).
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrate: acquire: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("migrate: lock: %w", err)
	}
	defer func() {
		// Use a fresh context so the lock is released even if ctx is done.
		uctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(uctx, `SELECT pg_advisory_unlock($1)`, migrationLockID)
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("migrate: create schema_migrations: %w", err)
	}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("migrate: read embedded files: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var done bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&done); err != nil {
			return fmt.Errorf("migrate: check %s: %w", name, err)
		}
		if done {
			continue
		}
		sqlText, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return fmt.Errorf("migrate: read %s: %w", name, err)
		}
		if err := applyMigration(ctx, conn, name, string(sqlText)); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, conn *pgxpool.Conn, name, sqlText string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migrate %s: begin: %w", name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// No arguments: pgx uses the simple protocol, which allows many statements.
	if _, err := tx.Exec(ctx, sqlText); err != nil {
		return fmt.Errorf("migrate %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
		return fmt.Errorf("migrate %s: record: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("migrate %s: commit: %w", name, err)
	}
	return nil
}

// sendBatch runs a batch and closes it, returning the first error.
func (r *Repo) sendBatch(ctx context.Context, b *pgx.Batch) error {
	if b.Len() == 0 {
		return nil
	}
	br := r.pool.SendBatch(ctx, b)
	var first error
	for i := 0; i < b.Len(); i++ {
		if _, err := br.Exec(); err != nil && first == nil {
			first = err
		}
	}
	if err := br.Close(); err != nil && first == nil {
		first = err
	}
	return first
}
