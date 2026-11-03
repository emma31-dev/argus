package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is FastNEAR's mainnet neardata server.
	DefaultBaseURL = "https://mainnet.neardata.xyz"
	// DefaultRPS is Argus's request rate. neardata documents 180 req/min/IP
	// (3 rps); 2 rps leaves headroom for other users of the same IP.
	DefaultRPS = 2.0

	maxBlockBytes = 64 << 20
)

// Client reads blocks from a neardata server. The zero value works with
// defaults; set fields before the first call.
type Client struct {
	BaseURL    string        // default DefaultBaseURL
	HTTPClient *http.Client  // default: 30s timeout, follows redirects
	RPS        float64       // 0 = DefaultRPS, <0 = unlimited
	APIKey     string        // optional, sent as ?apiKey= (survives redirects)
	MaxRetries int           // retries on 429/5xx/network error; 0 = 5, <0 = none
	Backoff    time.Duration // first retry delay, doubled each time; 0 = 500ms
	MaxBackoff time.Duration // 0 = 30s

	once    sync.Once
	limiter *Limiter
	sleep   func(ctx context.Context, d time.Duration) error // tests
}

// StatusError is a non-retryable (or retries-exhausted) HTTP failure.
type StatusError struct {
	Status int
	URL    string
}

func (e *StatusError) Error() string { return fmt.Sprintf("events: %s: HTTP %d", e.URL, e.Status) }

func (c *Client) init() {
	c.once.Do(func() {
		if c.limiter == nil {
			rps := c.RPS
			if rps == 0 {
				rps = DefaultRPS
			}
			c.limiter = NewLimiter(rps)
		}
		if c.sleep == nil {
			c.sleep = sleepCtx
		}
	})
}

func (c *Client) base() string {
	if c.BaseURL == "" {
		return DefaultBaseURL
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) url(path string) string {
	u := c.base() + path
	if c.APIKey != "" {
		u += "?apiKey=" + url.QueryEscape(c.APIKey)
	}
	return u
}

// do performs a GET with rate limiting and backoff on 429/5xx/network
// errors. noFollow makes 3xx responses come back instead of being followed.
// On success the caller owns resp.Body.
func (c *Client) do(ctx context.Context, u string, noFollow bool) (*http.Response, error) {
	c.init()
	hc := c.httpClient()
	if noFollow {
		cp := *hc
		cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		hc = &cp
	}
	retries := c.MaxRetries
	if retries == 0 {
		retries = 5
	}
	delay := c.Backoff
	if delay <= 0 {
		delay = 500 * time.Millisecond
	}
	maxDelay := c.MaxBackoff
	if maxDelay <= 0 {
		maxDelay = 30 * time.Second
	}
	var lastErr error
	for attempt := 0; ; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := hc.Do(req)
		wait := delay
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = &StatusError{Status: resp.StatusCode, URL: redact(u)}
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if s, perr := strconv.Atoi(ra); perr == nil && s >= 0 {
					wait = time.Duration(s) * time.Second
				}
			}
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
		case resp.StatusCode >= 400:
			resp.Body.Close()
			return nil, &StatusError{Status: resp.StatusCode, URL: redact(u)}
		default:
			return resp, nil
		}
		if retries < 0 || attempt >= retries {
			return nil, fmt.Errorf("events: giving up after %d attempts: %w", attempt+1, lastErr)
		}
		if wait > maxDelay {
			wait = maxDelay
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
		if delay *= 2; delay > maxDelay {
			delay = maxDelay
		}
	}
}

func redact(u string) string {
	if i := strings.Index(u, "?"); i >= 0 {
		return u[:i]
	}
	return u
}

// Block fetches the finalized block at height. A skipped height (the server
// answers `null`) returns (nil, nil). Redirects (to FastNEAR's archive
// hosts) are followed.
func (c *Client) Block(ctx context.Context, height uint64) (*Block, error) {
	resp, err := c.do(ctx, c.url("/v0/block/"+strconv.FormatUint(height, 10)), false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBlockBytes))
	if err != nil {
		return nil, fmt.Errorf("events: read block %d: %w", height, err)
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 || string(body) == "null" {
		return nil, nil
	}
	var b Block
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, fmt.Errorf("events: decode block %d: %w", height, err)
	}
	if b.Block.Header.Height == 0 {
		b.Block.Header.Height = height
	}
	return &b, nil
}

// LastFinal returns the height of the latest final block. The endpoint
// redirects to /v0/block/<height>; the height is read from the Location
// header without downloading the block. If the server answers 200 directly,
// the height is read from the URL we ended on, or else from the body.
func (c *Client) LastFinal(ctx context.Context) (uint64, error) {
	resp, err := c.do(ctx, c.url("/v0/last_block/final"), true)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc, err := resp.Location()
		if err != nil {
			return 0, fmt.Errorf("events: last_block redirect without Location: %w", err)
		}
		h, err := heightFromPath(loc.Path)
		if err != nil {
			return 0, err
		}
		return h, nil
	}
	if h, err := heightFromPath(resp.Request.URL.Path); err == nil {
		return h, nil
	}
	var b Block
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBlockBytes)).Decode(&b); err != nil {
		return 0, fmt.Errorf("events: decode last_block: %w", err)
	}
	if b.Block.Header.Height == 0 {
		return 0, errors.New("events: last_block has no height")
	}
	return b.Block.Header.Height, nil
}

func heightFromPath(p string) (uint64, error) {
	p = strings.TrimRight(p, "/")
	i := strings.LastIndex(p, "/")
	h, err := strconv.ParseUint(p[i+1:], 10, 64)
	if err != nil || h == 0 {
		return 0, fmt.Errorf("events: no block height in path %q", p)
	}
	return h, nil
}
