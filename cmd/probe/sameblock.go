package main

import (
	"errors"
	"flag"
	"strings"
	"time"

	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/snapshot"
	"github.com/emma31-dev/argus/internal/tokens"
)

func runSameBlock(args []string) {
	fs := flag.NewFlagSet("sameblock", flag.ExitOnError)
	lookback := fs.Bool("lookback", true, "also find how far back non-archival block_id reads work")
	_ = fs.Parse(args)

	s, err := newSession("sameblock", providers(urlsFrom(envRPC, defaultRPC()), false))
	if err != nil {
		die("sameblock", "setup: %v", err)
	}
	ctx, cancel := withTimeout(10 * time.Minute)
	defer cancel()

	blk, provider, err := s.client.LatestFinalBlock(ctx)
	if err != nil {
		s.fail("could not get latest final block")
		s.netHint(err)
		s.finish()
		return
	}
	s.note("pinning provider %s at final block %d (%s)", provider, blk.Height, blk.Hash)

	tok, _ := tokens.Parse("nep141:wrap.near")
	contract, method, margs := tokens.HeldMethod(tok)
	read := func(h uint64) (owed, held string, err error) {
		o, err := s.client.CallFunction(ctx, provider, "intents.near", "mt_supply", map[string]string{"token_id": tok.TokenID}, h)
		if err != nil {
			return "", "", err
		}
		hd, err := s.client.CallFunction(ctx, provider, contract, method, margs, h)
		return strings.Trim(string(o), `"`), strings.Trim(string(hd), `"`), err
	}

	owed, held, err := read(blk.Height)
	if err != nil {
		s.fail("same-block read at %d: %v", blk.Height, err)
		s.netHint(err)
		s.finish()
		return
	}
	gap, gerr := snapshot.Gap(held, owed)
	if gerr != nil {
		s.fail("gap: %v", gerr)
	} else {
		s.ok("block %d: owed=%s held=%s gap=%s (wNEAR, units %s)", blk.Height, owed, held, gap, snapshot.FormatUnits(gap, 24))
	}
	owed2, held2, err := read(blk.Height)
	if err == nil && owed2 == owed && held2 == held {
		s.ok("re-reading block %d returns identical values (block_id pins the state)", blk.Height)
	} else {
		s.fail("re-read at %d differs or failed: owed=%s held=%s err=%v", blk.Height, owed2, held2, err)
	}

	if !*lookback {
		s.finish()
		return
	}
	// Non-archival nodes keep a few epochs, so a coarse ladder is enough.
	offsets := []uint64{10, 100, 1000, 10_000, 50_000, 100_000, 200_000, 300_000, 500_000}
	var oldest uint64
	for _, off := range offsets {
		if off >= blk.Height {
			break
		}
		h := blk.Height - off
		_, err := s.client.CallFunction(ctx, provider, "intents.near", "mt_supply", map[string]string{"token_id": tok.TokenID}, h)
		if err == nil {
			oldest = off
			s.ok("block_id %d (-%d blocks) readable on non-archival provider", h, off)
			continue
		}
		if errors.Is(err, rpc.ErrUnknownBlock) || errors.Is(err, rpc.ErrNeedsArchival) {
			s.note("block_id %d (-%d blocks) not available: needs archival", h, off)
		} else {
			s.fail("lookback at -%d: %v", off, err)
			s.netHint(err)
		}
		break
	}
	s.note("non-archival look-back: at least %d blocks", oldest)
	s.finish()
}
