package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/emma31-dev/argus/internal/rpc"
)

const bogusID = "nep141:argus-does-not-exist.near"

func runOwed(_ []string) {
	s, err := newSession("owed", providers(urlsFrom(envRPC, defaultRPC()), false))
	if err != nil {
		die("owed", "setup: %v", err)
	}
	ctx, cancel := withTimeout(2 * time.Minute)
	defer cancel()

	blk, provider, err := s.client.LatestFinalBlock(ctx)
	if err != nil {
		s.fail("could not get latest final block")
		s.netHint(err)
		s.finish()
		return
	}
	s.note("block %d (%s) via %s", blk.Height, blk.Hash, provider)

	ids := sampleTokens()
	for _, id := range ids {
		raw, err := s.client.CallFunction(ctx, provider, "intents.near", "mt_supply", map[string]string{"token_id": id}, blk.Height)
		reportCall(s, "mt_supply "+id, raw, err)
	}

	raw, err := s.client.CallFunction(ctx, provider, "intents.near", "mt_batch_supply",
		map[string]any{"token_ids": append(append([]string{}, ids...), bogusID)}, blk.Height)
	reportCall(s, "mt_batch_supply (3 tokens + 1 unknown id)", raw, err)
	if err == nil {
		var vals []json.RawMessage
		if json.Unmarshal(raw, &vals) == nil && len(vals) == len(ids)+1 {
			s.ok("mt_batch_supply returned one entry per id; unknown id -> %s", string(vals[len(vals)-1]))
		} else {
			s.fail("mt_batch_supply response is not an array of %d entries: %s", len(ids)+1, raw)
		}
	}
	if err != nil && errors.Is(err, rpc.ErrMethodNotFound) {
		fmt.Println("\nmt_batch_supply is missing, so owed would have to come from mint/burn event deltas instead.")
	}
	s.finish()
}

func reportCall(s *session, what string, raw []byte, err error) {
	switch {
	case err == nil:
		s.ok("%s -> %s", what, clip(string(raw), 200))
	case errors.Is(err, rpc.ErrMethodNotFound):
		s.fail("%s: method not found on intents.near", what)
	default:
		s.fail("%s: %v", what, err)
		s.netHint(err)
	}
}
