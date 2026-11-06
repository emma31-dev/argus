package main

import (
	"errors"
	"flag"
	"time"

	"github.com/emma31-dev/argus/internal/tokens"
)

// runDiscovery must run from the repo root so config/tokens.yaml resolves.
func runDiscovery(args []string) {
	fs := flag.NewFlagSet("discovery", flag.ExitOnError)
	oneclickURL := fs.String("oneclick-url", tokens.DefaultOneClickURL, "1Click supported-tokens endpoint")
	configPath := fs.String("config", "config/tokens.yaml", "fallback token list")
	_ = fs.Parse(args)

	s, err := newSession("discovery", providers(urlsFrom(envRPC, defaultRPC()), false))
	if err != nil {
		die("discovery", "setup: %v", err)
	}
	ctx, cancel := withTimeout(5 * time.Minute)
	defer cancel()

	enum := &tokens.Enumeration{Caller: s.client}
	one := &tokens.OneClick{URL: *oneclickURL}
	cfg := &tokens.Config{Path: *configPath}

	working := 0
	for _, src := range []tokens.Source{enum, one, cfg} {
		ts, err := src.Fetch(ctx)
		switch {
		case err == nil:
			working++
			s.ok("%s: %d nep141/nep245 tokens", src.Name(), len(ts))
			s.save("tokens-"+src.Name()+".json", ts)
		case errors.Is(err, tokens.ErrNotSupported):
			s.note("%s: not supported by the contract (%v)", src.Name(), err)
		default:
			s.note("%s: unavailable: %v", src.Name(), err)
			s.netHint(err)
		}
	}
	if working == 0 {
		s.fail("no discovery source works")
	}

	chain := &tokens.Chain{Enumeration: enum, OneClick: one, Config: cfg}
	ts, src, err := chain.Discover(ctx)
	if err != nil {
		s.fail("chain: %v", err)
	} else {
		s.ok("chain discovery used %q: %d tokens", src, len(ts))
		s.save("tokens-merged.json", ts)
	}
	s.finish()
}
