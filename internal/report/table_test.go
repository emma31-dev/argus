package report

import (
	"strings"
	"testing"

	"github.com/emma31-dev/argus/internal/model"
)

func TestTable(t *testing.T) {
	out := Table([]Row{
		{TokenID: "nep141:wrap.near", Symbol: "wNEAR", Decimals: 24, Owed: "1000000000000000000000000000", Held: "1000000000000000000000000500", Gap: "500", Status: model.StatusGreen},
		{TokenID: "nep141:usdc", Symbol: "USDC", Decimals: 6, Owed: "2000000", Held: "1990000", Gap: "-10000", Status: model.StatusRed},
		{TokenID: "nep141:zero", Symbol: "Z", Decimals: 0, Owed: "0", Held: "0", Gap: "0", Status: model.StatusUnknown},
	})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("lines = %d:\n%s", len(lines), out)
	}
	for _, want := range []string{"1000", "+0.0000000000000000000005", "-0.01", "-0.5000%", "n/a", "red"} {
		if !strings.Contains(out, want) {
			t.Errorf("table lacks %q:\n%s", want, out)
		}
	}
	// every line has the same visible width up to the last column start
	idx := strings.Index(lines[0], "STATUS")
	for _, l := range lines[2:] {
		if len(l) <= idx {
			t.Errorf("misaligned line %q", l)
		}
	}
}
