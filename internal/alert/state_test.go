package alert

import (
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/emma31-dev/argus/internal/model"
)

const tok = "nep141:wrap.near"

var t0 = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func snap(block uint64, gap string) model.Snapshot {
	return model.Snapshot{
		TokenID: tok, BlockHeight: block, Gap: gap,
		BlockTime: t0.Add(time.Duration(block) * time.Second), TakenAt: t0.Add(time.Duration(block) * time.Second),
	}
}

// run feeds gaps at consecutive blocks starting at 100 and returns the
// statuses and the kinds emitted at each step.
func run(cfg Config, gaps ...string) (final model.TokenState, statuses []model.Status, kinds [][]Kind) {
	st := model.TokenState{TokenID: tok, Status: model.StatusUnknown}
	for i, g := range gaps {
		var trs []Transition
		st, trs = Step(st, snap(uint64(100+i), g), cfg)
		statuses = append(statuses, st.Status)
		var ks []Kind
		for _, t := range trs {
			ks = append(ks, t.Kind)
		}
		kinds = append(kinds, ks)
	}
	return
}

func TestStepScenarios(t *testing.T) {
	G, A, R := model.StatusGreen, model.StatusAmber, model.StatusRed
	tests := []struct {
		name     string
		cfg      Config
		gaps     []string
		statuses []model.Status
		kinds    [][]Kind
	}{
		{
			name: "in-flight deposit: one negative then positive",
			gaps: []string{"5", "-10", "7"}, statuses: []model.Status{G, A, G},
			kinds: [][]Kind{nil, {TurnedAmber}, {FalseAlarmCleared}},
		},
		{
			name: "in-flight withdrawal over three snapshots stays below K=4",
			gaps: []string{"5", "-10", "-30", "-20", "5"}, statuses: []model.Status{G, A, A, A, G},
			kinds: [][]Kind{nil, {TurnedAmber}, nil, nil, {FalseAlarmCleared}},
		},
		{
			name: "sustained deficit turns red exactly at snapshot K",
			gaps: []string{"-1", "-1", "-1", "-1", "-1"}, statuses: []model.Status{A, A, A, R, R},
			kinds: [][]Kind{{TurnedAmber}, nil, nil, {TurnedRed}, nil},
		},
		{
			name: "custom K=2",
			cfg:  Config{K: 2},
			gaps: []string{"-1", "-1", "0"}, statuses: []model.Status{A, R, G},
			kinds: [][]Kind{{TurnedAmber}, {TurnedRed}, {Recovered}},
		},
		{
			name: "zero gap is green",
			gaps: []string{"0", "0"}, statuses: []model.Status{G, G}, kinds: [][]Kind{nil, nil},
		},
		{
			name: "absolute threshold goes red immediately",
			cfg:  Config{DefaultAbsThreshold: big.NewInt(1000)},
			gaps: []string{"-999", "-1000"}, statuses: []model.Status{A, R},
			kinds: [][]Kind{{TurnedAmber}, {TurnedRed}},
		},
		{
			name: "absolute threshold on first negative snapshot skips amber transition",
			cfg:  Config{AbsThreshold: map[string]*big.Int{tok: big.NewInt(50)}},
			gaps: []string{"-500"}, statuses: []model.Status{R}, kinds: [][]Kind{{TurnedRed}},
		},
		{
			name: "per-token threshold overrides default; other token threshold ignored",
			cfg: Config{
				AbsThreshold:        map[string]*big.Int{tok: big.NewInt(10000), "other": big.NewInt(1)},
				DefaultAbsThreshold: big.NewInt(1),
			},
			gaps: []string{"-500"}, statuses: []model.Status{A}, kinds: [][]Kind{{TurnedAmber}},
		},
		{
			name:     "flapping: neg,neg,pos,neg,neg,neg,neg is red only after a fresh run of K",
			gaps:     []string{"-1", "-1", "1", "-1", "-1", "-1", "-1"},
			statuses: []model.Status{A, A, G, A, A, A, R},
			kinds:    [][]Kind{{TurnedAmber}, nil, {FalseAlarmCleared}, {TurnedAmber}, nil, nil, {TurnedRed}},
		},
		{
			name:     "red then recovery then a new amber",
			gaps:     []string{"-1", "-1", "-1", "-1", "5", "-1"},
			statuses: []model.Status{A, A, A, R, G, A},
			kinds:    [][]Kind{{TurnedAmber}, nil, nil, {TurnedRed}, {Recovered}, {TurnedAmber}},
		},
		{
			name:     "incident update only when deficit reaches a new max",
			gaps:     []string{"-5", "-5", "-5", "-5", "-5", "-6", "-6", "-3", "-7", "-7"},
			statuses: []model.Status{A, A, A, R, R, R, R, R, R, R},
			kinds:    [][]Kind{{TurnedAmber}, nil, nil, {TurnedRed}, nil, {IncidentUpdate}, nil, nil, {IncidentUpdate}, nil},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, statuses, kinds := run(tc.cfg, tc.gaps...)
			for i := range tc.gaps {
				if statuses[i] != tc.statuses[i] {
					t.Errorf("step %d (gap %s): status %s, want %s", i, tc.gaps[i], statuses[i], tc.statuses[i])
				}
				if !equalKinds(kinds[i], tc.kinds[i]) {
					t.Errorf("step %d (gap %s): transitions %v, want %v", i, tc.gaps[i], kinds[i], tc.kinds[i])
				}
			}
		})
	}
}

