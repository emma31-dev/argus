package alert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emma31-dev/argus/internal/model"
)

type fakeStates struct {
	m       map[string]model.TokenState
	saveErr error
}

func (f *fakeStates) LoadStates(_ context.Context, ids []string) (map[string]model.TokenState, error) {
	out := map[string]model.TokenState{}
	for _, id := range ids {
		if s, ok := f.m[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

func (f *fakeStates) SaveStates(_ context.Context, ss []model.TokenState) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	for _, s := range ss {
		f.m[s.TokenID] = s
	}
	return nil
}

type fakeIncidents struct {
	next    int64
	m       map[int64]*model.Incident
	openErr error
}

func (f *fakeIncidents) OpenIncident(_ context.Context, inc model.Incident) (int64, error) {
	if f.openErr != nil {
		return 0, f.openErr
	}
	for id, e := range f.m { // idempotent per token
		if e.TokenID == inc.TokenID && e.ClosedAt == nil {
			return id, nil
		}
	}
	f.next++
	inc.ID = f.next
	f.m[inc.ID] = &inc
	return inc.ID, nil
}

func (f *fakeIncidents) UpdateIncident(_ context.Context, id int64, max string, blk uint64) error {
	f.m[id].MaxDeficit, f.m[id].MaxDeficitBlock = max, blk
	return nil
}

func (f *fakeIncidents) CloseIncident(_ context.Context, id int64, end uint64, at time.Time, max string, blk uint64) error {
	i := f.m[id]
	i.EndBlock, i.ClosedAt, i.MaxDeficit, i.MaxDeficitBlock = end, &at, max, blk
	return nil
}

func (f *fakeIncidents) GetIncident(_ context.Context, id int64) (model.Incident, error) {
	if i, ok := f.m[id]; ok {
		return *i, nil
	}
	return model.Incident{}, errors.New("not found")
}

type fakeTokens struct{}

func (fakeTokens) ListTokens(context.Context) ([]model.Token, error) {
	return []model.Token{{TokenID: tok, Symbol: "wNEAR", Decimals: 24}}, nil
}

func newTestEngine(k int) (*Engine, *fakeStates, *fakeIncidents, *MemoryQueue) {
	st := &fakeStates{m: map[string]model.TokenState{}}
	inc := &fakeIncidents{m: map[int64]*model.Incident{}}
	q := NewMemoryQueue()
	e := NewEngine(EngineConfig{Alert: Config{K: k}, DashboardURL: "https://argus.example/", Now: func() time.Time { return t0 }}, st, inc, q, fakeTokens{})
	return e, st, inc, q
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

func TestEngineIncidentLifecycle(t *testing.T) {
	ctx := context.Background()
	e, st, inc, q := newTestEngine(3)
	gaps := []string{"-10", "-20", "-30", "-30", "-50", "100"}
	for i, g := range gaps {
		if err := e.Process(ctx, []model.Snapshot{snap(uint64(200+i), g)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	alerts := drain(q)
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
	i := inc.m[1]
	if i.StartBlock != 200 || i.RedBlock != 202 || i.EndBlock != 205 || i.ClosedAt == nil || i.MaxDeficit != "50" || i.MaxDeficitBlock != 204 {
		t.Errorf("incident: %+v", *i)
	}
	if s := st.m[tok]; s.Status != model.StatusGreen || s.OpenIncidentID != 0 || s.LastBlock != 205 {
		t.Errorf("state: %+v", s)
	}
}

func TestEngineFalseAlarmSendsNothing(t *testing.T) {
	ctx := context.Background()
	e, _, inc, q := newTestEngine(4)
	for i, g := range []string{"-1", "-1", "5"} {
		if err := e.Process(ctx, []model.Snapshot{snap(uint64(300+i), g)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if q.Len() != 0 || len(inc.m) != 0 {
		t.Fatalf("false alarm produced alerts=%d incidents=%d", q.Len(), len(inc.m))
	}
	if e.FalseAlarms() != 1 {
		t.Fatalf("false alarms = %d", e.FalseAlarms())
	}
}

func TestEngineReadErrorsKeepStreak(t *testing.T) {
	ctx := context.Background()
	e, st, _, q := newTestEngine(3)
	_ = e.Process(ctx, []model.Snapshot{snap(1, "-1")}, nil)
	_ = e.Process(ctx, nil, map[string]error{tok: errors.New("rpc down")})
	_ = e.Process(ctx, nil, map[string]error{tok: errors.New("rpc down")})
	s := st.m[tok]
	if s.NegativeStreak != 1 || !s.Stale || s.Status != model.StatusAmber {
		t.Fatalf("%+v", s)
	}
	_ = e.Process(ctx, []model.Snapshot{snap(2, "-1")}, nil)
	_ = e.Process(ctx, []model.Snapshot{snap(3, "-1")}, nil)
	if got := drain(q); len(got) != 1 || got[0].Kind != TurnedRed {
		t.Fatalf("alerts: %+v", got)
	}
	// An unknown token that only errors is stored as unknown.
	_ = e.Process(ctx, nil, map[string]error{"nep141:new.near": errors.New("x")})
	if s := st.m["nep141:new.near"]; s.Status != model.StatusUnknown || !s.Stale {
		t.Fatalf("%+v", s)
	}
}

func TestEngineOpenFailureRetriesWithoutLosingTransition(t *testing.T) {
	ctx := context.Background()
	e, st, inc, q := newTestEngine(2)
	_ = e.Process(ctx, []model.Snapshot{snap(1, "-1")}, nil)
	inc.openErr = errors.New("db down")
	if err := e.Process(ctx, []model.Snapshot{snap(2, "-1")}, nil); err == nil {
		t.Fatal("want error")
	}
	if st.m[tok].LastBlock != 1 || q.Len() != 0 {
		t.Fatalf("state must not advance on failure: %+v", st.m[tok])
	}
	inc.openErr = nil
	if err := e.Process(ctx, []model.Snapshot{snap(3, "-1")}, nil); err != nil {
		t.Fatal(err)
	}
	if got := drain(q); len(got) != 1 || got[0].Kind != TurnedRed || st.m[tok].OpenIncidentID != 1 {
		t.Fatalf("%+v %+v", got, st.m[tok])
	}
}

func TestEngineMultipleTokensIndependent(t *testing.T) {
	ctx := context.Background()
	e, st, _, _ := newTestEngine(2)
	a := snap(10, "-1")
	b := snap(10, "5")
	b.TokenID = "nep141:b.near"
	if err := e.Process(ctx, []model.Snapshot{a, b}, nil); err != nil {
		t.Fatal(err)
	}
	if st.m[tok].Status != model.StatusAmber || st.m["nep141:b.near"].Status != model.StatusGreen {
		t.Fatalf("%+v", st.m)
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
	e := NewEngine(EngineConfig{DashboardURL: "https://argus.example/", LinkFormat: DashboardLinkFormat}, nil, nil, nil, nil)
	got := e.tokenLink("nep141:wrap.near")
	if want := "https://argus.example/token/?id=nep141%3Awrap.near"; got != want {
		t.Fatalf("link = %q, want %q", got, want)
	}
}

func TestEngineOnTransitionHook(t *testing.T) {
	st := &fakeStates{m: map[string]model.TokenState{}}
	inc := &fakeIncidents{m: map[int64]*model.Incident{}}
	q := NewMemoryQueue()
	var got []Transition
	e := NewEngine(EngineConfig{Alert: Config{K: 2}, OnTransition: func(tr Transition) { got = append(got, tr) }}, st, inc, q, fakeTokens{})
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
	e2 := NewEngine(EngineConfig{Alert: Config{K: 1}, OnTransition: func(Transition) { panic("boom") }},
		&fakeStates{m: map[string]model.TokenState{}}, &fakeIncidents{m: map[int64]*model.Incident{}}, NewMemoryQueue(), fakeTokens{})
	if err := e2.Process(ctx, []model.Snapshot{mk(1, "-1")}, nil); err != nil {
		t.Fatalf("panic leaked: %v", err)
	}
}
