// Package rpc is a read-only NEAR JSON-RPC client with provider fallback,
// per-provider rate limiting and health tracking.
//
// It supports exactly what Argus needs: the latest final block, a block by
// height, and view-function calls (query/call_function) pinned to a block
// height. Failures are classified into typed errors (see errors.go) so
// callers can tell "this provider is down" from "the contract has no such
// method".
//
// Same-block consistency: LatestFinalBlock returns the provider that served
// it. Passing that name to CallFunction pins the call to that provider, which
// never falls back to another one. Snapshot code uses this so that both
// sides of a reading come from one provider at one block, and retries the
// whole round elsewhere if that provider fails.
package rpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const (
	maxResponseBytes = 32 << 20
	defaultTimeout   = 20 * time.Second
	defaultBackoff   = 250 * time.Millisecond
	defaultMaxBack   = 5 * time.Second
	defaultPinTries  = 2

	// A pinned call cannot move to another provider, so a 429 gets extra
	// retries on top of pinTries, each after the provider's cooldown.
	maxRateLimitRetries = 4
	// Cooldown used when a 429 says nothing about how long to wait, and the
	// longest cooldown honoured when it does.
	defaultCooldown = 2 * time.Second
	maxCooldown     = 30 * time.Second
)

var retryAfterText = regexp.MustCompile(`(?i)retry after (\d+) second`)

// Provider is one JSON-RPC endpoint.
type Provider struct {
	Name     string
	URL      string
	RPS      float64 // requests per second, <= 0 means unlimited
	Burst    int     // token-bucket burst, defaults to 1
	Archival bool    // serves blocks older than the ~3 day non-archival window
}

// Block is the subset of a NEAR block header Argus needs.
type Block struct {
	Height uint64
	Hash   string
	Time   time.Time
}

// ProviderHealth is a snapshot of one provider's recent behaviour.
type ProviderHealth struct {
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Healthy   bool      `json:"healthy"`
	LastError string    `json:"last_error,omitempty"`
	LastOK    time.Time `json:"last_ok,omitempty"`
}

// Recorder receives every raw JSON-RPC exchange, for evidence and caches.
// It is called synchronously and must be safe for concurrent use.
type Recorder func(provider, method string, request, response []byte)

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the HTTP client (default: a plain http.Client; the
// per-request timeout is applied through the request context).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithClock sets the time source used for rate limiting and health.
func WithClock(now func() time.Time) Option { return func(c *Client) { c.now = now } }

// WithSleep replaces the sleep function used for backoff and rate-limit
// waits. It must return early with ctx.Err() when ctx is done.
func WithSleep(s func(ctx context.Context, d time.Duration) error) Option {
	return func(c *Client) { c.sleep = s }
}

// WithBackoff sets the exponential backoff base and cap.
func WithBackoff(base, max time.Duration) Option {
	return func(c *Client) { c.backoffBase, c.backoffMax = base, max }
}

// WithRequestTimeout sets the per-request timeout (default 20s).
func WithRequestTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }

// WithPinnedAttempts sets how many times a pinned call tries its one
// provider on retryable errors (default 2).
func WithPinnedAttempts(n int) Option { return func(c *Client) { c.pinTries = n } }

// WithRecorder installs a raw exchange recorder.
func WithRecorder(r Recorder) Option { return func(c *Client) { c.rec = r } }

type providerState struct {
	Provider
	limiter *bucket

	mu        sync.Mutex
	healthy   bool
	lastError string
	lastOK    time.Time
	coolUntil time.Time // set after a 429; every caller waits it out
}

// cooldownLeft is how long callers must still wait before using the provider.
func (p *providerState) cooldownLeft(now time.Time) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.coolUntil.After(now) {
		return p.coolUntil.Sub(now)
	}
	return 0
}

// coolDown keeps every caller off the provider for d. Concurrent workers
// that all hit a 429 would otherwise retry together and trip it again.
func (p *providerState) coolDown(now time.Time, d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if until := now.Add(d); until.After(p.coolUntil) {
		p.coolUntil = until
	}
}

