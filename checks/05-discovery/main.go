// Command 05-discovery is capability check 5 (PRD section 7): try each token
// discovery source (NEP-245 enumeration, the 1Click list, config/tokens.yaml)
// and record how many tokens each finds.
//
// Run from the repository root: go run ./checks/05-discovery
package main

import (
	"errors"
	"flag"
	"time"

	"github.com/emma31-dev/argus/checks/internal/checkutil"
	"github.com/emma31-dev/argus/internal/tokens"
)

func main() {
	oneclickURL := flag.String("oneclick-url", tokens.DefaultOneClickURL, "1Click supported-tokens endpoint")
	configPath := flag.String("config", "config/tokens.yaml", "fallback token list")
	flag.Parse()

	env, err := checkutil.New("05-discovery", checkutil.Providers(checkutil.URLs(checkutil.EnvRPC, checkutil.DefaultRPC()), false))
	if err != nil {
		checkutil.Fatalf("05-discovery", "setup: %v", err)
	}
	ctx, cancel := checkutil.Context(5 * time.Minute)
	defer cancel()

	enum := &tokens.Enumeration{Caller: env.Client}
	one := &tokens.OneClick{URL: *oneclickURL}
	cfg := &tokens.Config{Path: *configPath}

	working := 0
	for _, s := range []tokens.Source{enum, one, cfg} {
		ts, err := s.Fetch(ctx)
		switch {
		case err == nil:
			working++
			env.Pass("%s: %d nep141/nep245 tokens", s.Name(), len(ts))
			env.Save("tokens-"+s.Name()+".json", ts)
		case errors.Is(err, tokens.ErrNotSupported):
			env.Info("%s: not supported by the contract (%v)", s.Name(), err)
		default:
			env.Info("%s: unavailable: %v", s.Name(), err)
			env.NetworkHint(err)
		}
	}
	if working == 0 {
		env.Fail("no discovery source works")
	}

	chain := &tokens.Chain{Enumeration: enum, OneClick: one, Config: cfg}
	ts, src, err := chain.Discover(ctx)
	if err != nil {
		env.Fail("chain: %v", err)
	} else {
		env.Pass("chain discovery used %q: %d tokens", src, len(ts))
		env.Save("tokens-merged.json", ts)
	}
	env.Exit()
}
