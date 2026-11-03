package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/emma31-dev/argus/internal/rpc"
)

// HeightFinder converts timestamps to block heights.
type HeightFinder interface {
	LatestFinalBlock(ctx context.Context) (rpc.Block, string, error)
	BlockAt(ctx context.Context, height uint64) (rpc.Block, string, error)
}

// HeightResult is the answer for one timestamp.
type HeightResult struct {
	Target time.Time
	// Block is the last block with time <= Target.
	Block rpc.Block
	Calls int
}

// maxSkip bounds the search for the next existing block after a skipped height.
const maxSkip = 12

// FindHeight bisects `block` calls to the last block at or before target.
// NEAR can skip heights (no block at that height); those are stepped over.
// Every call goes through the caching transport, so a re-run is free.
func FindHeight(ctx context.Context, c HeightFinder, target time.Time) (HeightResult, error) {
	res := HeightResult{Target: target}
	head, _, err := c.LatestFinalBlock(ctx)
	if err != nil {
		return res, fmt.Errorf("latest block: %w", err)
	}
	res.Calls++
	if target.After(head.Time) {
		return res, fmt.Errorf("%s is after the head block (%s, height %d); if you are offline the cached head may be older, re-run online",
			target.Format(time.RFC3339), head.Time.Format(time.RFC3339), head.Height)
	}
	// at returns the first existing block at or above h (stepping over
	// skipped heights), never above limit.
	at := func(h, limit uint64) (rpc.Block, error) {
		for i := 0; i < maxSkip && h+uint64(i) <= limit; i++ {
			b, _, err := c.BlockAt(ctx, h+uint64(i))
			res.Calls++
			if err == nil {
				return b, nil
			}
			if !errors.Is(err, rpc.ErrUnknownBlock) {
				return rpc.Block{}, err
			}
		}
		return rpc.Block{}, fmt.Errorf("no block found at heights %d..%d", h, h+maxSkip)
	}
	// Lower bound: NEAR produces at most about 2 blocks per second, so going
	// back 2 x seconds (+ margin) from the head must reach before the target.
	hi := head.Height
	span := uint64(head.Time.Sub(target).Seconds()*2) + 2000
	for {
		lo := uint64(1)
		if hi > span {
			lo = hi - span
		}
		b, err := at(lo, hi)
		if err != nil {
			return res, err
		}
		if !b.Time.After(target) || lo == 1 {
			res.Block = b
			return bisect(ctx, at, &res, b.Height, hi, head, target)
		}
		span *= 2
	}
}

func bisect(ctx context.Context, at func(h, limit uint64) (rpc.Block, error), res *HeightResult, lo, hi uint64, head rpc.Block, target time.Time) (HeightResult, error) {
	best := res.Block // time <= target
	for lo < hi {
		if err := ctx.Err(); err != nil {
			return *res, err
		}
		mid := lo + (hi-lo+1)/2
		b, err := at(mid, hi)
		if err != nil {
			// everything from mid up to hi was skipped; the answer is below mid
			hi = mid - 1
			continue
		}
		if b.Time.After(target) {
			hi = min(mid, b.Height) - 1
		} else {
			best = b
			lo = b.Height
		}
	}
	res.Block = best
	return *res, nil
}
