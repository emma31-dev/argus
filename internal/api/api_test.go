package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/store"
)

var t0 = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

type fakeStore struct {
	tokens    []model.Token
	states    map[string]model.TokenState
	snaps     []model.Snapshot // all history, any order
	incidents []model.Incident
	events    map[int64][]model.IncidentEvent
	round     *store.Round
	err       error

	lastRange store.Range
	lastLimit int
}

func (f *fakeStore) ListTokens(context.Context) ([]model.Token, error) { return f.tokens, f.err }
func (f *fakeStore) GetToken(_ context.Context, id string) (model.Token, error) {
	if f.err != nil {
		return model.Token{}, f.err
	}
	for _, t := range f.tokens {
		if t.TokenID == id {
			return t, nil
		}
	}
	return model.Token{}, store.ErrNotFound
}
func (f *fakeStore) LatestSnapshots(context.Context) ([]model.Snapshot, error) {
	latest := map[string]model.Snapshot{}
	for _, s := range f.snaps {
		if o, ok := latest[s.TokenID]; !ok || s.BlockHeight > o.BlockHeight {
			latest[s.TokenID] = s
		}
	}
	var out []model.Snapshot
	for _, s := range latest {
		out = append(out, s)
	}
	return out, f.err
}
func (f *fakeStore) LoadStates(_ context.Context, ids []string) (map[string]model.TokenState, error) {
	if len(ids) == 0 {
		return f.states, f.err
	}
	out := map[string]model.TokenState{}
	for _, id := range ids {
		if s, ok := f.states[id]; ok {
			out[id] = s
		}
	}
	return out, f.err
}
func (f *fakeStore) History(_ context.Context, id string, rg store.Range, limit int) ([]model.Snapshot, error) {
	f.lastRange, f.lastLimit = rg, limit
	var out []model.Snapshot
	for _, s := range f.snaps {
		if s.TokenID == id {
			out = append(out, s)
		}
	}
	return out, f.err
}
func (f *fakeStore) ExportRows(_ context.Context, id string, rg store.Range, max int, fn func(store.ExportRow) error) error {
	f.lastRange = rg
	if f.err != nil {
		return f.err
	}
	for _, s := range f.snaps {
		if id != "" && s.TokenID != id {
			continue
		}
		sym, dec := "", 0
		for _, t := range f.tokens {
			if t.TokenID == s.TokenID {
				sym, dec = t.Symbol, t.Decimals
			}
		}
		if err := fn(store.ExportRow{Symbol: sym, Decimals: dec, Snapshot: s}); err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeStore) ListIncidents(_ context.Context, fl store.IncidentFilter) ([]model.Incident, error) {
	var out []model.Incident
	for _, i := range f.incidents {
		open := i.ClosedAt == nil
		if (fl.Status == "open" && !open) || (fl.Status == "closed" && open) {
			continue
		}
		out = append(out, i)
	}
	return out, f.err
}
func (f *fakeStore) GetIncident(_ context.Context, id int64) (model.Incident, error) {
	for _, i := range f.incidents {
		if i.ID == id {
			return i, nil
		}
	}
	return model.Incident{}, store.ErrNotFound
}
func (f *fakeStore) IncidentEvents(_ context.Context, id int64) ([]model.IncidentEvent, error) {
	return f.events[id], nil
}
func (f *fakeStore) LastRound(context.Context) (store.Round, error) {
	if f.round == nil {
		return store.Round{}, store.ErrNotFound
	}
	return *f.round, nil
}

type fakeSrc struct{}

func (fakeSrc) Health() []rpc.ProviderHealth {
	return []rpc.ProviderHealth{{Name: "fastnear", URL: "https://rpc.example.com/SECRET-KEY?k=1", Healthy: true,
		LastError: `Post "https://rpc.example.com/SECRET-KEY": dial tcp: refused`}}
}

func snap(id string, h uint64, owed, held, gap string) model.Snapshot {
	return model.Snapshot{TokenID: id, BlockHeight: h, BlockTime: t0.Add(time.Duration(h) * time.Second),
		Owed: owed, Held: held, Gap: gap, Provider: "fastnear", TakenAt: t0}
}

const (
	idWrap = "nep141:wrap.near"
	idUSDC = "nep141:usdc.example.near"
	idHot  = "nep245:v2_1.omni.hot.tg:56_111"
	idEth  = "nep141:eth.omft.near"
	idNew  = "nep141:new.near"
)

func newFake() *fakeStore {
	f := &fakeStore{
		tokens: []model.Token{
			{TokenID: idWrap, Standard: model.StandardNEP141, Contract: "wrap.near", Symbol: "wNEAR", Decimals: 24},
			{TokenID: idUSDC, Standard: model.StandardNEP141, Contract: "usdc.example.near", Symbol: "USDC", Decimals: 6},
			{TokenID: idHot, Standard: model.StandardNEP245, Contract: "v2_1.omni.hot.tg", InnerID: "56_111", Symbol: "HOT", Decimals: 18},
			{TokenID: idEth, Standard: model.StandardNEP141, Contract: "eth.omft.near", Symbol: "ETH", Decimals: 18},
			{TokenID: idNew, Standard: model.StandardNEP141, Contract: "new.near", Symbol: "NEW", Decimals: 6},
		},
		states: map[string]model.TokenState{
			idWrap: {TokenID: idWrap, Status: model.StatusGreen, LastBlock: 105},
			idUSDC: {TokenID: idUSDC, Status: model.StatusAmber, NegativeStreak: 1, LastBlock: 105},
			idHot:  {TokenID: idHot, Status: model.StatusRed, NegativeStreak: 5, LastBlock: 105, OpenIncidentID: 7, Stale: true, LastError: "boom https://x.example/key123"},
			idEth:  {TokenID: idEth, Status: model.StatusRed, NegativeStreak: 5, LastBlock: 105},
			// idNew has no state and no snapshot.
		},
		snaps: []model.Snapshot{
			snap(idWrap, 104, "1000", "1000", "0"),
			snap(idWrap, 105, "1000", "1500", "500"),
			snap(idUSDC, 105, "1000", "990", "-10"),
			snap(idHot, 105, "1000", "500", "-500"),
			snap(idEth, 105, "1000000", "999000", "-1000"), // -0.1%: milder than HOT's -50%
		},
		events: map[int64][]model.IncidentEvent{7: {{IncidentID: 7, BlockHeight: 101, Event: "mt_mint", Standard: "nep245", Raw: "{}"}}},
		round:  &store.Round{BlockHeight: 105, FinishedAt: t0.Add(time.Minute), TokensOK: 4, Provider: "fastnear"},
	}
	f.incidents = []model.Incident{
		{ID: 7, TokenID: idHot, StartBlock: 100, RedBlock: 103, OpenedAt: t0, MaxDeficit: "500", MaxDeficitBlock: 105},
		{ID: 6, TokenID: idEth, StartBlock: 50, RedBlock: 53, EndBlock: 60, OpenedAt: t0.Add(-2 * time.Hour),
			ClosedAt: ptr(t0.Add(-time.Hour)), MaxDeficit: "9", MaxDeficitBlock: 55, Summary: "closed"},
	}
	return f
}

func ptr[T any](v T) *T { return &v }

func newServer(t *testing.T, f *fakeStore, mod ...func(*Config)) *httptest.Server {
	t.Helper()
	cfg := Config{Interval: 30 * time.Second, K: 4, Version: "test", Now: func() time.Time { return t0.Add(time.Minute) },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), RateLimit: 0}
	for _, m := range mod {
		m(&cfg)
	}
	srv := httptest.NewServer(New(cfg, f, fakeSrc{}))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func getJSON(t *testing.T, url string, want int, out any) *http.Response {
	t.Helper()
	resp, b := get(t, url)
	if resp.StatusCode != want {
		t.Fatalf("GET %s = %d, want %d: %s", url, resp.StatusCode, want, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("decode %s: %v\n%s", url, err, b)
		}
	}
	return resp
}

func TestStatus(t *testing.T) {
	srv := newServer(t, newFake())
	var got struct {
		Status        string         `json:"status"`
		MonitorStale  bool           `json:"monitor_stale"`
		LastBlock     uint64         `json:"last_block"`
		LastBlockTime string         `json:"last_block_time"`
		LastCheckedAt string         `json:"last_checked_at"`
		Interval      int            `json:"interval_seconds"`
		K             int            `json:"debounce_snapshots"`
		Total         int            `json:"tokens_total"`
		ByStatus      map[string]int `json:"tokens_by_status"`
		OpenIncidents int            `json:"open_incidents"`
		Providers     []providerView `json:"providers"`
		Version       string         `json:"version"`
	}
	resp := getJSON(t, srv.URL+"/v1/status", 200, &got)
	if got.Status != "red" || got.MonitorStale || got.LastBlock != 105 || got.Interval != 30 || got.K != 4 || got.Total != 5 || got.Version != "test" {
		t.Fatalf("status = %+v", got)
	}
	if got.ByStatus["red"] != 2 || got.ByStatus["amber"] != 1 || got.ByStatus["green"] != 1 || got.ByStatus["unknown"] != 1 {
		t.Fatalf("by status = %v", got.ByStatus)
	}
	if got.OpenIncidents != 1 || got.LastCheckedAt != "2026-11-03T12:01:00Z" || got.LastBlockTime == "" {
		t.Fatalf("status = %+v", got)
	}
	if len(got.Providers) != 1 || got.Providers[0].URL != "https://rpc.example.com" || strings.Contains(got.Providers[0].LastError, "SECRET") {
		t.Fatalf("provider URL/error not redacted: %+v", got.Providers)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("headers = %v", resp.Header)
	}
}

func TestStatusStaleAndEmpty(t *testing.T) {
	f := newFake()
	f.round.FinishedAt = t0.Add(-time.Hour) // much older than "now"
	srv := newServer(t, f)
	var got map[string]any
	getJSON(t, srv.URL+"/v1/status", 200, &got)
	// red still wins nothing: a stale monitor reports unknown regardless.
	if got["status"] != "unknown" || got["monitor_stale"] != true {
		t.Fatalf("stale monitor = %v", got)
	}

	empty := &fakeStore{}
	srv2 := newServer(t, empty)
	getJSON(t, srv2.URL+"/v1/status", 200, &got)
	if got["status"] != "unknown" || got["last_block_time"] != nil || got["last_checked_at"] != nil || got["tokens_total"].(float64) != 0 {
		t.Fatalf("empty = %v", got)
	}
	var green struct{ Status string }
	f2 := newFake()
	f2.tokens = f2.tokens[:1]
	getJSON(t, newServer(t, f2).URL+"/v1/status", 200, &green)
	if green.Status != "green" {
		t.Fatalf("all green = %q", green.Status)
	}
}

func TestTokensSortAndFields(t *testing.T) {
	srv := newServer(t, newFake())
	var got struct {
		BlockHeight uint64      `json:"block_height"`
		Tokens      []tokenView `json:"tokens"`
	}
	getJSON(t, srv.URL+"/v1/tokens", 200, &got)
	var order []string
	for _, tk := range got.Tokens {
		order = append(order, tk.ID)
	}
	// red: HOT (-50%) before ETH (-0.1%); amber USDC; unknown NEW; green wNEAR.
	want := []string{idHot, idEth, idUSDC, idNew, idWrap}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if got.BlockHeight != 105 {
		t.Fatalf("block height %d", got.BlockHeight)
	}
	hot := got.Tokens[0]
	if hot.Chain != "BNB Chain" {
		t.Fatalf("chain = %q", hot.Chain)
	}
	if eth := got.Tokens[1]; eth.Chain != "Ethereum" {
		t.Fatalf("eth chain = %q", eth.Chain)
	}
	if !hot.Stale || hot.LastError != "boom https://x.example" || hot.InnerID != "56_111" || hot.Standard != "nep245" || hot.NegativeStreak != 5 {
		t.Fatalf("hot = %+v", hot)
	}
	nw := got.Tokens[3]
	if nw.Status != model.StatusUnknown || nw.Latest != nil {
		t.Fatalf("new = %+v", nw)
	}
	if w := got.Tokens[4]; w.Latest == nil || w.Latest.Gap != "500" || w.Latest.Provider != "fastnear" || w.Status != model.StatusGreen {
		t.Fatalf("wrap = %+v", w)
	}
}

func TestRawJSONShape(t *testing.T) {
	srv := newServer(t, newFake())
	_, b := get(t, srv.URL+"/v1/tokens")
	for _, key := range []string{`"stale":`, `"last_error":`, `"negative_streak":`, `"latest":null`, `"inner_id":`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("response lacks %s", key)
		}
	}
}

