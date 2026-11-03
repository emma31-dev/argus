package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/fixturegen"
	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/report"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/rpc/rpctest"
	"github.com/emma31-dev/argus/internal/snapshot"
	"github.com/emma31-dev/argus/internal/tokens"
)

// Outcome is the result of replaying one fixture directory.
type Outcome struct {
	Dir       string
	Fixture   fixturegen.Fixture
	Exchanges int
	Blocks    []uint64
	Computed  fixturegen.Expected
	Rows      []report.Row
	LastBlock uint64
}

// header is the first line printed for a fixture set.
func (o Outcome) header() string {
	switch o.Fixture.Kind {
	case "synthetic":
		return "Fixture set: synthetic (not mainnet data)"
	default:
		return fmt.Sprintf("Fixture set: live capture at block %d from %s", o.LastBlock, o.Fixture.Provider)
	}
}

func readFixture(dir string) (fixturegen.Fixture, error) {
	var f fixturegen.Fixture
	b, err := os.ReadFile(filepath.Join(dir, "fixture.json"))
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("fixture.json: %w", err)
	}
	if f.Rounds < 1 || f.K < 1 || f.Provider == "" {
		return f, errors.New("fixture.json: rounds, k and provider are required")
	}
	if f.IntentsAccount == "" {
		f.IntentsAccount = tokens.DefaultIntentsAccount
	}
	return f, nil
}

