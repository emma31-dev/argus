package events

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emma31-dev/argus/internal/model"
)

// fakeSource serves synthetic blocks: every height has one intents.near
// mt_mint of token "tok-<h%3>" for amount h, except heights in nulls.
type fakeSource struct {
	head   uint64
	nulls  map[uint64]bool
	failAt map[uint64]error
	delay  time.Duration

	mu       sync.Mutex
	calls    []uint64
	inflight int32
	maxIn    int32
}

func (f *fakeSource) LastFinal(context.Context) (uint64, error) { return f.head, nil }

func (f *fakeSource) Block(ctx context.Context, h uint64) (*Block, error) {
	n := atomic.AddInt32(&f.inflight, 1)
	for {
		m := atomic.LoadInt32(&f.maxIn)
		if n <= m || atomic.CompareAndSwapInt32(&f.maxIn, m, n) {
			break
		}
	}
	defer atomic.AddInt32(&f.inflight, -1)
	f.mu.Lock()
	f.calls = append(f.calls, h)
	f.mu.Unlock()
	if f.delay > 0 {
		// later heights return faster to provoke out-of-order completion
		time.Sleep(f.delay - time.Duration(h%4)*f.delay/5)
	}
	if err := f.failAt[h]; err != nil {
		return nil, err
	}
	if f.nulls[h] {
		return nil, nil
	}
	return synthBlock(h, fmt.Sprintf("tok-%d", h%3), "intents.near"), nil
}

func synthBlock(h uint64, token, executor string) *Block {
	b := &Block{}
	b.Block.Header.Height = h
	var ro ReceiptOutcome
	ro.ExecutionOutcome.ID = fmt.Sprintf("r%d", h)
	ro.TxHash = fmt.Sprintf("tx%d", h)
	ro.ExecutionOutcome.Outcome = Outcome{ExecutorID: executor, Status: []byte(`{"SuccessValue":""}`),
		Logs: []string{fmt.Sprintf(`EVENT_JSON:{"standard":"nep245","version":"1.0.0","event":"mt_mint","data":[{"owner_id":"a.near","token_ids":[%q],"amounts":["%d"]}]}`, token, h)}}
	b.Shards = []Shard{{ReceiptExecutionOutcomes: []ReceiptOutcome{ro}}}
	return b
}

