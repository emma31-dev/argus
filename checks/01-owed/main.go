// Command 01-owed is capability check 1 (PRD section 7): does intents.near
// implement mt_supply and mt_batch_supply, and what do they return?
//
// Run: go run ./checks/01-owed
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/emma31-dev/argus/checks/internal/checkutil"
	"github.com/emma31-dev/argus/internal/rpc"
)

const bogusID = "nep141:argus-does-not-exist.near"

func main() {
	env, err := checkutil.New("01-owed", checkutil.Providers(checkutil.URLs(checkutil.EnvRPC, checkutil.DefaultRPC()), false))
	if err != nil {
		checkutil.Fatalf("01-owed", "setup: %v", err)
	}
	ctx, cancel := checkutil.Context(2 * time.Minute)
	defer cancel()

	blk, provider, err := env.Client.LatestFinalBlock(ctx)
	if err != nil {
		env.Fail("could not get latest final block")
		env.NetworkHint(err)
		env.Exit()
		return
	}
	env.Info("block %d (%s) via %s", blk.Height, blk.Hash, provider)

	ids := checkutil.WellKnown()
	for _, id := range ids {
		raw, err := env.Client.CallFunction(ctx, provider, "intents.near", "mt_supply", map[string]string{"token_id": id}, blk.Height)
		report(env, "mt_supply "+id, raw, err)
	}

	raw, err := env.Client.CallFunction(ctx, provider, "intents.near", "mt_batch_supply",
		map[string]any{"token_ids": append(append([]string{}, ids...), bogusID)}, blk.Height)
	report(env, "mt_batch_supply (3 tokens + 1 unknown id)", raw, err)
	if err == nil {
		var vals []json.RawMessage
		if json.Unmarshal(raw, &vals) == nil && len(vals) == len(ids)+1 {
			env.Pass("mt_batch_supply returned one entry per id; unknown id -> %s", string(vals[len(vals)-1]))
		} else {
			env.Fail("mt_batch_supply response is not an array of %d entries: %s", len(ids)+1, raw)
		}
	}
	if err != nil && errors.Is(err, rpc.ErrMethodNotFound) {
		fmt.Println("\nmt_batch_supply is missing: per the PRD, owed must switch to the event-delta method; update the PRD before continuing.")
	}
	env.Exit()
}

func report(env *checkutil.Env, what string, raw []byte, err error) {
	switch {
	case err == nil:
		env.Pass("%s -> %s", what, truncate(string(raw)))
	case errors.Is(err, rpc.ErrMethodNotFound):
		env.Fail("%s: method not found on intents.near", what)
	default:
		env.Fail("%s: %v", what, err)
		env.NetworkHint(err)
	}
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