// retryAfter reads how long a 429 asks us to wait: the Retry-After header
// (seconds), else "retry after N seconds" in the body, else a default. The
// result is capped so one provider cannot stall a round for long.
func retryAfter(h http.Header, body []byte) time.Duration {
	d := defaultCooldown
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil && s > 0 {
		d = time.Duration(s) * time.Second
	} else if m := retryAfterText.FindSubmatch(body); m != nil {
		if s, err := strconv.Atoi(string(m[1])); err == nil && s > 0 {
			d = time.Duration(s) * time.Second
		}
	}
	return min(d, maxCooldown)
}

// Client talks to a list of providers. It is safe for concurrent use.
type Client struct {
	providers   []*providerState
	http        *http.Client
	now         func() time.Time
	sleep       func(context.Context, time.Duration) error
	backoffBase time.Duration
	backoffMax  time.Duration
	timeout     time.Duration
	pinTries    int
	rec         Recorder
	reqID       uint64
	idMu        sync.Mutex
}

// New builds a client. Providers are tried in the given order.
func New(providers []Provider, opts ...Option) *Client {
	c := &Client{
		http:        &http.Client{},
		now:         time.Now,
		sleep:       sleepCtx,
		backoffBase: defaultBackoff,
		backoffMax:  defaultMaxBack,
		timeout:     defaultTimeout,
		pinTries:    defaultPinTries,
	}
	for _, o := range opts {
		o(c)
	}
	if c.pinTries < 1 {
		c.pinTries = 1
	}
	start := c.now()
	for _, p := range providers {
		c.providers = append(c.providers, &providerState{
			Provider: p,
			limiter:  newBucket(p.RPS, p.Burst, start),
			healthy:  true,
		})
	}
	return c
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Providers returns the configured provider names in priority order.
func (c *Client) Providers() []string {
	names := make([]string, len(c.providers))
	for i, p := range c.providers {
		names[i] = p.Name
	}
	return names
}

// Health reports each provider's current health.
func (c *Client) Health() []ProviderHealth {
	out := make([]ProviderHealth, len(c.providers))
	for i, p := range c.providers {
		p.mu.Lock()
		out[i] = ProviderHealth{Name: p.Name, URL: p.URL, Healthy: p.healthy, LastError: p.lastError, LastOK: p.lastOK}
		p.mu.Unlock()
	}
	return out
}

func (p *providerState) markOK(now time.Time) {
	p.mu.Lock()
	p.healthy, p.lastOK = true, now
	p.mu.Unlock()
}

func (p *providerState) markFail(err error) {
	p.mu.Lock()
	p.healthy, p.lastError = false, err.Error()
	p.mu.Unlock()
}

func (p *providerState) isHealthy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.healthy
}

// order returns the providers to try: the named one only, or all of them
// with healthy providers first (stable within each group).
func (c *Client) order(only string) ([]*providerState, error) {
	if only != "" {
		for _, p := range c.providers {
			if p.Name == only {
				return []*providerState{p}, nil
			}
		}
		return nil, fmt.Errorf("rpc: unknown provider %q", only)
	}
	if len(c.providers) == 0 {
		return nil, errors.New("rpc: no providers configured")
	}
	out := make([]*providerState, 0, len(c.providers))
	for _, p := range c.providers {
		if p.isHealthy() {
			out = append(out, p)
		}
	}
	for _, p := range c.providers {
		if !p.isHealthy() {
			out = append(out, p)
		}
	}
	return out, nil
}

func (c *Client) backoff(n int) time.Duration {
	d := c.backoffBase
	for i := 1; i < n && d < c.backoffMax; i++ {
		d *= 2
	}
	if d > c.backoffMax {
		d = c.backoffMax
	}
	if d <= 1 {
		return d
	}
	// Jitter in [d/2, d].
	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
}