func equalKinds(a, b []Kind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestStepMaxDeficitAndRecoveryDetails(t *testing.T) {
	st := model.TokenState{TokenID: tok, Status: model.StatusUnknown}
	cfg := Config{K: 3}
	gaps := []string{"4", "-10", "-40", "-25", "-30", "-50", "20"}
	var all [][]Transition
	for i, g := range gaps {
		var trs []Transition
		st, trs = Step(st, snap(uint64(100+i), g), cfg)
		all = append(all, trs)
		if i == 3 { // red at block 103
			if st.Status != model.StatusRed || st.FirstNegativeBlock != 101 || st.RedBlock != 103 {
				t.Fatalf("red state wrong: %+v", st)
			}
			if st.MaxDeficit != "40" || st.MaxDeficitBlock != 102 {
				t.Fatalf("max deficit during streak not tracked: %+v", st)
			}
			st.OpenIncidentID = 77 // what the Engine does
		}
	}
	red := all[3][0]
	if red.Kind != TurnedRed || red.StartBlock != 101 || red.RedBlock != 103 || red.MaxDeficit != "40" || red.Gap != "-25" {
		t.Errorf("TurnedRed = %+v", red)
	}
	upd := all[5][0]
	if upd.Kind != IncidentUpdate || upd.MaxDeficit != "50" || upd.MaxDeficitBlock != 105 || upd.IncidentID != 77 {
		t.Errorf("IncidentUpdate = %+v", upd)
	}
	rec := all[6][0]
	if rec.Kind != Recovered || rec.StartBlock != 101 || rec.RedBlock != 103 || rec.Block != 106 ||
		rec.MaxDeficit != "50" || rec.MaxDeficitBlock != 105 || rec.IncidentID != 77 {
		t.Errorf("Recovered = %+v", rec)
	}
	if !rec.BlockTime.Equal(t0.Add(106 * time.Second)) {
		t.Errorf("Recovered block time = %v", rec.BlockTime)
	}
	if st.Status != model.StatusGreen || st.NegativeStreak != 0 || st.MaxDeficit != "" ||
		st.OpenIncidentID != 0 || st.FirstNegativeBlock != 0 || st.RedBlock != 0 {
		t.Errorf("state not reset after recovery: %+v", st)
	}
}

func TestStepIgnoresStaleBlocks(t *testing.T) {
	cfg := Config{}
	st := model.TokenState{TokenID: tok, Status: model.StatusUnknown}
	st, _ = Step(st, snap(100, "-1"), cfg)
	st, _ = Step(st, snap(101, "-1"), cfg)
	before := st
	for _, b := range []uint64{101, 100, 50} { // duplicate and out-of-order
		next, trs := Step(st, snap(b, "-999"), cfg)
		if len(trs) != 0 || next != before {
			t.Fatalf("block %d not ignored: %+v %v", b, next, trs)
		}
	}
	// A positive but old snapshot must not recover a red token either.
	st, _ = Step(st, snap(102, "-1"), cfg)
	st, _ = Step(st, snap(103, "-1"), cfg)
	if st.Status != model.StatusRed {
		t.Fatal("want red")
	}
	next, trs := Step(st, snap(103, "10"), cfg)
	if next != st || len(trs) != 0 {
		t.Fatal("old positive snapshot changed state")
	}
	next, trs = Step(st, snap(104, "10"), cfg)
	if next.Status != model.StatusGreen || len(trs) != 1 || trs[0].Kind != Recovered {
		t.Fatalf("fresh positive snapshot should recover: %+v %v", next, trs)
	}
}

func TestStepBadGapIgnored(t *testing.T) {
	st := model.TokenState{TokenID: tok, Status: model.StatusGreen, LastBlock: 5}
	for _, g := range []string{"", "abc", "1.5"} {
		next, trs := Step(st, snap(6, g), Config{})
		if next != st || len(trs) != 0 {
			t.Errorf("gap %q should be ignored", g)
		}
	}
}

func TestStepErrorKeepsStreak(t *testing.T) {
	cfg := Config{}
	st := model.TokenState{TokenID: tok, Status: model.StatusUnknown}
	st, _ = Step(st, snap(100, "-1"), cfg)
	st, _ = Step(st, snap(101, "-1"), cfg)
	st = StepError(st, errors.New("rpc timeout"), cfg)
	st = StepError(st, errors.New("rpc timeout"), cfg)
	if st.NegativeStreak != 2 || st.Status != model.StatusAmber || !st.Stale || st.LastError != "rpc timeout" || st.FirstNegativeBlock != 100 {
		t.Fatalf("error changed streak/status: %+v", st)
	}
	// Streak continues from 2: two more negatives -> red at the 4th real snapshot.
	st, trs := Step(st, snap(102, "-1"), cfg)
	if st.Stale || st.LastError != "" || len(trs) != 0 || st.NegativeStreak != 3 {
		t.Fatalf("after error: %+v %v", st, trs)
	}
	st, trs = Step(st, snap(103, "-1"), cfg)
	if len(trs) != 1 || trs[0].Kind != TurnedRed {
		t.Fatalf("want red at 4th snapshot: %v", trs)
	}
	// Errors never change a red status.
	st = StepError(st, errors.New("x"), cfg)
	if st.Status != model.StatusRed {
		t.Fatalf("status %s", st.Status)
	}
}

func TestStepErrorNeverSeenIsUnknown(t *testing.T) {
	st := StepError(model.TokenState{}, errors.New("boom"), Config{})
	if st.Status != model.StatusUnknown || !st.Stale {
		t.Fatalf("%+v", st)
	}
	st = StepError(model.TokenState{TokenID: tok, Status: model.StatusUnknown}, errors.New(strings.Repeat("x", 1000)), Config{})
	if len(st.LastError) > 300 {
		t.Fatal("error not truncated")
	}
}

func TestStepHugeIntegers(t *testing.T) {
	// 10^40 and 2^200 are far beyond uint128/int64.
	huge := new(big.Int).Exp(big.NewInt(2), big.NewInt(200), nil)
	neg := func(x *big.Int) string { return new(big.Int).Neg(x).String() }
	bigger := new(big.Int).Add(huge, big.NewInt(1))
	cfg := Config{K: 2, DefaultAbsThreshold: new(big.Int).Mul(huge, big.NewInt(10))}

	st := model.TokenState{TokenID: tok, Status: model.StatusUnknown}
	st, _ = Step(st, snap(1, neg(huge)), cfg)
	st, trs := Step(st, snap(2, neg(huge)), cfg)
	if st.Status != model.StatusRed || len(trs) != 1 || trs[0].MaxDeficit != huge.String() {
		t.Fatalf("%+v %v", st, trs)
	}
	// Differ only in the last digit: must register as new max.
	st, trs = Step(st, snap(3, neg(bigger)), cfg)
	if len(trs) != 1 || trs[0].Kind != IncidentUpdate || trs[0].MaxDeficit != bigger.String() {
		t.Fatalf("%v", trs)
	}
	// Absolute threshold comparison with big numbers.
	st2 := model.TokenState{TokenID: tok}
	_, trs = Step(st2, snap(1, neg(new(big.Int).Mul(huge, big.NewInt(10)))), cfg)
	if len(trs) != 1 || trs[0].Kind != TurnedRed {
		t.Fatalf("threshold: %v", trs)
	}
}
