package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/rpc"
)

type call struct {
	provider, account, method string
	args                      any
	height                    uint64
}

// fakeCaller is an in-memory Caller. Behaviour is injected per test.
type fakeCaller struct {
	providers []string
	heights   map[string]uint64 // per-provider latest block

	// handle answers a view call; nil result with nil error is a bug in the test.
	handle func(c call) ([]byte, error)

	mu       sync.Mutex
	calls    []call
	inflight atomic.Int32
	maxSeen  atomic.Int32
	delay    time.Duration
}

func (f *fakeCaller) Providers() []string { return f.providers }

func (f *fakeCaller) LatestFinalBlock(context.Context) (rpc.Block, string, error) {
	p := f.providers[0]
	return rpc.Block{Height: f.heights[p], Hash: "hash-" + p, Time: time.Unix(1790000000, 0).UTC()}, p, nil
}

func (f *fakeCaller) LatestFinalBlockOn(_ context.Context, p string) (rpc.Block, error) {
	return rpc.Block{Height: f.heights[p], Hash: "hash-" + p, Time: time.Unix(1790000000, 0).UTC()}, nil
}

func (f *fakeCaller) CallFunction(_ context.Context, provider, account, method string, args any, h uint64) ([]byte, error) {
	n := f.inflight.Add(1)
	defer f.inflight.Add(-1)
	for {
		m := f.maxSeen.Load()
		if n <= m || f.maxSeen.CompareAndSwap(m, n) {
			break
		}
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	c := call{provider, account, method, args, h}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	return f.handle(c)
}

func (f *fakeCaller) callsTo(method string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

func mkTokens(n int) []model.Token {
	out := make([]model.Token, n)
	for i := range out {
		id := fmt.Sprintf("tok%d.near", i)
		out[i] = model.Token{TokenID: "nep141:" + id, Standard: model.StandardNEP141, Contract: id}
	}
	return out
}

// supplyOf is the synthetic owed for token index i; heldOf the held.
func supplyOf(id string) string { return "1000" + id[len("nep141:tok"):len(id)-len(".near")] }

func okHandler(held func(contract string) (string, error)) func(call) ([]byte, error) {
	return func(c call) ([]byte, error) {
		switch c.method {
		case "mt_batch_supply":
			ids := c.args.(map[string]any)["token_ids"].([]string)
			vals := make([]*string, len(ids))
			for i, id := range ids {
				s := supplyOf(id)
				vals[i] = &s
			}
			return json.Marshal(vals)
		case "ft_balance_of":
			h, err := held(c.account)
			if err != nil {
				return nil, err
			}
			return json.Marshal(h)
		}
		return nil, fmt.Errorf("unexpected method %s", c.method)
	}
}

func newFake(handle func(call) ([]byte, error)) *fakeCaller {
	return &fakeCaller{
		providers: []string{"p0", "p1", "p2"},
		heights:   map[string]uint64{"p0": 100, "p1": 101, "p2": 102},
		handle:    handle,
	}
}

func TestRoundBatching(t *testing.T) {
	for _, tc := range []struct {
		n      int
		chunks []int
	}{{0, nil}, {1, []int{1}}, {50, []int{50}}, {51, []int{50, 1}}, {120, []int{50, 50, 20}}} {
		t.Run(fmt.Sprint(tc.n), func(t *testing.T) {
			f := newFake(okHandler(func(string) (string, error) { return "2000", nil }))
			r := &Reader{Caller: f, Now: func() time.Time { return time.Unix(5, 0) }}
			res, err := r.Round(context.Background(), mkTokens(tc.n))
			if err != nil {
				t.Fatal(err)
			}
			var got []int
			for _, c := range f.callsTo("mt_batch_supply") {
				got = append(got, len(c.args.(map[string]any)["token_ids"].([]string)))
				if c.account != "intents.near" || c.height != 100 || c.provider != "p0" {
					t.Fatalf("bad call %+v", c)
				}
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.chunks) {
				t.Fatalf("chunks %v want %v", got, tc.chunks)
			}
			if len(res.Snapshots) != tc.n || len(res.Errors) != 0 {
				t.Fatalf("snaps %d errs %v", len(res.Snapshots), res.Errors)
			}
			if tc.n > 0 {
				s := res.Snapshots[0]
				if s.Owed != "10000" || s.Held != "2000" || s.Gap != "-8000" || s.BlockHeight != 100 ||
					s.BlockHash != "hash-p0" || s.Provider != "p0" || s.TakenAt != time.Unix(5, 0).UTC() {
					t.Fatalf("snapshot %+v", s)
				}
			}
		})
	}
}

func TestRoundCustomBatchSize(t *testing.T) {
	f := newFake(okHandler(func(string) (string, error) { return "1", nil }))
	r := &Reader{Caller: f, BatchSize: 7}
	if _, err := r.Round(context.Background(), mkTokens(15)); err != nil {
		t.Fatal(err)
	}
	if n := len(f.callsTo("mt_batch_supply")); n != 3 {
		t.Fatalf("calls %d", n)
	}
}

func TestRoundHugeValues(t *testing.T) {
	huge := "340282366920938463463374607431768211457" // 2^128+1
	f := newFake(func(c call) ([]byte, error) {
		if c.method == "mt_batch_supply" {
			return []byte(`["340282366920938463463374607431768211456"]`), nil
		}
		return json.Marshal(huge)
	})
	res, err := (&Reader{Caller: f}).Round(context.Background(), mkTokens(1))
	if err != nil || len(res.Snapshots) != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	if s := res.Snapshots[0]; s.Gap != "1" || s.Owed != "340282366920938463463374607431768211456" || s.Held != huge {
		t.Fatalf("%+v", s)
	}
}

func TestRoundNullSupplyIsAnErrorNotZero(t *testing.T) {
	f := newFake(func(c call) ([]byte, error) {
		if c.method == "mt_batch_supply" {
			return []byte(`["500", null, "700"]`), nil
		}
		return []byte(`"600"`), nil
	})
	res, err := (&Reader{Caller: f}).Round(context.Background(), mkTokens(3))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) != 2 || len(res.Errors) != 1 || res.Errors[0].TokenID != "nep141:tok1.near" {
		t.Fatalf("%+v", res)
	}
	for _, s := range res.Snapshots {
		if s.TokenID == "nep141:tok1.near" {
			t.Fatal("null-supply token must not produce a snapshot")
		}
	}
	// And the held call for the null token must not even be made.
	for _, c := range f.callsTo("ft_balance_of") {
		if c.account == "tok1.near" {
			t.Fatal("held was read for a token with unknown owed")
		}
	}
}

func TestRoundPerTokenFailuresDoNotFailRound(t *testing.T) {
	f := newFake(okHandler(func(contract string) (string, error) {
		switch contract {
		case "tok1.near":
			return "", &rpc.Error{Kind: rpc.ErrContractPanic, Message: "boom"}
		case "tok2.near":
			return "not-a-number", nil
		}
		return "99999", nil
	}))
	res, err := (&Reader{Caller: f}).Round(context.Background(), append(mkTokens(4), model.Token{
		TokenID: "nep171:nft.near:1", Standard: model.StandardNEP171, Contract: "nft.near", InnerID: "1"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) != 2 || len(res.Errors) != 3 {
		t.Fatalf("snaps %d errs %v", len(res.Snapshots), res.Errors)
	}
	errFor := map[string]error{}
	for _, e := range res.Errors {
		errFor[e.TokenID] = e.Err
	}
	if !errors.Is(errFor["nep141:tok1.near"], rpc.ErrContractPanic) {
		t.Errorf("tok1: %v", errFor["nep141:tok1.near"])
	}
	if !errors.Is(errFor["nep141:tok2.near"], ErrInvalidAmount) {
		t.Errorf("tok2: %v", errFor["nep141:tok2.near"])
	}
	if errFor["nep171:nft.near:1"] == nil {
		t.Error("nft should be an error")
	}
}

func TestRoundOwedUnsupported(t *testing.T) {
	f := newFake(func(c call) ([]byte, error) {
		return nil, &rpc.Error{Kind: rpc.ErrMethodNotFound}
	})
	_, err := (&Reader{Caller: f}).Round(context.Background(), mkTokens(3))
	if !errors.Is(err, ErrOwedUnsupported) {
		t.Fatalf("err %v", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("should stop at first call, got %d", len(f.calls))
	}
}

func TestRoundBatchContractErrorMarksChunkOnly(t *testing.T) {
	f := newFake(func(c call) ([]byte, error) {
		if c.method == "mt_batch_supply" {
			ids := c.args.(map[string]any)["token_ids"].([]string)
			if ids[0] == "nep141:tok0.near" {
				return nil, &rpc.Error{Kind: rpc.ErrContractPanic}
			}
			return []byte(`["1","1"]`), nil
		}
		return []byte(`"1"`), nil
	})
	res, err := (&Reader{Caller: f, BatchSize: 2}).Round(context.Background(), mkTokens(4))
	if err != nil || len(res.Snapshots) != 2 || len(res.Errors) != 2 {
		t.Fatalf("%v snaps=%d errs=%v", err, len(res.Snapshots), res.Errors)
	}
}

func TestRoundMalformedBatchResponse(t *testing.T) {
	f := newFake(func(c call) ([]byte, error) { return []byte(`["1"]`), nil }) // wrong length
	res, err := (&Reader{Caller: f}).Round(context.Background(), mkTokens(3))
	if err != nil || len(res.Snapshots) != 0 || len(res.Errors) != 3 {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestRoundRetriesWholeRoundOnNextProvider(t *testing.T) {
	// p0 serves the supply batch and some held reads, then fails mid-round.
	f := newFake(func(c call) ([]byte, error) {
		if c.provider == "p0" && c.account == "tok3.near" {
			return nil, &rpc.Error{Provider: "p0", Kind: rpc.ErrProviderUnavailable}
		}
		return okHandler(func(string) (string, error) { return "20000", nil })(c)
	})
	res, err := (&Reader{Caller: f, Concurrency: 2}).Round(context.Background(), mkTokens(6))
	if err != nil {
		t.Fatal(err)
	}
	if res.Provider != "p1" || res.Block.Height != 101 || len(res.Snapshots) != 6 {
		t.Fatalf("provider %s height %d snaps %d", res.Provider, res.Block.Height, len(res.Snapshots))
	}
	for _, s := range res.Snapshots {
		if s.Provider != "p1" || s.BlockHeight != 101 || s.BlockHash != "hash-p1" {
			t.Fatalf("mixed provider/block in %+v", s)
		}
	}
	// Every call is pinned to a provider and to that provider's own block.
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		want := map[string]uint64{"p0": 100, "p1": 101}[c.provider]
		if want == 0 || c.height != want {
			t.Fatalf("call %+v inconsistent", c)
		}
	}
}

func TestRoundBatchProviderFailureRetriesNextProvider(t *testing.T) {
	f := newFake(func(c call) ([]byte, error) {
		if c.provider != "p2" {
			return nil, &rpc.Error{Kind: rpc.ErrUnknownBlock}
		}
		return okHandler(func(string) (string, error) { return "5", nil })(c)
	})
	res, err := (&Reader{Caller: f}).Round(context.Background(), mkTokens(2))
	if err != nil || res.Provider != "p2" || len(res.Snapshots) != 2 {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestRoundAllProvidersFail(t *testing.T) {
	f := newFake(func(c call) ([]byte, error) { return nil, &rpc.Error{Kind: rpc.ErrProviderUnavailable} })
	_, err := (&Reader{Caller: f}).Round(context.Background(), mkTokens(2))
	if err == nil || !rpc.IsProviderFailure(err) {
		t.Fatalf("err %v", err)
	}
	// Each provider is tried exactly once.
	if n := len(f.callsTo("mt_batch_supply")); n != 3 {
		t.Fatalf("attempts %d", n)
	}
}

func TestRoundConcurrencyBound(t *testing.T) {
	f := newFake(okHandler(func(string) (string, error) { return "1", nil }))
	f.delay = 3 * time.Millisecond
	res, err := (&Reader{Caller: f, Concurrency: 3}).Round(context.Background(), mkTokens(30))
	if err != nil || len(res.Snapshots) != 30 {
		t.Fatalf("%v", err)
	}
	if m := f.maxSeen.Load(); m > 3 || m < 2 {
		t.Fatalf("max in flight %d, want 2..3", m)
	}
	f2 := newFake(okHandler(func(string) (string, error) { return "1", nil }))
	f2.delay = 3 * time.Millisecond
	if _, err := (&Reader{Caller: f2}).Round(context.Background(), mkTokens(40)); err != nil {
		t.Fatal(err)
	}
	if m := f2.maxSeen.Load(); m > DefaultConcurrency {
		t.Fatalf("default concurrency exceeded: %d", m)
	}
}

func TestRoundContextCanceled(t *testing.T) {
	f := newFake(okHandler(func(string) (string, error) { return "1", nil }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.handle = func(c call) ([]byte, error) { return nil, ctx.Err() }
	if _, err := (&Reader{Caller: f}).Round(ctx, mkTokens(2)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestRoundNEP245Held(t *testing.T) {
	f := newFake(func(c call) ([]byte, error) {
		switch c.method {
		case "mt_batch_supply":
			return []byte(`["10"]`), nil
		case "mt_balance_of":
			a := c.args.(map[string]string)
			if c.account != "mt.near" || a["account_id"] != "intents.near" || a["token_id"] != "56_1" {
				return nil, fmt.Errorf("bad args %v on %s", a, c.account)
			}
			return []byte(`"12"`), nil
		}
		return nil, errors.New("unexpected")
	})
	tok := model.Token{TokenID: "nep245:mt.near:56_1", Standard: model.StandardNEP245, Contract: "mt.near", InnerID: "56_1"}
	res, err := (&Reader{Caller: f}).Round(context.Background(), []model.Token{tok})
	if err != nil || len(res.Snapshots) != 1 || res.Snapshots[0].Gap != "2" {
		t.Fatalf("%v %+v", err, res)
	}
}

func mt245(contract string, n int) []model.Token {
	out := make([]model.Token, n)
	for i := range out {
		inner := fmt.Sprintf("t%d", i)
		out[i] = model.Token{TokenID: "nep245:" + contract + ":" + inner, Standard: model.StandardNEP245, Contract: contract, InnerID: inner}
	}
	return out
}

func mtHandler(batch func(c call) ([]byte, error)) func(call) ([]byte, error) {
	return func(c call) ([]byte, error) {
		switch c.method {
		case "mt_batch_supply":
			ids := c.args.(map[string]any)["token_ids"].([]string)
			vals := make([]string, len(ids))
			for i := range vals {
				vals[i] = "100"
			}
			return json.Marshal(vals)
		case "ft_balance_of", "mt_balance_of":
			return []byte(`"100"`), nil
		case "mt_batch_balance_of":
			return batch(c)
		}
		return nil, fmt.Errorf("unexpected method %s", c.method)
	}
}

func batchEcho(c call) ([]byte, error) {
	ids := c.args.(map[string]any)["token_ids"].([]string)
	vals := make([]string, len(ids))
	for i := range vals {
		vals[i] = "100"
	}
	return json.Marshal(vals)
}

func TestRoundBatchesNEP245HeldPerContract(t *testing.T) {
	toks := append(append(mkTokens(3), mt245("big.near", 120)...), mt245("small.near", 2)...)
	f := newFake(mtHandler(batchEcho))
	res, err := (&Reader{Caller: f}).Round(context.Background(), toks)
	if err != nil || len(res.Snapshots) != len(toks) || len(res.Errors) != 0 {
		t.Fatalf("%v snaps=%d errs=%v", err, len(res.Snapshots), res.Errors)
	}
	sizes := map[string][]int{}
	for _, c := range f.callsTo("mt_batch_balance_of") {
		a := c.args.(map[string]any)
		if a["account_id"] != "intents.near" {
			t.Fatalf("holder %v", a["account_id"])
		}
		sizes[c.account] = append(sizes[c.account], len(a["token_ids"].([]string)))
	}
	slices.SortFunc(sizes["big.near"], func(a, b int) int { return b - a }) // workers finish in any order
	if fmt.Sprint(sizes["big.near"]) != "[50 50 20]" || fmt.Sprint(sizes["small.near"]) != "[2]" {
		t.Fatalf("batch sizes %v", sizes)
	}
	if n := len(f.callsTo("mt_balance_of")); n != 0 {
		t.Fatalf("%d single mt_balance_of calls, want 0", n)
	}
	if n := len(f.callsTo("ft_balance_of")); n != 3 {
		t.Fatalf("%d ft_balance_of calls, want 3", n)
	}
}

func TestRoundBatchedHeldKeepsOrderAndValues(t *testing.T) {
	toks := mt245("mt.near", 3)
	f := newFake(mtHandler(func(c call) ([]byte, error) { return []byte(`["100","90","130"]`), nil }))
	res, err := (&Reader{Caller: f}).Round(context.Background(), toks)
	if err != nil || len(res.Snapshots) != 3 {
		t.Fatalf("%v %+v", err, res)
	}
	gaps := map[string]string{}
	for _, s := range res.Snapshots {
		gaps[s.TokenID] = s.Gap
	}
	if gaps["nep245:mt.near:t0"] != "0" || gaps["nep245:mt.near:t1"] != "-10" || gaps["nep245:mt.near:t2"] != "30" {
		t.Fatalf("gaps %v", gaps)
	}
}

func TestRoundFailedBatchFallsBackToSingleReads(t *testing.T) {
	toks := mt245("mt.near", 4)
	f := newFake(func(c call) ([]byte, error) {
		if c.method == "mt_balance_of" && c.args.(map[string]string)["token_id"] == "t2" {
			return nil, &rpc.Error{Kind: rpc.ErrContractPanic, Message: "bad token"}
		}
		return mtHandler(func(call) ([]byte, error) {
			return nil, &rpc.Error{Kind: rpc.ErrContractPanic, Message: "batch panics"}
		})(c)
	})
	res, err := (&Reader{Caller: f}).Round(context.Background(), toks)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) != 3 || len(res.Errors) != 1 || res.Errors[0].TokenID != "nep245:mt.near:t2" {
		t.Fatalf("snaps=%d errs=%v", len(res.Snapshots), res.Errors)
	}
	if n := len(f.callsTo("mt_balance_of")); n != 4 {
		t.Fatalf("%d single reads, want 4", n)
	}
}

func TestRoundWrongShapeBatchFallsBackToSingleReads(t *testing.T) {
	f := newFake(mtHandler(func(call) ([]byte, error) { return []byte(`["100"]`), nil }))
	res, err := (&Reader{Caller: f}).Round(context.Background(), mt245("mt.near", 3))
	if err != nil || len(res.Snapshots) != 3 || len(res.Errors) != 0 {
		t.Fatalf("%v snaps=%d errs=%v", err, len(res.Snapshots), res.Errors)
	}
	if n := len(f.callsTo("mt_balance_of")); n != 3 {
		t.Fatalf("%d single reads, want 3", n)
	}
}

func TestRoundBatchProviderFailureRetriesWholeRound(t *testing.T) {
	f := newFake(mtHandler(func(c call) ([]byte, error) {
		if c.provider == "p0" {
			return nil, &rpc.Error{Provider: "p0", Kind: rpc.ErrProviderUnavailable}
		}
		return batchEcho(c)
	}))
	res, err := (&Reader{Caller: f}).Round(context.Background(), mt245("mt.near", 5))
	if err != nil || res.Provider != "p1" || len(res.Snapshots) != 5 {
		t.Fatalf("%v provider=%s snaps=%d", err, res.Provider, len(res.Snapshots))
	}
	if n := len(f.callsTo("mt_balance_of")); n != 0 {
		t.Fatalf("provider failure must not fall back to single reads, got %d", n)
	}
}

func rateLimitErr(provider string) error {
	return &rpc.Error{Provider: provider, Method: "query", Kind: rpc.ErrProviderUnavailable, Code: "HTTP_429", Message: "slow down"}
}

func TestRoundRateLimitedTokenIsStaleNotAFailedRound(t *testing.T) {
	f := newFake(func(c call) ([]byte, error) {
		if c.method == "ft_balance_of" && c.account == "tok3.near" {
			return nil, rateLimitErr(c.provider)
		}
		return okHandler(func(string) (string, error) { return "20000", nil })(c)
	})
	res, err := (&Reader{Caller: f, Concurrency: 2}).Round(context.Background(), mkTokens(30))
	if err != nil {
		t.Fatalf("one rate-limited read must not fail the round: %v", err)
	}
	if res.Provider != "p0" || len(res.Snapshots) != 29 || len(res.Errors) != 1 || res.Errors[0].TokenID != "nep141:tok3.near" {
		t.Fatalf("provider=%s snaps=%d errs=%v", res.Provider, len(res.Snapshots), res.Errors)
	}
	if !rpc.IsRateLimited(res.Errors[0].Err) {
		t.Fatalf("error should say why: %v", res.Errors[0].Err)
	}
	for _, c := range f.callsTo("mt_batch_supply") {
		if c.provider != "p0" {
			t.Fatalf("round was retried on %s", c.provider)
		}
	}
}

func TestRoundMostlyRateLimitedProviderIsAbandoned(t *testing.T) {
	f := newFake(func(c call) ([]byte, error) {
		if c.provider == "p0" && c.method == "ft_balance_of" {
			return nil, rateLimitErr("p0")
		}
		return okHandler(func(string) (string, error) { return "20000", nil })(c)
	})
	res, err := (&Reader{Caller: f, Concurrency: 2}).Round(context.Background(), mkTokens(40))
	if err != nil {
		t.Fatal(err)
	}
	if res.Provider != "p1" || len(res.Snapshots) != 40 || len(res.Errors) != 0 {
		t.Fatalf("provider=%s snaps=%d errs=%v", res.Provider, len(res.Snapshots), res.Errors)
	}
}

func TestRoundRateLimitedBatchMarksItsTokensStale(t *testing.T) {
	toks := append(mkTokens(2), mt245("mt.near", 3)...)
	f := newFake(mtHandler(func(c call) ([]byte, error) { return nil, rateLimitErr(c.provider) }))
	res, err := (&Reader{Caller: f}).Round(context.Background(), toks)
	if err != nil || res.Provider != "p0" {
		t.Fatalf("%v provider=%s", err, res.Provider)
	}
	if len(res.Snapshots) != 2 || len(res.Errors) != 3 {
		t.Fatalf("snaps=%d errs=%v", len(res.Snapshots), res.Errors)
	}
	if n := len(f.callsTo("mt_balance_of")); n != 0 {
		t.Fatalf("a rate-limited batch must not fan out into single reads, got %d", n)
	}
}
