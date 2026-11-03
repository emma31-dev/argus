package alert

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type retryErr struct{ d time.Duration }

func (e retryErr) Error() string             { return "rate limited" }
func (e retryErr) RetryAfter() time.Duration { return e.d }

type errNotifier struct {
	errs  []error // returned in order; nil entry = success; last repeats
	mu    sync.Mutex
	calls int
}

func (n *errNotifier) count() int { n.mu.Lock(); defer n.mu.Unlock(); return n.calls }

func (n *errNotifier) Name() string { return "n" }
func (n *errNotifier) Notify(context.Context, Alert) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	e := n.errs[min(n.calls, len(n.errs)-1)]
	n.calls++
	return e
}

func recordSleeps(d *Dispatcher) *[]time.Duration {
	var slept []time.Duration
	d.sleep = func(_ context.Context, dur time.Duration) error { slept = append(slept, dur); return nil }
	return &slept
}

// Review finding 4: with the old defaults (5 attempts, 1s base, 1m cap) a red
// alert was dead-lettered after about 15 s. The defaults now retry for tens
// of minutes.
func TestDispatcherDefaultsRetryForMinutes(t *testing.T) {
	ctx := context.Background()
	q := NewMemoryQueue()
	_ = q.Enqueue(ctx, sampleAlert(TurnedRed))
	n := &errNotifier{errs: []error{errors.New("telegram down")}}
	d := NewDispatcher(DispatcherConfig{}, q, n)
	slept := recordSleeps(d)
	if _, err := d.ProcessOne(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if n.calls != 12 {
		t.Fatalf("attempts = %d, want 12", n.calls)
	}
	var total time.Duration
	for _, s := range *slept {
		total += s
	}
	if len(*slept) != 11 || (*slept)[0] != 2*time.Second || (*slept)[10] != 5*time.Minute || total < 20*time.Minute {
		t.Fatalf("backoff %v (total %v)", *slept, total)
	}
	if len(q.DeadLetters()) != 1 {
		t.Fatal("not dead-lettered after the last attempt")
	}
}

func TestDispatcherHonoursRetryAfter(t *testing.T) {
	ctx := context.Background()
	q := NewMemoryQueue()
	_ = q.Enqueue(ctx, sampleAlert(TurnedRed))
	wrapped := fmt.Errorf("chat 1: %w", retryErr{d: 45 * time.Second})
	n := &errNotifier{errs: []error{errors.Join(wrapped, errors.New("other")), retryErr{d: time.Millisecond}, nil}}
	d := NewDispatcher(DispatcherConfig{}, q, n)
	slept := recordSleeps(d)
	if _, err := d.ProcessOne(ctx, 0); err != nil {
		t.Fatal(err)
	}
	// First wait: retry_after (45s) beats the 2s backoff; second: the 4s backoff beats 1ms.
	if len(*slept) != 2 || (*slept)[0] != 45*time.Second || (*slept)[1] != 4*time.Second {
		t.Fatalf("slept %v", *slept)
	}
	if n.calls != 3 || len(q.DeadLetters()) != 0 {
		t.Fatalf("calls %d", n.calls)
	}
}

func TestDispatcherRetryAfterIsBounded(t *testing.T) {
	q := NewMemoryQueue()
	_ = q.Enqueue(context.Background(), sampleAlert(TurnedRed))
	n := &errNotifier{errs: []error{retryErr{d: 1000 * time.Hour}, nil}}
	d := NewDispatcher(DispatcherConfig{}, q, n)
	slept := recordSleeps(d)
	_, _ = d.ProcessOne(context.Background(), 0)
	if len(*slept) != 1 || (*slept)[0] != time.Hour {
		t.Fatalf("slept %v", *slept)
	}
}

// The Telegram client turns a 429 with parameters.retry_after into an error
// the dispatcher can read.
func TestTelegram429CarriesRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 17","parameters":{"retry_after":17}}`))
	}))
	defer srv.Close()
	tg, err := NewTelegram(TelegramConfig{Token: "T", BaseURL: srv.URL, DefaultChatID: "1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	nerr := tg.Notify(context.Background(), sampleAlert(TurnedRed))
	if nerr == nil {
		t.Fatal("429 must be an error (retryable)")
	}
	if got := retryAfter([]error{nerr}); got != 17*time.Second {
		t.Fatalf("retryAfter = %v (%v)", got, nerr)
	}
	var apiErr *APIError
	if !errors.As(nerr, &apiErr) || apiErr.permanent() {
		t.Fatalf("%v", nerr)
	}
}

func TestMemoryQueueRequeueDead(t *testing.T) {
	ctx := context.Background()
	q := NewMemoryQueue()
	for i := 0; i < 3; i++ {
		a := sampleAlert(TurnedRed)
		a.IncidentID = int64(i + 1)
		_ = q.Enqueue(ctx, a)
		d, _ := q.Dequeue(ctx, 0)
		_ = q.Dead(ctx, d, "boom")
	}
	q.dead[0].At = time.Now().Add(-25 * time.Hour) // too old
	n, err := q.RequeueDead(ctx, 1)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if d, _ := q.Dequeue(ctx, 0); d == nil || d.Alert.IncidentID != 2 {
		t.Fatalf("oldest eligible must come first: %+v", d)
	}
	if n, _ := q.RequeueDead(ctx, 10); n != 1 {
		t.Fatalf("second pass n=%d", n)
	}
	if n, _ := q.RequeueDead(ctx, 10); n != 0 {
		t.Fatalf("only the too-old entry is left, n=%d", n)
	}
	if got := q.DeadLetters(); len(got) != 1 || got[0].Alert.IncidentID != 1 {
		t.Fatalf("%+v", got)
	}
}

// The dispatcher requeues dead letters periodically, so an alert that
// exhausted its attempts during a long outage is delivered after recovery.
func TestDispatcherRequeuesDeadLettersPeriodically(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q := NewMemoryQueue()
	_ = q.Enqueue(ctx, sampleAlert(TurnedRed))
	n := &errNotifier{errs: []error{errors.New("down"), errors.New("down"), nil}}
	d := NewDispatcher(DispatcherConfig{MaxAttempts: 2, RequeueEvery: 20 * time.Millisecond, PollWait: 5 * time.Millisecond}, q, n)
	d.sleep = func(context.Context, time.Duration) error { return nil }
	done := make(chan struct{})
	go func() { _ = d.Run(ctx); close(done) }()
	for n.count() < 3 && ctx.Err() == nil {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if n.count() != 3 {
		t.Fatalf("alert not redelivered after dead-lettering: %d calls", n.count())
	}
	if len(q.DeadLetters()) != 0 {
		t.Fatalf("dead letters left: %+v", q.DeadLetters())
	}
}
