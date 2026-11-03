package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/tokens"
)

type staticDisc struct{ err error }

func (s staticDisc) Discover(context.Context) ([]model.Token, string, error) {
	if s.err != nil {
		return nil, "", s.err
	}
	t, _ := tokens.Parse("nep141:wrap.near")
	t.Symbol, t.Decimals = "wNEAR", 24
	nft, _ := tokens.Parse("nep171:nft.near:1")
	return []model.Token{t, nft}, "config", nil
}

func TestDiscoverScopedFiltersOutOfScope(t *testing.T) {
	ts, src, err := discoverScoped(context.Background(), staticDisc{})
	if err != nil || len(ts) != 1 || ts[0].TokenID != "nep141:wrap.near" || src != "config" {
		t.Fatalf("ts=%v src=%s err=%v", ts, src, err)
	}
	if _, _, err := discoverScoped(context.Background(), staticDisc{err: errors.New("down")}); err == nil {
		t.Fatal("expected error")
	}
}

func TestHealthcheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err := healthcheck(":"+port, time.Second); err != nil {
		t.Fatalf("healthy server: %v", err)
	}
	if err := healthcheck("0.0.0.0:"+port, time.Second); err != nil {
		t.Fatalf("0.0.0.0 addr: %v", err)
	}
	srv.Close()
	if err := healthcheck(":"+port, time.Second); err == nil {
		t.Fatal("closed server reported healthy")
	}
	if err := healthcheck("nonsense", time.Second); err == nil {
		t.Fatal("bad address accepted")
	}
}

func TestNewLogger(t *testing.T) {
	var b strings.Builder
	l := newLogger("warn", &b)
	l.Info("hidden")
	l.Warn("shown")
	if strings.Contains(b.String(), "hidden") || !strings.Contains(b.String(), `"msg":"shown"`) {
		t.Fatalf("log = %s", b.String())
	}
	_ = slog.LevelInfo
}