// call sends one JSON-RPC request with fallback (only == "") or pinned to a
// single provider. It returns the "result" member and the serving provider.
func (c *Client) call(ctx context.Context, only, method string, params any) (json.RawMessage, string, error) {
	order, err := c.order(only)
	if err != nil {
		return nil, "", err
	}
	attempts := 1
	if only != "" {
		attempts = c.pinTries
	}
	var errs []error
	failures := 0
	triedArchival, sawBlockErr := false, false
	for _, p := range order {
		triedArchival = triedArchival || p.Archival
		rateRetries := 0
		for a := 0; a < attempts; a++ {
			if failures > 0 {
				if err := c.sleep(ctx, c.backoff(failures)); err != nil {
					return nil, "", err
				}
			}
			res, err := c.once(ctx, p, method, params)
			if err == nil {
				return res, p.Name, nil
			}
			if ctx.Err() != nil {
				return nil, "", ctx.Err()
			}
			if !IsProviderFailure(err) {
				return nil, p.Name, err // deterministic: other providers would agree
			}
			errs = append(errs, err)
			failures++
			if only != "" && IsRateLimited(err) && rateRetries < maxRateLimitRetries {
				// Pinned: there is nowhere else to go, and once() waits out
				// the cooldown, so this retry is not counted as an attempt.
				rateRetries++
				a--
			}
			if errors.Is(err, ErrUnknownBlock) {
				sawBlockErr = true
				break // retrying the same provider cannot help
			}
		}
	}
	joined := errors.Join(errs...)
	if sawBlockErr && !triedArchival {
		joined = fmt.Errorf("%w: %w", ErrNeedsArchival, joined)
	}
	return nil, "", fmt.Errorf("rpc: %s failed on %d provider(s): %w", method, len(order), joined)
}

func (c *Client) nextID() uint64 {
	c.idMu.Lock()
	defer c.idMu.Unlock()
	c.reqID++
	return c.reqID
}

// once performs a single HTTP exchange with one provider.
func (c *Client) once(ctx context.Context, p *providerState, method string, params any) (json.RawMessage, error) {
	fail := func(kind, cause error, code, msg string) error {
		e := &Error{Provider: p.Name, Method: method, Kind: kind, Cause: cause, Code: code, Message: msg}
		if IsProviderFailure(e) {
			p.markFail(e)
		} else {
			p.markOK(c.now()) // it answered; the call itself was bad
		}
		return e
	}
	if w := p.cooldownLeft(c.now()); w > 0 {
		if err := c.sleep(ctx, w); err != nil {
			return nil, err
		}
	}
	if w := p.limiter.reserve(c.now()); w > 0 {
		if err := c.sleep(ctx, w); err != nil {
			return nil, err
		}
	}
	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": strconv.FormatUint(c.nextID(), 10),
		"method": method, "params": params,
	})
	if err != nil {
		return nil, &Error{Provider: p.Name, Method: method, Kind: ErrInvalidRequest, Cause: err}
	}
	rctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodPost, p.URL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, &Error{Provider: p.Name, Method: method, Kind: ErrInvalidRequest, Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fail(ErrProviderUnavailable, err, "", "")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fail(ErrProviderUnavailable, err, "", "")
	}
	if c.rec != nil {
		c.rec(p.Name, method, reqBody, body)
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			p.coolDown(c.now(), retryAfter(resp.Header, body))
		}
		return nil, fail(ErrProviderUnavailable, nil, "HTTP_"+strconv.Itoa(resp.StatusCode), truncate(string(body), 200))
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fail(ErrProviderUnavailable, err, "", "undecodable response")
	}
	if env.Error != nil {
		kind, code := classifyRPCError(env.Error)
		return nil, fail(kind, nil, code, env.Error.detail())
	}
	if len(env.Result) == 0 || string(env.Result) == "null" {
		return nil, fail(ErrProviderUnavailable, nil, "", "empty result")
	}
	if method == "query" {
		// call_function failures can arrive as HTTP 200 with result.error.
		var q struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(env.Result, &q) == nil && q.Error != "" {
			return nil, fail(classifyMessage(q.Error), nil, "", q.Error)
		}
	}
	p.markOK(c.now())
	return env.Result, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

type blockResult struct {
	Header struct {
		Height       uint64 `json:"height"`
		Hash         string `json:"hash"`
		TimestampNS  string `json:"timestamp_nanosec"`
		TimestampRaw uint64 `json:"timestamp"`
	} `json:"header"`
}

func (b blockResult) block() (Block, error) {
	if b.Header.Height == 0 || b.Header.Hash == "" {
		return Block{}, errors.New("block header missing height or hash")
	}
	ns := b.Header.TimestampRaw
	if b.Header.TimestampNS != "" {
		v, err := strconv.ParseUint(b.Header.TimestampNS, 10, 64)
		if err != nil {
			return Block{}, fmt.Errorf("bad timestamp_nanosec %q: %w", b.Header.TimestampNS, err)
		}
		ns = v
	}
	return Block{Height: b.Header.Height, Hash: b.Header.Hash, Time: time.Unix(0, int64(ns)).UTC()}, nil
}

