package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/store"
)

// fakeNEAR is a SYNTHETIC JSON-RPC server (not mainnet data). Balances are
// fixed per token; the block height advances by one on every "block" call.
type fakeNEAR struct {
	height atomic.Uint64
	owed   map[string]string // token id -> owed
	held   map[string]string // token id -> held
}

func (f *fakeNEAR) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     any             `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	reply := func(result any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}
	switch req.Method {
	case "block":
		h := f.height.Add(1)
		reply(map[string]any{"header": map[string]any{
			"height": h, "hash": fmt.Sprintf("SyntheticHash%d", h),
			// Block time follows the wall clock: /v1/status marks the monitor stale
			// when the newest block is older than the staleness bound.
			"timestamp_nanosec": fmt.Sprint(time.Now().UnixNano()),
		}})
	case "query":
		var p struct {
			Account string `json:"account_id"`
			Method  string `json:"method_name"`
			Args    string `json:"args_base64"`
		}
		_ = json.Unmarshal(req.Params, &p)
		raw, _ := base64.StdEncoding.DecodeString(p.Args)
		var args map[string]any
		_ = json.Unmarshal(raw, &args)
		var out any
		switch {
		case p.Account == "intents.near" && p.Method == "mt_tokens":
			reply(map[string]any{"error": "wasm execution failed with error: FunctionCallError(MethodResolveError(MethodNotFound))", "logs": []string{}})
			return
		case p.Account == "intents.near" && p.Method == "mt_batch_supply":
			var vals []string
			for _, id := range args["token_ids"].([]any) {
				vals = append(vals, f.owed[id.(string)])
			}
			out = vals
		case p.Method == "ft_balance_of":
			out = f.held["nep141:"+p.Account]
		case p.Method == "mt_balance_of":
			out = f.held["nep245:"+p.Account+":"+args["token_id"].(string)]
		default:
			http.Error(w, "unexpected call "+p.Account+"."+p.Method, 500)
			return
		}
		b, _ := json.Marshal(out)
		bytes := make([]int, len(b))
		for i, c := range b {
			bytes[i] = int(c)
		}
		reply(map[string]any{"result": bytes, "logs": []string{}, "block_height": f.height.Load()})
	default:
		http.Error(w, "unexpected method "+req.Method, 500)
	}
}

func testPool(t *testing.T) *store.Repo {
	t.Helper()
	url := os.Getenv("ARGUS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("ARGUS_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("argus_e2e_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return store.New(pool)
}

const tokensYAML = `tokens:
  - id: "nep141:wrap.near"
    symbol: wNEAR
    decimals: 24
  - id: "nep141:usdc.example.near"
    symbol: USDC
    decimals: 6
  - id: "nep245:hot.example.near:56_1"
    symbol: HOT
    decimals: 18
