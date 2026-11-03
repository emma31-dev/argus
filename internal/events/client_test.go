package events

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSleeper struct {
	mu sync.Mutex
	ds []time.Duration
}

func (f *fakeSleeper) sleep(_ context.Context, d time.Duration) error {
	f.mu.Lock()
	f.ds = append(f.ds, d)
	f.mu.Unlock()
	return nil
}

func newTestClient(url string) (*Client, *fakeSleeper) {
	fs := &fakeSleeper{}
	c := &Client{BaseURL: url, RPS: -1, sleep: fs.sleep, Backoff: 100 * time.Millisecond}
	return c, fs
}

func TestBlockDecodeAndNull(t *testing.T) {
	blk, _ := os.ReadFile("testdata/block_events.json")
	null, _ := os.ReadFile("testdata/block_null.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/block/1000":
			w.Write(blk)
		case "/v0/block/1001":
			w.Write(null)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, _ := newTestClient(srv.URL)
	b, err := c.Block(context.Background(), 1000)
	if err != nil || b == nil || b.Height() != 1000 {
		t.Fatalf("b=%v err=%v", b, err)
	}
	b, err = c.Block(context.Background(), 1001)
	if b != nil || err != nil {
		t.Fatalf("skipped height: b=%v err=%v", b, err)
	}
	if _, err = c.Block(context.Background(), 5); err == nil {
		t.Fatal("404 should error")
	} else {
		var se *StatusError
		if !errors.As(err, &se) || se.Status != 404 {
			t.Fatalf("err %v", err)
		}
	}
}

func TestBlockFollowsRedirect(t *testing.T) {
	blk, _ := os.ReadFile("testdata/block_events.json")
	archive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/archive/1000" {
			http.NotFound(w, r)
			return
		}
		w.Write(blk)
	}))
	defer archive.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, archive.URL+"/archive/1000", http.StatusFound)
	}))
	defer srv.Close()
	c, _ := newTestClient(srv.URL)
	b, err := c.Block(context.Background(), 1000)
	if err != nil || b == nil || b.Height() != 1000 {
		t.Fatalf("b=%v err=%v", b, err)
	}
}

func TestLastFinalRedirect(t *testing.T) {
	var bodyFetched atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/last_block/final":
			http.Redirect(w, r, "/v0/block/123456789", http.StatusFound)
		case "/v0/block/123456789":
			bodyFetched.Store(true)
			w.Write([]byte(`null`))
		}
	}))
	defer srv.Close()
	c, _ := newTestClient(srv.URL)
	h, err := c.LastFinal(context.Background())
	if err != nil || h != 123456789 {
		t.Fatalf("h=%d err=%v", h, err)
	}
	if bodyFetched.Load() {
		t.Error("LastFinal should not download the block when Location is available")
	}
}

func TestLastFinalBodyFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"block":{"header":{"height":777}},"shards":[]}`))
	}))
	defer srv.Close()
	c, _ := newTestClient(srv.URL)
	h, err := c.LastFinal(context.Background())
	if err != nil || h != 777 {
		t.Fatalf("h=%d err=%v", h, err)
	}
}

func TestBackoffOn429And5xx(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch n.Add(1) {
		case 1:
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(429)
		case 2:
			w.WriteHeader(503)
		default:
			w.Write([]byte(`null`))
		}
	}))
	defer srv.Close()
	c, fs := newTestClient(srv.URL)
	b, err := c.Block(context.Background(), 1)
	if err != nil || b != nil {
		t.Fatalf("b=%v err=%v", b, err)
	}
	if n.Load() != 3 {
		t.Fatalf("requests = %d", n.Load())
	}
	if len(fs.ds) != 2 || fs.ds[0] != 2*time.Second || fs.ds[1] != 200*time.Millisecond {
		t.Fatalf("sleeps %v", fs.ds)
	}
}

func TestBackoffGivesUp(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(429)
	}))
	defer srv.Close()
	c, _ := newTestClient(srv.URL)
	c.MaxRetries = 2
	_, err := c.Block(context.Background(), 1)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 429 || n.Load() != 3 {
		t.Fatalf("err=%v n=%d", err, n.Load())
	}
}

func TestAPIKeyNotLeakedInErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apiKey") != "sekret" {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(403)
	}))
	defer srv.Close()
	c, _ := newTestClient(srv.URL)
	c.APIKey = "sekret"
	_, err := c.Block(context.Background(), 1)
	if err == nil || contains(err.Error(), "sekret") {
		t.Fatalf("err=%v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestLimiterSpacing(t *testing.T) {
	l := NewLimiter(2)
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	var slept []time.Duration
	l.sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil }
	for i := 0; i < 4; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	want := []time.Duration{500 * time.Millisecond, time.Second, 1500 * time.Millisecond}
	if len(slept) != 3 || slept[0] != want[0] || slept[1] != want[1] || slept[2] != want[2] {
		t.Fatalf("slept %v want %v", slept, want)
	}
	now = now.Add(10 * time.Second) // idle: no debt carried
	slept = nil
	l.Wait(context.Background())
	if len(slept) != 0 {
		t.Fatalf("slept after idle: %v", slept)
	}
}

func TestLimiterRealRateAndCancel(t *testing.T) {
	l := NewLimiter(50)
	start := time.Now()
	for i := 0; i < 6; i++ {
		l.Wait(context.Background())
	}
	if el := time.Since(start); el < 90*time.Millisecond {
		t.Fatalf("6 waits at 50rps took only %v", el)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	slow := NewLimiter(0.001)
	slow.Wait(context.Background())
	if err := slow.Wait(ctx); err == nil {
		t.Fatal("cancelled wait should fail")
	}
	if err := NewLimiter(-1).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientDefaults(t *testing.T) {
	c := &Client{}
	if c.base() != DefaultBaseURL {
		t.Error(c.base())
	}
	c.init()
	if c.limiter.interval != 500*time.Millisecond {
		t.Errorf("default interval %v", c.limiter.interval)
	}
}
