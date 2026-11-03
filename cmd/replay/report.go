package main

import (
	"fmt"
	"strings"

	"github.com/emma31-dev/argus/internal/snapshot"
)

// Markdown renders the readable evidence report.
func Markdown(f *ReplayFile, jsonName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s: incident replay\n\n", f.Title)
	fmt.Fprintf(&b, "Generated %s from archival RPC (%s). Machine-readable twin: `%s`.\n\n", f.GeneratedAt.Format("2006-01-02 15:04:05 UTC"), f.RPCProvider, jsonName)
	b.WriteString("Invariant checked at every block: `held(T) >= owed(T)`, where `owed` is `intents.near.mt_batch_supply` and `held` is the token contract's balance of `intents.near`, both read at the same `block_id`. `gap = held - owed`.\n\n")
	b.WriteString("## Notes\n\n" + f.Notes + "\n\n")
	fmt.Fprintf(&b, "Debounce K = %d consecutive negative snapshots.\n\n", f.DebounceK)
	b.WriteString("## Summary\n\n| Token | Chain | First negative block | Detected red at block | Max deficit | Time to detect (30 s cadence) |\n|---|---|---|---|---|---|\n")
	for _, t := range f.Tokens {
		ttd := "not demonstrated"
		if t.TimeToDetectSeconds != nil {
			ttd = fmt.Sprintf("%d s", *t.TimeToDetectSeconds)
		}
		first, red := "none", "never"
		if t.FirstNegativeBlock != 0 {
			first = fmt.Sprint(t.FirstNegativeBlock)
		}
		if t.DetectedRedBlock != 0 {
			red = fmt.Sprint(t.DetectedRedBlock)
		}
		fmt.Fprintf(&b, "| %s (`%s`) | %s | %s | %s | %s %s | %s |\n", t.Symbol, t.ID, t.Chain, first, red,
			snapshot.FormatUnits(t.MaxDeficit, t.Decimals), t.Symbol, ttd)
	}
	for _, t := range f.Tokens {
		fmt.Fprintf(&b, "\n## %s (`%s`), %d decimals\n\n%s\n\n", t.Symbol, t.ID, t.Decimals, t.DetectionNote)
		b.WriteString("| Block | Block time (UTC) | Label | Owed | Held | Gap | Status |\n|---:|---|---|---:|---:|---:|---|\n")
		for _, p := range t.Points {
			fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s | %s |\n", p.BlockHeight, p.BlockTime.Format("2006-01-02 15:04:05"), p.Label,
				snapshot.FormatUnits(p.Owed, t.Decimals), snapshot.FormatUnits(p.Held, t.Decimals),
				snapshot.FormatUnits(p.Gap, t.Decimals), p.Status)
		}
		b.WriteString("\nRPC queries behind each row (exact; the amounts above are these results divided by 10^decimals):\n\n")
		for _, p := range t.Points {
			fmt.Fprintf(&b, "- block %d (%s)\n  - owed: `%s`\n  - held: `%s`\n", p.BlockHeight, p.Label, p.OwedQuery, p.HeldQuery)
		}
	}
	return b.String()
}
