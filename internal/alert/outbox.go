package alert

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Outbox stores alerts durably. The Engine adds the alerts of a token inside
// the same transaction that writes the token's incident and state, so an
// alert exists if and only if the state change that caused it was committed.
type Outbox interface {
	AddOutbox(ctx context.Context, alerts []Alert) error
}

// Tx is the set of stores the Engine writes to for one token, all inside one
// atomic unit.
type Tx interface {
	StateStore
	IncidentStore
	Outbox
}

// TxRunner runs fn atomically: when fn returns nil every write it made
// through the Tx is committed, otherwise none of them is.
type TxRunner interface {
	InTx(ctx context.Context, fn func(Tx) error) error
}

// OutboxEntry is one stored alert waiting to be moved to the Queue.
type OutboxEntry struct {
	ID    int64
	Alert Alert
}

// OutboxStore is the relay's view of the outbox.
type OutboxStore interface {
	// PendingOutbox returns up to limit entries not yet enqueued, oldest
	// (lowest id) first.
	PendingOutbox(ctx context.Context, limit int) ([]OutboxEntry, error)
	MarkOutboxEnqueued(ctx context.Context, ids []int64) error
	// PruneOutbox deletes entries enqueued before olderThan.
	PruneOutbox(ctx context.Context, olderThan time.Time) (int64, error)
}

// RelayConfig tunes a Relay.
type RelayConfig struct {
	Interval  time.Duration // poll interval when not woken, default 2s
	BatchSize int           // entries per store round trip, default 100
	Retention time.Duration // how long enqueued rows are kept, default 7 days
	Logger    *slog.Logger
	Now       func() time.Time
}

// Relay moves alerts from the outbox to the Queue in id order and marks
// them enqueued. Delivery is at-least-once: a crash between Enqueue and
// MarkOutboxEnqueued enqueues the alert again, which the notifiers tolerate
// because they de-duplicate by alert key.
type Relay struct {
	cfg   RelayConfig
	store OutboxStore
	queue Queue
	wake  chan struct{}
	mu    sync.Mutex // serialises Flush
}

// NewRelay builds a Relay.
func NewRelay(cfg RelayConfig, store OutboxStore, queue Queue) *Relay {
	if cfg.Interval <= 0 {
		cfg.Interval = 2 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.Retention <= 0 {
		cfg.Retention = 7 * 24 * time.Hour
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Relay{cfg: cfg, store: store, queue: queue, wake: make(chan struct{}, 1)}
}

// Wake asks the relay to flush now instead of waiting for the next poll. It
// never blocks.
func (r *Relay) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Flush moves every pending entry to the queue and returns how many it
// moved. It stops at the first Enqueue error so ordering is preserved; the
// entries already moved are still marked.
func (r *Relay) Flush(ctx context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for {
		entries, err := r.store.PendingOutbox(ctx, r.cfg.BatchSize)
		if err != nil {
			return total, err
		}
		if len(entries) == 0 {
			return total, nil
		}
		ids := make([]int64, 0, len(entries))
		var enqErr error
		for _, e := range entries {
			if enqErr = r.queue.Enqueue(ctx, e.Alert); enqErr != nil {
				break
			}
			ids = append(ids, e.ID)
		}
		if len(ids) > 0 {
			if err := r.store.MarkOutboxEnqueued(ctx, ids); err != nil {
				return total, err
			}
			total += len(ids)
		}
		if enqErr != nil {
			return total, enqErr
		}
		if len(entries) < r.cfg.BatchSize {
			return total, nil
		}
	}
}

// Run flushes on every wake-up and poll tick until ctx is cancelled, and
// prunes old enqueued rows about once an hour. It returns nil on
// cancellation.
func (r *Relay) Run(ctx context.Context) error {
	t := time.NewTicker(r.cfg.Interval)
	defer t.Stop()
	var lastPrune time.Time
	for {
		if n, err := r.Flush(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			r.cfg.Logger.Error("alert relay: flush failed", "moved", n, "err", err)
		} else if n > 0 {
			r.cfg.Logger.Debug("alert relay: moved outbox alerts to the queue", "count", n)
		}
		if now := r.cfg.Now(); now.Sub(lastPrune) > time.Hour {
			lastPrune = now
			if n, err := r.store.PruneOutbox(ctx, now.Add(-r.cfg.Retention)); err != nil && ctx.Err() == nil {
				r.cfg.Logger.Warn("alert relay: pruning outbox failed", "err", err)
			} else if n > 0 {
				r.cfg.Logger.Info("alert relay: pruned outbox", "rows", n)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		case <-r.wake:
		}
	}
}
