package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/emma31-dev/argus/internal/alert"
)

const subCols = `id, channel, target, token_id, secret, created_at`

func scanSub(row pgx.Row) (alert.Subscription, error) {
	var s alert.Subscription
	err := row.Scan(&s.ID, &s.Channel, &s.Target, &s.TokenID, &s.Secret, &s.CreatedAt)
	return s, err
}

func (r *Repo) querySubs(ctx context.Context, q string, args ...any) ([]alert.Subscription, error) {
	rows, err := r.q.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: subscriptions: %w", err)
	}
	defer rows.Close()
	var out []alert.Subscription
	for rows.Next() {
		s, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// AddSubscription inserts a subscription; on a duplicate (channel, target,
// token) it keeps the row and only replaces a non-empty secret.
func (r *Repo) AddSubscription(ctx context.Context, s alert.Subscription) (alert.Subscription, error) {
	if s.TokenID == "" {
		s.TokenID = alert.AllTokens
	}
	out, err := scanSub(r.q.QueryRow(ctx, `
		INSERT INTO subscriptions (channel, target, token_id, secret) VALUES ($1,$2,$3,$4)
		ON CONFLICT (channel, target, token_id) DO UPDATE SET
			secret = CASE WHEN EXCLUDED.secret <> '' THEN EXCLUDED.secret ELSE subscriptions.secret END
		RETURNING `+subCols, s.Channel, s.Target, s.TokenID, s.Secret))
	if err != nil {
		return out, fmt.Errorf("store: add subscription: %w", err)
	}
	return out, nil
}

// RemoveSubscription deletes subscriptions of a target; tokenID == "" deletes
// all of the target's subscriptions on the channel. It reports whether
// anything was deleted.
func (r *Repo) RemoveSubscription(ctx context.Context, channel, target, tokenID string) (bool, error) {
	var tag interface{ RowsAffected() int64 }
	var err error
	if tokenID == "" {
		tag, err = r.q.Exec(ctx, `DELETE FROM subscriptions WHERE channel = $1 AND target = $2`, channel, target)
	} else {
		tag, err = r.q.Exec(ctx, `DELETE FROM subscriptions WHERE channel = $1 AND target = $2 AND token_id = $3`, channel, target, tokenID)
	}
	if err != nil {
		return false, fmt.Errorf("store: remove subscription: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListSubscriptions returns a target's subscriptions on a channel.
func (r *Repo) ListSubscriptions(ctx context.Context, channel, target string) ([]alert.Subscription, error) {
	return r.querySubs(ctx, `SELECT `+subCols+` FROM subscriptions WHERE channel = $1 AND target = $2 ORDER BY token_id`, channel, target)
}

// SubscriptionsForToken returns the channel's subscriptions that apply to a
// token, including "*" (all tokens) subscriptions.
func (r *Repo) SubscriptionsForToken(ctx context.Context, channel, tokenID string) ([]alert.Subscription, error) {
	return r.querySubs(ctx, `SELECT `+subCols+` FROM subscriptions WHERE channel = $1 AND token_id IN ($2, '*') ORDER BY id`, channel, tokenID)
}
