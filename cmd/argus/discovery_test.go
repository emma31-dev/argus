package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/tokens"
)

// scriptDisc returns the scripted results in order (the last one repeats).
type scriptDisc struct {
	mu    sync.Mutex
	steps []discStep
	calls int
}

type discStep struct {
	ids    []string
	source string
	err    error
}

func (d *scriptDisc) Discover(context.Context) ([]model.Token, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := min(d.calls, len(d.steps)-1)
	d.calls++
	st := d.steps[i]
	if st.err != nil {
		return nil, "", st.err
	}
	var out []model.Token
	for _, id := range st.ids {
		t, err := tokens.Parse(id)
		if err != nil {
			panic(err)
		}
		t.Symbol, t.Decimals, t.Source = id, 6, st.source
		out = append(out, t)
	}
	return out, st.source, nil
}

func (d *scriptDisc) Calls() int { d.mu.Lock(); defer d.mu.Unlock(); return d.calls }

func tokIDs(n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("nep141:t%d.near", i))
	}
	return out
}

func idSet(ts []model.Token) map[string]bool {
	m := map[string]bool{}
	for _, t := range ts {
		m[t.TokenID] = true
	}
	return m
}

// Review finding 2: a transient mt_tokens failure makes discovery fall
// through to the smaller 1Click list. That must neither deactivate the other
// tokens nor count as a complete discovery.
func TestDiscoveryDowngradeKeepsActiveTokens(t *testing.T) {
	repo := testPool(t)
	ctx := context.Background()
	disc := &scriptDisc{steps: []discStep{
		{ids: tokIDs(6), source: tokens.SourceEnumeration},
		{ids: tokIDs(2), source: tokens.SourceOneClick},
		{ids: tokIDs(6), source: tokens.SourceEnumeration},
		{ids: tokIDs(4), source: tokens.SourceEnumeration},
	}}
	app := NewApp(Config{IntentsAccount: "intents.near", DiscoveryInterval: time.Hour},
		slog.New(slog.NewTextHandler(io.Discard, nil)), repo, newRPC(Config{}), alert.NewMemoryQueue())
	app.disc = disc

	if degraded, err := app.discover(ctx); err != nil || degraded {
		t.Fatalf("first: %v %v", degraded, err)
	}
	if len(app.Tokens()) != 6 {
		t.Fatalf("tokens %d", len(app.Tokens()))
	}

	degraded, err := app.discover(ctx)
	if err != nil || !degraded {
		t.Fatalf("1click after enumeration must be degraded: %v %v", degraded, err)
	}
	if got := len(app.Tokens()); got != 6 {
		t.Fatalf("in-memory list shrank to %d", got)
	}
	active, _ := repo.ListTokens(ctx)
	if len(active) != 6 {
		t.Fatalf("a downgraded discovery deactivated tokens: %d active", len(active))
	}
	for _, tk := range active {
		if tk.Source != tokens.SourceEnumeration {
			t.Fatalf("stored source overwritten by the degraded pass: %+v", tk)
		}
	}

	if degraded, err := app.discover(ctx); err != nil || degraded {
		t.Fatalf("recovery: %v %v", degraded, err)
	}
	// A genuine shrink reported by the preferred source still deactivates.
	if degraded, err := app.discover(ctx); err != nil || degraded {
		t.Fatalf("shrink: %v %v", degraded, err)
	}
	active, _ = repo.ListTokens(ctx)
	if len(active) != 4 || len(app.Tokens()) != 4 {
		t.Fatalf("active %d in-memory %d, want 4", len(active), len(app.Tokens()))
	}
}

