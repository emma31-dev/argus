package rpctest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"net/http"

	"github.com/emma31-dev/argus/internal/rpc"
)

func writeEx(t *testing.T, dir, name string, req, resp any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"provider": "p0:test", "request": req, "response": resp})
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func blockReq(params map[string]any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": "1", "method": "block", "params": params}
}

func blockResp(h int) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": "1", "result": map[string]any{"header": map[string]any{
		"height": h, "hash": "H", "timestamp_nanosec": "1790000000000000000"}}}
}

func TestReplayThroughRPCClient(t *testing.T) {
	dir := t.TempDir()
	args := base64.StdEncoding.EncodeToString([]byte(`{"account_id":"intents.near"}`))
	writeEx(t, dir, "rpc-001-block.json", blockReq(map[string]any{"finality": "final"}), blockResp(10))
	writeEx(t, dir, "rpc-002-block.json", blockReq(map[string]any{"finality": "final"}), blockResp(11))
	writeEx(t, dir, "rpc-003-query.json", map[string]any{"jsonrpc": "2.0", "id": "2", "method": "query", "params": map[string]any{
		"request_type": "call_function", "block_id": 10, "account_id": "wrap.near", "method_name": "ft_balance_of", "args_base64": args}},
		map[string]any{"jsonrpc": "2.0", "id": "2", "result": map[string]any{"result": []int{34, 53, 34}, "logs": []string{}}})
	if err := os.WriteFile(filepath.Join(dir, "expected.json"), []byte(`{"not":"an exchange"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	rp, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rp.Exchanges() != 3 || rp.FinalBlocks() != 2 || len(rp.Providers()) != 1 {
		t.Fatalf("loaded %d exchanges, %d finals", rp.Exchanges(), rp.FinalBlocks())
	}
	c := rpc.New([]rpc.Provider{{Name: "p0:test", URL: "http://replay.invalid/"}},
		rpc.WithHTTPClient(&http.Client{Transport: rp}),
		rpc.WithSleep(func(context.Context, time.Duration) error { return nil }))
	ctx := context.Background()

	b1, _, err := c.LatestFinalBlock(ctx)
	if err != nil || b1.Height != 10 {
		t.Fatalf("first final = %+v %v", b1, err)
	}
	b2, _, err := c.LatestFinalBlock(ctx)
	if err != nil || b2.Height != 11 {
		t.Fatalf("second final = %+v %v", b2, err)
	}
	if _, _, err := c.LatestFinalBlock(ctx); err == nil {
		t.Fatal("a third final block should not be served")
	}
	out, err := c.CallFunction(ctx, "p0:test", "wrap.near", "ft_balance_of", map[string]string{"account_id": "intents.near"}, 10)
	if err != nil || string(out) != `"5"` {
		t.Fatalf("call = %q %v", out, err)
	}
	// Different block, account, method or args are misses.
	for _, f := range []func() error{
		func() error {
			_, e := c.CallFunction(ctx, "p0:test", "wrap.near", "ft_balance_of", map[string]string{"account_id": "intents.near"}, 11)
			return e
		},
		func() error {
			_, e := c.CallFunction(ctx, "p0:test", "usdc.near", "ft_balance_of", map[string]string{"account_id": "intents.near"}, 10)
			return e
		},
		func() error {
			_, e := c.CallFunction(ctx, "p0:test", "wrap.near", "ft_balance_of", map[string]string{"account_id": "other"}, 10)
			return e
		},
	} {
		if err := f(); err == nil {
			t.Fatal("unrecorded call succeeded")
		}
	}
	misses := rp.Misses()
	if len(misses) < 4 || !strings.Contains(strings.Join(misses, "\n"), "block_id=11") {
		t.Fatalf("misses = %v", misses)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("empty dir should fail")
	}
	dir := t.TempDir()
	writeEx(t, dir, "bad.json", map[string]any{"method": "status", "params": map[string]any{"finality": "final"}}, map[string]any{})
	if _, err := Load(dir); err == nil {
		t.Fatal("unsupported method should fail")
	}
}

func TestMissIsErrNotRecorded(t *testing.T) {
	dir := t.TempDir()
	writeEx(t, dir, "a.json", blockReq(map[string]any{"block_id": 5}), blockResp(5))
	rp, _ := Load(dir)
	req, _ := http.NewRequest("POST", "http://x/", strings.NewReader(`{"method":"block","params":{"block_id":6}}`))
	_, err := rp.RoundTrip(req)
	if !errors.Is(err, ErrNotRecorded) {
		t.Fatalf("err = %v", err)
	}
}
