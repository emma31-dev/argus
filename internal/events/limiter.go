package events

import (
	"context"
	"sync"
	"time"
)

// Limiter spaces requests evenly at a fixed rate. The zero value is not
// usable; build one with NewLimiter.
type Limiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
	now      func() time.Time
	sleep    func(ctx context.Context, d time.Duration) error
}

// NewLimiter allows rps requests per second (rps <= 0 means unlimited).
func NewLimiter(rps float64) *Limiter {
	l := &Limiter{now: time.Now, sleep: sleepCtx}
	if rps > 0 {
		l.interval = time.Duration(float64(time.Second) / rps)
	}
	return l
}

// Wait blocks until the caller may send one request, or ctx ends. Slots are
// reserved in call order, so concurrent callers are spaced, not bunched.
func (l *Limiter) Wait(ctx context.Context) error {
	if l == nil || l.interval <= 0 {
		return ctx.Err()
	}
	l.mu.Lock()
	now := l.now()
	at := l.next
	if at.Before(now) {
		at = now
	}
	l.next = at.Add(l.interval)
	l.mu.Unlock()
	if d := at.Sub(now); d > 0 {
		return l.sleep(ctx, d)
	}
	return ctx.Err()
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
