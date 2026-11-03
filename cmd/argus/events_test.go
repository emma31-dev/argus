package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/store"
)

type fakeAttacher struct {
	mu    sync.Mutex
	calls [][4]uint64 // incident, from, to, 0
	err   error
	// failFirst makes the first failFirst calls fail (when err is nil too).
	failFirst int
}

func (f *fakeAttacher) AttachRange(_ context.Context, id int64, _ string, from, to uint64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, [4]uint64{uint64(id), from, to})
	if len(f.calls) <= f.failFirst {
		return 0, errors.New("neardata down")
	}
	return 1, f.err
}

func (f *fakeAttacher) sorted() [][4]uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := append([][4]uint64(nil), f.calls...)
	sort.Slice(c, func(i, j int) bool { return c[i][1] < c[j][1] })
	return c
}

var quietLog = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestIncidentEventsRanges(t *testing.T) {
	fa := &fakeAttacher{}
	ie := newIncidentEvents(context.Background(), fa, 100, quietLog)
	ie.OnTransition(alert.Transition{Kind: alert.TurnedAmber, TokenID: "x", Block: 1000, StartBlock: 1000}) // ignored: no incident
	ie.OnTransition(alert.Transition{Kind: alert.TurnedRed, TokenID: "x", IncidentID: 7, StartBlock: 1000, Block: 1040})
	ie.OnTransition(alert.Transition{Kind: alert.IncidentUpdate, TokenID: "x", IncidentID: 7, StartBlock: 1000, Block: 1100})
	ie.OnTransition(alert.Transition{Kind: alert.Recovered, TokenID: "x", IncidentID: 7, StartBlock: 1000, Block: 1300})
	ie.Wait()
	got := fa.sorted()
	// red: [850,1040] is 191 blocks -> chunks of 100; update: [1041,1100]; recovery tail [1101,1300] -> 2 chunks.
	want := [][4]uint64{{7, 850, 949}, {7, 950, 1040}, {7, 1041, 1100}, {7, 1101, 1200}, {7, 1201, 1300}}
	if len(got) != len(want) {
		t.Fatalf("calls = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls = %v, want %v", got, want)
		}
	}
	if len(ie.inc) != 0 {
		t.Fatalf("incident state not released: %v", ie.inc)
	}
}

func TestIncidentEventsErrorsAreOnlyLogged(t *testing.T) {
	fa := &fakeAttacher{err: errors.New("neardata down")}
	ie := newIncidentEvents(context.Background(), fa, 0, quietLog)
	ie.OnTransition(alert.Transition{Kind: alert.TurnedRed, TokenID: "x", IncidentID: 1, StartBlock: 10, Block: 20})
	ie.Wait()
	if got := fa.sorted(); len(got) != 1 || got[0][1] != 1 || got[0][2] != 20 {
		t.Fatalf("calls = %v", got)
	}
}

// Review finding 8: the attached-up-to block advances only after AttachRange
// succeeds. A failed range is requested again, together with the new blocks,
// by the next transition (before, it was skipped forever).
func TestIncidentEventsFailedRangeIsRetried(t *testing.T) {
	fa := &fakeAttacher{failFirst: 1}
	ie := newIncidentEvents(context.Background(), fa, 1000, quietLog)
	ie.OnTransition(alert.Transition{Kind: alert.TurnedRed, TokenID: "x", IncidentID: 7, StartBlock: 1000, Block: 1040})
	ie.Wait() // [850,1040] fails
	ie.OnTransition(alert.Transition{Kind: alert.IncidentUpdate, TokenID: "x", IncidentID: 7, StartBlock: 1000, Block: 1100})
	ie.Wait()
	ie.OnTransition(alert.Transition{Kind: alert.Recovered, TokenID: "x", IncidentID: 7, StartBlock: 1000, Block: 1200})
	ie.Wait()
	got := fa.sorted()
	want := [][4]uint64{{7, 850, 1040}, {7, 850, 1100}, {7, 1101, 1200}}
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls = %v, want %v", got, want)
		}
	}
	if len(ie.inc) != 0 {
		t.Fatalf("state leaked: %v", ie.inc)
	}
}

