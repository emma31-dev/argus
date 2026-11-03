package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emma31-dev/argus/internal/fixturegen"
	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/snapshot"
	"github.com/emma31-dev/argus/internal/tokens"
)

type captureOptions struct {
	Out string
	RPC string
	K   int
}

func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

// runCapture does ONE live round with the recorder on and writes a fixture
// set that `verify` can replay offline. The expected.json of a live set is
// computed by this program (there is no independent oracle for real data),
// so it pins today's behaviour for regression checks; review it.
func runCapture(ctx context.Context, opt captureOptions) error {
	raw := opt.RPC
	if raw == "" {
		raw = envOr("ARGUS_RPC_URLS", "https://free.rpc.fastnear.com")
	}
	var rpcURL string
	for _, u := range strings.Split(raw, ",") {
		if u = strings.TrimSpace(u); u != "" {
			rpcURL = u
			break
		}
	}
	pu, err := url.Parse(rpcURL)
	if err != nil || pu.Host == "" {
		return fmt.Errorf("bad RPC URL (host shown only): %q", rpcURL[:min(len(rpcURL), 12)])
	}
	name := pu.Host // the provider name stored in the fixture never includes a path or key
	provider := rpc.Provider{Name: name, URL: rpcURL, RPS: 2, Burst: 2}
	hc := rpc.WithHTTPClient(&http.Client{Timeout: 30 * time.Second})

	// Discovery uses its own client without the recorder: the fixture holds
	// the token list as tokens.yaml, not the discovery calls.
	discClient := rpc.New([]rpc.Provider{provider}, hc)
	chain := &tokens.Chain{
		Enumeration: &tokens.Enumeration{Caller: discClient, Account: envOr("ARGUS_INTENTS_ACCOUNT", tokens.DefaultIntentsAccount)},
		OneClick:    &tokens.OneClick{URL: os.Getenv("ARGUS_ONECLICK_TOKENS_URL"), Client: &http.Client{Timeout: 30 * time.Second}},
	}
	if f := os.Getenv("ARGUS_TOKENS_FILE"); f != "" {
		chain.Config = &tokens.Config{Path: f}
	} else if _, err := os.Stat("config/tokens.yaml"); err == nil {
		chain.Config = &tokens.Config{Path: "config/tokens.yaml"}
	}
	all, source, err := chain.Discover(ctx)
	if err != nil {
		return fmt.Errorf("token discovery: %w", err)
	}
	var toks []model.Token
	for _, t := range all {
		if tokens.InScope(t) {
			toks = append(toks, t)
		}
	}

	tmp, err := os.MkdirTemp(filepath.Dir(opt.Out), ".capture-*")
	if err != nil {
		if err2 := os.MkdirAll(filepath.Dir(opt.Out), 0o755); err2 != nil {
			return err2
		}
		if tmp, err = os.MkdirTemp(filepath.Dir(opt.Out), ".capture-*"); err != nil {
			return err
		}
	}
	defer os.RemoveAll(tmp)

	var mu sync.Mutex
	seq := 0
	var recErr error
	rec := func(prov, method string, req, resp []byte) {
		mu.Lock()
		defer mu.Unlock()
		seq++
		var respJSON any = string(resp) // non-JSON bodies are stored as strings
		if json.Valid(resp) {
			respJSON = json.RawMessage(resp)
		}
		b, _ := json.MarshalIndent(map[string]any{"provider": prov, "request": json.RawMessage(req), "response": respJSON}, "", "  ")
		if err := os.WriteFile(filepath.Join(tmp, fmt.Sprintf("rpc-%04d-%s.json", seq, method)), append(b, '\n'), 0o644); err != nil && recErr == nil {
			recErr = err
		}
	}
	client := rpc.New([]rpc.Provider{provider}, hc, rpc.WithRecorder(rec))
	account := envOr("ARGUS_INTENTS_ACCOUNT", tokens.DefaultIntentsAccount)
	reader := &snapshot.Reader{Caller: client, IntentsAccount: account}
	fmt.Printf("capturing one live round: %d tokens (from %s discovery) via %s\n", len(toks), source, name)
	res, err := reader.Round(ctx, toks)
	if err != nil {
		return fmt.Errorf("live round: %w", err)
	}
	if recErr != nil {
		return fmt.Errorf("writing recordings: %w", recErr)
	}

	var y strings.Builder
	fmt.Fprintf(&y, "# Tokens of the live capture at block %d (%s discovery).\ntokens:\n", res.Block.Height, source)
	for _, t := range toks {
		id, _ := json.Marshal(t.TokenID)
		sym, _ := json.Marshal(t.Symbol)
		fmt.Fprintf(&y, "  - id: %s\n    symbol: %s\n    decimals: %d\n", id, sym, t.Decimals)
	}
	if err := os.WriteFile(filepath.Join(tmp, "tokens.yaml"), []byte(y.String()), 0o644); err != nil {
		return err
	}
	fx := fixturegen.Fixture{
		Kind: "live", Provider: name, Rounds: 1, K: opt.K, IntentsAccount: account,
		Description: fmt.Sprintf("One live round at block %d captured %s with `verify -capture`. expected.json was computed by this program, not by an independent oracle.",
			res.Block.Height, time.Now().UTC().Format(time.RFC3339)),
	}
	fxb, _ := json.MarshalIndent(fx, "", "  ")
	if err := os.WriteFile(filepath.Join(tmp, "fixture.json"), append(fxb, '\n'), 0o644); err != nil {
		return err
	}
	// Replay what was just recorded to compute expected.json.
	o, err := replayFixture(ctx, tmp)
	if err != nil {
		return fmt.Errorf("the recording does not replay cleanly: %w", err)
	}
	o.Computed.Note = "Self-computed at capture time by `verify -capture` from this recording; review before trusting."
	if err := os.WriteFile(filepath.Join(tmp, "expected.json"), mustJSON(o.Computed), 0o644); err != nil {
		return err
	}
	dest := strings.ReplaceAll(opt.Out, "{block}", strconv.FormatUint(res.Block.Height, 10))
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists; remove it or pick another -out", dest)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	_ = os.Chmod(dest, 0o755)
	fmt.Printf("saved %d exchanges for block %d to %s\nrun `go run ./cmd/verify` to replay it offline\n", seq, res.Block.Height, dest)
	return nil
}

func mustJSON(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}
