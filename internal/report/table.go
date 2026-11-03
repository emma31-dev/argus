// Package report renders human-readable status tables for the command line
// tools (argus -once, verify, replay). Amounts are formatted exactly with
// math/big via the snapshot package; no floats are involved.
package report

import (
	"strings"
	"unicode/utf8"

	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/snapshot"
)

// Row is one table line. Amounts are smallest-unit integer strings.
type Row struct {
	TokenID  string
	Symbol   string
	Decimals int
	Owed     string
	Held     string
	Gap      string
	Status   model.Status
}

// GapPercent returns gap as a percentage of owed with four decimals and a
// trailing "%", or "n/a" when owed is zero or a value is malformed.
func GapPercent(gap, owed string) string {
	p, err := snapshot.Ratio(gap, owed)
	if err != nil {
		return "n/a"
	}
	return p + "%"
}

// Table renders rows as an aligned text table. Text columns are left
// aligned, numbers right aligned.
func Table(rows []Row) string {
	header := []string{"TOKEN", "SYMBOL", "OWED", "HELD", "GAP", "GAP %", "STATUS"}
	right := []bool{false, false, true, true, true, true, false}
	cells := make([][]string, 0, len(rows)+1)
	cells = append(cells, header)
	for _, r := range rows {
		gap := snapshot.FormatUnits(r.Gap, r.Decimals)
		if !strings.HasPrefix(gap, "-") && gap != "0" {
			gap = "+" + gap
		}
		cells = append(cells, []string{
			r.TokenID, r.Symbol,
			snapshot.FormatUnits(r.Owed, r.Decimals), snapshot.FormatUnits(r.Held, r.Decimals),
			gap, GapPercent(r.Gap, r.Owed), string(r.Status),
		})
	}
	widths := make([]int, len(header))
	for _, row := range cells {
		for i, c := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(c))
		}
	}
	var b strings.Builder
	line := func(row []string) {
		for i, c := range row {
			pad := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c))
			switch {
			case right[i]:
				b.WriteString(pad + c)
			case i == len(row)-1:
				b.WriteString(c)
			default:
				b.WriteString(c + pad)
			}
			if i < len(row)-1 {
				b.WriteString("  ")
			}
		}
		b.WriteByte('\n')
	}
	line(cells[0])
	sep := make([]string, len(header))
	for i, w := range widths {
		sep[i] = strings.Repeat("-", w)
	}
	line(sep)
	for _, row := range cells[1:] {
		line(row)
	}
	return b.String()
}