func TestSortTokensEdgeCases(t *testing.T) {
	mk := func(id string, st model.Status, owed, gap string) tokenView {
		v := tokenView{ID: id, Symbol: id, Status: st}
		if owed != "" {
			v.Latest = &snapshotView{Owed: owed, Gap: gap}
		}
		return v
	}
	ts := []tokenView{
		mk("g-zero-owed", model.StatusGreen, "0", "0"),
		mk("g-big", model.StatusGreen, "1000000000000000000000000000000", "5"),
		mk("g-neg-zero-owed", model.StatusGreen, "0", "-1"),
		mk("g-small", model.StatusGreen, "10", "1"),
		mk("r-b", model.StatusRed, "100", "-10"),
		mk("r-a", model.StatusRed, "100", "-10"),
		mk("r-deep", model.StatusRed, "100", "-90"),
		mk("a-1", model.StatusAmber, "100", "-1"),
	}
	sortTokens(ts)
	var got []string
	for _, t := range ts {
		got = append(got, t.ID)
	}
	want := "r-deep r-a r-b a-1 g-neg-zero-owed g-big g-small g-zero-owed"
	if strings.Join(got, " ") != want {
		t.Fatalf("order = %v\nwant    %s", got, want)
	}
}

func TestTokenDetailEscaping(t *testing.T) {
	srv := newServer(t, newFake())
	for _, path := range []string{
		"/v1/tokens/" + url.PathEscape(idHot),
		"/v1/tokens/nep245%3Av2_1.omni.hot.tg%3A56_111",
		"/v1/tokens/nep245:v2_1.omni.hot.tg:56_111",
	} {
		var got struct {
			tokenView
			OpenIncident *incidentView `json:"open_incident"`
		}
		getJSON(t, srv.URL+path, 200, &got)
		if got.ID != idHot || got.OpenIncident == nil || got.OpenIncident.ID != 7 || got.OpenIncident.ClosedAt != nil || got.OpenIncident.Symbol != "HOT" {
			t.Fatalf("%s: got %+v / %+v", path, got.tokenView, got.OpenIncident)
		}
	}
	var green map[string]any
	getJSON(t, srv.URL+"/v1/tokens/"+url.PathEscape(idWrap), 200, &green)
	if v, ok := green["open_incident"]; !ok || v != nil {
		t.Fatalf("open_incident should be null: %v", green)
	}
	var e map[string]string
	getJSON(t, srv.URL+"/v1/tokens/nep141%3Anope.near", 404, &e)
	if e["error"] != "token not found" {
		t.Fatalf("404 body = %v", e)
	}
}

