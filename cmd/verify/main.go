// Command verify is the offline end-to-end check: no network, no keys.
//
// It replays recorded JSON-RPC exchanges through the real snapshot reader
// and the real alert engine, prints a status table and the transitions it
// saw, and exits non-zero if the result differs from the fixture's
// expected.json.
//
//	go run ./cmd/verify                 # check every fixture set under data/fixtures
//	go run ./cmd/verify -capture        # record ONE live round into data/fixtures/live-<block>
//
// Fixture sets are searched in this order: data/fixtures/live-*/ (real
// captures), then data/fixtures/synthetic/ (invented data, see
// internal/fixturegen; regenerate with `go run ./cmd/verify/gen`).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/emma31-dev/argus/internal/fixturegen"
	"github.com/emma31-dev/argus/internal/tokens"
)

func main() {
	var (
		fixtures = flag.String("fixtures", "data/fixtures", "directory holding the fixture sets")
		capture  = flag.Bool("capture", false, "perform one LIVE round against ARGUS_RPC_URLS and save it as a fixture set")
		out      = flag.String("out", "", "with -capture: output directory; {block} is replaced by the block height (default <fixtures>/live-{block})")
		rpcURLs  = flag.String("rpc", "", "with -capture: comma-separated RPC URLs (default ARGUS_RPC_URLS); only the first is used so the recording has one provider")
		k        = flag.Int("k", 4, "with -capture: debounce K recorded in the fixture")
	)
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *capture {
		dir := *out
		if dir == "" {
			dir = filepath.Join(*fixtures, "live-{block}")
		}
		if err := runCapture(ctx, captureOptions{Out: dir, RPC: *rpcURLs, K: *k}); err != nil {
			fmt.Fprintln(os.Stderr, "capture failed:", err)
			os.Exit(1)
		}
		return
	}
	if err := runVerify(ctx, *fixtures); err != nil {
		fmt.Fprintln(os.Stderr, "\nVERIFY FAILED:", err)
		os.Exit(1)
	}
}

// findSets returns live-* directories (newest name last) then synthetic.
func findSets(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("fixtures directory: %w (run from the repository root)", err)
	}
	var live []string
	var synth string
	for _, e := range entries {
		switch {
		case e.IsDir() && strings.HasPrefix(e.Name(), "live-"):
			live = append(live, filepath.Join(root, e.Name()))
		case e.IsDir() && e.Name() == "synthetic":
			synth = filepath.Join(root, e.Name())
		}
	}
	sort.Strings(live)
	if synth != "" {
		live = append(live, synth)
	}
	if len(live) == 0 {
		return nil, fmt.Errorf("no fixture sets under %s (expected live-*/ or synthetic/)", root)
	}
	return live, nil
}

func runVerify(ctx context.Context, root string) error {
	sets, err := findSets(root)
	if err != nil {
		return err
	}
	var failed []string
	for i, dir := range sets {
		if i > 0 {
			fmt.Println("\n" + strings.Repeat("=", 78) + "\n")
		}
		o, err := replayFixture(ctx, dir)
		if err != nil {
			fmt.Printf("Fixture set: %s\n  could not replay: %v\n", dir, err)
			failed = append(failed, dir)
			continue
		}
		symbols := symbolsOf(dir)
		printOutcome(os.Stdout, o, symbols)

		raw, err := os.ReadFile(filepath.Join(dir, "expected.json"))
		if err != nil {
			fmt.Printf("\nExpectations: FAIL (%v)\n", err)
			failed = append(failed, dir)
			continue
		}
		var want fixturegen.Expected
		if err := json.Unmarshal(raw, &want); err != nil {
			fmt.Printf("\nExpectations: FAIL (expected.json: %v)\n", err)
			failed = append(failed, dir)
			continue
		}
		fixturegen.SortExpected(&want)
		if diffs := compare(want, o.Computed); len(diffs) > 0 {
			fmt.Printf("\nExpectations: FAIL, %d difference(s) from %s\n", len(diffs), filepath.Join(dir, "expected.json"))
			for _, d := range diffs {
				fmt.Println("  - " + d)
			}
			failed = append(failed, dir)
			continue
		}
		fmt.Printf("\nExpectations: PASS (%d final statuses, %d transitions, %d alerts match %s)\n",
			len(want.Final), len(want.Transitions), len(want.Alerts), filepath.Join(dir, "expected.json"))
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d fixture set(s) did not match: %s", len(failed), strings.Join(failed, ", "))
	}
	fmt.Printf("\nverify OK: %d fixture set(s) replayed offline (no network used)\n", len(sets))
	return nil
}

// symbolsOf maps token id to symbol from the set's tokens.yaml.
func symbolsOf(dir string) map[string]string {
	m := map[string]string{}
	raw, err := os.ReadFile(filepath.Join(dir, "tokens.yaml"))
	if err != nil {
		return m
	}
	toks, _ := tokens.ParseConfig(raw)
	for _, t := range toks {
		m[t.TokenID] = t.Symbol
	}
	return m
}
