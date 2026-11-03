// Command 03-same-block is capability check 3 (PRD section 7): read owed
// and held at the same recent block_id from the same provider, and find how
// far back a non-archival provider accepts block_id.
//
// Run: go run ./checks/03-same-block
package main

import (
	"errors"
	"flag"
	"strings"
	"time"

	"github.com/emma31-dev/argus/checks/internal/checkutil"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/snapshot"
	"github.com/emma31-dev/argus/internal/tokens"
)

func main() {
	probe := flag.Bool("probe", true, "also probe how far back non-archival block_id reads work")
	flag.Parse()

	env, err := checkutil.New("03-same-block", checkutil.Providers(checkutil.URLs(checkutil.EnvRPC, checkutil.DefaultRPC()), false))
	if err != nil {
		checkutil.Fatalf("03-same-block", "setup: %v", err)
	}
	ctx, cancel := checkutil.Context(10 * time.Minute)
	defer cancel()

	blk, provider, err := env.Client.LatestFinalBlock(ctx)
	if err != nil {
		env.Fail("could not get latest final block")
		env.NetworkHint(err)
		env.Exit()
		return
	}
	env.Info("pinning provider %s at final block %d (%s)", provider, blk.Height, blk.Hash)

	tok, _ := tokens.Parse("nep141:wrap.near")
	contract, method, args := tokens.HeldMethod(tok)
	read := func(h uint64) (owed, held string, err error) {
		o, err := env.Client.CallFunction(ctx, provider, "intents.near", "mt_supply", map[string]string{"token_id": tok.TokenID}, h)
		if err != nil {
			return "", "", err
		}
		hd, err := env.Client.CallFunction(ctx, provider, contract, method, args, h)
		return strings.Trim(string(o), `"`), strings.Trim(string(hd), `"`), err
	}

	owed, held, err := read(blk.Height)
	if err != nil {
		env.Fail("same-block read at %d: %v", blk.Height, err)
		env.NetworkHint(err)
		env.Exit()
		return
	}
	gap, gerr := snapshot.Gap(held, owed)
	if gerr != nil {
		env.Fail("gap: %v", gerr)
	} else {
		env.Pass("block %d: owed=%s held=%s gap=%s (wNEAR, units %s)", blk.Height, owed, held, gap, snapshot.FormatUnits(gap, 24))
	}
	// Repeat to show that a pinned block_id is stable (same answer twice).
	owed2, held2, err := read(blk.Height)
	if err == nil && owed2 == owed && held2 == held {
		env.Pass("re-reading block %d returns identical values (block_id pins the state)", blk.Height)
	} else {
		env.Fail("re-read at %d differs or failed: owed=%s held=%s err=%v", blk.Height, owed2, held2, err)
	}

	if !*probe {
		env.Exit()
		return
	}
	// Non-archival nodes keep roughly 3 epochs (~2.5 days, ~250k blocks).
	offsets := []uint64{10, 100, 1000, 10_000, 50_000, 100_000, 200_000, 300_000, 500_000}
	var oldest uint64
	for _, off := range offsets {
		if off >= blk.Height {
			break
		}
		h := blk.Height - off
		_, err := env.Client.CallFunction(ctx, provider, "intents.near", "mt_supply", map[string]string{"token_id": tok.TokenID}, h)
		if err == nil {
			oldest = off
			env.Pass("block_id %d (-%d blocks) readable on non-archival provider", h, off)
			continue
		}
		if errors.Is(err, rpc.ErrUnknownBlock) || errors.Is(err, rpc.ErrNeedsArchival) {
			env.Info("block_id %d (-%d blocks) not available: needs archival", h, off)
		} else {
			env.Fail("probe at -%d: %v", off, err)
			env.NetworkHint(err)
		}
		break
	}
	env.Info("non-archival look-back: at least %d blocks (probe granularity is coarse)", oldest)
	env.Exit()
}
