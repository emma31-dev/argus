package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// callResult builds a synthetic call_function result carrying body.
func callResult(body string) []byte {
	ints := make([]int, len(body))
	for i := range body {
		ints[i] = int(body[i])
	}
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "1",
		"result": map[string]any{"result": ints, "logs": []string{}, "block_height": 1, "block_hash": "h"},
	})
	return b
}

type recSleep struct {
	mu sync.Mutex
	ds []time.Duration
}

func (r *recSleep) sleep(ctx context.Context, d time.Duration) error {
	r.mu.Lock()
	r.ds = append(r.ds, d)
	r.mu.Unlock()
	return ctx.Err()
}

func newTestClient(t *testing.T, urls []string, opts ...Option) (*Client, *recSleep) {
	t.Helper()
	rs := &recSleep{}
	var ps []Provider
	for i, u := range urls {
		ps = append(ps, Provider{Name: string(rune('a' + i)), URL: u})
	}
	opts = append([]Option{WithSleep(rs.sleep), WithBackoff(10*time.Millisecond, 80*time.Millisecond)}, opts...)
	return New(ps, opts...), rs
}

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func TestLatestFinalBlock(t *testing.T) {
	var got map[string]any
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write(fixture(t, "block.json"))
	})
	c, _ := newTestClient(t, []string{s.URL})
	b, prov, err := c.LatestFinalBlock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.Height != 123456789 || b.Hash == "" || prov != "a" {
		t.Fatalf("block %+v prov %q", b, prov)
	}
	if b.Time.UnixNano() != 1790000000123456789 {
		t.Fatalf("time %v", b.Time.UnixNano())
	}
	if got["method"] != "block" || got["params"].(map[string]any)["finality"] != "final" {
		t.Fatalf("request %v", got)
	}
}

func TestBlockAt(t *testing.T) {
	var got map[string]any
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write(fixture(t, "block.json"))
	})
	c, _ := newTestClient(t, []string{s.URL})
	if _, _, err := c.BlockAt(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if got["params"].(map[string]any)["block_id"] != float64(42) {
		t.Fatalf("params %v", got["params"])
	}
}