func TestHistory(t *testing.T) {
	f := newFake()
	srv := newServer(t, f)
	var got struct {
		Token  map[string]any `json:"token"`
		Points []pointView    `json:"points"`
	}
	getJSON(t, srv.URL+"/v1/tokens/"+url.PathEscape(idWrap)+"/history", 200, &got)
	if got.Token["symbol"] != "wNEAR" || len(got.Points) != 2 || got.Points[1].Held != "1500" {
		t.Fatalf("history = %+v", got)
	}
	// default: last 24h, default limit
	wantFrom := t0.Add(time.Minute).Add(-24 * time.Hour)
	if !f.lastRange.FromTime.Equal(wantFrom) || f.lastLimit != DefaultHistoryLimit || f.lastRange.FromBlock != 0 {
		t.Fatalf("default range = %+v limit %d", f.lastRange, f.lastLimit)
	}

	base := srv.URL + "/v1/tokens/" + url.PathEscape(idWrap) + "/history"
	getJSON(t, base+"?from=100&to=200&limit=99999", 200, &got)
	if f.lastRange.FromBlock != 100 || f.lastRange.ToBlock != 200 || !f.lastRange.FromTime.IsZero() || f.lastLimit != MaxHistoryLimit {
		t.Fatalf("block range = %+v limit %d", f.lastRange, f.lastLimit)
	}
	getJSON(t, base+"?from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00%2B02:00&limit=5", 200, &got)
	if !f.lastRange.FromTime.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) ||
		!f.lastRange.ToTime.Equal(time.Date(2026, 10, 1, 22, 0, 0, 0, time.UTC)) || f.lastRange.FromBlock != 0 || f.lastLimit != 5 {
		t.Fatalf("time range = %+v limit %d", f.lastRange, f.lastLimit)
	}
	for _, bad := range []string{"?from=yesterday", "?to=1.5", "?limit=0", "?limit=abc", "?from=0"} {
		var e map[string]string
		getJSON(t, base+bad, 400, &e)
		if e["error"] == "" {
			t.Fatalf("%s: no error message", bad)
		}
	}
	var e map[string]string
	getJSON(t, srv.URL+"/v1/tokens/nope/history", 404, &e)
}

