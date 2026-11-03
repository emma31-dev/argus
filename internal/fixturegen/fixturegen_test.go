package fixturegen

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDeterministic(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := Generate(a); err != nil {
		t.Fatal(err)
	}
	if err := Generate(b); err != nil {
		t.Fatal(err)
	}
	assertSame(t, a, b)
}

func TestCommittedSetIsUpToDate(t *testing.T) {
	committed := filepath.Join("..", "..", "evidence", "fixtures", "synthetic")
	if _, err := os.Stat(committed); err != nil {
		t.Skip("committed synthetic set not present")
	}
	fresh := t.TempDir()
	if err := Generate(fresh); err != nil {
		t.Fatal(err)
	}
	assertSame(t, fresh, committed)
}

func assertSame(t *testing.T, want, got string) {
	t.Helper()
	list := func(dir string) map[string][]byte {
		m := map[string][]byte{}
		es, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range es {
			b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			m[e.Name()] = b
		}
		return m
	}
	w, g := list(want), list(got)
	if len(w) != len(g) {
		t.Fatalf("file count differs: %d vs %d (run `go run ./cmd/verify/gen` to regenerate)", len(w), len(g))
	}
	for name, b := range w {
		if !bytes.Equal(b, g[name]) {
			t.Fatalf("%s differs (run `go run ./cmd/verify/gen` to regenerate)", name)
		}
	}
}

func TestOracleScenario(t *testing.T) {
	exp := oracle(tokens())
	want := map[string]string{
		"nep141:eth.synthetic.near":  "red",
		"nep141:wbtc.synthetic.near": "green",
		"nep141:dai.synthetic.near":  "green",
	}
	for id, st := range want {
		if exp.Final[id].Status != st {
			t.Errorf("%s final = %s, want %s", id, exp.Final[id].Status, st)
		}
	}
	has := func(tok, kind string, blk uint64) bool {
		for _, tr := range exp.Transitions {
			if tr.Token == tok && tr.Kind == kind && tr.Block == blk {
				return true
			}
		}
		return false
	}
	if !has("nep141:eth.synthetic.near", "amber", height(5)) || !has("nep141:eth.synthetic.near", "red", height(8)) ||
		!has("nep141:wbtc.synthetic.near", "red", height(5)) || !has("nep141:wbtc.synthetic.near", "recovered", height(7)) ||
		!has("nep141:dai.synthetic.near", "false_alarm_cleared", height(7)) {
		t.Fatalf("transitions = %+v", exp.Transitions)
	}
}
