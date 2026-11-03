// Command 06-archival is capability check 6 (PRD section 7): can free
// archival RPC serve a handful of view calls at a block from the 2026-10-01
// incident, so the replay (PRD 5.5) is possible?
//
// The block height of the incident is not hard-coded because it must come
// from the postmortem or an explorer. Give it with -block, or give -date
// (YYYY-MM-DD) to bisect to the first block of that UTC day (about 25
// archival requests; mind the rate limits).
//
// Run: go run ./checks/06-archival -block 123456789
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/emma31-dev/argus/checks/internal/checkutil"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/tokens"
)

func main() {
	height := flag.Uint64("block", 0, "block height to read (required unless -date is given)")
	date := flag.String("date", "", "resolve the first block of this UTC date (YYYY-MM-DD) by bisection instead of -block")
	flag.Parse()
	if *height == 0 && *date == "" {
		fmt.Fprintln(os.Stderr, "FAIL  give -block <height> (from the postmortem or an explorer) or -date YYYY-MM-DD")
		fmt.Fprintln(os.Stderr, "      for the replay, use a block from 2026-10-01; this check does not guess one")
		os.Exit(2)
	}

	env, err := checkutil.New("06-archival", checkutil.Providers(checkutil.URLs(checkutil.EnvArchival, checkutil.DefaultArchival()), true))
	if err != nil {
		checkutil.Fatalf("06-archival", "setup: %v", err)
	}
	ctx, cancel := checkutil.Context(10 * time.Minute)
	defer cancel()

	h := *height
	if h == 0 {
		day, err := time.Parse("2006-01-02", *date)
		if err != nil {
			checkutil.Fatalf("06-archival", "bad -date: %v", err)
		}
		h, err = resolve(ctx, env.Client, day)
		if err != nil {
			env.Fail("could not resolve a block for %s: %v", *date, err)
			env.NetworkHint(err)
			env.Exit()
			return
		}
		env.Info("resolved %s to block %d", *date, h)
	}

	blk, provider, err := env.Client.BlockAt(ctx, h)
	if err != nil {
		env.Fail("block %d: %v", h, err)
		env.NetworkHint(err)
		env.Exit()
		return
	}
	env.Pass("block %d served by %s: hash %s, time %s", blk.Height, provider, blk.Hash, blk.Time.Format(time.RFC3339))

	ids := checkutil.WellKnown()
	read := func(account, method string, args any) {
		raw, err := env.Client.CallFunction(ctx, provider, account, method, args, h)
		if err != nil {
			env.Fail("%s.%s at %d: %v", account, method, h, err)
			if errors.Is(err, rpc.ErrNeedsArchival) {
				env.Info("the provider says the block is unavailable: the replay needs a different archival source")
			}
			env.NetworkHint(err)
			return
		}
		env.Pass("%s.%s at %d -> %s", account, method, h, truncate(strings.TrimSpace(string(raw))))
	}
	read("intents.near", "mt_batch_supply", map[string]any{"token_ids": ids})
	for _, id := range ids[:2] {
		t, _ := tokens.Parse(id)
		c, m, a := tokens.HeldMethod(t)
		read(c, m, a)
	}
	env.Exit()
}

func truncate(s string) string {
	if len(s) > 160 {
		return s[:160] + "..."
	}
	return s
}

// resolve finds the first block at or after the start of day (UTC) by
// bisecting block timestamps on the archival provider. Skipped heights are
// handled by probing a few heights upward.
func resolve(ctx context.Context, c *rpc.Client, day time.Time) (uint64, error) {
	latest, _, err := c.LatestFinalBlock(ctx)
	if err != nil {
		return 0, err
	}
	lo, hi := uint64(9_820_210), latest.Height // mainnet genesis height .. now
	probe := func(h uint64) (rpc.Block, uint64, error) {
		var last error
		for d := uint64(0); d < 8; d++ {
			b, _, err := c.BlockAt(ctx, h+d)
			if err == nil {
				return b, h + d, nil
			}
			if !errors.Is(err, rpc.ErrUnknownBlock) {
				return rpc.Block{}, 0, err
			}
			last = err
		}
		return rpc.Block{}, 0, last
	}
	for lo < hi {
		mid := lo + (hi-lo)/2
		b, at, err := probe(mid)
		if err != nil {
			return 0, err
		}
		if b.Time.Before(day) {
			lo = at + 1
		} else {
			hi = mid
		}
	}
	return lo, nil
}
