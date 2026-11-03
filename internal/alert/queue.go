package alert

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Delivery is an alert taken from a Queue that must be Acked or dead-lettered.
type Delivery struct {
	Alert Alert
	raw   string // queue-specific handle (the encoded payload for Redis)
	id    uint64 // queue-specific handle (memory queue)
}

// DeadRequeueMaxAge is how long a dead-lettered alert remains eligible for
// RequeueDead.
const DeadRequeueMaxAge = 24 * time.Hour

// DeadAlert is a dead-lettered alert with the reason it gave up.
type DeadAlert struct {
	Alert  Alert     `json:"alert"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// Queue is the alert queue between the Engine (producer) and the Dispatcher
// (single consumer). Delivery is at-least-once.
type Queue interface {
	Enqueue(ctx context.Context, a Alert) error
	// Dequeue waits up to wait for the oldest alert. It returns (nil, nil)
	// on timeout. The delivery stays "in flight" until Ack or Dead.
	Dequeue(ctx context.Context, wait time.Duration) (*Delivery, error)
	Ack(ctx context.Context, d *Delivery) error
	// Dead moves the delivery to the dead-letter list and removes it from
	// the in-flight set.
	Dead(ctx context.Context, d *Delivery, reason string) error
	// RequeueDead moves up to max dead-lettered alerts that were
	// dead-lettered less than DeadRequeueMaxAge ago back to the pending
	// queue, oldest first, and returns how many. Delivery stays
	// at-least-once; the notifiers de-duplicate.
	RequeueDead(ctx context.Context, max int) (int, error)
	// Recover re-queues deliveries left in flight by a previous run and
	// returns how many.
	Recover(ctx context.Context) (int, error)
}

// MemoryQueue is an in-process Queue for tests and for running without Redis.
// Alerts are lost on restart.
type MemoryQueue struct {
	mu         sync.Mutex
	pending    []*Delivery
	inflight   map[uint64]*Delivery
	dead       []DeadAlert
	next       uint64
	notify     chan struct{}
	maxPending int
}

// NewMemoryQueue returns an empty MemoryQueue.
func NewMemoryQueue() *MemoryQueue {
	return &MemoryQueue{inflight: map[uint64]*Delivery{}, notify: make(chan struct{}, 1), maxPending: 10000}
}

// Enqueue implements Queue. The oldest alert is dropped beyond 10k pending.
func (q *MemoryQueue) Enqueue(_ context.Context, a Alert) error {
	q.mu.Lock()
	q.next++
	q.pending = append(q.pending, &Delivery{Alert: a, id: q.next})
	if len(q.pending) > q.maxPending {
		q.pending = q.pending[1:]
	}
	q.mu.Unlock()
	select {
	case q.notify <- struct{}{}:
	default:
	}
	return nil
}

// Dequeue implements Queue.
func (q *MemoryQueue) Dequeue(ctx context.Context, wait time.Duration) (*Delivery, error) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		q.mu.Lock()
		if len(q.pending) > 0 {
			d := q.pending[0]
			q.pending = q.pending[1:]
			q.inflight[d.id] = d
			q.mu.Unlock()
			return d, nil
		}
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, nil
		case <-q.notify:
		}
	}
}

// Ack implements Queue.
func (q *MemoryQueue) Ack(_ context.Context, d *Delivery) error {
	q.mu.Lock()
	delete(q.inflight, d.id)
	q.mu.Unlock()
	return nil
}

// Dead implements Queue.
func (q *MemoryQueue) Dead(_ context.Context, d *Delivery, reason string) error {
	q.mu.Lock()
	delete(q.inflight, d.id)
	q.dead = append(q.dead, DeadAlert{Alert: d.Alert, Reason: reason, At: time.Now().UTC()})
	q.mu.Unlock()
	return nil
}

// Recover implements Queue.
func (q *MemoryQueue) Recover(_ context.Context) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.inflight)
	for id, d := range q.inflight {
		q.pending = append([]*Delivery{d}, q.pending...)
		delete(q.inflight, id)
	}
	return n, nil
}

// RequeueDead implements Queue.
func (q *MemoryQueue) RequeueDead(_ context.Context, max int) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	cutoff := time.Now().Add(-DeadRequeueMaxAge)
	n := 0
	keep := q.dead[:0:0]
	for _, d := range q.dead { // oldest first
		if n < max && !d.At.Before(cutoff) {
			q.next++
			q.pending = append(q.pending, &Delivery{Alert: d.Alert, id: q.next})
			n++
			continue
		}
		keep = append(keep, d)
	}
	q.dead = keep
	if n > 0 {
		select {
		case q.notify <- struct{}{}:
		default:
		}
	}
	return n, nil
}

// Len returns the number of pending alerts.
func (q *MemoryQueue) Len() int { q.mu.Lock(); defer q.mu.Unlock(); return len(q.pending) }

// DeadLetters returns a copy of the dead-lettered alerts.
func (q *MemoryQueue) DeadLetters() []DeadAlert {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]DeadAlert(nil), q.dead...)
}

func encodeAlert(a Alert) (string, error) {
	b, err := json.Marshal(a)
	return string(b), err
}