func (c *Client) block(ctx context.Context, only string, params any) (Block, string, error) {
	raw, name, err := c.call(ctx, only, "block", params)
	if err != nil {
		return Block{}, name, err
	}
	var br blockResult
	if err := json.Unmarshal(raw, &br); err != nil {
		return Block{}, name, &Error{Provider: name, Method: "block", Kind: ErrProviderUnavailable, Cause: err, Message: "undecodable block"}
	}
	b, err := br.block()
	if err != nil {
		return Block{}, name, &Error{Provider: name, Method: "block", Kind: ErrProviderUnavailable, Cause: err}
	}
	return b, name, nil
}

// LatestFinalBlock returns the latest final block and the name of the
// provider that served it (use that name to pin follow-up reads).
func (c *Client) LatestFinalBlock(ctx context.Context) (Block, string, error) {
	return c.block(ctx, "", map[string]any{"finality": "final"})
}

// LatestFinalBlockOn is LatestFinalBlock restricted to one provider.
func (c *Client) LatestFinalBlockOn(ctx context.Context, provider string) (Block, error) {
	b, _, err := c.block(ctx, provider, map[string]any{"finality": "final"})
	return b, err
}

// BlockAt returns the block at the given height and the serving provider.
// Old heights need an archival provider; the client falls back through the
// provider list and reports ErrNeedsArchival if none could serve it.
func (c *Client) BlockAt(ctx context.Context, height uint64) (Block, string, error) {
	return c.block(ctx, "", map[string]any{"block_id": height})
}

// CallFunction runs a view method at the given block height and returns the
// raw JSON the contract returned. args is JSON-marshalled (nil sends {}).
// With provider == "" the fallback order is used; otherwise the call is
// pinned to that provider and never falls back.
func (c *Client) CallFunction(ctx context.Context, provider, account, method string, args any, blockHeight uint64) ([]byte, error) {
	if args == nil {
		args = struct{}{}
	}
	argBytes, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("rpc: marshal args: %w", err)
	}
	params := map[string]any{
		"request_type": "call_function",
		"block_id":     blockHeight,
		"account_id":   account,
		"method_name":  method,
		"args_base64":  base64.StdEncoding.EncodeToString(argBytes),
	}
	raw, name, err := c.call(ctx, provider, "query", params)
	if err != nil {
		return nil, err
	}
	out, err := decodeCallResult(raw)
	if err != nil {
		c.markFailByName(name, err)
		return nil, &Error{Provider: name, Method: "query", Kind: ErrProviderUnavailable, Cause: err, Message: "undecodable call_function result"}
	}
	return out, nil
}

func (c *Client) markFailByName(name string, err error) {
	for _, p := range c.providers {
		if p.Name == name {
			p.markFail(err)
		}
	}
}

// decodeCallResult converts the "result" member of a query response (a JSON
// array of byte values) into raw bytes.
func decodeCallResult(raw json.RawMessage) ([]byte, error) {
	var r struct {
		Result []int `json:"result"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	out := make([]byte, len(r.Result))
	for i, v := range r.Result {
		if v < 0 || v > 255 {
			return nil, fmt.Errorf("result byte %d out of range: %d", i, v)
		}
		out[i] = byte(v)
	}
	return out, nil
}

// PinnedClient is a Client restricted to one provider.
type PinnedClient struct {
	c    *Client
	name string
}

// Pinned returns a view of the client that only ever talks to provider.
func (c *Client) Pinned(provider string) *PinnedClient { return &PinnedClient{c: c, name: provider} }

// Provider is the pinned provider's name.
func (p *PinnedClient) Provider() string { return p.name }

// CallFunction is Client.CallFunction on the pinned provider.
func (p *PinnedClient) CallFunction(ctx context.Context, account, method string, args any, blockHeight uint64) ([]byte, error) {
	return p.c.CallFunction(ctx, p.name, account, method, args, blockHeight)
}

// LatestFinalBlock is Client.LatestFinalBlockOn on the pinned provider.
func (p *PinnedClient) LatestFinalBlock(ctx context.Context) (Block, error) {
	return p.c.LatestFinalBlockOn(ctx, p.name)
}
