package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var chainT0 = time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)

const (
	headHeight = 1_000_000
	tokA       = "nep141:a.test.near"
	tokB       = "nep245:m.test.near:56_X"
)

// fakeArchival is a SYNTHETIC archival node: block h is at chainT0+h seconds,
// heights divisible by 7 are skipped, and token A is under-collateralised
// between blocks 5000 and 5999 (gap -50 SYNTH units); token B is always fine.
type fakeArchival struct{ requests atomic.Int64 }

func supply(id string, h uint64) string { return "1000" }
func held(id string, h uint64) string {
	if id == tokA && h >= 5000 && h < 6000 {
		return "950"
	}
	return "1000"
}

func (f *fakeArchival) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests.Add(1)
	var req struct {
		ID     any             `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	enc := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	result := func(v any) { enc(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": v}) }
	var p struct {
		BlockID    uint64 `json:"block_id"`
		Finality   string `json:"finality"`
		Account    string `json:"account_id"`
		Method     string `json:"method_name"`
		ArgsBase64 string `json:"args_base64"`
	}
	_ = json.Unmarshal(req.Params, &p)
	blockResult := func(h uint64) map[string]any {
		return map[string]any{"header": map[string]any{"height": h, "hash": fmt.Sprintf("H%d", h),
			"timestamp_nanosec": fmt.Sprint(chainT0.Add(time.Duration(h) * time.Second).UnixNano())}}
	}
	switch req.Method {
	case "block":
		if p.Finality == "final" {
			result(blockResult(headHeight))
			return
		}
		if p.BlockID%7 == 0 || p.BlockID > headHeight {
			enc(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"name": "HANDLER_ERROR", "code": -32000,
				"cause": map[string]any{"name": "UNKNOWN_BLOCK"}, "message": "Server error"}})
			return
		}
		result(blockResult(p.BlockID))
	case "query":
		raw, _ := base64.StdEncoding.DecodeString(p.ArgsBase64)
		var args map[string]any
		_ = json.Unmarshal(raw, &args)
		var out any
		switch p.Method {
		case "mt_batch_supply":
			var vs []string
			for _, id := range args["token_ids"].([]any) {
				vs = append(vs, supply(id.(string), p.BlockID))
			}
			out = vs
		case "ft_balance_of":
			out = held("nep141:"+p.Account, p.BlockID)
		case "mt_balance_of":
			out = held("nep245:"+p.Account+":"+args["token_id"].(string), p.BlockID)
		}
		b, _ := json.Marshal(out)
		bs := make([]int, len(b))
		for i, c := range b {
			bs[i] = int(c)
		}
		result(map[string]any{"result": bs, "logs": []string{}, "block_height": p.BlockID})
	}
}

const planYAML = `title: "Synthetic test plan"
notes: "test"
tokens:
  - {id: "nep141:a.test.near", symbol: AAA, decimals: 2}
  - {id: "nep245:m.test.near:56_X", symbol: MMM, decimals: 2}
points:
  - {block_height: 6001, label: "after"}
  - {block_height: 4001, label: "before"}
  - {block_height: 5001, label: "first deficit"}
  - {block_height: 5101, label: "deficit +100s"}
  - {block_height: 5202, label: "deficit +200s"}
  - {block_height: 5302, label: "deficit +300s"}
`

func setup(t *testing.T) (options, *fakeArchival, *httptest.Server) {
	t.Helper()
	fa := &fakeArchival{}
	srv := httptest.NewServer(fa)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	plan := filepath.Join(dir, "plan.yaml")
	if err := os.WriteFile(plan, []byte(planYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return options{Plan: plan, OutJSON: filepath.Join(dir, "out.json"), OutMD: filepath.Join(dir, "out.md"),
		CacheDir: filepath.Join(dir, "cache"), K: 4, RPS: 1000, RPC: srv.URL + "/key-should-not-leak"}, fa, srv
}

func TestReplayEndToEndAndOffline(t *testing.T) {
	o, fa, _ := setup(t)
	if err := runReplay(context.Background(), o, os.Stderr); err != nil {
		t.Fatal(err)
	}
	online := fa.requests.Load()
	if online == 0 {
		t.Fatal("no network requests were made")
	}
	var f ReplayFile
	raw, _ := os.ReadFile(o.OutJSON)
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Tokens) != 2 || strings.Contains(string(raw), "key-should-not-leak") {
		t.Fatalf("tokens=%d, or url leaked", len(f.Tokens))
	}
	a := f.Tokens[0]
	if a.ID != tokA || len(a.Points) != 6 || a.Points[0].BlockHeight != 4001 {
		t.Fatalf("points not sorted: %+v", a.Points)
	}
	wantStatus := []string{"green", "amber", "amber", "amber", "red", "green"}
	for i, p := range a.Points {
		if string(p.Status) != wantStatus[i] {
			t.Errorf("point %d (block %d) status %s, want %s", i, p.BlockHeight, p.Status, wantStatus[i])
		}
	}
	if a.FirstNegativeBlock != 5001 || a.DetectedRedBlock != 5302 || a.MaxDeficit != "50" || a.Points[2].Gap != "-50" {
		t.Fatalf("detection = %+v", a)
	}
	if a.TimeToDetectSeconds == nil || *a.TimeToDetectSeconds != 90 {
		t.Fatalf("time to detect = %v (%s)", a.TimeToDetectSeconds, a.DetectionNote)
	}
	b := f.Tokens[1]
	if b.FirstNegativeBlock != 0 || b.DetectedRedBlock != 0 || b.TimeToDetectSeconds != nil || b.Chain != "BNB Chain" && b.Chain != "NEAR" {
		t.Fatalf("healthy token = %+v", b)
	}
	if !strings.Contains(f.Notes, "sparse") || f.RPCProvider == "" {
		t.Fatalf("notes/provider = %q / %q", f.Notes, f.RPCProvider)
	}
	md, _ := os.ReadFile(o.OutMD)
	for _, want := range []string{"| 5001 |", "first deficit", "mt_batch_supply", "ft_balance_of", "block_id 5001", "-0.5", "red"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("markdown lacks %q", want)
		}
	}

	// Offline: identical numbers, no network.
	o2 := o
	o2.Offline = true
	o2.OutJSON, o2.OutMD = o.OutJSON+".2", o.OutMD+".2"
	if err := runReplay(context.Background(), o2, os.Stderr); err != nil {
		t.Fatalf("offline: %v", err)
	}
	var f2 ReplayFile
	raw2, _ := os.ReadFile(o2.OutJSON)
	_ = json.Unmarshal(raw2, &f2)
	f.GeneratedAt, f2.GeneratedAt = time.Time{}, time.Time{}
	j1, _ := json.Marshal(f)
	j2, _ := json.Marshal(f2)
	if string(j1) != string(j2) {
		t.Fatal("offline result differs from the online one")
	}
	if fa.requests.Load() != online {
		t.Fatal("offline run touched the network")
	}
}

func TestOfflineMissFails(t *testing.T) {
	o, _, _ := setup(t)
	o.Offline = true
	err := runReplay(context.Background(), o, os.Stderr)
	if err == nil || !strings.Contains(err.Error(), "-offline") {
		t.Fatalf("err = %v", err)
	}
}

func TestCacheKeyIgnoresID(t *testing.T) {
	k1, v1, _ := CacheKey([]byte(`{"jsonrpc":"2.0","id":"1","method":"query","params":{"a":1,"b":2}}`))
	k2, _, _ := CacheKey([]byte(`{"params":{"a":1,"b":2},"method":"query","id":"99","jsonrpc":"2.0"}`))
	k3, _, _ := CacheKey([]byte(`{"jsonrpc":"2.0","id":"1","method":"query","params":{"a":1,"b":3}}`))
	_, v4, _ := CacheKey([]byte(`{"jsonrpc":"2.0","id":"1","method":"block","params":{"finality":"final"}}`))
	if k1 != k2 || k1 == k3 || v1 || !v4 {
		t.Fatalf("keys: %s %s %s volatile=%v/%v", k1, k2, k3, v1, v4)
	}
}

func TestErrorsAreNotCached(t *testing.T) {
	if cacheable([]byte(`{"error":{"name":"X"}}`)) || cacheable([]byte(`{"result":{"error":"boom"}}`)) || cacheable([]byte(`garbage`)) {
		t.Fatal("failures must not be cached")
	}
	if !cacheable([]byte(`{"result":{"result":[1]}}`)) {
		t.Fatal("success must be cached")
	}
}

func TestPlanValidation(t *testing.T) {
	ex, err := os.ReadFile("../../evidence/replay-plan.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParsePlan(ex)
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("the example plan must be rejected as a placeholder, got %v", err)
	}
	for name, y := range map[string]string{
		"unknown field": "title: x\nbogus: 1\n",
		"dup height":    strings.Replace(planYAML, "6001", "4001", 1),
		"no label":      strings.Replace(planYAML, `label: "after"`, `label: ""`, 1),
		"nft":           strings.Replace(planYAML, "nep141:a.test.near", "nep171:n.test.near:1", 1),
	} {
		if _, err := ParsePlan([]byte(y)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if p, err := ParsePlan([]byte(planYAML)); err != nil || p.IntentsAccount != "intents.near" {
		t.Fatalf("valid plan: %v %+v", err, p)
	}
}

func TestFindHeights(t *testing.T) {
	o, _, _ := setup(t)
	client, _, err := newClient(o, []string{o.RPC})
	if err != nil {
		t.Fatal(err)
	}
	for _, off := range []int64{12_345, 400_000, 700_001, 700_007, 700_014} {
		target := chainT0.Add(time.Duration(off)*time.Second + 400*time.Millisecond)
		r, err := FindHeight(context.Background(), client, target)
		if err != nil {
			t.Fatalf("offset %d: %v", off, err)
		}
		want := uint64(off)
		if want%7 == 0 {
			want-- // skipped height: the last existing block at or before is one earlier
		}
		if r.Block.Height != want {
			t.Errorf("offset %d: height %d, want %d", off, r.Block.Height, want)
		}
		if r.Block.Time.After(target) {
			t.Errorf("block after target")
		}
	}
	if _, err := FindHeight(context.Background(), client, chainT0.Add(100*24*time.Hour)); err == nil {
		t.Fatal("a target after the head must fail")
	}
}

func TestTimeToDetectNotDemonstrated(t *testing.T) {
	tok := &ReplayToken{ID: tokA, Decimals: 2}
	base := chainT0
	for i, g := range []string{"0", "-5", "-5", "0"} {
		tok.Points = append(tok.Points, ReplayPoint{BlockHeight: uint64(100 + i), BlockTime: base.Add(time.Duration(i) * 20 * time.Second), Owed: "100", Held: "100", Gap: g})
	}
	evaluate(tok, 4)
	if tok.TimeToDetectSeconds != nil || tok.DetectedRedBlock != 0 || tok.FirstNegativeBlock != 101 {
		t.Fatalf("%+v", tok)
	}
	if !strings.Contains(tok.DetectionNote, "sparse") {
		t.Fatalf("note = %s", tok.DetectionNote)
	}
}
