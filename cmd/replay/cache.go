package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrOfflineMiss is returned in -offline mode for a request that is not in
// the cache.
var ErrOfflineMiss = errors.New("replay: request not in the cache and -offline is set")

// CachingTransport answers JSON-RPC requests from a directory when it can and
// otherwise forwards them (rate limited) and stores the answer. The cache key
// is the SHA-256 of the canonical request body with the per-request "id"
// removed, so it is stable across runs. Cache files use the recorder format
// ({"provider","request","response"}), so they are also valid rpctest input.
//
// "Latest final block" requests (params.finality) are never answered from the
// cache while online, because the answer changes; the fresh answer is stored
// and used for -offline runs.
type CachingTransport struct {
	Dir     string
	Next    http.RoundTripper
	Offline bool
	// MinInterval is the minimum time between two network requests.
	MinInterval time.Duration

	mu      sync.Mutex
	last    time.Time
	network int
	hits    int
}

// Stats returns the number of cache hits and network requests so far.
func (t *CachingTransport) Stats() (hits, network int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hits, t.network
}

// CacheKey is the cache key of a JSON-RPC request body.
func CacheKey(body []byte) (string, bool, error) {
	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		return "", false, err
	}
	delete(req, "id")
	canon, err := json.Marshal(req) // map keys are sorted; nested raw JSON is kept as is
	if err != nil {
		return "", false, err
	}
	var params struct {
		Finality string `json:"finality"`
	}
	_ = json.Unmarshal(req["params"], &params)
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), params.Finality != "", nil
}

func (t *CachingTransport) path(key string) string { return filepath.Join(t.Dir, key+".json") }

func (t *CachingTransport) readCache(key string) ([]byte, bool) {
	raw, err := os.ReadFile(t.path(key))
	if err != nil {
		return nil, false
	}
	var ex struct {
		Response json.RawMessage `json:"response"`
	}
	if json.Unmarshal(raw, &ex) != nil || len(ex.Response) == 0 {
		return nil, false
	}
	return ex.Response, true
}

func (t *CachingTransport) respond(req *http.Request, body []byte) *http.Response {
	return &http.Response{
		StatusCode: 200, Status: "200 OK", Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req,
	}
}

// RoundTrip implements http.RoundTripper.
func (t *CachingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	key, volatile, err := CacheKey(body)
	if err != nil {
		return nil, fmt.Errorf("replay: undecodable request: %w", err)
	}
	if !volatile || t.Offline {
		if resp, ok := t.readCache(key); ok {
			t.mu.Lock()
			t.hits++
			t.mu.Unlock()
			return t.respond(req, resp), nil
		}
	}
	if t.Offline {
		return nil, fmt.Errorf("%w: %s", ErrOfflineMiss, describe(body))
	}
	if err := t.wait(req.Context()); err != nil {
		return nil, err
	}
	out := req.Clone(req.Context())
	out.Body = io.NopCloser(bytes.NewReader(body))
	out.ContentLength = int64(len(body))
	resp, err := t.Next.RoundTrip(out)
	if err != nil {
		return nil, err
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK && cacheable(respBody) {
		if err := t.store(key, req.URL.Host, body, respBody); err != nil {
			return nil, fmt.Errorf("replay: writing cache: %w", err)
		}
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	return resp, nil
}

// wait enforces MinInterval between network requests.
func (t *CachingTransport) wait(ctx context.Context) error {
	t.mu.Lock()
	now := time.Now()
	at := t.last.Add(t.MinInterval)
	if at.Before(now) {
		at = now
	}
	t.last = at
	t.network++
	t.mu.Unlock()
	if d := time.Until(at); d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

// cacheable is true for a successful JSON-RPC result (not an error, not a
// contract-level failure delivered inside result.error).
func cacheable(body []byte) bool {
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &env) != nil || len(env.Result) == 0 || string(env.Result) == "null" || len(env.Error) > 0 {
		return false
	}
	var inner struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(env.Result, &inner) == nil && inner.Error != "" {
		return false
	}
	return true
}

func (t *CachingTransport) store(key, host string, req, resp []byte) error {
	if err := os.MkdirAll(t.Dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(map[string]any{
		"provider": host, "request": json.RawMessage(req), "response": json.RawMessage(resp),
		"cached_at": time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return err
	}
	tmp := t.path(key) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, t.path(key))
}

func describe(body []byte) string {
	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	_ = json.Unmarshal(body, &req)
	s := string(req.Params)
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return req.Method + " " + s
}
