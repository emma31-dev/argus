package alert

import (
	"sync"
	"time"
)

// updateThrottle limits IncidentUpdate alerts to one per interval per incident.
type updateThrottle struct {
	mu    sync.Mutex
	every time.Duration
	last  map[int64]time.Time
	now   func() time.Time
}

func newUpdateThrottle(every time.Duration, now func() time.Time) *updateThrottle {
	return &updateThrottle{every: every, last: map[int64]time.Time{}, now: now}
}

// ok reports whether an update for the incident may be sent now.
func (t *updateThrottle) ok(incident int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	last, seen := t.last[incident]
	return !seen || t.now().Sub(last) >= t.every
}

func (t *updateThrottle) mark(incident int64) {
	t.mu.Lock()
	t.last[incident] = t.now()
	t.mu.Unlock()
}

func (t *updateThrottle) forget(incident int64) {
	t.mu.Lock()
	delete(t.last, incident)
	t.mu.Unlock()
}

// sentSet remembers recent (alert, recipient) deliveries so that a retry
// after a partial failure does not message the same recipient twice.
type sentSet struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]time.Time
	now func() time.Time
}

func newSentSet(ttl time.Duration, now func() time.Time) *sentSet {
	return &sentSet{ttl: ttl, m: map[string]time.Time{}, now: now}
}

func (s *sentSet) has(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.m[key]
	return ok
}

func (s *sentSet) add(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, t := range s.m {
		if now.Sub(t) > s.ttl {
			delete(s.m, k)
		}
	}
	s.m[key] = now
}
