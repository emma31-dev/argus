package rpc

import (
	"sync"
	"time"
)

// bucket is a small token-bucket rate limiter. It never sleeps itself:
// reserve tells the caller how long to wait, which keeps it testable with a
// fake clock.
type bucket struct {
	mu     sync.Mutex
	rps    float64
	burst  float64
	tokens float64
	last   time.Time
}

// newBucket returns a limiter allowing rps requests per second with the
// given burst. rps <= 0 disables limiting. The bucket starts full.
func newBucket(rps float64, burst int, now time.Time) *bucket {
	if burst < 1 {
		burst = 1
	}
	return &bucket{rps: rps, burst: float64(burst), tokens: float64(burst), last: now}
}

// reserve consumes one token and returns how long the caller must wait
// before using it (0 if one was available).
func (b *bucket) reserve(now time.Time) time.Duration {
	if b == nil || b.rps <= 0 {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if now.After(b.last) {
		b.tokens += now.Sub(b.last).Seconds() * b.rps
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.last = now
	}
	b.tokens--
	if b.tokens >= 0 {
		return 0
	}
	return time.Duration(-b.tokens / b.rps * float64(time.Second))
}