func TestCallFunctionDecodeAndArgs(t *testing.T) {
	var params map[string]any
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Params map[string]any }
		_ = json.NewDecoder(r.Body).Decode(&req)
		params = req.Params
		_, _ = w.Write(callResult(`"1234567890123456789012345678901234567890"`))
	})
	c, _ := newTestClient(t, []string{s.URL})
	out, err := c.CallFunction(context.Background(), "", "intents.near", "mt_supply", map[string]any{"token_id": "nep141:wrap.near"}, 777)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"1234567890123456789012345678901234567890"` {
		t.Fatalf("out %s", out)
	}
	if params["request_type"] != "call_function" || params["account_id"] != "intents.near" ||
		params["method_name"] != "mt_supply" || params["block_id"] != float64(777) {
		t.Fatalf("params %v", params)
	}
	dec, err := base64.StdEncoding.DecodeString(params["args_base64"].(string))
	if err != nil || string(dec) != `{"token_id":"nep141:wrap.near"}` {
		t.Fatalf("args %q %v", dec, err)
	}
}

func TestFallbackOnTransientFailures(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"429": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) },
		"500": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) },
		"503": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) },
		"garbage": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("<html>not json"))
		},
		"timeout": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(300 * time.Millisecond):
			}
		},
		"retryable rpc error": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","error":{"name":"HANDLER_ERROR","cause":{"name":"UNAVAILABLE_SHARD","info":{}},"code":-32000,"message":"Server error"}}`))
		},
		"result.error not tracking shard": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"error":"The node does not track the shard ID 3","logs":[]}}`))
		},
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			var goodHits atomic.Int32
			badS := serve(t, bad)
			goodS := serve(t, func(w http.ResponseWriter, r *http.Request) {
				goodHits.Add(1)
				_, _ = w.Write(callResult(`"5"`))
			})
			c, rs := newTestClient(t, []string{badS.URL, goodS.URL}, WithRequestTimeout(80*time.Millisecond))
			out, err := c.CallFunction(context.Background(), "", "x.near", "m", nil, 1)
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != `"5"` || goodHits.Load() != 1 {
				t.Fatalf("out %s hits %d", out, goodHits.Load())
			}
			if len(rs.ds) != 1 {
				t.Fatalf("expected one backoff sleep, got %v", rs.ds)
			}
			h := c.Health()
			if h[0].Healthy || h[0].LastError == "" || !h[1].Healthy || h[1].LastOK.IsZero() {
				t.Fatalf("health %+v", h)
			}
		})
	}
}

func TestHealthyProviderPreferredAfterFailure(t *testing.T) {
	var badHits, goodHits atomic.Int32
	badS := serve(t, func(w http.ResponseWriter, r *http.Request) { badHits.Add(1); w.WriteHeader(500) })
	goodS := serve(t, func(w http.ResponseWriter, r *http.Request) { goodHits.Add(1); _, _ = w.Write(callResult(`1`)) })
	c, _ := newTestClient(t, []string{badS.URL, goodS.URL})
	for i := 0; i < 3; i++ {
		if _, err := c.CallFunction(context.Background(), "", "x", "m", nil, 1); err != nil {
			t.Fatal(err)
		}
	}
	if badHits.Load() != 1 || goodHits.Load() != 3 {
		t.Fatalf("bad %d good %d", badHits.Load(), goodHits.Load())
	}
}

func TestAllProvidersFail(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) })
	c, rs := newTestClient(t, []string{s.URL, s.URL, s.URL})
	_, err := c.CallFunction(context.Background(), "", "x", "m", nil, 1)
	if !errors.Is(err, ErrProviderUnavailable) || !IsProviderFailure(err) {
		t.Fatalf("err %v", err)
	}
	// Backoff grows exponentially (with jitter in [d/2, d]) and is capped.
	if len(rs.ds) != 2 || rs.ds[0] > 10*time.Millisecond || rs.ds[1] > 20*time.Millisecond || rs.ds[1] < 10*time.Millisecond {
		t.Fatalf("sleeps %v", rs.ds)
	}
}

func TestContextCancelStops(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	c, _ := newTestClient(t, []string{s.URL, s.URL})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.CallFunction(ctx, "", "x", "m", nil, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		fixture string
		is      error
		isNot   error
		failure bool
	}{
		{"query_error_method_not_found.json", ErrMethodNotFound, ErrContractPanic, false},
		{"error_contract_execution.json", ErrContractPanic, ErrMethodNotFound, false},
		{"error_unknown_account.json", ErrAccountNotFound, ErrContractPanic, false},
		{"error_unknown_block.json", ErrUnknownBlock, ErrMethodNotFound, true},
		{"error_gc_block.json", ErrNeedsArchival, ErrMethodNotFound, true},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			var hits atomic.Int32
			s1 := serve(t, func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				_, _ = w.Write(fixture(t, tc.fixture))
			})
			s2 := serve(t, func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				_, _ = w.Write(fixture(t, tc.fixture))
			})
			c, _ := newTestClient(t, []string{s1.URL, s2.URL})
			_, err := c.CallFunction(context.Background(), "", "x", "m", nil, 1)
			if !errors.Is(err, tc.is) || errors.Is(err, tc.isNot) {
				t.Fatalf("err %v", err)
			}
			if IsProviderFailure(err) != tc.failure {
				t.Fatalf("IsProviderFailure=%v for %v", !tc.failure, err)
			}
			wantHits := int32(1) // deterministic errors do not fall back
			if tc.failure {
				wantHits = 2
			}
			if hits.Load() != wantHits {
				t.Fatalf("hits %d want %d", hits.Load(), wantHits)
			}
			var re *Error
			if !errors.As(err, &re) && !tc.failure {
				t.Fatalf("no *Error in %v", err)
			}
		})
	}
}

func TestUnknownBlockFallsThroughToArchival(t *testing.T) {
	pruned := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(fixture(t, "error_gc_block.json")) })
	arch := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(callResult(`"9"`)) })
	rs := &recSleep{}
	c := New([]Provider{{Name: "pruned", URL: pruned.URL}, {Name: "arch", URL: arch.URL, Archival: true}}, WithSleep(rs.sleep))
	out, err := c.CallFunction(context.Background(), "", "x", "m", nil, 1)
	if err != nil || string(out) != `"9"` {
		t.Fatalf("%s %v", out, err)
	}
}

func TestNeedsArchivalWhenNoArchivalProvider(t *testing.T) {
	pruned := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(fixture(t, "error_unknown_block.json")) })
	c, _ := newTestClient(t, []string{pruned.URL})
	_, err := c.CallFunction(context.Background(), "", "x", "m", nil, 1)
	if !errors.Is(err, ErrNeedsArchival) || !errors.Is(err, ErrUnknownBlock) {
		t.Fatalf("err %v", err)
	}
}

func TestPinnedNeverFallsBack(t *testing.T) {
	var aHits, bHits atomic.Int32
	a := serve(t, func(w http.ResponseWriter, r *http.Request) { aHits.Add(1); w.WriteHeader(500) })
	b := serve(t, func(w http.ResponseWriter, r *http.Request) { bHits.Add(1); _, _ = w.Write(callResult(`1`)) })
	c, _ := newTestClient(t, []string{a.URL, b.URL})

	_, err := c.CallFunction(context.Background(), "a", "x", "m", nil, 1)
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err %v", err)
	}
	if aHits.Load() != 2 || bHits.Load() != 0 { // default pinned attempts = 2
		t.Fatalf("a %d b %d", aHits.Load(), bHits.Load())
	}
	if _, err := c.Pinned("b").CallFunction(context.Background(), "x", "m", nil, 1); err != nil {
		t.Fatal(err)
	}
	if bHits.Load() != 1 {
		t.Fatalf("b %d", bHits.Load())
	}
	if _, err := c.CallFunction(context.Background(), "nope", "x", "m", nil, 1); err == nil {
		t.Fatal("expected unknown provider error")
	}
}

func TestRecorder(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(callResult(`1`)) })
	var n int
	c, _ := newTestClient(t, []string{s.URL}, WithRecorder(func(p, m string, req, resp []byte) {
		n++
		if p != "a" || m != "query" || len(req) == 0 || len(resp) == 0 {
			t.Errorf("bad record %q %q", p, m)
		}
	}))
	if _, err := c.CallFunction(context.Background(), "", "x", "m", nil, 1); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestBucket(t *testing.T) {
	now := time.Unix(1000, 0)
	b := newBucket(2, 3, now) // 2 rps, burst 3
	for i := 0; i < 3; i++ {
		if w := b.reserve(now); w != 0 {
			t.Fatalf("burst %d waited %v", i, w)
		}
	}
	if w := b.reserve(now); w != 500*time.Millisecond {
		t.Fatalf("4th wait %v", w)
	}
	if w := b.reserve(now); w != time.Second {
		t.Fatalf("5th wait %v", w)
	}
	// After 10 seconds the bucket is full again (capped at burst).
	now = now.Add(10 * time.Second)
	for i := 0; i < 3; i++ {
		if w := b.reserve(now); w != 0 {
			t.Fatalf("refill %d waited %v", i, w)
		}
	}
	if w := b.reserve(now); w <= 0 {
		t.Fatal("expected wait after burst")
	}
	if w := newBucket(0, 1, now).reserve(now); w != 0 {
		t.Fatal("rps 0 must be unlimited")
	}
}

func TestClientRateLimitWaits(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(callResult(`1`)) })
	rs := &recSleep{}
	now := time.Unix(5000, 0)
	c := New([]Provider{{Name: "a", URL: s.URL, RPS: 1, Burst: 1}},
		WithSleep(rs.sleep), WithClock(func() time.Time { return now }))
	for i := 0; i < 3; i++ {
		if _, err := c.CallFunction(context.Background(), "", "x", "m", nil, 1); err != nil {
			t.Fatal(err)
		}
	}
	// Burst of 1, frozen clock: calls 2 and 3 wait 1s and 2s.
	if len(rs.ds) != 2 || rs.ds[0] != time.Second || rs.ds[1] != 2*time.Second {
		t.Fatalf("waits %v", rs.ds)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	hdr := func(v string) http.Header { h := http.Header{}; h.Set("Retry-After", v); return h }
	for _, tc := range []struct {
		name string
		h    http.Header
		body string
		want time.Duration
	}{
		{"header", hdr("7"), "", 7 * time.Second},
		{"body text", http.Header{}, `Rate limit exceeded, retry after 12 seconds`, 12 * time.Second},
		{"nothing said", http.Header{}, `Rate limits exceeded`, defaultCooldown},
		{"bad header falls to body", hdr("soon"), `retry after 5 seconds`, 5 * time.Second},
		{"capped", hdr("600"), "", maxCooldown},
	} {
		if got := retryAfter(tc.h, []byte(tc.body)); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestPinnedCallWaitsOutRateLimit(t *testing.T) {
	var hits atomic.Int32
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= 3 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(callResult(`1`))
	})
	rs := &recSleep{}
	now := time.Unix(9000, 0)
	c := New([]Provider{{Name: "a", URL: s.URL}},
		WithSleep(rs.sleep), WithClock(func() time.Time { return now }), WithBackoff(10*time.Millisecond, 80*time.Millisecond))
	if _, err := c.CallFunction(context.Background(), "a", "x", "m", nil, 1); err != nil {
		t.Fatalf("a pinned call should outlast three 429s: %v", err)
	}
	if hits.Load() != 4 {
		t.Fatalf("hits %d, want 4", hits.Load())
	}
	cooled := 0
	for _, d := range rs.ds {
		if d == 3*time.Second {
			cooled++
		}
	}
	if cooled != 3 {
		t.Fatalf("slept %v: want three 3s cooldown waits", rs.ds)
	}
}

func TestRateLimitCooldownIsSharedAcrossCallers(t *testing.T) {
	var hits atomic.Int32
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(callResult(`1`))
	})
	rs := &recSleep{}
	now := time.Unix(9000, 0)
	c := New([]Provider{{Name: "a", URL: s.URL}},
		WithSleep(rs.sleep), WithClock(func() time.Time { return now }), WithBackoff(time.Millisecond, time.Millisecond))
	// First call hits the 429 and starts the cooldown; a different call made
	// afterwards must wait it out too, not just the call that was refused.
	if _, err := c.CallFunction(context.Background(), "a", "x", "m", nil, 1); err != nil {
		t.Fatal(err)
	}
	rs.ds = nil
	c.providers[0].coolDown(now, 5*time.Second)
	if _, err := c.CallFunction(context.Background(), "a", "y", "m", nil, 1); err != nil {
		t.Fatal(err)
	}
	if len(rs.ds) == 0 || rs.ds[0] != 5*time.Second {
		t.Fatalf("second caller did not wait the shared cooldown: %v", rs.ds)
	}
}

func TestUnpinnedRateLimitFailsOverWithoutRetrying(t *testing.T) {
	var aHits, bHits atomic.Int32
	a := serve(t, func(w http.ResponseWriter, r *http.Request) { aHits.Add(1); w.WriteHeader(http.StatusTooManyRequests) })
	b := serve(t, func(w http.ResponseWriter, r *http.Request) { bHits.Add(1); _, _ = w.Write(callResult(`1`)) })
	c, _ := newTestClient(t, []string{a.URL, b.URL})
	if _, err := c.CallFunction(context.Background(), "", "x", "m", nil, 1); err != nil {
		t.Fatal(err)
	}
	if aHits.Load() != 1 || bHits.Load() != 1 {
		t.Fatalf("a %d b %d, want 1 and 1", aHits.Load(), bHits.Load())
	}
}

func TestRateLimitRetriesAreBounded(t *testing.T) {
	var hits atomic.Int32
	s := serve(t, func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(http.StatusTooManyRequests) })
	c, _ := newTestClient(t, []string{s.URL})
	_, err := c.CallFunction(context.Background(), "a", "x", "m", nil, 1)
	if !IsRateLimited(err) || !IsProviderFailure(err) {
		t.Fatalf("err %v", err)
	}
	if want := int32(defaultPinTries + maxRateLimitRetries); hits.Load() != want {
		t.Fatalf("hits %d, want %d", hits.Load(), want)
	}
}