func TestBackfillOrderAndConcurrency(t *testing.T) {
	src := &fakeSource{delay: 10 * time.Millisecond, nulls: map[uint64]bool{13: true}}
	var heights []uint64
	var nEvents int
	err := Backfill(context.Background(), src, "", 4, 10, 29, func(h uint64, evs []model.IncidentEvent) error {
		heights = append(heights, h)
		nEvents += len(evs)
		if h == 13 && len(evs) != 0 {
			t.Error("null height has events")
		}
		for _, e := range evs {
			if e.BlockHeight != h {
				t.Errorf("event height %d at %d", e.BlockHeight, h)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(heights) != 20 || nEvents != 19 {
		t.Fatalf("heights %d events %d", len(heights), nEvents)
	}
	for i, h := range heights {
		if h != uint64(10+i) {
			t.Fatalf("out of order: %v", heights)
		}
	}
	if m := atomic.LoadInt32(&src.maxIn); m > 4 || m < 2 {
		t.Fatalf("max in flight = %d, want 2..4", m)
	}
}

func TestBackfillSequentialAndErrors(t *testing.T) {
	src := &fakeSource{failAt: map[uint64]error{5: errors.New("boom")}}
	var seen []uint64
	err := Backfill(context.Background(), src, "", 1, 1, 10, func(h uint64, _ []model.IncidentEvent) error { seen = append(seen, h); return nil })
	if err == nil || !reflect.DeepEqual(seen, []uint64{1, 2, 3, 4}) {
		t.Fatalf("err=%v seen=%v", err, seen)
	}
	if m := atomic.LoadInt32(&src.maxIn); m != 1 {
		t.Fatalf("concurrency 1 had %d in flight", m)
	}
	// fn error stops early
	stop := errors.New("stop")
	err = Backfill(context.Background(), &fakeSource{}, "", 4, 1, 100, func(h uint64, _ []model.IncidentEvent) error {
		if h == 3 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("err=%v", err)
	}
	if err := Backfill(context.Background(), src, "", 4, 9, 3, nil); err != nil {
		t.Fatal("empty range should be a no-op")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Backfill(ctx, &fakeSource{delay: time.Millisecond}, "", 4, 1, 5, func(uint64, []model.IncidentEvent) error { return nil }); err == nil {
		t.Fatal("cancelled backfill should fail")
	}
}

type memCursor struct{ h uint64 }

func (c *memCursor) Load(context.Context) (uint64, error) { return c.h, nil }
func (c *memCursor) Save(_ context.Context, h uint64) error {
	c.h = h
	return nil
}

type recSink struct {
	heights []uint64
	events  int
	failAt  uint64
}

func (s *recSink) HandleBlock(_ context.Context, h uint64, evs []model.IncidentEvent) error {
	if h == s.failAt {
		return errors.New("sink down")
	}
	s.heights = append(s.heights, h)
	s.events += len(evs)
	return nil
}

func TestFollowerTickBoundedCatchUp(t *testing.T) {
	src := &fakeSource{head: 25, nulls: map[uint64]bool{12: true}}
	cur, sink := &memCursor{h: 9}, &recSink{}
	f := &Follower{Client: src, Sink: sink, Cursor: cur, MaxPerTick: 10}
	n, caught, err := f.Tick(context.Background())
	if err != nil || n != 10 || caught || cur.h != 19 {
		t.Fatalf("n=%d caught=%v err=%v cursor=%d", n, caught, err, cur.h)
	}
	n, caught, err = f.Tick(context.Background())
	if err != nil || n != 6 || !caught || cur.h != 25 {
		t.Fatalf("n=%d caught=%v err=%v cursor=%d", n, caught, err, cur.h)
	}
	n, caught, _ = f.Tick(context.Background())
	if n != 0 || !caught {
		t.Fatalf("idle tick n=%d caught=%v", n, caught)
	}
	if len(sink.heights) != 16 || sink.heights[0] != 10 || sink.heights[15] != 25 || sink.events != 15 {
		t.Fatalf("sink %v events=%d", sink.heights, sink.events)
	}
}

func TestFollowerStartsAtHeadAndSinkErrorKeepsCursor(t *testing.T) {
	src := &fakeSource{head: 100}
	cur, sink := &memCursor{}, &recSink{failAt: 102}
	f := &Follower{Client: src, Sink: sink, Cursor: cur}
	n, _, err := f.Tick(context.Background())
	if err != nil || n != 1 || cur.h != 100 || !reflect.DeepEqual(sink.heights, []uint64{100}) {
		t.Fatalf("first tick n=%d err=%v cursor=%d sink=%v", n, err, cur.h, sink.heights)
	}
	src.head = 103
	_, _, err = f.Tick(context.Background())
	if err == nil || cur.h != 101 {
		t.Fatalf("err=%v cursor=%d", err, cur.h)
	}
	sink.failAt = 0
	if _, caught, err := f.Tick(context.Background()); err != nil || !caught || cur.h != 103 {
		t.Fatalf("retry err=%v cursor=%d", err, cur.h)
	}
}

func TestFollowerRunStopsOnCancel(t *testing.T) {
	src := &fakeSource{head: 5}
	f := &Follower{Client: src, Sink: &recSink{}, Cursor: &memCursor{h: 3}, PollInterval: time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := f.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run = %v", err)
	}
	if f.Cursor.(*memCursor).h != 5 {
		t.Fatal("did not catch up")
	}
}

type fakeStore struct {
	got []model.IncidentEvent
	err error
}

func (s *fakeStore) AddIncidentEvents(_ context.Context, evs []model.IncidentEvent) error {
	s.got = append(s.got, evs...)
	return s.err
}

// attachSource: block 50 has a mint of tok-A and tok-B plus a token_diff
// touching tok-A and tok-C, block 51 only tok-B, 52 an intents_executed.
type attachSource struct{}

func (attachSource) LastFinal(context.Context) (uint64, error) { return 100, nil }
func (attachSource) Block(_ context.Context, h uint64) (*Block, error) {
	mk := func(logs ...string) *Block {
		b := &Block{}
		b.Block.Header.Height = h
		var ro ReceiptOutcome
		ro.ExecutionOutcome.ID = fmt.Sprintf("r%d", h)
		ro.ExecutionOutcome.Outcome = Outcome{ExecutorID: "intents.near", Status: []byte(`{"SuccessValue":""}`), Logs: logs}
		b.Shards = []Shard{{ReceiptExecutionOutcomes: []ReceiptOutcome{ro}}}
		return b
	}
	switch h {
	case 50:
		return mk(
			`EVENT_JSON:{"standard":"nep245","version":"1.0.0","event":"mt_mint","data":[{"owner_id":"a","token_ids":["tok-A","tok-B"],"amounts":["1","2"]}]}`,
			`EVENT_JSON:{"standard":"dip4","version":"0.4.3","event":"token_diff","data":[{"account_id":"a","diff":{"tok-A":"-5","tok-C":"5"}}]}`,
			`EVENT_JSON:{"standard":"dip4","version":"0.4.3","event":"token_diff","data":[{"account_id":"a","diff":{"tok-D":"-5","tok-C":"5"}}]}`,
		), nil
	case 51:
		return mk(`EVENT_JSON:{"standard":"nep245","version":"1.0.0","event":"mt_burn","data":[{"owner_id":"a","token_ids":["tok-B"],"amounts":["2"]}]}`), nil
	case 52:
		return mk(`EVENT_JSON:{"standard":"dip4","version":"0.4.3","event":"intents_executed","data":[{"account_id":"a","nonce":"n"}]}`), nil
	case 53:
		return mk(`EVENT_JSON:{"standard":"dip4","version":"0.4.3","event":"mt_withdraw","data":[{"account_id":"a","token":"mt.near","receiver_id":"b","token_ids":["z"],"amounts":["1"]}]}`), nil
	}
	return nil, nil
}

func TestAttachRangeFilters(t *testing.T) {
	st := &fakeStore{}
	a := &Attacher{Source: attachSource{}, Store: st}
	n, err := a.AttachRange(context.Background(), 42, "tok-A", 48, 55)
	if err != nil {
		t.Fatal(err)
	}
	// mint tok-A, token_diff tok-A, token_diff tok-C (same event mentions tok-A)
	if n != 3 || len(st.got) != 3 {
		t.Fatalf("n=%d got=%+v", n, st.got)
	}
	for _, e := range st.got {
		if e.IncidentID != 42 || e.BlockHeight != 50 {
			t.Errorf("bad row %+v", e)
		}
	}
	if st.got[0].Event != "mt_mint" || st.got[0].TokenID != "tok-A" || st.got[1].TokenID != "tok-A" || st.got[2].TokenID != "tok-C" {
		t.Errorf("rows %+v", st.got)
	}
	// tok-B: mint at 50 and burn at 51 only
	st2 := &fakeStore{}
	n, _ = (&Attacher{Source: attachSource{}, Store: st2}).AttachRange(context.Background(), 1, "tok-B", 48, 55)
	if n != 2 || st2.got[0].Event != "mt_mint" || st2.got[1].Event != "mt_burn" {
		t.Errorf("tok-B %+v", st2.got)
	}
	// nothing relevant: store not called
	st3 := &fakeStore{err: errors.New("must not be called")}
	if n, err := (&Attacher{Source: attachSource{}, Store: st3}).AttachRange(context.Background(), 1, "tok-Z", 48, 55); n != 0 || err != nil {
		t.Errorf("n=%d err=%v", n, err)
	}
}

func TestAttachRangeErrors(t *testing.T) {
	a := &Attacher{Source: attachSource{}, Store: &fakeStore{err: errors.New("db down")}, MaxBlocks: 10}
	if _, err := a.AttachRange(context.Background(), 1, "tok-A", 50, 50); err == nil {
		t.Error("store error should propagate")
	}
	if _, err := a.AttachRange(context.Background(), 1, "tok-A", 1, 100); !errors.Is(err, ErrRangeTooLarge) {
		t.Errorf("err=%v", err)
	}
	if n, err := a.AttachRange(context.Background(), 1, "tok-A", 9, 3); n != 0 || err != nil {
		t.Error("inverted range")
	}
}