func TestDiscoveryDowngradeNewTokensAreAdded(t *testing.T) {
	repo := testPool(t)
	ctx := context.Background()
	disc := &scriptDisc{steps: []discStep{
		{ids: tokIDs(3), source: tokens.SourceEnumeration},
		{ids: append(tokIDs(1), "nep141:brand-new.near"), source: tokens.SourceOneClick},
	}}
	app := NewApp(Config{IntentsAccount: "intents.near", DiscoveryInterval: time.Hour},
		slog.New(slog.NewTextHandler(io.Discard, nil)), repo, newRPC(Config{}), alert.NewMemoryQueue())
	app.disc = disc
	_, _ = app.discover(ctx)
	if degraded, err := app.discover(ctx); err != nil || !degraded {
		t.Fatalf("%v %v", degraded, err)
	}
	got := idSet(app.Tokens())
	if len(got) != 4 || !got["nep141:brand-new.near"] || !got["nep141:t2.near"] {
		t.Fatalf("union wrong: %v", got)
	}
}

// A token with an open incident survives a complete discovery that drops it.
func TestDiscoveryNeverDeactivatesTokenWithOpenIncident(t *testing.T) {
	repo := testPool(t)
	ctx := context.Background()
	disc := &scriptDisc{steps: []discStep{
		{ids: tokIDs(3), source: tokens.SourceEnumeration},
		{ids: tokIDs(1), source: tokens.SourceEnumeration},
	}}
	app := NewApp(Config{IntentsAccount: "intents.near", DiscoveryInterval: time.Hour},
		slog.New(slog.NewTextHandler(io.Discard, nil)), repo, newRPC(Config{}), alert.NewMemoryQueue())
	app.disc = disc
	_, _ = app.discover(ctx)
	id, err := repo.OpenIncident(ctx, model.Incident{TokenID: "nep141:t2.near", StartBlock: 1, RedBlock: 2, MaxDeficit: "1", MaxDeficitBlock: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveStates(ctx, []model.TokenState{{TokenID: "nep141:t2.near", Status: model.StatusRed, OpenIncidentID: id}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.discover(ctx); err != nil {
		t.Fatal(err)
	}
	got := idSet(app.Tokens())
	if len(got) != 2 || !got["nep141:t0.near"] || !got["nep141:t2.near"] {
		t.Fatalf("tokens after discovery: %v", got)
	}
}

// The loop retries a degraded pass after degradedBackoff, not after the
// discovery interval, until the preferred source answers again.
func TestDiscoveryLoopRetriesDegradedUntilPreferredReturns(t *testing.T) {
	repo := testPool(t)
	disc := &scriptDisc{steps: []discStep{
		{ids: tokIDs(5), source: tokens.SourceEnumeration},
		{ids: tokIDs(1), source: tokens.SourceOneClick},
		{ids: tokIDs(1), source: tokens.SourceOneClick},
		{ids: tokIDs(5), source: tokens.SourceEnumeration},
	}}
	app := NewApp(Config{IntentsAccount: "intents.near", DiscoveryInterval: time.Hour},
		slog.New(slog.NewTextHandler(io.Discard, nil)), repo, newRPC(Config{}), alert.NewMemoryQueue())
	app.disc = disc
	app.degradedBackoff = 10 * time.Millisecond
	if _, err := app.discover(context.Background()); err != nil { // the last good pass
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { app.discoveryLoop(ctx); close(done) }()
	deadline := time.Now().Add(10 * time.Second)
	for disc.Calls() < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if disc.Calls() < 4 {
		t.Fatalf("degraded discovery was not retried: %d calls", disc.Calls())
	}
	if n := len(app.Tokens()); n != 5 {
		t.Fatalf("tokens = %d", n)
	}
}

func TestSourceRank(t *testing.T) {
	if !(tokens.SourceRank(tokens.SourceEnumeration) > tokens.SourceRank(tokens.SourceOneClick) &&
		tokens.SourceRank(tokens.SourceOneClick) > tokens.SourceRank(tokens.SourceConfig) &&
		tokens.SourceRank(tokens.SourceConfig) > tokens.SourceRank("")) {
		t.Fatal("rank order")
	}
}
