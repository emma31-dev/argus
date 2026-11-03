package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/emma31-dev/argus/internal/model"
)

// BlockSource is what Follower and Backfill need; *Client implements it.
type BlockSource interface {
	Block(ctx context.Context, height uint64) (*Block, error)
	LastFinal(ctx context.Context) (uint64, error)
}

// Sink receives the intents.near events of each processed block, in order.
// It is called for every height including skipped ones and blocks without
// events (events is then empty), so it can track progress or per-block
// aggregates. A returned error stops the follower without advancing the
// cursor; the block is retried on the next tick, so HandleBlock must be
// idempotent.
type Sink interface {
	HandleBlock(ctx context.Context, height uint64, events []model.IncidentEvent) error
}

// Cursor persists the last fully processed block height. Load returns 0
// when nothing is stored yet.
type Cursor interface {
	Load(ctx context.Context) (uint64, error)
	Save(ctx context.Context, height uint64) error
}

// Follower processes blocks sequentially from the cursor up to the final
// head, then polls for new ones.
type Follower struct {
	Client  BlockSource
	Account string // default DefaultAccount
	Sink    Sink
	Cursor  Cursor

	// MaxPerTick bounds the blocks processed per Tick (default 100), so a
	// long catch-up yields regularly and a shutdown is noticed.
	MaxPerTick int
	// PollInterval is the wait when caught up (default 1s).
	PollInterval time.Duration
	// ErrorBackoff is the wait after a failed tick (default 5s).
	ErrorBackoff time.Duration
	// Concurrency for Backfill (default 4).
	Concurrency int
	Logger      *slog.Logger
}

func (f *Follower) account() string {
	if f.Account == "" {
		return DefaultAccount
	}
	return f.Account
}

// Tick processes up to MaxPerTick blocks. It returns how many it handled and
// whether the cursor has reached the final head. With an empty cursor it
// starts at the current final head (no history is fetched).
func (f *Follower) Tick(ctx context.Context) (n int, caughtUp bool, err error) {
	head, err := f.Client.LastFinal(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("events: last final: %w", err)
	}
	cur, err := f.Cursor.Load(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("events: load cursor: %w", err)
	}
	if cur == 0 {
		cur = head - 1
	}
	limit := f.MaxPerTick
	if limit <= 0 {
		limit = 100
	}
	for cur < head && n < limit {
		if ctx.Err() != nil {
			return n, false, ctx.Err()
		}
		h := cur + 1
		b, err := f.Client.Block(ctx, h)
		if err != nil {
			return n, false, fmt.Errorf("events: block %d: %w", h, err)
		}
		if err := f.Sink.HandleBlock(ctx, h, Extract(b, f.account())); err != nil {
			return n, false, fmt.Errorf("events: sink at block %d: %w", h, err)
		}
		if err := f.Cursor.Save(ctx, h); err != nil {
			return n, false, fmt.Errorf("events: save cursor %d: %w", h, err)
		}
		cur = h
		n++
	}
	return n, cur >= head, nil
}

// Run ticks until ctx ends. Errors are logged and retried after
// ErrorBackoff; Run returns ctx.Err().
func (f *Follower) Run(ctx context.Context) error {
	poll, backoff := f.PollInterval, f.ErrorBackoff
	if poll <= 0 {
		poll = time.Second
	}
	if backoff <= 0 {
		backoff = 5 * time.Second
	}
	for {
		_, caught, err := f.Tick(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var wait time.Duration
		switch {
		case err != nil:
			if f.Logger != nil {
				f.Logger.Warn("event follower tick failed", "err", err)
			}
			wait = backoff
		case caught:
			wait = poll
		}
		if wait > 0 {
			if err := sleepCtx(ctx, wait); err != nil {
				return err
			}
		}
	}
}

// Backfill calls fn for every height in [from, to], in ascending order,
// with the extracted events (empty for skipped heights). See Backfill.
func (f *Follower) Backfill(ctx context.Context, from, to uint64, fn func(height uint64, events []model.IncidentEvent) error) error {
	return Backfill(ctx, f.Client, f.account(), f.Concurrency, from, to, fn)
}

// Backfill fetches blocks [from, to] with at most concurrency fetches in
// flight (default 4) and calls fn in ascending height order. Request pacing
// is the source's rate limiter (Client.RPS), so concurrency only hides
// latency. It stops at the first error from the source or fn.
func Backfill(ctx context.Context, src BlockSource, account string, concurrency int, from, to uint64, fn func(height uint64, events []model.IncidentEvent) error) error {
	if from > to {
		return nil
	}
	if concurrency <= 0 {
		concurrency = 4
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		events []model.IncidentEvent
		err    error
	}
	pending := make(chan chan result, concurrency-1)
	go func() {
		defer close(pending)
		for h := from; ; h++ {
			ch := make(chan result, 1)
			select {
			case pending <- ch:
			case <-ctx.Done():
				return
			}
			go func(h uint64) {
				b, err := src.Block(ctx, h)
				ch <- result{Extract(b, account), err}
			}(h)
			if h == to { // also guards uint64 overflow
				return
			}
		}
	}()
	h := from
	for ch := range pending {
		var r result
		select {
		case r = <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
		if r.err != nil {
			return fmt.Errorf("events: backfill block %d: %w", h, r.err)
		}
		if err := fn(h, r.events); err != nil {
			return err
		}
		h++
	}
	if h <= to {
		if err := ctx.Err(); err != nil {
			return err
		}
		return errors.New("events: backfill ended early")
	}
	return nil
}