func TestIncidents(t *testing.T) {
	srv := newServer(t, newFake())
	var got struct{ Incidents []incidentView }
	getJSON(t, srv.URL+"/v1/incidents?status=all", 200, &got)
	if len(got.Incidents) != 2 {
		t.Fatalf("all = %d", len(got.Incidents))
	}
	getJSON(t, srv.URL+"/v1/incidents?status=open", 200, &got)
	if len(got.Incidents) != 1 || got.Incidents[0].ID != 7 || got.Incidents[0].DurationSeconds != nil || got.Incidents[0].EndBlock != nil {
		t.Fatalf("open = %+v", got.Incidents)
	}
	getJSON(t, srv.URL+"/v1/incidents?status=closed", 200, &got)
	c := got.Incidents[0]
	if len(got.Incidents) != 1 || c.ID != 6 || c.DurationSeconds == nil || *c.DurationSeconds != 3600 || *c.EndBlock != 60 || c.Symbol != "ETH" || c.Decimals != 18 || c.Summary != "closed" {
		t.Fatalf("closed = %+v", c)
	}
	var e map[string]string
	getJSON(t, srv.URL+"/v1/incidents?status=bogus", 400, &e)
	getJSON(t, srv.URL+"/v1/incidents?limit=-1", 400, &e)
	// empty list is [] not null
	empty := newFake()
	empty.incidents = nil
	_, b := get(t, newServer(t, empty).URL+"/v1/incidents")
	if !strings.Contains(string(b), `"incidents":[]`) {
		t.Fatalf("empty incidents = %s", b)
	}
}

