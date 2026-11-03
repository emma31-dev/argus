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
//
// Per incident it keeps the highest block REQUESTED (want) and the highest
// block CONFIRMED attached (last). Ranges are queued per incident and run in
// order by one worker; last advances only after AttachRange succeeded. When a
// range fails the rest of that incident's queue is dropped and want falls
// back to last, so the next transition requests the failed blocks again
// together with the new ones.
type incidentEvents struct {
	ctx      context.Context
	att      rangeAttacher
	maxChunk uint64
	log      *slog.Logger

	mu  sync.Mutex
	inc map[int64]*incState
	wg  sync.WaitGroup
	sem chan struct{}
}

type incState struct {
	want, last uint64
	queue      [][2]uint64 // pending [from, to] ranges
	running    bool        // a worker owns the queue
	closing    bool        // recovered: forget the incident once idle
}

func newIncidentEvents(ctx context.Context, att rangeAttacher, maxChunk int, log *slog.Logger) *incidentEvents {
	if maxChunk <= 0 {
		maxChunk = events.DefaultMaxAttachBlocks
	}
	return &incidentEvents{ctx: ctx, att: att, maxChunk: uint64(maxChunk), log: log,
		inc: map[int64]*incState{}, sem: make(chan struct{}, 2)}
}

// OnTransition is the alert.EngineConfig hook. It never blocks.
func (a *incidentEvents) OnTransition(t alert.Transition) {
	if t.IncidentID == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.inc[t.IncidentID]
	switch t.Kind {
	case alert.TurnedRed:
		from := uint64(1)
		if t.StartBlock > preRollBlocks {
			from = t.StartBlock - preRollBlocks
		}
		if st == nil {
			st = &incState{want: from - 1, last: from - 1}
			a.inc[t.IncidentID] = st
		}
		a.request(t.IncidentID, st, t.TokenID, max(from, st.want+1), t.Block)
	case alert.IncidentUpdate, alert.Recovered:
		var from uint64
		if st == nil { // e.g. after a restart: start from the pre-roll, bounded
			from = 1
			if t.StartBlock > preRollBlocks {
				from = t.StartBlock - preRollBlocks
			}
			if t.Block > a.maxChunk && from < t.Block-a.maxChunk {
				from = t.Block - a.maxChunk
			}
			st = &incState{want: from - 1, last: from - 1}
			a.inc[t.IncidentID] = st
		} else {
			from = st.want + 1
		}
		a.request(t.IncidentID, st, t.TokenID, from, t.Block)
		if t.Kind == alert.Recovered {
			st.closing = true
			if !st.running {
				delete(a.inc, t.IncidentID)
			}
		}
	}
}

// request queues [from, to] (when non-empty) and starts the incident's
// worker if none is running. Callers hold a.mu.
func (a *incidentEvents) request(id int64, st *incState, token string, from, to uint64) {
	if to < from {
		return
	}
	st.want = to
	st.queue = append(st.queue, [2]uint64{from, to})
	if st.running {
		return
	}
	st.running = true
	a.wg.Add(1)
	go a.work(id, st, token)
}

// work drains one incident's queue in order.
func (a *incidentEvents) work(id int64, st *incState, token string) {
	defer a.wg.Done()
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	case <-a.ctx.Done():
		a.finish(id, st, false)
		return
	}
	for {
		a.mu.Lock()
		if len(st.queue) == 0 {
			a.finishLocked(id, st)
			a.mu.Unlock()
			return
		}
		r := st.queue[0]
		st.queue = st.queue[1:]
		a.mu.Unlock()
		if !a.attach(id, st, token, r[0], r[1]) {
			a.finish(id, st, true)
			return
		}
	}
}

// attach runs one range in chunks, advancing st.last after each chunk that
// AttachRange confirmed. It reports whether the whole range succeeded.
func (a *incidentEvents) attach(id int64, st *incState, token string, from, to uint64) bool {
	total := 0
	for start := from; start <= to; {
		end := min(start+a.maxChunk-1, to)
		n, err := a.att.AttachRange(a.ctx, id, token, start, end)
		if err != nil {
			if a.ctx.Err() == nil {
				a.log.Warn("attaching incident events failed; will retry from the last confirmed block with the next transition",
					"incident", id, "token", token, "from", start, "to", end, "err", err)
			}
			return false
		}
		total += n
		a.mu.Lock()
		st.last = max(st.last, end)
		a.mu.Unlock()
		start = end + 1
	}
	a.log.Info("incident events attached", "incident", id, "token", token, "from", from, "to", to, "events", total)
	return true
}

// finish ends a worker; after a failure it drops the queued ranges and rolls
// want back to the last confirmed block.
func (a *incidentEvents) finish(id int64, st *incState, failed bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if failed || a.ctx.Err() != nil {
		st.queue = nil
		st.want = st.last
	}
	a.finishLocked(id, st)
}

func (a *incidentEvents) finishLocked(id int64, st *incState) {
	st.running = false
	if st.closing {
		delete(a.inc, id)
	}
}

// Wait blocks until pending attachments finish (shutdown, tests).
func (a *incidentEvents) Wait() { a.wg.Wait() }
