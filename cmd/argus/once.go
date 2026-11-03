package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/report"
	"github.com/emma31-dev/argus/internal/snapshot"
)

// runOnce does one discovery and one snapshot round against the configured
// RPC endpoints and prints the result. It needs no database: states start
// empty, so a single round can show green or amber but never red (red needs
// K consecutive negative snapshots).
func runOnce(ctx context.Context, cfg Config, out io.Writer) error {
	client := newRPC(cfg)
	toks, source, err := discoverScoped(ctx, buildDiscoverer(cfg, client))
	if err != nil {
		return fmt.Errorf("token discovery: %w", err)
	}
	reader := &snapshot.Reader{
		Caller: client, IntentsAccount: cfg.IntentsAccount, BatchSize: cfg.BatchSize, Concurrency: cfg.Concurrency,
	}
	res, err := reader.Round(ctx, toks)
	if err != nil {
		if errors.Is(err, snapshot.ErrOwedUnsupported) {
			return fmt.Errorf("%s has no mt_batch_supply; owed cannot be read (the event-delta fallback is not implemented): %w", cfg.IntentsAccount, err)
		}
		return fmt.Errorf("snapshot round: %w", err)
	}
	fmt.Fprintf(out, "Argus %s, one round: block %d (%s) via %s; %d tokens from %s discovery; %d read, %d failed\n\n",
		version, res.Block.Height, res.Block.Time.UTC().Format(time.RFC3339), res.Provider, len(toks), source,
		len(res.Snapshots), len(res.Errors))
	fmt.Fprint(out, StatusTable(toks, res, cfg.K))
	fmt.Fprintln(out, "\nOne round only: amber means a negative gap right now; red needs "+
		fmt.Sprint(cfg.K)+" consecutive negative snapshots.")
	return nil
}

// StatusTable renders a round as a table (negative gaps first), followed by
// the per-token read errors.
func StatusTable(toks []model.Token, res snapshot.Result, k int) string {
	meta := make(map[string]model.Token, len(toks))
	for _, t := range toks {
		meta[t.TokenID] = t
	}
	var rows []report.Row
	for _, s := range res.Snapshots {
		st, _ := alert.Step(model.TokenState{TokenID: s.TokenID, Status: model.StatusUnknown}, s, alert.Config{K: k})
		t := meta[s.TokenID]
		rows = append(rows, report.Row{
			TokenID: s.TokenID, Symbol: t.Symbol, Decimals: t.Decimals,
			Owed: s.Owed, Held: s.Held, Gap: s.Gap, Status: st.Status,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rank(rows[i].Status) < rank(rows[j].Status) })
	out := report.Table(rows)
	if len(res.Errors) > 0 {
		out += "\nRead errors (status unknown):\n"
		for _, e := range res.Errors {
			out += fmt.Sprintf("  %s: %v\n", e.TokenID, e.Err)
		}
	}
	return out
}

func rank(s model.Status) int {
	switch s {
	case model.StatusRed:
		return 0
	case model.StatusAmber:
		return 1
	case model.StatusUnknown:
		return 2
	}
	return 3
}