// A failed chunk in the middle of a range keeps the confirmed prefix.
func TestIncidentEventsPartialRangeKeepsConfirmedChunks(t *testing.T) {
	fa := &fakeAttacher{failFirst: 0}
	fa2 := &failSecond{fakeAttacher: fa}
	ie := newIncidentEvents(context.Background(), fa2, 100, quietLog)
	ie.OnTransition(alert.Transition{Kind: alert.TurnedRed, TokenID: "x", IncidentID: 1, StartBlock: 1000, Block: 1040}) // [850,949] ok, [950,1040] fails
	ie.Wait()
	ie.OnTransition(alert.Transition{Kind: alert.IncidentUpdate, TokenID: "x", IncidentID: 1, StartBlock: 1000, Block: 1050})
	ie.Wait()
	got := fa.sorted()
	want := [][4]uint64{{1, 850, 949}, {1, 950, 1040}, {1, 950, 1049}, {1, 1050, 1050}}
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls = %v, want %v", got, want)
		}
	}
}

// failSecond fails the second AttachRange call only.
type failSecond struct{ *fakeAttacher }

func (f *failSecond) AttachRange(ctx context.Context, id int64, tok string, from, to uint64) (int, error) {
	n, err := f.fakeAttacher.AttachRange(ctx, id, tok, from, to)
	f.mu.Lock()
	calls := len(f.calls)
	f.mu.Unlock()
	if calls == 2 {
		return 0, errors.New("neardata hiccup")
	}
	return n, err
}

func TestIncidentEventsUnknownIncidentAfterRestart(t *testing.T) {
	fa := &fakeAttacher{}
	ie := newIncidentEvents(context.Background(), fa, 3000, quietLog)
	ie.OnTransition(alert.Transition{Kind: alert.IncidentUpdate, TokenID: "x", IncidentID: 3, StartBlock: 100000, Block: 100500})
	ie.Wait()
	if got := fa.sorted(); len(got) != 1 || got[0][1] != 99850 || got[0][2] != 100500 {
		t.Fatalf("calls = %v", got)
	}
}

type memKV struct {
	m   map[string]string
	err error
}

func (k *memKV) GetKV(_ context.Context, key string) (string, error) {
	v, ok := k.m[key]
	if !ok {
		return "", store.ErrNotFound
	}
	return v, k.err
}
func (k *memKV) SetKV(_ context.Context, key, v string) error { k.m[key] = v; return nil }

func TestKVCursor(t *testing.T) {
	kv := &memKV{m: map[string]string{}}
	c := newKVCursor(kv, "intents.near")
	if h, err := c.Load(context.Background()); err != nil || h != 0 {
		t.Fatalf("empty cursor = %d %v", h, err)
	}
	if err := c.Save(context.Background(), 12345); err != nil {
		t.Fatal(err)
	}
	if kv.m["events_cursor:intents.near"] != "12345" {
		t.Fatalf("kv = %v", kv.m)
	}
	if h, _ := c.Load(context.Background()); h != 12345 {
		t.Fatalf("load = %d", h)
	}
	kv.m["events_cursor:intents.near"] = "garbage"
	if _, err := c.Load(context.Background()); err == nil {
		t.Fatal("garbage cursor accepted")
	}
}

func TestEventsConfig(t *testing.T) {
	c, err := LoadConfig(env(map[string]string{"ARGUS_NEARDATA_RPS": "0"}), false)
	if err == nil || c.NeardataRPS != 2 {
		t.Fatalf("rps: %v %v", c.NeardataRPS, err)
	}
	c, err = LoadConfig(env(map[string]string{"ARGUS_EVENTS_ENABLED": "true", "ARGUS_NEARDATA_API_KEY": "k"}), false)
	if err != nil || !c.EventsEnabled || c.NeardataURL != "https://mainnet.neardata.xyz" || c.NeardataAPIKey != "k" {
		t.Fatalf("cfg = %+v %v", c, err)
	}
}
