package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emma31-dev/argus/internal/fixturegen"
)

const syntheticDir = "../../data/fixtures/synthetic"

func TestSyntheticSetMatchesExpected(t *testing.T) {
	o, err := replayFixture(context.Background(), syntheticDir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(syntheticDir, "expected.json"))
	var want fixturegen.Expected
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	fixturegen.SortExpected(&want)
	if diffs := compare(want, o.Computed); len(diffs) != 0 {
		t.Fatalf("diffs: %v", diffs)
	}
	var buf bytes.Buffer
	printOutcome(&buf, o, symbolsOf(syntheticDir))
	if !strings.HasPrefix(buf.String(), "Fixture set: synthetic (not mainnet data)\n") {
		t.Fatalf("header = %q", strings.SplitN(buf.String(), "\n", 2)[0])
	}
	for _, want := range []string{"turned RED", "false alarm cleared", "RECOVERED", "4200018.123456789012345678901234"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output lacks %q", want)
		}
	}
}

func TestCompareDetectsMismatch(t *testing.T) {
	o, err := replayFixture(context.Background(), syntheticDir)
	if err != nil {
		t.Fatal(err)
	}
	want := o.Computed
	want.Final = map[string]fixturegen.FinalState{}
	for k, v := range o.Computed.Final {
		want.Final[k] = v
	}
	f := want.Final["nep141:eth.synthetic.near"]
	f.Status = "green"
	want.Final["nep141:eth.synthetic.near"] = f
	want.Transitions = want.Transitions[1:]
	want.Alerts = append(append([]fixturegen.AlertRec{}, want.Alerts...), fixturegen.AlertRec{Kind: "turned_red", Token: "x", Block: 1})
	diffs := compare(want, o.Computed)
	joined := strings.Join(diffs, "\n")
	for _, s := range []string{"final nep141:eth.synthetic.near", "transition produced but not expected", "alert expected but missing"} {
		if !strings.Contains(joined, s) {
			t.Errorf("diffs lack %q:\n%s", s, joined)
		}
	}
}

func TestMissingRecordingFailsLoudly(t *testing.T) {
	dir := t.TempDir()
	if err := fixturegen.Generate(dir); err != nil {
		t.Fatal(err)
	}
	// Drop one held-balance recording.
	files, _ := filepath.Glob(filepath.Join(dir, "rpc-0*-query.json"))
	if err := os.Remove(files[5]); err != nil {
		t.Fatal(err)
	}
	if _, err := replayFixture(context.Background(), dir); err == nil {
		t.Fatal("a fixture with a missing recording must not pass")
	}
}

func TestFindSetsOrder(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"synthetic", "live-200", "live-100", "other"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o755)
	}
	sets, err := findSets(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range sets {
		names = append(names, filepath.Base(s))
	}
	if strings.Join(names, ",") != "live-100,live-200,synthetic" {
		t.Fatalf("order = %v", names)
	}
	if _, err := findSets(t.TempDir()); err == nil {
		t.Fatal("empty root should error")
	}
}

// TestCaptureAgainstFakeRPC runs -capture against a SYNTHETIC local server
// and replays the result.
func TestCaptureAgainstFakeRPC(t *testing.T) {
	const holder = "intents.near"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		reply := func(res any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": res})
		}
		if req.Method == "block" {
			reply(map[string]any{"header": map[string]any{"height": 777, "hash": "SYNTH", "timestamp_nanosec": "1790000000000000000"}})
			return
		}
		var p struct{ Account, Method, Args string }
		var q map[string]string
		_ = json.Unmarshal(req.Params, &q)
		p.Account, p.Method, p.Args = q["account_id"], q["method_name"], q["args_base64"]
		raw, _ := base64.StdEncoding.DecodeString(p.Args)
		var out any
		switch {
		case p.Method == "mt_tokens":
			reply(map[string]any{"error": "FunctionCallError(MethodResolveError(MethodNotFound))", "logs": []string{}})
			return
		case p.Method == "mt_batch_supply":
			var a struct {
				IDs []string `json:"token_ids"`
			}
			_ = json.Unmarshal(raw, &a)
			vals := make([]string, len(a.IDs))
			for i := range vals {
				vals[i] = "1000"
			}
			out = vals
		default:
			out = "900"
		}
		b, _ := json.Marshal(out)
		bs := make([]int, len(b))
		for i, c := range b {
			bs[i] = int(c)
		}
		reply(map[string]any{"result": bs, "logs": []string{}})
	}))
	defer srv.Close()
	dead := httptest.NewServer(http.NotFoundHandler())
	defer dead.Close()

	dir := t.TempDir()
	tf := filepath.Join(dir, "tokens.yaml")
	_ = os.WriteFile(tf, []byte("tokens:\n  - id: \"nep141:a.test.near\"\n    symbol: A\n    decimals: 6\n  - id: \"nep245:m.test.near:1_x\"\n    symbol: M\n    decimals: 18\n"), 0o644)
	t.Setenv("ARGUS_TOKENS_FILE", tf)
	t.Setenv("ARGUS_ONECLICK_TOKENS_URL", dead.URL)
	t.Setenv("ARGUS_INTENTS_ACCOUNT", holder)

	out := filepath.Join(dir, "fixtures", "live-{block}")
	if err := runCapture(context.Background(), captureOptions{Out: out, RPC: srv.URL + "/secret-key, http://ignored", K: 4}); err != nil {
		t.Fatal(err)
	}
	set := filepath.Join(dir, "fixtures", "live-777")
	o, err := replayFixture(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	if o.header() != "Fixture set: live capture at block 777 from "+strings.TrimPrefix(srv.URL, "http://") {
		t.Fatalf("header = %q", o.header())
	}
	if s := o.Computed.Final["nep141:a.test.near"]; s.Status != "amber" || s.Gap != "-100" {
		t.Fatalf("final = %+v", o.Computed.Final)
	}
	// The API key in the URL path must not be stored anywhere.
	files, _ := filepath.Glob(filepath.Join(set, "*"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if strings.Contains(string(b), "secret-key") {
			t.Fatalf("%s leaks the provider URL path", f)
		}
	}
	if err := runVerify(context.Background(), filepath.Join(dir, "fixtures")); err == nil {
		t.Log("verify of live-only dir passed")
	} else {
		t.Fatal(err)
	}
	_ = fmt.Sprint
}
