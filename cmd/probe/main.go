// Command probe checks, against live NEAR mainnet, the contract and endpoint
// behaviour the monitor depends on. Read-only, no keys.
//
//	go run ./cmd/probe <name> [flags]
package main

import (
	"fmt"
	"os"
	"sort"
)

var probes = map[string]struct {
	about string
	run   func(args []string)
}{
	"owed":      {"intents.near answers mt_supply and mt_batch_supply", runOwed},
	"held":      {"token contracts answer ft_balance_of / mt_balance_of for intents.near", runHeld},
	"sameblock": {"owed and held read at one block_id agree on re-read; how far back block_id works", runSameBlock},
	"events":    {"neardata serves recent blocks with logs and intents.near events parse", runEvents},
	"discovery": {"which token discovery sources work and how many tokens each finds", runDiscovery},
	"archival":  {"archival RPC serves view calls at an old block (-block or -date)", runArchival},
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if os.Args[1] == "all" {
		// archival needs a block height, so it is left out of "all".
		for _, name := range []string{"owed", "held", "sameblock", "events", "discovery"} {
			fmt.Printf("--- %s\n", name)
			probes[name].run(nil)
		}
		return
	}
	p, ok := probes[os.Args[1]]
	if !ok {
		usage()
		os.Exit(2)
	}
	p.run(os.Args[2:])
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: probe <name> [flags]   (or: probe all)")
	names := make([]string, 0, len(probes))
	for n := range probes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(os.Stderr, "  %-10s %s\n", n, probes[n].about)
	}
}