`

// TestEndToEnd runs the real discovery and snapshot loops against a
// synthetic fake RPC and a real Postgres, then reads the public API.
func TestEndToEnd(t *testing.T) {
	repo := testPool(t)
	fake := &fakeNEAR{
		owed: map[string]string{
			"nep141:wrap.near":             "1000000000000000000000000000", // 1000 wNEAR, 24 decimals
			"nep141:usdc.example.near":     "5000000",
			"nep245:hot.example.near:56_1": "7000000000000000000",
		},
		held: map[string]string{
			"nep141:wrap.near":             "1000000000000000000000000500", // small surplus
			"nep141:usdc.example.near":     "4000000",                      // sustained deficit of 1 USDC
			"nep245:hot.example.near:56_1": "7000000000000000000",
		},
	}
	rpcSrv := httptest.NewServer(fake)
	defer rpcSrv.Close()

	dir := t.TempDir()
	tokensFile := filepath.Join(dir, "tokens.yaml")
	if err := os.WriteFile(tokensFile, []byte(tokensYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		HTTPAddr: "127.0.0.1:0", RPCURLs: []string{rpcSrv.URL}, RPCRPS: 1000, IntentsAccount: "intents.near",
		TokensFile: tokensFile, Interval: 80 * time.Millisecond, K: 2, BatchSize: 50, Concurrency: 4,
		DiscoveryInterval: time.Hour, APIRateLimit: 0, CORSOrigins: []string{"*"}, PublicURL: "https://argus.example",
		ReplayFile: filepath.Join(dir, "none.json"), Retention: time.Hour,
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	queue := alert.NewMemoryQueue()
	app := NewApp(cfg, log, repo, newRPC(cfg), queue)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := app.Discover(ctx); err != nil {
		t.Fatalf("discover: %v", err)
	}
	if got := len(app.Tokens()); got != 3 {
		t.Fatalf("tokens = %d", got)
	}

	loopCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { app.snapshotLoop(loopCtx); close(done) }()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if n := countRounds(ctx, t, repo); n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("snapshot loop did not complete 3 rounds")
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	<-done

	api := httptest.NewServer(app.APIServer("e2e").Handler)
	defer api.Close()

	var tokens struct {
		BlockHeight uint64 `json:"block_height"`
		Tokens      []struct {
			ID     string `json:"id"`
			Symbol string `json:"symbol"`
			Status string `json:"status"`
			Latest *struct {
				Owed string `json:"owed"`
				Held string `json:"held"`
				Gap  string `json:"gap"`
			} `json:"latest"`
		} `json:"tokens"`
	}
	getJSON(t, api.URL+"/v1/tokens", &tokens)
	if len(tokens.Tokens) != 3 || tokens.BlockHeight < 3 {
		t.Fatalf("tokens = %+v", tokens)
	}
	if first := tokens.Tokens[0]; first.ID != "nep141:usdc.example.near" || first.Status != "red" ||
		first.Latest == nil || first.Latest.Gap != "-1000000" {
		t.Fatalf("expected USDC red first with gap -1000000, got %+v", first)
	}
	byID := map[string]string{}
	for _, tk := range tokens.Tokens {
		byID[tk.ID] = tk.Status
	}
	if byID["nep141:wrap.near"] != "green" || byID["nep245:hot.example.near:56_1"] != "green" {
		t.Fatalf("healthy tokens should be green: %v", byID)
	}
	if g := tokens.Tokens[len(tokens.Tokens)-1]; g.Latest == nil || g.Status != "green" {
		t.Fatalf("green tokens must sort last: %+v", g)
	}

	var status map[string]any
	getJSON(t, api.URL+"/v1/status", &status)
	if status["status"] != "red" || status["tokens_total"].(float64) != 3 || status["open_incidents"].(float64) != 1 ||
		status["version"] != "e2e" || status["monitor_stale"] != false || status["interval_seconds"].(float64) != 0 {
		// interval 80ms truncates to 0 seconds; everything else must match.
		t.Fatalf("status = %v", status)
	}
	provs := status["providers"].([]any)
	if len(provs) != 1 || strings.Contains(fmt.Sprint(provs[0]), "/v1") {
		t.Fatalf("providers = %v", provs)
	}

	// The red transition produced exactly one alert on the queue (moved there
	// by the outbox relay), linking the dashboard route.
	if _, err := app.relay.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if queue.Len() < 1 {
		t.Fatal("no alert was queued for the red token")
	}
	del, err := queue.Dequeue(ctx, time.Second)
	if err != nil || del == nil {
		t.Fatalf("dequeue: %v %v", del, err)
	}
	if del.Alert.Kind != alert.TurnedRed || del.Alert.TokenID != "nep141:usdc.example.near" ||
		del.Alert.DashboardURL != "https://argus.example/token/?id=nep141%3Ausdc.example.near" {
		t.Fatalf("alert = %+v", del.Alert)
	}

	for _, p := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(api.URL + p)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: %v %v", p, resp, err)
		}
		resp.Body.Close()
	}
	// A failing discovery pass keeps the previous token list.
	app.disc = staticDisc{err: errors.New("all sources down")}
	if err := app.Discover(ctx); err == nil || len(app.Tokens()) != 3 {
		t.Fatalf("failed discovery must keep the list: err=%v tokens=%d", err, len(app.Tokens()))
	}
	text, err := app.StatusText(ctx)
	if err != nil || !strings.Contains(text, "1 red") || !strings.Contains(text, "RED USDC") {
		t.Fatalf("status text = %q err %v", text, err)
	}
}

func countRounds(ctx context.Context, t *testing.T, repo *store.Repo) int {
	t.Helper()
	var n int
	if err := repo.Pool().QueryRow(ctx, `SELECT count(*) FROM rounds`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s = %d: %s", url, resp.StatusCode, b)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("decode %s: %v\n%s", url, err, b)
	}
}

// TestRoundFailureMarksStale: when every provider is down the round fails,
// state is untouched apart from the stale flag, and nothing alerts.
func TestRoundFailureMarksStale(t *testing.T) {
	repo := testPool(t)
	rpcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", 503) }))
	defer rpcSrv.Close()
	cfg := Config{RPCURLs: []string{rpcSrv.URL}, RPCRPS: 1000, IntentsAccount: "intents.near", K: 2, BatchSize: 50, Concurrency: 2}
	queue := alert.NewMemoryQueue()
	app := NewApp(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), repo,
		rpc.New([]rpc.Provider{{Name: "down", URL: rpcSrv.URL}}, rpc.WithSleep(func(context.Context, time.Duration) error { return nil })), queue)
	toks, _, err := discoverScoped(context.Background(), staticDisc{})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertTokens(context.Background(), toks); err != nil {
		t.Fatal(err)
	}
	app.setTokens(toks)
	if err := app.RunRound(context.Background()); err == nil {
		t.Fatal("expected the round to fail")
	}
	states, err := repo.LoadStates(context.Background(), nil)
	if err != nil || len(states) != 1 {
		t.Fatalf("states = %v err %v", states, err)
	}
	for _, s := range states {
		if !s.Stale || s.Status != "unknown" || s.LastError == "" {
			t.Fatalf("state = %+v", s)
		}
	}
	if queue.Len() != 0 {
		t.Fatal("alert raised on a failed round")
	}
}
