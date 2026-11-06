package main

import (
	"context"
	"errors"
	"flag"
	"strings"
	"time"

	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/tokens"
)

// runHeld reads the held side on the token contracts themselves:
//
//	nep141: <contract>.ft_balance_of({"account_id":"intents.near"})
//	nep245: <contract>.mt_balance_of({"account_id":"intents.near","token_id":<inner>})
func runHeld(args []string) {
	fs := flag.NewFlagSet("held", flag.ExitOnError)
	nep245 := fs.String("nep245", "", "NEP-245 Intents token id to read (default: first one found by discovery)")
	_ = fs.Parse(args)

	s, err := newSession("held", providers(urlsFrom(envRPC, defaultRPC()), false))
	if err != nil {
		die("held", "setup: %v", err)
	}
	ctx, cancel := withTimeout(3 * time.Minute)
	defer cancel()

	blk, provider, err := s.client.LatestFinalBlock(ctx)
	if err != nil {
		s.fail("could not get latest final block")
		s.netHint(err)
		s.finish()
		return
	}
	s.note("block %d via %s", blk.Height, provider)

	ids := sampleTokens()
	id245 := *nep245
	if id245 == "" {
		id245 = findNEP245(ctx, s)
	}
	if id245 != "" {
		ids = append(ids, id245)
	} else {
		s.fail("no NEP-245 token available: pass -nep245 <id> (e.g. a nep245:<contract>:<inner> id from the 1Click list)")
	}

	for _, id := range ids {
		t, err := tokens.Parse(id)
		if err != nil {
			s.fail("%s: %v", id, err)
			continue
		}
		contract, method, margs := tokens.HeldMethod(t)
		raw, err := s.client.CallFunction(ctx, provider, contract, method, margs, blk.Height)
		switch {
		case err == nil:
			s.ok("%s.%s -> %s", contract, method, strings.TrimSpace(string(raw)))
		case errors.Is(err, rpc.ErrMethodNotFound), errors.Is(err, rpc.ErrAccountNotFound):
			s.fail("%s.%s: %v", contract, method, err)
		default:
			s.fail("%s.%s: %v", contract, method, err)
			s.netHint(err)
		}
	}
	s.finish()
}

func findNEP245(ctx context.Context, s *session) string {
	chain := &tokens.Chain{
		Enumeration: &tokens.Enumeration{Caller: s.client},
		OneClick:    &tokens.OneClick{},
	}
	ts, src, err := chain.Discover(ctx)
	if err != nil {
		s.note("discovery failed while looking for a NEP-245 token: %v", err)
		return ""
	}
	for _, t := range ts {
		if t.Standard == model.StandardNEP245 {
			s.note("using NEP-245 token %s (found via %s)", t.TokenID, src)
			return t.TokenID
		}
	}
	s.note("discovery found no NEP-245 tokens")
	return ""
}