func TestIncidentDetail(t *testing.T) {
	f := newFake()
	f.snaps = append(f.snaps, snap(idHot, 101, "1000", "900", "-100"))
	srv := newServer(t, f)
	var got struct {
		incidentView
		Events    []model.IncidentEvent `json:"events"`
		Snapshots []pointView           `json:"snapshots"`
	}
	getJSON(t, srv.URL+"/v1/incidents/7", 200, &got)
	if got.ID != 7 || len(got.Events) != 1 || got.Events[0].Event != "mt_mint" || len(got.Snapshots) != 2 {
		t.Fatalf("detail = %+v", got)
	}
	if f.lastRange.FromBlock != 100 || f.lastRange.ToBlock != 0 {
		t.Fatalf("open incident range = %+v", f.lastRange)
	}
	getJSON(t, srv.URL+"/v1/incidents/6", 200, &got)
	if f.lastRange.FromBlock != 50 || f.lastRange.ToBlock != 60 || got.Events == nil {
		t.Fatalf("closed incident range = %+v events=%v", f.lastRange, got.Events)
	}
	var e map[string]string
	getJSON(t, srv.URL+"/v1/incidents/999", 404, &e)
	getJSON(t, srv.URL+"/v1/incidents/abc", 400, &e)
}

func TestExportCSV(t *testing.T) {
	f := newFake()
	f.tokens[0].Symbol = "=cmd" // formula injection attempt in on-chain metadata
	srv := newServer(t, f)
	resp, b := get(t, srv.URL+"/v1/export.csv?token="+url.QueryEscape(idWrap))
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("export = %d %v", resp.StatusCode, resp.Header)
	}
	rows, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rows[0], ",") != "token_id,symbol,decimals,block_height,block_time,owed,held,gap,provider" {
		t.Fatalf("header = %v", rows[0])
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d: %s", len(rows), b)
	}
	if strings.Join(rows[2], ",") != "nep141:wrap.near,'=cmd,24,105,2026-11-03T12:01:45Z,1000,1500,500,fastnear" {
		t.Fatalf("row = %v", rows[2])
	}
	// all tokens
	_, b = get(t, srv.URL+"/v1/export.csv")
	if n := strings.Count(string(b), "\n"); n != 1+5 {
		t.Fatalf("all-token export has %d lines", n)
	}
	// negative gap must not be altered
	if !strings.Contains(string(b), ",-500,fastnear") {
		t.Fatalf("negative gap mangled:\n%s", b)
	}
	// empty export still has a header
	_, b = get(t, srv.URL+"/v1/export.csv?token=nep141:none.near")
	if strings.TrimSpace(string(b)) != strings.Join(csvHeader, ",") {
		t.Fatalf("empty export = %q", b)
	}
	var e map[string]string
	getJSON(t, srv.URL+"/v1/export.csv?from=junk", 400, &e)
}

