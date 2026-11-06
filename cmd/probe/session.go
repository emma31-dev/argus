package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/emma31-dev/argus/internal/rpc"
)

const (
	envRPC      = "ARGUS_RPC_URLS"
	envArchival = "ARGUS_ARCHIVAL_RPC_URLS"
)

func defaultRPC() []string {
	return []string{"https://free.rpc.fastnear.com", "https://rpc.mainnet.fastnear.com", "https://rpc.mainnet.near.org"}
}

func defaultArchival() []string {
	return []string{"https://archival-rpc.mainnet.near.org", "https://archival-rpc.mainnet.fastnear.com"}
}

// urlsFrom reads a comma-separated URL list from env, else returns def.
func urlsFrom(env string, def []string) []string {
	var out []string
	for _, u := range strings.Split(os.Getenv(env), ",") {
		if u = strings.TrimSpace(u); u != "" {
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

// providers keeps to 2 req/s per endpoint, which every free tier tolerates.
func providers(urls []string, archival bool) []rpc.Provider {
	ps := make([]rpc.Provider, len(urls))
	for i, u := range urls {
		ps[i] = rpc.Provider{Name: fmt.Sprintf("p%d:%s", i, hostOf(u)), URL: u, RPS: 2, Burst: 2, Archival: archival}
	}
	return ps
}

func hostOf(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if i := strings.IndexByte(u, '/'); i >= 0 {
		u = u[:i]
	}
	return u
}

type session struct {
	name     string
	dir      string
	client   *rpc.Client
	failures int
	mu       sync.Mutex
	seq      int
}

// newSession records every raw JSON-RPC exchange under data/probe/<name>/.
func newSession(name string, ps []rpc.Provider) (*session, error) {
	dir := filepath.Join("data", "probe", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &session{name: name, dir: dir}
	rec := func(provider, method string, req, resp []byte) {
		s.mu.Lock()
		s.seq++
		n := s.seq
		s.mu.Unlock()
		_ = s.writeJSON(fmt.Sprintf("rpc-%03d-%s.json", n, method), map[string]any{
			"provider": provider, "request": json.RawMessage(req), "response": rawOrString(resp),
		})
	}
	s.client = rpc.New(ps, rpc.WithRecorder(rec), rpc.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}))
	return s, nil
}

func rawOrString(resp []byte) any {
	if json.Valid(resp) {
		return json.RawMessage(resp)
	}
	return string(resp)
}

func (s *session) writeJSON(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, name), append(b, '\n'), 0o644)
}

func (s *session) save(name string, v any) {
	if err := s.writeJSON(name, v); err != nil {
		fmt.Printf("warn  could not write %s: %v\n", name, err)
	}
}

func (s *session) ok(format string, args ...any)   { fmt.Printf("ok    "+format+"\n", args...) }
func (s *session) note(format string, args ...any) { fmt.Printf("      "+format+"\n", args...) }

func (s *session) fail(format string, args ...any) {
	s.failures++
	fmt.Printf("FAIL  "+format+"\n", args...)
}

// netHint stops a blocked network from being read as a contract result.
func (s *session) netHint(err error) {
	if err == nil {
		return
	}
	fmt.Printf("      error: %v\n", err)
	msg := err.Error()
	if strings.Contains(msg, "provider unavailable") || strings.Contains(msg, "dial") ||
		strings.Contains(msg, "no such host") || strings.Contains(msg, "HTTP_403") || strings.Contains(msg, "timeout") {
		fmt.Println("      looks like a network or provider problem, not a contract answer.")
		fmt.Println("      check connectivity and ARGUS_RPC_URLS, then run it again.")
	}
}

func (s *session) finish() {
	if s.failures > 0 {
		fmt.Printf("\n%s: %d failed (raw responses in %s)\n", s.name, s.failures, s.dir)
		os.Exit(1)
	}
	fmt.Printf("\n%s: all good (raw responses in %s)\n", s.name, s.dir)
}

func withTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func die(name, format string, args ...any) {
	fmt.Printf("FAIL  "+format+"\n", args...)
	fmt.Printf("\n%s: failed\n", name)
	os.Exit(1)
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// sampleTokens: wNEAR, USDC and bridged ETH.
func sampleTokens() []string {
	return []string{
		"nep141:wrap.near",
		"nep141:17208628f84f5d6ad33f0da3bbbeb27ffcb398eac501a31bd6ad2011e36133a1",
		"nep141:eth.omft.near",
	}
}
