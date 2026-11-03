package main

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/events"
	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/store"
)

// preRollBlocks is how far before an incident's first negative block events
// are attached (the deposits that caused a gap precede the gap).
const preRollBlocks = 150

// kvStore is the part of the repository the cursor needs.
type kvStore interface {
	GetKV(ctx context.Context, key string) (string, error)
	SetKV(ctx context.Context, key, value string) error
}

// kvCursor persists the event follower's position under
// "events_cursor:<account>".
type kvCursor struct {
	kv  kvStore
	key string
}

func newKVCursor(kv kvStore, account string) *kvCursor {
	return &kvCursor{kv: kv, key: "events_cursor:" + account}
}

// Load implements events.Cursor.
func (c *kvCursor) Load(ctx context.Context) (uint64, error) {
	v, err := c.kv.GetKV(ctx, c.key)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(v, 10, 64)
}

// Save implements events.Cursor.
func (c *kvCursor) Save(ctx context.Context, height uint64) error {
	return c.kv.SetKV(ctx, c.key, strconv.FormatUint(height, 10))
}

// debugSink only counts: Argus reads owed from mt_batch_supply, so per-block
// deltas are not needed. Events reach the database through incident
// attachment.
type debugSink struct{ log *slog.Logger }

func (s debugSink) HandleBlock(_ context.Context, height uint64, evs []model.IncidentEvent) error {
	if len(evs) > 0 {
		s.log.Debug("intents.near events in block", "block", height, "events", len(evs))
	}
	return nil
}

// rangeAttacher is what incidentEvents needs from events.Attacher.
type rangeAttacher interface {
	AttachRange(ctx context.Context, incidentID int64, tokenID string, from, to uint64) (int, error)
}

// incidentEvents attaches intents.near events to incidents as they open,
// grow and close. Everything runs in goroutines off the alert path and
// failures are only logged.
type incidentEvents struct {
	ctx      context.Context
	att      rangeAttacher
	maxChunk uint64
	log      *slog.Logger

	mu   sync.Mutex
	last map[int64]uint64 // highest block already requested per incident
	wg   sync.WaitGroup
	sem  chan struct{}
}

func newIncidentEvents(ctx context.Context, att rangeAttacher, maxChunk int, log *slog.Logger) *incidentEvents {
	if maxChunk <= 0 {
		maxChunk = events.DefaultMaxAttachBlocks
	}
	return &incidentEvents{ctx: ctx, att: att, maxChunk: uint64(maxChunk), log: log,
		last: map[int64]uint64{}, sem: make(chan struct{}, 2)}
}

// OnTransition is the alert.EngineConfig hook. It never blocks.
func (a *incidentEvents) OnTransition(t alert.Transition) {
	if t.IncidentID == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch t.Kind {
	case alert.TurnedRed:
		from := uint64(1)
		if t.StartBlock > preRollBlocks {
			from = t.StartBlock - preRollBlocks
		}
		a.last[t.IncidentID] = t.Block
		a.spawn(t.IncidentID, t.TokenID, from, t.Block)
	case alert.IncidentUpdate, alert.Recovered:
		last, known := a.last[t.IncidentID]
		from := last + 1
		if !known { // e.g. after a restart: start from the pre-roll, bounded
			from = 1
			if t.StartBlock > preRollBlocks {
				from = t.StartBlock - preRollBlocks
			}
			if t.Block > a.maxChunk && from < t.Block-a.maxChunk {
				from = t.Block - a.maxChunk
			}
		}
		if t.Block >= from {
			a.last[t.IncidentID] = t.Block
			a.spawn(t.IncidentID, t.TokenID, from, t.Block)
		}
		if t.Kind == alert.Recovered {
			delete(a.last, t.IncidentID)
		}
	}
}

func (a *incidentEvents) spawn(id int64, token string, from, to uint64) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		select {
		case a.sem <- struct{}{}:
			defer func() { <-a.sem }()
		case <-a.ctx.Done():
			return
		}
		total := 0
		for start := from; start <= to; {
			end := min(start+a.maxChunk-1, to)
			n, err := a.att.AttachRange(a.ctx, id, token, start, end)
			if err != nil {
				if a.ctx.Err() == nil {
					a.log.Warn("attaching incident events failed", "incident", id, "token", token, "from", start, "to", end, "err", err)
				}
				return
			}
			total += n
			start = end + 1
		}
		a.log.Info("incident events attached", "incident", id, "token", token, "from", from, "to", to, "events", total)
	}()
}

// Wait blocks until pending attachments finish (shutdown, tests).
func (a *incidentEvents) Wait() { a.wg.Wait() }