func TestExportStoreError(t *testing.T) {
	f := newFake()
	f.err = errors.New("db down")
	srv := newServer(t, f)
	var e map[string]string
	getJSON(t, srv.URL+"/v1/export.csv", 500, &e)
	if e["error"] != "internal error" || strings.Contains(fmt.Sprint(e), "db down") {
		t.Fatalf("error leaked: %v", e)
	}
}

func TestExportLarge(t *testing.T) {
	f := newFake()
	for h := uint64(1); h <= 3500; h++ {
		f.snaps = append(f.snaps, snap(idUSDC, h, "1", "1", "0"))
	}
	srv := newServer(t, f)
	_, b := get(t, srv.URL+"/v1/export.csv?token="+url.QueryEscape(idUSDC))
	rows, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	if err != nil || len(rows) != 1+3501 {
		t.Fatalf("rows = %d err %v", len(rows), err)
	}
}

func TestReplay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "replay.json")
	srv := newServer(t, newFake(), func(c *Config) { c.ReplayPath = path })
	var e map[string]string
	getJSON(t, srv.URL+"/v1/replay", 404, &e)
	if e["error"] != "replay not available" {
		t.Fatalf("404 = %v", e)
	}
	body := `{"title":"x","tokens":[]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, b := get(t, srv.URL+"/v1/replay")
	if resp.StatusCode != 200 || strings.TrimSpace(string(b)) != body || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("replay = %d %s", resp.StatusCode, b)
	}
	// invalid JSON on disk is not served
	_ = os.WriteFile(path, []byte("{oops"), 0o644)
	getJSON(t, srv.URL+"/v1/replay", 404, &e)
	// unconfigured
	getJSON(t, newServer(t, newFake()).URL+"/v1/replay", 404, &e)
}

func TestBadges(t *testing.T) {
	srv := newServer(t, newFake())
	cases := map[string][2]string{
		"all.svg":                            {"red", "#e05d44"},
		url.PathEscape(idWrap) + ".svg":      {"green", "#4c1"},
		url.PathEscape(idUSDC) + ".svg":      {"amber", "#dfb317"},
		url.PathEscape(idHot) + ".svg":       {"red", "#e05d44"},
		url.PathEscape(idNew) + ".svg":       {"unknown", "#9f9f9f"},
		"nep245:v2_1.omni.hot.tg:56_111.svg": {"red", "#e05d44"},
	}
	for path, want := range cases {
		resp, b := get(t, srv.URL+"/v1/badge/"+path)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/svg+xml; charset=utf-8" || resp.Header.Get("Cache-Control") != "public, max-age=60" {
			t.Fatalf("%s: %d %v", path, resp.StatusCode, resp.Header)
		}
		assertValidSVG(t, b)
		s := string(b)
		if !strings.Contains(s, ">argus<") || !strings.Contains(s, ">"+want[0]+"<") || !strings.Contains(s, want[1]) {
			t.Fatalf("%s: unexpected badge %s", path, s)
		}
	}
	// unknown token: SVG with 404
	resp, b := get(t, srv.URL+"/v1/badge/"+url.PathEscape("nep141:zzz.near")+".svg")
	if resp.StatusCode != 404 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	assertValidSVG(t, b)
	// no .svg suffix
	var e map[string]string
	getJSON(t, srv.URL+"/v1/badge/all", 404, &e)
}

func TestBadgeAllStaleIsUnknown(t *testing.T) {
	f := newFake()
	f.round = nil
	_, b := get(t, newServer(t, f).URL+"/v1/badge/all.svg")
	if !strings.Contains(string(b), ">unknown<") {
		t.Fatalf("badge = %s", b)
	}
}

func assertValidSVG(t *testing.T, b []byte) {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(b))
	var root string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid XML: %v\n%s", err, b)
		}
		if se, ok := tok.(xml.StartElement); ok && root == "" {
			root = se.Name.Local
			if se.Name.Space != "http://www.w3.org/2000/svg" {
				t.Fatalf("svg namespace = %q", se.Name.Space)
			}
		}
	}
	if root != "svg" {
		t.Fatalf("root = %q", root)
	}
}

func TestBadgeEscapesText(t *testing.T) {
	b := renderBadge("a<b", `x"&y`, "#000")
	assertValidSVG(t, b)
	if strings.Contains(string(b), "a<b") {
		t.Fatal("label not escaped")
	}
}

