package tokens

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/rpc"
)

func TestParseOneClick(t *testing.T) {
	b, err := os.ReadFile("testdata/oneclick.json")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := ParseOneClick(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 3 {
		t.Fatalf("got %d tokens: %+v", len(ts), ts)
	}
	if ts[0].TokenID != "nep141:wrap.near" || ts[0].Symbol != "wNEAR" || ts[0].Decimals != 24 {
		t.Fatalf("%+v", ts[0])
	}
	if ts[1].Decimals != 18 { // decimals given as a string
		t.Fatalf("%+v", ts[1])
	}
	if ts[2].Standard != model.StandardNEP245 || ts[2].InnerID != "56_11111111111111111111" {
		t.Fatalf("%+v", ts[2])
	}
	wrapped := []byte(`{"tokens":[{"assetId":"nep141:wrap.near","decimals":24,"symbol":"wNEAR"}]}`)
	if ts, err := ParseOneClick(wrapped); err != nil || len(ts) != 1 {
		t.Fatalf("wrapped: %v %v", ts, err)
	}
	if _, err := ParseOneClick([]byte(`{"error":"x"}`)); err == nil {
		t.Fatal("expected error for response without tokens")
	}
	if _, err := ParseOneClick([]byte(`not json`)); err == nil {
		t.Fatal("expected error for garbage")
	}
}

func TestOneClickFetch(t *testing.T) {
	b, _ := os.ReadFile("testdata/oneclick.json")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/tokens" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write(b)
	}))
	defer s.Close()
	oc := &OneClick{URL: s.URL + "/v0/tokens"}
	if ts, err := oc.Fetch(context.Background()); err != nil || len(ts) != 3 {
		t.Fatalf("%v %v", ts, err)
	}
	bad := &OneClick{URL: s.URL + "/nope"}
	if _, err := bad.Fetch(context.Background()); err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestParseConfig(t *testing.T) {
	b, _ := os.ReadFile("testdata/tokens_valid.yaml")
	ts, err := ParseConfig(b)
	if err != nil || len(ts) != 2 || ts[1].InnerID != "56_111" || ts[1].Decimals != 18 {
		t.Fatalf("%v %v", ts, err)
	}
	for name, y := range map[string]string{
		"unknown field": "tokens:\n  - {id: 'nep141:a.near', symbol: A, decimals: 1, extra: 1}\n",
		"bad id":        "tokens:\n  - {id: 'foo', symbol: A, decimals: 1}\n",
		"nft":           "tokens:\n  - {id: 'nep171:n.near:1', symbol: A, decimals: 0}\n",
		"duplicate":     "tokens:\n  - {id: 'nep141:a.near', decimals: 1}\n  - {id: 'nep141:a.near', decimals: 1}\n",
		"bad decimals":  "tokens:\n  - {id: 'nep141:a.near', decimals: -1}\n",
	} {
		if _, err := ParseConfig([]byte(y)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

// The committed config must always parse.
func TestCommittedConfig(t *testing.T) {
	ts, err := (&Config{Path: "../../config/tokens.yaml"}).Fetch(context.Background())
	if err != nil || len(ts) < 6 {
		t.Fatalf("%d tokens, err %v", len(ts), err)
	}
}

type fakeSource struct {
	name string
	ts   []model.Token
	err  error
}

func (f fakeSource) Name() string                                 { return f.name }
func (f fakeSource) Fetch(context.Context) ([]model.Token, error) { return f.ts, f.err }

func tok(id, sym string, dec int) model.Token {
	t, err := Parse(id)
	if err != nil {
		panic(err)
	}
	t.Symbol, t.Decimals = sym, dec
	return t
}

func TestChain(t *testing.T) {
	now := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	enum := fakeSource{"enumeration", []model.Token{tok("nep141:a.near", "", 0), tok("nep141:b.near", "", 0), tok("nep141:c.near", "", 0)}, nil}
	one := fakeSource{"1click", []model.Token{tok("nep141:a.near", "A1", 6), tok("nep141:b.near", "B1", 8), tok("nep141:z.near", "Z", 1)}, nil}
	cfg := fakeSource{"config", []model.Token{tok("nep141:b.near", "B-cfg", 9), tok("nep141:q.near", "Q", 2)}, nil}

	t.Run("enumeration with enrichment and config override", func(t *testing.T) {
		ch := &Chain{Enumeration: enum, OneClick: one, Config: cfg, Now: func() time.Time { return now }}
		got, src, err := ch.Discover(context.Background())
		if err != nil || src != "enumeration" || len(got) != 3 {
			t.Fatalf("%v %s %v", got, src, err)
		}
		if got[0].Symbol != "A1" || got[0].Decimals != 6 { // 1click enriches
			t.Fatalf("a: %+v", got[0])
		}
		if got[1].Symbol != "B-cfg" || got[1].Decimals != 9 { // config overrides
			t.Fatalf("b: %+v", got[1])
		}
		if got[2].Symbol != "" || got[2].Source != "enumeration" || !got[2].UpdatedAt.Equal(now) {
			t.Fatalf("c: %+v", got[2])
		}
	})
	t.Run("enumeration unsupported falls to 1click", func(t *testing.T) {
		ch := &Chain{Enumeration: fakeSource{"enumeration", nil, fmt.Errorf("x: %w", ErrNotSupported)}, OneClick: one, Config: cfg}
		got, src, err := ch.Discover(context.Background())
		if err != nil || src != "1click" || len(got) != 3 {
			t.Fatalf("%v %s %v", got, src, err)
		}
		for _, g := range got {
			if g.TokenID == "nep141:b.near" && g.Symbol != "B-cfg" {
				t.Fatalf("config override missing: %+v", g)
			}
		}
	})
	t.Run("everything else fails: config", func(t *testing.T) {
		ch := &Chain{
			Enumeration: fakeSource{"enumeration", nil, ErrNotSupported},
			OneClick:    fakeSource{"1click", nil, errors.New("boom")},
			Config:      cfg,
		}
		got, src, err := ch.Discover(context.Background())
		if err != nil || src != "config" || len(got) != 2 {
			t.Fatalf("%v %s %v", got, src, err)
		}
	})
	t.Run("nothing works", func(t *testing.T) {
		ch := &Chain{OneClick: fakeSource{"1click", nil, errors.New("boom")}}
		if _, _, err := ch.Discover(context.Background()); err == nil {
			t.Fatal("expected error")
		}
	})
}

type fakeCaller struct {
	pages  map[int][]string
	err    error
	args   []map[string]any
	method string
}

func (f *fakeCaller) LatestFinalBlock(context.Context) (rpc.Block, string, error) {
	return rpc.Block{Height: 100}, "p1", nil
}

func (f *fakeCaller) CallFunction(_ context.Context, provider, account, method string, args any, h uint64) ([]byte, error) {
	f.method = method
	if f.err != nil {
		return nil, f.err
	}
	if provider != "p1" || h != 100 || account != "intents.near" {
		return nil, fmt.Errorf("unexpected call %s %s %d", provider, account, h)
	}
	a := args.(map[string]any)
	f.args = append(f.args, a)
	var from int
	fmt.Sscan(a["from_index"].(string), &from)
	var items []map[string]any
	for _, id := range f.pages[from] {
		items = append(items, map[string]any{"token_id": id})
	}
	return json.Marshal(items)
}

func TestEnumeration(t *testing.T) {
	fc := &fakeCaller{pages: map[int][]string{
		0: {"nep141:a.near", "nep141:b.near"},
		2: {"nep245:mt.near:1", "nep171:nft.near:1"}, // nft skipped
		4: {"imt:m.near:x"},                          // unsupported skipped
	}}
	e := &Enumeration{Caller: fc, PageSize: 2}
	ts, err := e.Fetch(context.Background())
	if err != nil || len(ts) != 3 {
		t.Fatalf("%v %v", ts, err)
	}
	// Three pages with data, then the empty page that ends the loop.
	if len(fc.args) != 4 || fc.args[1]["from_index"] != "2" || fc.args[1]["limit"] != 2 || fc.args[3]["from_index"] != "5" {
		t.Fatalf("args %v", fc.args)
	}
	if ts[2].Source != "enumeration" {
		t.Fatalf("%+v", ts[2])
	}
}

func TestEnumerationDefaultPage(t *testing.T) {
	fc := &fakeCaller{pages: map[int][]string{0: {"nep141:a.near"}}}
	if _, err := (&Enumeration{Caller: fc}).Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fc.args[0]["limit"] != 5000 {
		t.Fatalf("default limit = %v", fc.args[0]["limit"])
	}
}

// A provider that caps limit below what was asked returns short pages while
// more remain. Every page must still be read.
func TestEnumerationSurvivesCappedPages(t *testing.T) {
	fc := &fakeCaller{pages: map[int][]string{
		0: {"nep141:a.near", "nep141:b.near"},
		2: {"nep141:c.near", "nep141:d.near"},
		4: {"nep245:mt.near:1"},
	}}
	ts, err := (&Enumeration{Caller: fc, PageSize: 100}).Fetch(context.Background())
	if err != nil || len(ts) != 5 {
		t.Fatalf("got %d tokens, err %v", len(ts), err)
	}
}

func TestEnumerationNotSupported(t *testing.T) {
	fc := &fakeCaller{err: fmt.Errorf("wrapped: %w", rpc.ErrMethodNotFound)}
	_, err := (&Enumeration{Caller: fc}).Fetch(context.Background())
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("err %v", err)
	}
	fc.err = errors.New("network")
	if _, err := (&Enumeration{Caller: fc}).Fetch(context.Background()); err == nil || errors.Is(err, ErrNotSupported) {
		t.Fatalf("err %v", err)
	}
}
