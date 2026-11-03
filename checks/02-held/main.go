// Command 02-held is capability check 2 (PRD section 7): read how much of
// each token intents.near holds on the token contract itself.
//
//	nep141: <contract>.ft_balance_of({"account_id":"intents.near"})
//	nep245: <contract>.mt_balance_of({"account_id":"intents.near","token_id":<inner>})
//
// It checks wNEAR, USDC, one bridged *.omft.near token and one NEP-245
// token. Pass -nep245 <token id> to choose the NEP-245 token; otherwise one
// is taken from token discovery.
//
// Run: go run ./checks/02-held
package main

import (
	"context"
	"errors"
	"flag"
	"strings"
	"time"

	"github.com/emma31-dev/argus/checks/internal/checkutil"
	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/tokens"
)

func main() {
	nep245 := flag.String("nep245", "", "NEP-245 Intents token id to read (default: first one found by discovery)")
	flag.Parse()

	env, err := checkutil.New("02-held", checkutil.Providers(checkutil.URLs(checkutil.EnvRPC, checkutil.DefaultRPC()), false))
	if err != nil {
		checkutil.Fatalf("02-held", "setup: %v", err)
	}
	ctx, cancel := checkutil.Context(3 * time.Minute)
	defer cancel()

	blk, provider, err := env.Client.LatestFinalBlock(ctx)
	if err != nil {
		env.Fail("could not get latest final block")
		env.NetworkHint(err)
		env.Exit()
		return
	}
	env.Info("block %d via %s", blk.Height, provider)

	ids := checkutil.WellKnown()
	id245 := *nep245
	if id245 == "" {
		id245 = findNEP245(ctx, env, env.Client)
	}
	if id245 != "" {
		ids = append(ids, id245)
	} else {
		env.Fail("no NEP-245 token available: pass -nep245 <id> (e.g. a nep245:<contract>:<inner> id from the 1Click list)")
	}

	for _, id := range ids {
		t, err := tokens.Parse(id)
		if err != nil {
			env.Fail("%s: %v", id, err)
			continue
		}
		contract, method, args := tokens.HeldMethod(t)
		raw, err := env.Client.CallFunction(ctx, provider, contract, method, args, blk.Height)
		switch {
		case err == nil:
			env.Pass("%s.%s -> %s", contract, method, strings.TrimSpace(string(raw)))
		case errors.Is(err, rpc.ErrMethodNotFound), errors.Is(err, rpc.ErrAccountNotFound):
			env.Fail("%s.%s: %v", contract, method, err)
		default:
			env.Fail("%s.%s: %v", contract, method, err)
			env.NetworkHint(err)
		}
	}
	env.Exit()
}

func findNEP245(ctx context.Context, env *checkutil.Env, c *rpc.Client) string {
	chain := &tokens.Chain{
		Enumeration: &tokens.Enumeration{Caller: c},
		OneClick:    &tokens.OneClick{},
	}
	ts, src, err := chain.Discover(ctx)
	if err != nil {
		env.Info("discovery failed while looking for a NEP-245 token: %v", err)
		return ""
	}
	for _, t := range ts {
		if t.Standard == model.StandardNEP245 {
			env.Info("using NEP-245 token %s (discovered via %s)", t.TokenID, src)
			return t.TokenID
		}
	}
	env.Info("discovery found no NEP-245 tokens")
	return ""
}