func TestHealthAndReady(t *testing.T) {
	f := newFake()
	srv := newServer(t, f)
	resp, b := get(t, srv.URL+"/healthz")
	if resp.StatusCode != 200 || strings.TrimSpace(string(b)) != "ok" {
		t.Fatalf("healthz %d %s", resp.StatusCode, b)
	}
	if resp, _ := get(t, srv.URL+"/readyz"); resp.StatusCode != 200 {
		t.Fatalf("readyz %d", resp.StatusCode)
	}
	f.round = nil
	if resp, _ := get(t, srv.URL+"/readyz"); resp.StatusCode != 503 {
		t.Fatalf("readyz without round = %d", resp.StatusCode)
	}
}

func TestNotFoundAndMethods(t *testing.T) {
	srv := newServer(t, newFake())
	var e map[string]string
	resp := getJSON(t, srv.URL+"/v1/nope", 404, &e)
	if e["error"] == "" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("404: %v %v", e, resp.Header)
	}
	r, err := http.Post(srv.URL+"/v1/status", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.StatusCode != 405 || r.Header.Get("Allow") == "" {
		t.Fatalf("POST = %d %v", r.StatusCode, r.Header)
	}
	rh, _ := http.Head(srv.URL + "/v1/status")
	rh.Body.Close()
	if rh.StatusCode != 200 {
		t.Fatalf("HEAD = %d", rh.StatusCode)
	}
}

func TestStoreErrorsAre500WithoutDetails(t *testing.T) {
	f := newFake()
	f.err = errors.New("pq: password=hunter2")
	srv := newServer(t, f)
	for _, p := range []string{"/v1/status", "/v1/tokens", "/v1/incidents", "/v1/badge/all.svg"} {
		resp, b := get(t, srv.URL+p)
		if resp.StatusCode != 500 || strings.Contains(string(b), "hunter2") {
			t.Fatalf("%s: %d %s", p, resp.StatusCode, b)
		}
	}
}