// replayFixture runs the real snapshot.Reader over the recorded exchanges
// in dir for every recorded round, feeds the results to a real alert.Engine
// with in-memory stores, and returns what happened. It never touches the
// network: the HTTP transport is the replay.
func replayFixture(ctx context.Context, dir string) (*Outcome, error) {
	fx, err := readFixture(dir)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "tokens.yaml"))
	if err != nil {
		return nil, err
	}
	toks, err := tokens.ParseConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("tokens.yaml: %w", err)
	}
	for i := range toks {
		toks[i].Chain = tokens.ChainOf(toks[i].TokenID)
	}
	rp, err := rpctest.Load(dir)
	if err != nil {
		return nil, err
	}
	if rp.FinalBlocks() < fx.Rounds {
		return nil, fmt.Errorf("fixture declares %d rounds but only %d recorded block requests exist", fx.Rounds, rp.FinalBlocks())
	}
	client := rpc.New([]rpc.Provider{{Name: fx.Provider, URL: "http://fixture.invalid/"}},
		rpc.WithHTTPClient(&http.Client{Transport: rp}),
		rpc.WithSleep(func(context.Context, time.Duration) error { return nil }))
	reader := &snapshot.Reader{Caller: client, IntentsAccount: fx.IntentsAccount, Now: func() time.Time { return time.Unix(0, 0) }}

	states, incidents, queue := newMemStates(), newMemIncidents(), alert.NewMemoryQueue()
	engine := alert.NewEngine(alert.EngineConfig{
		Alert: alert.Config{K: fx.K}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, states, incidents, queue, memTokens(toks))

	out := &Outcome{Dir: dir, Fixture: fx, Exchanges: rp.Exchanges()}
	var last snapshot.Result
	for r := 1; r <= fx.Rounds; r++ {
		res, err := reader.Round(ctx, toks)
		if err != nil {
			return nil, fmt.Errorf("round %d: %w (misses: %v)", r, err, rp.Misses())
		}
		errs := map[string]error{}
		for _, te := range res.Errors {
			errs[te.TokenID] = te.Err
		}
		if err := engine.Process(ctx, res.Snapshots, errs); err != nil {
			return nil, fmt.Errorf("round %d: alert engine: %w", r, err)
		}
		out.Blocks = append(out.Blocks, res.Block.Height)
		last = res
	}
	if m := rp.Misses(); len(m) > 0 {
		return nil, fmt.Errorf("%d request(s) were not in the fixture:\n  %s", len(m), strings.Join(m, "\n  "))
	}
	out.LastBlock = last.Block.Height

	out.Computed = fixturegen.Expected{Rounds: fx.Rounds, Final: map[string]fixturegen.FinalState{}}
	meta := map[string]model.Token{}
	for _, t := range toks {
		meta[t.TokenID] = t
	}
	for _, s := range last.Snapshots {
		st := states.m[s.TokenID]
		out.Computed.Final[s.TokenID] = fixturegen.FinalState{Status: string(st.Status), Gap: s.Gap}
		t := meta[s.TokenID]
		out.Rows = append(out.Rows, report.Row{TokenID: s.TokenID, Symbol: t.Symbol, Decimals: t.Decimals,
			Owed: s.Owed, Held: s.Held, Gap: s.Gap, Status: st.Status})
	}
	for _, te := range last.Errors {
		out.Computed.Final[te.TokenID] = fixturegen.FinalState{Status: string(model.StatusUnknown)}
	}
	sort.SliceStable(out.Rows, func(i, j int) bool { return rank(out.Rows[i].Status) < rank(out.Rows[j].Status) })
	out.Computed.Transitions = append(out.Computed.Transitions, states.transitions...)
	for {
		d, err := queue.Dequeue(ctx, time.Millisecond)
		if err != nil || d == nil {
			break
		}
		out.Computed.Alerts = append(out.Computed.Alerts, fixturegen.AlertRec{Kind: string(d.Alert.Kind), Token: d.Alert.TokenID, Block: d.Alert.Block})
	}
	fixturegen.SortExpected(&out.Computed)
	return out, nil
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

// compare returns one line per difference between want and got.
func compare(want, got fixturegen.Expected) []string {
	var diffs []string
	if want.Rounds != got.Rounds {
		diffs = append(diffs, fmt.Sprintf("rounds: expected %d, replayed %d", want.Rounds, got.Rounds))
	}
	ids := map[string]bool{}
	for id := range want.Final {
		ids[id] = true
	}
	for id := range got.Final {
		ids[id] = true
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	for _, id := range sorted {
		w, wok := want.Final[id]
		g, gok := got.Final[id]
		switch {
		case !wok:
			diffs = append(diffs, fmt.Sprintf("final %s: unexpected token (status %s)", id, g.Status))
		case !gok:
			diffs = append(diffs, fmt.Sprintf("final %s: expected but not produced", id))
		case w != g:
			diffs = append(diffs, fmt.Sprintf("final %s: expected status=%s gap=%s, got status=%s gap=%s", id, w.Status, w.Gap, g.Status, g.Gap))
		}
	}
	diffs = append(diffs, setDiff("transition", keys(want.Transitions, func(t fixturegen.Transition) string {
		return fmt.Sprintf("%s %s at block %d", t.Token, t.Kind, t.Block)
	}), keys(got.Transitions, func(t fixturegen.Transition) string {
		return fmt.Sprintf("%s %s at block %d", t.Token, t.Kind, t.Block)
	}))...)
	diffs = append(diffs, setDiff("alert", keys(want.Alerts, func(a fixturegen.AlertRec) string {
		return fmt.Sprintf("%s %s at block %d", a.Token, a.Kind, a.Block)
	}), keys(got.Alerts, func(a fixturegen.AlertRec) string {
		return fmt.Sprintf("%s %s at block %d", a.Token, a.Kind, a.Block)
	}))...)
	return diffs
}

func keys[T any](xs []T, f func(T) string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}

func setDiff(what string, want, got []string) []string {
	count := map[string]int{}
	for _, w := range want {
		count[w]++
	}
	for _, g := range got {
		count[g]--
	}
	var diffs []string
	for _, w := range want {
		if count[w] > 0 {
			diffs = append(diffs, fmt.Sprintf("%s expected but missing: %s", what, w))
			count[w]--
		}
	}
	for _, g := range got {
		if count[g] < 0 {
			diffs = append(diffs, fmt.Sprintf("%s produced but not expected: %s", what, g))
			count[g]++
		}
	}
	return diffs
}

// printOutcome writes the human-readable result of one fixture set.
func printOutcome(w io.Writer, o *Outcome, symbols map[string]string) {
	fmt.Fprintln(w, o.header())
	fmt.Fprintf(w, "  directory:        %s\n", o.Dir)
	fmt.Fprintf(w, "  recorded exchanges: %d, rounds replayed: %d (blocks %d .. %d), K = %d, provider: %s\n\n",
		o.Exchanges, len(o.Blocks), o.Blocks[0], o.LastBlock, o.Fixture.K, o.Fixture.Provider)
	fmt.Fprintf(w, "Status after the last round (block %d)\n", o.LastBlock)
	fmt.Fprint(w, report.Table(o.Rows))
	fmt.Fprintln(w, "\nTransitions observed")
	if len(o.Computed.Transitions) == 0 {
		fmt.Fprintln(w, "  none")
	}
	labels := map[string]string{
		"amber": "turned AMBER", "red": "turned RED", "recovered": "RECOVERED (red -> green)",
		"false_alarm_cleared": "false alarm cleared (amber -> green, never red)",
	}
	for _, t := range o.Computed.Transitions {
		fmt.Fprintf(w, "  block %-9d %-8s %s\n", t.Block, symbols[t.Token], labels[t.Kind])
	}
	fmt.Fprintln(w, "\nAlerts the engine queued")
	if len(o.Computed.Alerts) == 0 {
		fmt.Fprintln(w, "  none")
	}
	for _, a := range o.Computed.Alerts {
		fmt.Fprintf(w, "  block %-9d %-8s %s\n", a.Block, symbols[a.Token], a.Kind)
	}
}
