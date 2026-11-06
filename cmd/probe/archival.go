package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/tokens"
)

// runArchival checks that archival RPC can serve view calls at an old block,
// which the incident replay depends on. The height is never guessed: pass
// -block, or -date to bisect to the first block of that UTC day (about 25
// archival requests, so mind the rate limits).
func runArchival(args []string) {
	fs := flag.NewFlagSet("archival", flag.ExitOnError)
	height := fs.Uint64("block", 0, "block height to read (required unless -date is given)")
	date := fs.String("date", "", "resolve the first block of this UTC date (YYYY-MM-DD) by bisection instead of -block")
	_ = fs.Parse(args)
	if *height == 0 && *date == "" {
		fmt.Fprintln(os.Stderr, "FAIL  give -block <height> (from the postmortem or an explorer) or -date YYYY-MM-DD")
		fmt.Fprintln(os.Stderr, "      for the replay, use a block from 2026-10-01")
		os.Exit(2)
	}

	s, err := newSession("archival", providers(urlsFrom(envArchival, defaultArchival()), true))
	if err != nil {
		die("archival", "setup: %v", err)
	}
	ctx, cancel := withTimeout(10 * time.Minute)
	defer cancel()

	h := *height
	if h == 0 {
		day, err := time.Parse("2006-01-02", *date)
		if err != nil {
			die("archival", "bad -date: %v", err)
		}
		h, err = resolve(ctx, s.client, day)
		if err != nil {
			s.fail("could not resolve a block for %s: %v", *date, err)
			s.netHint(err)
			s.finish()
			return
		}
		s.note("resolved %s to block %d", *date, h)
	}

	blk, provider, err := s.client.BlockAt(ctx, h)
	if err != nil {
		s.fail("block %d: %v", h, err)
		s.netHint(err)
		s.finish()
		return
	}
	s.ok("block %d served by %s: hash %s, time %s", blk.Height, provider, blk.Hash, blk.Time.Format(time.RFC3339))

	ids := sampleTokens()
	read := func(account, method string, args any) {
		raw, err := s.client.CallFunction(ctx, provider, account, method, args, h)
		if err != nil {
			s.fail("%s.%s at %d: %v", account, method, h, err)
			if errors.Is(err, rpc.ErrNeedsArchival) {
				s.note("the provider says the block is unavailable: the replay needs a different archival source")
			}
			s.netHint(err)
			return
		}
		s.ok("%s.%s at %d -> %s", account, method, h, clip(strings.TrimSpace(string(raw)), 160))
	}
	read("intents.near", "mt_batch_supply", map[string]any{"token_ids": ids})
	for _, id := range ids[:2] {
		t, _ := tokens.Parse(id)
		c, m, a := tokens.HeldMethod(t)
		read(c, m, a)
	}
	s.finish()
}

// resolve finds the first block at or after the start of day (UTC) by
// bisecting block timestamps. Skipped heights are handled by probing a few
// heights upward.
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