func TestCORS(t *testing.T) {
	srv := newServer(t, newFake(), func(c *Config) { c.CORSOrigins = []string{"https://argus.pages.dev"} })
	do := func(method, origin string) *http.Response {
		req, _ := http.NewRequest(method, srv.URL+"/v1/status", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	if got := do("GET", "https://argus.pages.dev").Header.Get("Access-Control-Allow-Origin"); got != "https://argus.pages.dev" {
		t.Fatalf("allowed origin = %q", got)
	}
	if got := do("GET", "https://evil.example").Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed origin got %q", got)
	}
	pre := do("OPTIONS", "https://argus.pages.dev")
	if pre.StatusCode != 204 || pre.Header.Get("Access-Control-Allow-Methods") == "" {
		t.Fatalf("preflight = %d %v", pre.StatusCode, pre.Header)
	}
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func TestRateLimit(t *testing.T) {
	clk := &clock{t: t0}
	srv := newServer(t, newFake(), func(c *Config) { c.RateLimit = 3; c.Now = clk.now })
	do := func(xff string) *http.Response {
		req, _ := http.NewRequest("GET", srv.URL+"/v1/status", nil)
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	for i := 0; i < 3; i++ {
		if r := do(""); r.StatusCode != 200 {
			t.Fatalf("request %d = %d", i, r.StatusCode)
		}
	}
	r := do("")
	if r.StatusCode != 429 || r.Header.Get("Retry-After") != "20" {
		t.Fatalf("limited = %d Retry-After=%q", r.StatusCode, r.Header.Get("Retry-After"))
	}
	var e map[string]string
	resp, b := get(t, srv.URL+"/v1/status")
	_ = json.Unmarshal(b, &e)
	if resp.StatusCode != 429 || e["error"] == "" {
		t.Fatalf("429 body = %s", b)
	}
	// health probes are exempt
	if resp, _ := get(t, srv.URL+"/healthz"); resp.StatusCode != 200 {
		t.Fatalf("healthz limited: %d", resp.StatusCode)
	}
	// XFF is ignored without TrustProxy: still limited
	if r := do("9.9.9.9"); r.StatusCode != 429 {
		t.Fatalf("XFF honoured without trust: %d", r.StatusCode)
	}
	clk.add(21 * time.Second) // one token refilled
	if r := do(""); r.StatusCode != 200 {
		t.Fatalf("after refill = %d", r.StatusCode)
	}
}

func TestRateLimitTrustProxy(t *testing.T) {
	clk := &clock{t: t0}
	srv := newServer(t, newFake(), func(c *Config) { c.RateLimit = 1; c.TrustProxy = true; c.Now = clk.now })
	do := func(xff string) int {
		req, _ := http.NewRequest("GET", srv.URL+"/v1/status", nil)
		req.Header.Set("X-Forwarded-For", xff)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	// The last entry is the proxy-appended one; a spoofed first entry must not
	// give a client a fresh bucket.
	if c := do("1.1.1.1, 10.0.0.1"); c != 200 {
		t.Fatalf("first = %d", c)
	}
	if c := do("2.2.2.2, 10.0.0.1"); c != 429 {
		t.Fatalf("spoofed first entry bypassed the limit: %d", c)
	}
	if c := do("2.2.2.2, 10.0.0.2"); c != 200 {
		t.Fatalf("different client = %d", c)
	}
}

func TestPanicRecovery(t *testing.T) {
	s := New(Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, newFake(), nil)
	h := s.recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "internal error") || strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("recovered = %d %s", rec.Code, rec.Body)
	}
}

func TestRequestLogging(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	srv := httptest.NewServer(New(Config{Logger: log, Now: time.Now}, newFake(), nil))
	defer srv.Close()
	get(t, srv.URL+"/v1/tokens")
	for _, want := range []string{`"msg":"http request"`, `"path":"/v1/tokens"`, `"status":200`} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("log lacks %s: %s", want, buf.String())
		}
	}
}
