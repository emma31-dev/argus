// Package checkutil holds the small helpers shared by the capability-check
// programs in checks/: RPC setup from the environment, PASS/FAIL printing and
// evidence files.
package checkutil

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

// Environment variables.
const (
	EnvRPC      = "ARGUS_RPC_URLS"
	EnvArchival = "ARGUS_ARCHIVAL_RPC_URLS"
)

// DefaultRPC and DefaultArchival are used when the environment is unset.
func DefaultRPC() []string {
	return []string{"https://rpc.mainnet.fastnear.com", "https://rpc.mainnet.near.org"}
}

// DefaultArchival lists the free archival endpoints, in order.
func DefaultArchival() []string {
	return []string{"https://archival-rpc.mainnet.fastnear.com", "https://archival-rpc.mainnet.near.org"}
}

// URLs reads a comma-separated URL list from env, else returns def.
func URLs(env string, def []string) []string {
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

// Providers turns URLs into rpc.Providers with conservative free-tier rate
// limits (2 requests/s, burst 2).
func Providers(urls []string, archival bool) []rpc.Provider {
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

// Env bundles what every check needs.
type Env struct {
	Check    string // e.g. "01-owed"
	Dir      string // evidence directory
	Client   *rpc.Client
	failures int
	mu       sync.Mutex
	seq      int
}

// New creates the evidence directory and an RPC client. Raw JSON-RPC
// exchanges are written to the evidence directory automatically.
func New(check string, providers []rpc.Provider) (*Env, error) {
	dir := filepath.Join("evidence", "checks", check)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	e := &Env{Check: check, Dir: dir}
	rec := func(provider, method string, req, resp []byte) {
		e.mu.Lock()
		e.seq++
		n := e.seq
		e.mu.Unlock()
		_ = e.writeJSON(fmt.Sprintf("rpc-%03d-%s.json", n, method), map[string]any{
			"provider": provider, "request": json.RawMessage(req), "response": rawOrString(resp),
		})
	}
	e.Client = rpc.New(providers, rpc.WithRecorder(rec), rpc.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}))
	return e, nil
}

// rawOrString embeds resp as JSON when valid, else as a string.
func rawOrString(resp []byte) any {
	if json.Valid(resp) {
		return json.RawMessage(resp)
	}
	return string(resp)
}

func (e *Env) writeJSON(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(e.Dir, name), append(b, '\n'), 0o644)
}

// Save writes v as indented JSON to <evidence dir>/<name>.
func (e *Env) Save(name string, v any) {
	if err := e.writeJSON(name, v); err != nil {
		fmt.Printf("WARN  could not write evidence %s: %v\n", name, err)
	}
}

// Pass prints a PASS line.
func (e *Env) Pass(format string, args ...any) { fmt.Printf("PASS  "+format+"\n", args...) }

// Info prints an informational line.
func (e *Env) Info(format string, args ...any) { fmt.Printf("INFO  "+format+"\n", args...) }

// Fail prints a FAIL line and records the failure.
func (e *Env) Fail(format string, args ...any) {
	e.failures++
	fmt.Printf("FAIL  "+format+"\n", args...)
}

// NetworkHint prints a clear message when err looks like a connectivity
// problem, so a blocked network is not mistaken for a contract problem.
func (e *Env) NetworkHint(err error) {
	if err == nil {
		return
	}
	fmt.Printf("      error: %v\n", err)
	s := err.Error()
	if strings.Contains(s, "provider unavailable") || strings.Contains(s, "dial") ||
		strings.Contains(s, "no such host") || strings.Contains(s, "HTTP_403") || strings.Contains(s, "timeout") {
		fmt.Println("      hint: this looks like a network or provider problem, not a contract result.")
		fmt.Println("            Check connectivity/proxy and ARGUS_RPC_URLS, then re-run.")
	}
}

// Exit prints the verdict and exits non-zero if anything failed.
func (e *Env) Exit() {
	if e.failures > 0 {
		fmt.Printf("\nRESULT %s: FAIL (%d failed). Evidence in %s\n", e.Check, e.failures, e.Dir)
		os.Exit(1)
	}
	fmt.Printf("\nRESULT %s: PASS. Evidence in %s\n", e.Check, e.Dir)
}

// Context returns a context with a generous overall deadline.
func Context(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// Fatalf prints a FAIL line for a setup problem and exits 1.
func Fatalf(check, format string, args ...any) {
	fmt.Printf("FAIL  "+format+"\n", args...)
	fmt.Printf("\nRESULT %s: FAIL\n", check)
	os.Exit(1)
}

// WellKnown are token ids the checks probe. Replace via flags where offered.
func WellKnown() []string {
	return []string{
		"nep141:wrap.near",
		"nep141:17208628f84f5d6ad33f0da3bbbeb27ffcb398eac501a31bd6ad2011e36133a1", // USDC
		"nep141:eth.omft.near", // bridged ETH
	}
}
