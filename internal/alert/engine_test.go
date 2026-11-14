package alert

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emma31-dev/argus/internal/model"
)

// faultyRunner wraps a TxRunner and injects errors into the writes of the
// transactions it runs.
type faultyRunner struct {
	inner                       TxRunner
	openErr, outboxErr, saveErr error
}

func (f *faultyRunner) InTx(ctx context.Context, fn func(Tx) error) error {
	return f.inner.InTx(ctx, func(tx Tx) error { return fn(faultyTx{Tx: tx, f: f}) })
}

type faultyTx struct {
	Tx
	f *faultyRunner
}

func (t faultyTx) OpenIncident(ctx context.Context, inc model.Incident) (int64, error) {
	if t.f.openErr != nil {
		return 0, t.f.openErr
	}
	return t.Tx.OpenIncident(ctx, inc)
}

func (t faultyTx) AddOutbox(ctx context.Context, a []Alert) error {
	if t.f.outboxErr != nil {
		return t.f.outboxErr
	}
	return t.Tx.AddOutbox(ctx, a)
}

func (t faultyTx) SaveStates(ctx context.Context, s []model.TokenState) error {
	if t.f.saveErr != nil {
		return t.f.saveErr
	}
	return t.Tx.SaveStates(ctx, s)
}

type fakeTokens struct{}

func (fakeTokens) ListTokens(context.Context) ([]model.Token, error) {
	return []model.Token{{TokenID: tok, Symbol: "wNEAR", Decimals: 24}}, nil
}

// rig is an Engine over a MemStore with fault injection, plus the relay and
// queue that carry its alerts to a consumer.
type rig struct {
	e     *Engine
	mem   *MemStore
	fault *faultyRunner
	relay *Relay
	q     *MemoryQueue
}

func newRig(k int, mut ...func(*EngineConfig)) *rig {
	mem := NewMemStore()
	fault := &faultyRunner{inner: mem}
	q := NewMemoryQueue()
	cfg := EngineConfig{Alert: Config{K: k}, DashboardURL: "https://argus.example/", Now: func() time.Time { return t0 }}
	for _, m := range mut {
		m(&cfg)
	}
	return &rig{e: NewEngine(cfg, mem, fault, fakeTokens{}), mem: mem, fault: fault, relay: NewRelay(RelayConfig{}, mem, q), q: q}
}

// drain relays committed alerts to the queue and returns what is queued.
func (r *rig) drain() []Alert {
	if _, err := r.relay.Flush(context.Background()); err != nil {
		panic(err)
	}
	var out []Alert
	for {
		d, _ := r.q.Dequeue(context.Background(), 0)
		if d == nil {
			return out
		}
		out = append(out, d.Alert)
	}
}

