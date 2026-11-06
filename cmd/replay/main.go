// Command replay re-runs the Argus snapshot calculation at historical block
// heights on archival RPC and writes the result files.
//
//	replay -plan data/replay/plan.yaml                # produce replay JSON + Markdown
//	replay -offline                                   # same, from the cache only
//	replay -find-heights 2026-10-01T05:54:00Z,...     # timestamps -> block heights
//
// Every request and response is cached in data/replay/cache/ (keyed by
// the SHA-256 of the request body), so the committed cache lets anyone redo
// the replay offline. Network requests are rate limited (default 1 per
// second) and retried with backoff.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/emma31-dev/argus/internal/rpc"
)

// version is set at build time.
var version = "dev"

// DefaultArchival are the free archival endpoints, in order (docs/research.md).
var DefaultArchival = []string{"https://archival-rpc.mainnet.near.org", "https://archival-rpc.mainnet.fastnear.com"}

type options struct {
	Plan, OutJSON, OutMD, CacheDir string
	Offline                        bool
	K                              int
	RPS                            float64
	RPC                            string
	FindHeights                    string
}

func main() {
	var o options
	flag.StringVar(&o.Plan, "plan", "data/replay/plan.yaml", "replay plan (YAML)")
	flag.StringVar(&o.OutJSON, "out-json", "data/replay/2026-10-01.json", "output JSON served by /v1/replay")
	flag.StringVar(&o.OutMD, "out-md", "data/replay/2026-10-01.md", "output Markdown report")
	flag.StringVar(&o.CacheDir, "cache", "data/replay/cache", "request/response cache directory")
	flag.BoolVar(&o.Offline, "offline", false, "use the cache only; fail if a request is missing")
	flag.IntVar(&o.K, "k", 4, "debounce K (consecutive negative snapshots to turn red)")
	flag.Float64Var(&o.RPS, "rps", 1, "maximum network requests per second")
	flag.StringVar(&o.RPC, "rpc", "", "comma-separated archival RPC URLs (overrides the plan and ARGUS_ARCHIVAL_RPC_URLS)")
	flag.StringVar(&o.FindHeights, "find-heights", "", "comma-separated UTC timestamps (RFC 3339) to convert to block heights; no replay is run")
	showVersion := flag.Bool("version", false, "print the version")
	flag.Parse()
	if *showVersion {
		fmt.Println("replay", version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	if o.FindHeights != "" {
		err = runFindHeights(ctx, o, os.Stdout)
	} else {
		err = runReplay(ctx, o, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func urlsFrom(o options, plan *Plan) []string {
	var raw []string
	switch {
	case o.RPC != "":
		raw = strings.Split(o.RPC, ",")
	case plan != nil && len(plan.ArchivalProviders) > 0:
		raw = plan.ArchivalProviders
	case os.Getenv("ARGUS_ARCHIVAL_RPC_URLS") != "":
		raw = strings.Split(os.Getenv("ARGUS_ARCHIVAL_RPC_URLS"), ",")
	default:
		raw = DefaultArchival
	}
	var out []string
	for _, u := range raw {
		if u = strings.TrimSpace(u); u != "" {
			out = append(out, u)
		}
	}
	return out
}

func newClient(o options, urls []string) (*rpc.Client, *CachingTransport, error) {
	if len(urls) == 0 {
		return nil, nil, errNoProvider
	}
	ps := make([]rpc.Provider, len(urls))
	for i, u := range urls {
		pu, err := url.Parse(u)
		if err != nil || pu.Host == "" {
			return nil, nil, fmt.Errorf("bad archival URL #%d", i+1)
		}
		// The provider name is the host only: URLs may carry API keys.
		ps[i] = rpc.Provider{Name: pu.Host, URL: u, Archival: true}
	}
	tr := &CachingTransport{Dir: o.CacheDir, Next: http.DefaultTransport, Offline: o.Offline}
	if o.RPS > 0 {
		tr.MinInterval = time.Duration(float64(time.Second) / o.RPS)
	}
	opts := []rpc.Option{
		rpc.WithHTTPClient(&http.Client{Transport: tr, Timeout: 60 * time.Second}),
		rpc.WithBackoff(time.Second, 30*time.Second),
	}
	if o.Offline {
		opts = append(opts, rpc.WithSleep(func(ctx context.Context, _ time.Duration) error { return ctx.Err() }))
	}
	return rpc.New(ps, opts...), tr, nil
}

func runReplay(ctx context.Context, o options, w *os.File) error {
	plan, err := LoadPlan(o.Plan)
	if err != nil {
		return fmt.Errorf("%s: %w", o.Plan, err)
	}
	client, tr, err := newClient(o, urlsFrom(o, plan))
	if err != nil {
		return err
	}
	f, err := Run(ctx, client, plan, o.K, time.Now(), func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) })
	if err != nil {
		return err
	}
	if err := writeJSON(o.OutJSON, f); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(o.OutMD), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(o.OutMD, []byte(Markdown(f, filepath.Base(o.OutJSON))), 0o644); err != nil {
		return err
	}
	hits, net := tr.Stats()
	fmt.Fprintf(w, "wrote %s and %s (%d cache hits, %d network requests)\n", o.OutJSON, o.OutMD, hits, net)
	for _, t := range f.Tokens {
		fmt.Fprintf(w, "  %s: first negative block %d, detected red at %d\n", t.ID, t.FirstNegativeBlock, t.DetectedRedBlock)
	}
	return nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func runFindHeights(ctx context.Context, o options, w *os.File) error {
	client, tr, err := newClient(o, urlsFrom(o, nil))
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "timestamp (UTC)         last block at or before   block time")
	for _, s := range strings.Split(o.FindHeights, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return fmt.Errorf("bad timestamp %q: want RFC 3339 such as 2026-10-01T05:54:00Z", s)
		}
		r, err := FindHeight(ctx, client, ts)
		if err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
		fmt.Fprintf(w, "%-22s  %-24d  %s  (%d block calls)\n", ts.UTC().Format("2006-01-02T15:04:05Z"), r.Block.Height, r.Block.Time.UTC().Format(time.RFC3339), r.Calls)
	}
	hits, net := tr.Stats()
	fmt.Fprintf(w, "(%d cache hits, %d network requests; heights go into points[].block_height of the plan)\n", hits, net)
	return nil
}