func TestEngineIncidentLifecycle(t *testing.T) {
	ctx := context.Background()
	r := newRig(3)
	e := r.e
	gaps := []string{"-10", "-20", "-30", "-30", "-50", "100"}
	for i, g := range gaps {
		if err := e.Process(ctx, []model.Snapshot{snap(uint64(200+i), g)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	alerts := r.drain()
	if len(alerts) != 3 {
		t.Fatalf("want 3 alerts (red, update, recovered), got %d: %+v", len(alerts), alerts)
	}
	red, upd, rec := alerts[0], alerts[1], alerts[2]
	if red.Kind != TurnedRed || red.Symbol != "wNEAR" || red.Decimals != 24 || red.StartBlock != 200 || red.Block != 202 ||
		red.IncidentID != 1 || red.MaxDeficit != "30" || red.DashboardURL != "https://argus.example/token/nep141:wrap.near" {
		t.Errorf("red alert: %+v", red)
	}
	if upd.Kind != IncidentUpdate || upd.MaxDeficit != "50" || upd.IncidentID != 1 {
		t.Errorf("update alert: %+v", upd)
	}
	if rec.Kind != Recovered || rec.IncidentID != 1 || rec.MaxDeficit != "50" || rec.DurationSeconds != 3 {
		// opened at block time 202s, closed at 205s
		t.Errorf("recovered alert: %+v", rec)
	}
	i := r.mem.Incidents()[0]
	if i.StartBlock != 200 || i.RedBlock != 202 || i.EndBlock != 205 || i.ClosedAt == nil || i.MaxDeficit != "50" || i.MaxDeficitBlock != 204 {
		t.Errorf("incident: %+v", i)
	}
	if s := r.mem.State(tok); s.Status != model.StatusGreen || s.OpenIncidentID != 0 || s.LastBlock != 205 {
		t.Errorf("state: %+v", s)
	}
}

func TestEngineFalseAlarmSendsNothing(t *testing.T) {
	ctx := context.Background()
	r := newRig(4)
	e := r.e
	for i, g := range []string{"-1", "-1", "5"} {
		if err := e.Process(ctx, []model.Snapshot{snap(uint64(300+i), g)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(r.drain()); n != 0 || len(r.mem.Incidents()) != 0 {
		t.Fatalf("false alarm produced alerts=%d incidents=%d", n, len(r.mem.Incidents()))
	}
	if e.FalseAlarms() != 1 {
		t.Fatalf("false alarms = %d", e.FalseAlarms())
	}
}

func TestEngineReadErrorsKeepStreak(t *testing.T) {
	ctx := context.Background()
	r := newRig(3)
	e := r.e
	_ = e.Process(ctx, []model.Snapshot{snap(1, "-1")}, nil)
	_ = e.Process(ctx, nil, map[string]error{tok: errors.New("rpc down")})
	_ = e.Process(ctx, nil, map[string]error{tok: errors.New("rpc down")})
	s := r.mem.State(tok)
	if s.NegativeStreak != 1 || !s.Stale || s.Status != model.StatusAmber {
		t.Fatalf("%+v", s)
	}
	_ = e.Process(ctx, []model.Snapshot{snap(2, "-1")}, nil)
	_ = e.Process(ctx, []model.Snapshot{snap(3, "-1")}, nil)
	if got := r.drain(); len(got) != 1 || got[0].Kind != TurnedRed {
		t.Fatalf("alerts: %+v", got)
	}
	// An unknown token that only errors is stored as unknown.
	_ = e.Process(ctx, nil, map[string]error{"nep141:new.near": errors.New("x")})
	if s := r.mem.State("nep141:new.near"); s.Status != model.StatusUnknown || !s.Stale {
		t.Fatalf("%+v", s)
	}
}

func TestEngineOpenFailureRetriesWithoutLosingTransition(t *testing.T) {
	ctx := context.Background()
	r := newRig(2)
	e := r.e
	_ = e.Process(ctx, []model.Snapshot{snap(1, "-1")}, nil)
	r.fault.openErr = errors.New("db down")
	if err := e.Process(ctx, []model.Snapshot{snap(2, "-1")}, nil); err == nil {
		t.Fatal("want error")
	}
	if r.mem.State(tok).LastBlock != 1 || len(r.drain()) != 0 {
		t.Fatalf("state must not advance on failure: %+v", r.mem.State(tok))
	}
	r.fault.openErr = nil
	if err := e.Process(ctx, []model.Snapshot{snap(3, "-1")}, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.drain(); len(got) != 1 || got[0].Kind != TurnedRed || r.mem.State(tok).OpenIncidentID != 1 {
		t.Fatalf("%+v %+v", got, r.mem.State(tok))
	}
}

func TestEngineMultipleTokensIndependent(t *testing.T) {
	ctx := context.Background()
	r := newRig(2)
	e := r.e
	a := snap(10, "-1")
	b := snap(10, "5")
	b.TokenID = "nep141:b.near"
	if err := e.Process(ctx, []model.Snapshot{a, b}, nil); err != nil {
		t.Fatal(err)
	}
	if r.mem.State(tok).Status != model.StatusAmber || r.mem.State("nep141:b.near").Status != model.StatusGreen {
		t.Fatalf("%+v", r.mem.State(tok))
	}
}

func TestFormatUnits(t *testing.T) {
	tests := []struct {
		in   string
		dec  int
		want string
	}{
		{"1500000", 6, "1.5"}, {"0", 6, "0"}, {"5", 6, "0.000005"}, {"-5", 6, "-0.000005"},
		{"1000000000000000000000000", 24, "1"}, {"123", 0, "123"},
		{"340282366920938463463374607431768211457", 18, "340282366920938463463.374607431768211457"},
		{"nope", 3, "nope"},
	}
	for _, tc := range tests {
		if got := FormatUnits(tc.in, tc.dec); got != tc.want {
			t.Errorf("FormatUnits(%q,%d)=%q want %q", tc.in, tc.dec, got, tc.want)
		}
	}
}

func TestEngineLinkFormat(t *testing.T) {
	e := NewEngine(EngineConfig{DashboardURL: "https://argus.example/", LinkFormat: DashboardLinkFormat}, nil, nil, nil)
	got := e.tokenLink("nep141:wrap.near")
	if want := "https://argus.example/token/?id=nep141%3Awrap.near"; got != want {
		t.Fatalf("link = %q, want %q", got, want)
	}
}

func TestEngineOnTransitionHook(t *testing.T) {
	var got []Transition
	e := newRig(2, func(c *EngineConfig) { c.OnTransition = func(tr Transition) { got = append(got, tr) } }).e
	mk := func(block uint64, gap string) model.Snapshot {
		return model.Snapshot{TokenID: "nep141:t.near", BlockHeight: block, BlockTime: t0.Add(time.Duration(block) * time.Second), Gap: gap}
	}
	ctx := context.Background()
	for i, gap := range []string{"-5", "-5", "-9", "0"} {
		if err := e.Process(ctx, []model.Snapshot{mk(uint64(10+i), gap)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var kinds []Kind
	for _, tr := range got {
		kinds = append(kinds, tr.Kind)
		if (tr.Kind == TurnedRed || tr.Kind == IncidentUpdate || tr.Kind == Recovered) && tr.IncidentID != 1 {
			t.Errorf("%s carries incident %d", tr.Kind, tr.IncidentID)
		}
	}
	want := []Kind{TurnedAmber, TurnedRed, IncidentUpdate, Recovered}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
	// A panicking hook must not break processing.
	e2 := newRig(1, func(c *EngineConfig) { c.OnTransition = func(Transition) { panic("boom") } }).e
	if err := e2.Process(ctx, []model.Snapshot{mk(1, "-1")}, nil); err != nil {
		t.Fatalf("panic leaked: %v", err)
	}
}

// Regression (review finding 1, scenario A): K=2, gaps -5 -5 with the alert
// write failing, then +10. Before the outbox the incident was opened before
// the enqueue failed and stayed open forever after the amber -> green
// FalseAlarmCleared. Now the failed round rolls back as a whole.
func TestEngineAtomicOutboxFailureAmberThenGreen(t *testing.T) {
	ctx := context.Background()
	r := newRig(2)
	_ = r.e.Process(ctx, []model.Snapshot{snap(1, "-5")}, nil)
	r.fault.outboxErr = errors.New("outbox down")
	if err := r.e.Process(ctx, []model.Snapshot{snap(2, "-5")}, nil); err == nil {
		t.Fatal("want error")
	}
	if n := len(r.mem.Incidents()); n != 0 {
		t.Fatalf("incident survived a rolled-back round: %d", n)
	}
	if s := r.mem.State(tok); s.Status != model.StatusAmber || s.LastBlock != 1 || s.OpenIncidentID != 0 {
		t.Fatalf("state must stay at the previous round: %+v", s)
	}
	r.fault.outboxErr = nil
	if err := r.e.Process(ctx, []model.Snapshot{snap(3, "10")}, nil); err != nil {
		t.Fatal(err)
	}
	if s := r.mem.State(tok); s.Status != model.StatusGreen || s.OpenIncidentID != 0 {
		t.Fatalf("state: %+v", s)
	}
	for _, inc := range r.mem.Incidents() {
		if inc.ClosedAt == nil {
			t.Fatalf("incident %d left open", inc.ID)
		}
	}
	if got := r.drain(); len(got) != 0 {
		t.Fatalf("no red was ever committed, yet alerts: %+v", got)
	}
}

// Regression (finding 1, scenario B): red, then +10 with the alert write
// failing, then K negatives. Before, the incident was closed but the state
// stayed red, so the second deficit never opened an incident or alerted.
func TestEngineAtomicOutboxFailureRedThenRecoverThenDeficit(t *testing.T) {
	ctx := context.Background()
	r := newRig(2)
	// Order matters: feeding block 2 before block 1 marks block 1 stale, so
	// the map iteration this test used to rely on was nondeterministic.
	for _, tc := range []struct {
		block uint64
		gap   string
	}{{1, "-5"}, {2, "-5"}} {
		if err := r.e.Process(ctx, []model.Snapshot{snap(tc.block, tc.gap)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	r.fault.outboxErr = errors.New("outbox down")
	if err := r.e.Process(ctx, []model.Snapshot{snap(3, "10")}, nil); err == nil {
		t.Fatal("want error")
	}
	if s := r.mem.State(tok); s.Status != model.StatusRed || s.OpenIncidentID != 1 {
		t.Fatalf("rolled back state: %+v", s)
	}
	if inc := r.mem.Incidents()[0]; inc.ClosedAt != nil {
		t.Fatalf("close must roll back with the failed round: %+v", inc)
	}
	r.fault.outboxErr = nil
	// The round is retried on the next snapshot, which is still recovered.
	if err := r.e.Process(ctx, []model.Snapshot{snap(4, "10")}, nil); err != nil {
		t.Fatal(err)
	}
	for b := uint64(5); b <= 6; b++ {
		if err := r.e.Process(ctx, []model.Snapshot{snap(b, "-3")}, nil); err != nil {
			t.Fatal(err)
		}
	}
	got := r.drain()
	var kinds []Kind
	for _, a := range got {
		kinds = append(kinds, a.Kind)
	}
	want := []Kind{TurnedRed, Recovered, TurnedRed}
	if !equalKinds(kinds, want) {
		t.Fatalf("alerts %v, want %v", kinds, want)
	}
	if got[0].IncidentID != 1 || got[1].IncidentID != 1 || got[2].IncidentID != 2 {
		t.Fatalf("incident ids: %+v", got)
	}
	incs := r.mem.Incidents()
	if len(incs) != 2 || incs[0].ClosedAt == nil || incs[1].ClosedAt != nil {
		t.Fatalf("incidents: %+v", incs)
	}
}

func TestEngineStateSaveFailureRollsBackIncident(t *testing.T) {
	ctx := context.Background()
	r := newRig(1)
	r.fault.saveErr = errors.New("state db down")
	if err := r.e.Process(ctx, []model.Snapshot{snap(1, "-5")}, nil); err == nil {
		t.Fatal("want error")
	}
	if len(r.mem.Incidents()) != 0 || len(r.mem.OutboxAlerts()) != 0 {
		t.Fatalf("partial writes survived: %+v %+v", r.mem.Incidents(), r.mem.OutboxAlerts())
	}
	r.fault.saveErr = nil
	if err := r.e.Process(ctx, []model.Snapshot{snap(2, "-5")}, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.drain(); len(got) != 1 || got[0].Kind != TurnedRed || got[0].IncidentID != 1 {
		t.Fatalf("%+v", got)
	}
}

// failingQueue fails Enqueue while down is set.
type failingQueue struct {
	*MemoryQueue
	down bool
}

func (f *failingQueue) Enqueue(ctx context.Context, a Alert) error {
	if f.down {
		return errors.New("redis down")
	}
	return f.MemoryQueue.Enqueue(ctx, a)
}

func TestRelayDeliversOutboxOnceQueueRecovers(t *testing.T) {
	ctx := context.Background()
	r := newRig(1)
	fq := &failingQueue{MemoryQueue: NewMemoryQueue(), down: true}
	relay := NewRelay(RelayConfig{BatchSize: 2}, r.mem, fq)
	for b, g := range []string{"-5", "-9", "10"} {
		if err := r.e.Process(ctx, []model.Snapshot{snap(uint64(b+1), g)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := relay.Flush(ctx); err == nil || n != 0 {
		t.Fatalf("flush with the queue down: %d %v", n, err)
	}
	if p, _ := r.mem.PendingOutbox(ctx, 10); len(p) != 3 {
		t.Fatalf("outbox must keep all 3 alerts: %d", len(p))
	}
	fq.down = false
	if n, err := relay.Flush(ctx); err != nil || n != 3 {
		t.Fatalf("flush after recovery: %d %v", n, err)
	}
	if n, _ := relay.Flush(ctx); n != 0 {
		t.Fatalf("second flush moved %d", n)
	}
	got := drain(fq.MemoryQueue)
	if len(got) != 3 || got[0].Kind != TurnedRed || got[1].Kind != IncidentUpdate || got[2].Kind != Recovered {
		t.Fatalf("order/content: %+v", got)
	}
}

func drain(q *MemoryQueue) []Alert {
	var out []Alert
	for {
		d, _ := q.Dequeue(context.Background(), 0)
		if d == nil {
			return out
		}
		out = append(out, d.Alert)
	}
}

// The defensive invariant: a token that goes green while an incident is
// recorded as open closes it, even if the status was not red.
func TestEngineGreenClosesStrayOpenIncident(t *testing.T) {
	ctx := context.Background()
	r := newRig(3)
	id, _ := r.mem.OpenIncident(ctx, model.Incident{TokenID: tok, StartBlock: 1, RedBlock: 2, OpenedAt: t0, MaxDeficit: "5", MaxDeficitBlock: 2})
	_ = r.mem.SaveStates(ctx, []model.TokenState{{TokenID: tok, Status: model.StatusAmber, NegativeStreak: 1, FirstNegativeBlock: 3, LastBlock: 3, OpenIncidentID: id}})
	if err := r.e.Process(ctx, []model.Snapshot{snap(4, "10")}, nil); err != nil {
		t.Fatal(err)
	}
	if inc := r.mem.Incidents()[0]; inc.ClosedAt == nil {
		t.Fatalf("stray incident still open: %+v", inc)
	}
	if s := r.mem.State(tok); s.OpenIncidentID != 0 || s.Status != model.StatusGreen {
		t.Fatalf("%+v", s)
	}
	if got := r.drain(); len(got) != 1 || got[0].Kind != Recovered {
		t.Fatalf("%+v", got)
	}
}

func TestStepGreenWithOpenIncidentRecovers(t *testing.T) {
	prev := model.TokenState{TokenID: tok, Status: model.StatusAmber, NegativeStreak: 1, LastBlock: 3, OpenIncidentID: 9}
	next, trs := Step(prev, snap(4, "1"), Config{})
	if len(trs) != 1 || trs[0].Kind != Recovered || trs[0].IncidentID != 9 || next.OpenIncidentID != 0 {
		t.Fatalf("%+v %+v", next, trs)
	}
}

// Findings 5/6: a snapshot that does not advance the block in a new round is
// a failed read; StaleSince is set once and cleared by the next success.
func TestEngineBlockNotAdvancingMarksStale(t *testing.T) {
	ctx := context.Background()
	clock := t0
	r := newRig(3, func(c *EngineConfig) { c.Now = func() time.Time { return clock } })
	_ = r.e.Process(ctx, []model.Snapshot{snap(10, "5")}, nil)
	if s := r.mem.State(tok); s.Stale || !s.StaleSince.IsZero() {
		t.Fatalf("%+v", s)
	}
	clock = t0.Add(time.Minute)
	_ = r.e.Process(ctx, []model.Snapshot{snap(10, "5")}, nil) // same block, new round
	s := r.mem.State(tok)
	if !s.Stale || !s.StaleSince.Equal(clock) || s.Status != model.StatusGreen || !strings.Contains(s.LastError, "block not advancing") {
		t.Fatalf("%+v", s)
	}
	first := clock
	clock = t0.Add(5 * time.Minute)
	_ = r.e.Process(ctx, []model.Snapshot{snap(9, "5")}, nil)
	if s := r.mem.State(tok); !s.StaleSince.Equal(first) {
		t.Fatalf("StaleSince must keep the first failure: %+v", s)
	}
	_ = r.e.Process(ctx, []model.Snapshot{snap(11, "5")}, nil)
	if s := r.mem.State(tok); s.Stale || !s.StaleSince.IsZero() || s.LastBlock != 11 {
		t.Fatalf("success must clear: %+v", s)
	}
	// Two snapshots of one token inside ONE round are a duplicate, not a stall.
	_ = r.e.Process(ctx, []model.Snapshot{snap(12, "5"), snap(12, "5")}, nil)
	if s := r.mem.State(tok); s.Stale || s.LastBlock != 12 {
		t.Fatalf("in-round duplicate treated as stall: %+v", s)
	}
}

func TestStepErrorSetsStaleSinceOnce(t *testing.T) {
	st := StepError(model.TokenState{TokenID: tok, Status: model.StatusGreen, LastBlock: 5}, errors.New("x"), Config{}, t0)
	st = StepError(st, errors.New("y"), Config{}, t0.Add(time.Hour))
	if !st.StaleSince.Equal(t0) || st.LastError != "y" {
		t.Fatalf("%+v", st)
	}
}
