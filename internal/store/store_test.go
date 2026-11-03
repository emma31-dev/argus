package store

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/model"
)

// testRepo returns a Repo on a fresh schema of the database named by
// ARGUS_TEST_DATABASE_URL, or skips the test.
func testRepo(t *testing.T) (*Repo, context.Context) {
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
	schema := fmt.Sprintf("argus_t_%d", time.Now().UnixNano())
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
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return New(pool), ctx
}

var base = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func mkSnap(token string, block uint64, owed, held *big.Int) model.Snapshot {
	return model.Snapshot{
		TokenID: token, BlockHeight: block, BlockHash: fmt.Sprintf("h%d", block),
		BlockTime: base.Add(time.Duration(block) * time.Second),
		Owed:      owed.String(), Held: held.String(), Gap: new(big.Int).Sub(held, owed).String(),
		Provider: "test", TakenAt: base.Add(time.Duration(block) * time.Second),
	}
}

func TestMigrateIdempotent(t *testing.T) {
	r, ctx := testRepo(t)
	if err := Migrate(ctx, r.pool); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("migrations recorded: %d %v", n, err)
	}
	// Concurrent migrators must not fail.
	errc := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { errc <- Migrate(ctx, r.pool) }()
	}
	for i := 0; i < 4; i++ {
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTokens(t *testing.T) {
	r, ctx := testRepo(t)
	toks := []model.Token{
		{TokenID: "nep141:b.near", Standard: model.StandardNEP141, Contract: "b.near", Symbol: "B", Decimals: 6, Source: "config"},
		{TokenID: "nep245:m.tg:1_2", Standard: model.StandardNEP245, Contract: "m.tg", InnerID: "1_2", Symbol: "A", Decimals: 18, Source: "1click"},
	}
	if err := r.UpsertTokens(ctx, toks); err != nil {
		t.Fatal(err)
	}
	toks[0].Symbol = "BB"
	if err := r.UpsertTokens(ctx, toks[:1]); err != nil {
		t.Fatal(err)
	}
	got, err := r.ListTokens(ctx)
	if err != nil || len(got) != 2 || got[0].Symbol != "A" || got[1].Symbol != "BB" || got[0].InnerID != "1_2" || got[0].Standard != model.StandardNEP245 {
		t.Fatalf("%+v %v", got, err)
	}
	if n, err := r.DeactivateTokensExcept(ctx, []string{"nep141:b.near"}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if got, _ := r.ListTokens(ctx); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	if tk, err := r.GetToken(ctx, "nep245:m.tg:1_2"); err != nil || tk.Symbol != "A" {
		t.Fatal(tk, err)
	}
	if _, err := r.GetToken(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestSnapshotsHugeNumbersAndLatest(t *testing.T) {
	r, ctx := testRepo(t)
	_ = r.UpsertTokens(ctx, []model.Token{{TokenID: "t1", Symbol: "T1", Decimals: 24}, {TokenID: "t2", Symbol: "T2"}, {TokenID: "t3", Symbol: "T3"}})
	max256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	held := new(big.Int).Sub(max256, big.NewInt(12345))
	snaps := []model.Snapshot{
		mkSnap("t1", 100, max256, held), // gap = -12345 with 78-digit values
		mkSnap("t1", 101, big.NewInt(5), big.NewInt(9)),
		mkSnap("t2", 50, big.NewInt(1), big.NewInt(1)),
	}
	n, err := r.InsertSnapshots(ctx, snaps)
	if err != nil || n != 3 {
		t.Fatal(n, err)
	}
	if n, err = r.InsertSnapshots(ctx, snaps); err != nil || n != 0 {
		t.Fatalf("duplicates must be skipped: %d %v", n, err)
	}
	if n, err = r.InsertSnapshots(ctx, nil); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	h, err := r.History(ctx, "t1", Range{}, 0)
	if err != nil || len(h) != 2 {
		t.Fatal(h, err)
	}
	if h[0].Owed != max256.String() || h[0].Held != held.String() || h[0].Gap != "-12345" || h[0].BlockHash != "h100" {
		t.Fatalf("precision lost: %+v", h[0])
	}
	latest, err := r.LatestSnapshots(ctx)
	if err != nil || len(latest) != 2 || latest[0].TokenID != "t1" || latest[0].BlockHeight != 101 || latest[0].Gap != "4" || latest[1].BlockHeight != 50 {
		t.Fatalf("%+v %v", latest, err)
	}
}

func TestHistoryDownsamplingKeepsNegatives(t *testing.T) {
	r, ctx := testRepo(t)
	_ = r.UpsertTokens(ctx, []model.Token{{TokenID: "t", Symbol: "T"}})
	var snaps []model.Snapshot
	negatives := map[uint64]bool{}
	for b := uint64(1); b <= 1000; b++ {
		held := big.NewInt(100)
		if b == 333 || b == 334 || b == 777 {
			held = big.NewInt(90)
			negatives[b] = true
		}
		snaps = append(snaps, mkSnap("t", b, big.NewInt(100), held))
	}
	if _, err := r.InsertSnapshots(ctx, snaps); err != nil {
		t.Fatal(err)
	}
	got, err := r.History(ctx, "t", Range{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 100+len(negatives)+2 || len(got) < 90 {
		t.Fatalf("downsampled to %d rows", len(got))
	}
	seen := map[uint64]bool{}
	var prev uint64
	for _, s := range got {
		if s.BlockHeight <= prev {
			t.Fatal("not ordered")
		}
		prev = s.BlockHeight
		seen[s.BlockHeight] = true
	}
	for b := range negatives {
		if !seen[b] {
			t.Errorf("negative block %d dropped", b)
		}
	}
	if !seen[1] || !seen[1000] {
		t.Error("endpoints dropped")
	}
	// Under the limit: everything returned. Ranges by block and by time.
	all, _ := r.History(ctx, "t", Range{FromBlock: 10, ToBlock: 19}, 100)
	if len(all) != 10 {
		t.Fatalf("block range: %d", len(all))
	}
	all, _ = r.History(ctx, "t", Range{FromTime: base.Add(500 * time.Second), ToTime: base.Add(509 * time.Second)}, 100)
	if len(all) != 10 {
		t.Fatalf("time range: %d", len(all))
	}
}

func TestExportAndPrune(t *testing.T) {
	r, ctx := testRepo(t)
	_ = r.UpsertTokens(ctx, []model.Token{{TokenID: "a", Symbol: "AAA", Decimals: 6}, {TokenID: "b", Symbol: "BBB", Decimals: 8}})
	var snaps []model.Snapshot
	for b := uint64(1); b <= 10; b++ {
		for _, id := range []string{"a", "b"} {
			held := big.NewInt(10)
			if id == "a" && b == 4 {
				held = big.NewInt(1)
			}
			snaps = append(snaps, mkSnap(id, b, big.NewInt(5), held))
		}
	}
	_, _ = r.InsertSnapshots(ctx, snaps)
	var rows []ExportRow
	if err := r.ExportRows(ctx, "", Range{}, 0, func(e ExportRow) error { rows = append(rows, e); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 20 || rows[0].Symbol != "AAA" || rows[0].Decimals != 6 || rows[19].TokenID != "b" || rows[19].BlockHeight != 10 {
		t.Fatalf("%d rows, first %+v", len(rows), rows[0])
	}
	rows = nil
	_ = r.ExportRows(ctx, "b", Range{FromBlock: 3}, 5, func(e ExportRow) error { rows = append(rows, e); return nil })
	if len(rows) != 5 || rows[0].TokenID != "b" || rows[0].BlockHeight != 3 {
		t.Fatalf("%+v", rows)
	}
	stop := errors.New("stop")
	if err := r.ExportRows(ctx, "", Range{}, 0, func(ExportRow) error { return stop }); !errors.Is(err, stop) {
		t.Fatal(err)
	}

	// Prune everything taken before block 9's time, keeping negatives and newest rows.
	cut := base.Add(9 * time.Second)
	n, err := r.PruneSnapshots(ctx, cut, true)
	if err != nil {
		t.Fatal(err)
	}
	// a: blocks 1..8 minus negative block 4 = 7 rows; b: 8 rows.
	if n != 15 {
		t.Fatalf("pruned %d", n)
	}
	h, _ := r.History(ctx, "a", Range{}, 0)
	if len(h) != 3 { // negative block 4, plus blocks 9 and 10 (9 is not < cut)
		t.Fatalf("a rows: %d", len(h))
	}
	if h[0].BlockHeight != 4 || h[len(h)-1].BlockHeight != 10 {
		t.Fatalf("%+v", h)
	}
	// Without keepNegative the negative goes too, but the newest row always stays.
	if _, err := r.PruneSnapshots(ctx, base.Add(time.Hour), false); err != nil {
		t.Fatal(err)
	}
	latest, _ := r.LatestSnapshots(ctx)
	if len(latest) != 2 || latest[0].BlockHeight != 10 {
		t.Fatalf("%+v", latest)
	}
}

func TestRounds(t *testing.T) {
	r, ctx := testRepo(t)
	if _, err := r.LastRound(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		_ = r.RecordRound(ctx, Round{BlockHeight: uint64(i), StartedAt: base, FinishedAt: base.Add(time.Duration(i) * time.Second), TokensOK: 100 + i, TokensFailed: i, Provider: "p"})
	}
	rd, err := r.LastRound(ctx)
	if err != nil || rd.BlockHeight != 3 || rd.TokensOK != 103 || rd.TokensFailed != 3 || rd.Provider != "p" {
		t.Fatalf("%+v %v", rd, err)
	}
	if n, _ := r.PruneRounds(ctx, base.Add(2*time.Second)); n != 1 {
		t.Fatal(n)
	}
}

func TestStatesRoundTrip(t *testing.T) {
	r, ctx := testRepo(t)
	huge := new(big.Int).Lsh(big.NewInt(1), 250).String()
	in := []model.TokenState{
		{TokenID: "a", Status: model.StatusRed, NegativeStreak: 5, FirstNegativeBlock: 10, OpenIncidentID: 3, LastBlock: 15,
			MaxDeficit: huge, MaxDeficitBlock: 14, RedBlock: 13, UpdatedAt: base},
		{TokenID: "b", Status: model.StatusUnknown, Stale: true, LastError: "boom", UpdatedAt: base},
	}
	if err := r.SaveStates(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := r.LoadStates(ctx, []string{"a", "zzz"})
	utc := func(s model.TokenState) model.TokenState { s.UpdatedAt = s.UpdatedAt.UTC(); return s }
	if err != nil || len(got) != 1 || utc(got["a"]) != in[0] {
		t.Fatalf("%+v\n%+v %v", got["a"], in[0], err)
	}
	all, _ := r.LoadStates(ctx, nil)
	if len(all) != 2 || utc(all["b"]) != in[1] {
		t.Fatalf("%+v", all)
	}
	in[0].Status, in[0].MaxDeficit = model.StatusGreen, ""
	_ = r.SaveStates(ctx, in[:1])
	got, _ = r.LoadStates(ctx, []string{"a"})
	if got["a"].Status != model.StatusGreen || got["a"].MaxDeficit != "" {
		t.Fatalf("%+v", got["a"])
	}
}

func TestIncidents(t *testing.T) {
	r, ctx := testRepo(t)
	id, err := r.OpenIncident(ctx, model.Incident{TokenID: "a", StartBlock: 10, RedBlock: 13, OpenedAt: base, MaxDeficit: "40", MaxDeficitBlock: 12})
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.OpenIncident(ctx, model.Incident{TokenID: "a", StartBlock: 10, RedBlock: 14, OpenedAt: base, MaxDeficit: "50", MaxDeficitBlock: 14})
	if err != nil || again != id {
		t.Fatalf("OpenIncident not idempotent: %d vs %d (%v)", again, id, err)
	}
	other, _ := r.OpenIncident(ctx, model.Incident{TokenID: "b", StartBlock: 1, RedBlock: 4, OpenedAt: base.Add(time.Hour), MaxDeficit: "1", MaxDeficitBlock: 1})
	if other == id {
		t.Fatal("tokens share incident")
	}
	if err := r.UpdateIncident(ctx, id, "45", 20); err != nil { // lower than 50: ignored
		t.Fatal(err)
	}
	huge := new(big.Int).Lsh(big.NewInt(1), 240).String()
	_ = r.UpdateIncident(ctx, id, huge, 21)
	inc, err := r.GetIncident(ctx, id)
	if err != nil || inc.MaxDeficit != huge || inc.MaxDeficitBlock != 21 || inc.ClosedAt != nil || inc.EndBlock != 0 || inc.StartBlock != 10 {
		t.Fatalf("%+v %v", inc, err)
	}
	closed := base.Add(10 * time.Minute)
	if err := r.CloseIncident(ctx, id, 30, closed, huge, 21); err != nil {
		t.Fatal(err)
	}
	_ = r.CloseIncident(ctx, id, 99, closed.Add(time.Hour), "1", 1) // second close is a no-op
	inc, _ = r.GetIncident(ctx, id)
	if inc.ClosedAt == nil || !inc.ClosedAt.Equal(closed) || inc.EndBlock != 30 {
		t.Fatalf("%+v", inc)
	}
	// After closing, the token can have a new incident.
	id2, err := r.OpenIncident(ctx, model.Incident{TokenID: "a", StartBlock: 50, RedBlock: 54, OpenedAt: base.Add(2 * time.Hour), MaxDeficit: "7", MaxDeficitBlock: 52})
	if err != nil || id2 == id {
		t.Fatal(id2, err)
	}
	open, _ := r.ListIncidents(ctx, IncidentFilter{Status: "open"})
	if len(open) != 2 || open[0].ID != id2 { // newest first
		t.Fatalf("%+v", open)
	}
	if cl, _ := r.ListIncidents(ctx, IncidentFilter{Status: "closed"}); len(cl) != 1 || cl[0].ID != id {
		t.Fatalf("%+v", cl)
	}
	if l, _ := r.ListIncidents(ctx, IncidentFilter{TokenID: "a", Limit: 1}); len(l) != 1 {
		t.Fatalf("%+v", l)
	}
	if _, err := r.ListIncidents(ctx, IncidentFilter{Status: "bogus"}); err == nil {
		t.Fatal("want error")
	}
	if _, err := r.GetIncident(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}

	ev := []model.IncidentEvent{
		{IncidentID: id, BlockHeight: 12, ReceiptID: "r1", TxHash: "tx1", Standard: "nep245", Event: "mt_mint", TokenID: "a", Amount: "100", Raw: `{"x":1}`},
		{IncidentID: id, BlockHeight: 11, ReceiptID: "r0", Standard: "dip4", Event: "ft_withdraw", Raw: `{"y":2}`},
	}
	if err := r.AddIncidentEvents(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if err := r.AddIncidentEvents(ctx, ev); err != nil { // duplicates ignored
		t.Fatal(err)
	}
	got, err := r.IncidentEvents(ctx, id)
	if err != nil || len(got) != 2 || got[0].ReceiptID != "r0" || got[1] != ev[0] {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSubscriptions(t *testing.T) {
	r, ctx := testRepo(t)
	s, err := r.AddSubscription(ctx, alert.Subscription{Channel: "telegram", Target: "42", TokenID: "a"})
	if err != nil || s.ID == 0 || s.TokenID != "a" {
		t.Fatal(s, err)
	}
	s2, _ := r.AddSubscription(ctx, alert.Subscription{Channel: "telegram", Target: "42", TokenID: "a"})
	if s2.ID != s.ID {
		t.Fatal("duplicate created")
	}
	_, _ = r.AddSubscription(ctx, alert.Subscription{Channel: "telegram", Target: "43"}) // defaults to *
	_, _ = r.AddSubscription(ctx, alert.Subscription{Channel: "webhook", Target: "https://x/h", TokenID: "a", Secret: "s1"})
	w, _ := r.AddSubscription(ctx, alert.Subscription{Channel: "webhook", Target: "https://x/h", TokenID: "a"})
	if w.Secret != "s1" {
		t.Fatalf("empty secret overwrote: %+v", w)
	}
	w, _ = r.AddSubscription(ctx, alert.Subscription{Channel: "webhook", Target: "https://x/h", TokenID: "a", Secret: "s2"})
	if w.Secret != "s2" {
		t.Fatal("secret not updated")
	}
	if _, err := r.AddSubscription(ctx, alert.Subscription{Channel: "sms", Target: "1"}); err == nil {
		t.Fatal("channel check not enforced")
	}
	got, _ := r.SubscriptionsForToken(ctx, "telegram", "a")
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	got, _ = r.SubscriptionsForToken(ctx, "telegram", "zzz")
	if len(got) != 1 || got[0].Target != "43" || got[0].TokenID != "*" {
		t.Fatalf("%+v", got)
	}
	if l, _ := r.ListSubscriptions(ctx, "telegram", "42"); len(l) != 1 {
		t.Fatal(l)
	}
	if ok, _ := r.RemoveSubscription(ctx, "telegram", "42", "a"); !ok {
		t.Fatal("not removed")
	}
	if ok, _ := r.RemoveSubscription(ctx, "telegram", "42", "a"); ok {
		t.Fatal("removed twice")
	}
	if ok, _ := r.RemoveSubscription(ctx, "webhook", "https://x/h", ""); !ok {
		t.Fatal("remove all failed")
	}
}

// TestEngineAgainstPostgres runs the alert engine end to end on the real store.
func TestEngineAgainstPostgres(t *testing.T) {
	r, ctx := testRepo(t)
	_ = r.UpsertTokens(ctx, []model.Token{{TokenID: "t", Symbol: "TT", Decimals: 6}})
	q := alert.NewMemoryQueue()
	e := alert.NewEngine(alert.EngineConfig{Alert: alert.Config{K: 2}}, r, r, r)
	gaps := []int64{5, -3, -4, -9, 2}
	for i, g := range gaps {
		b := uint64(100 + i)
		s := mkSnap("t", b, big.NewInt(100), big.NewInt(100+g))
		if _, err := r.InsertSnapshots(ctx, []model.Snapshot{s}); err != nil {
			t.Fatal(err)
		}
		if err := e.Process(ctx, []model.Snapshot{s}, nil); err != nil {
			t.Fatal(err)
		}
	}
	incs, _ := r.ListIncidents(ctx, IncidentFilter{})
	if len(incs) != 1 || incs[0].StartBlock != 101 || incs[0].RedBlock != 102 || incs[0].EndBlock != 104 ||
		incs[0].MaxDeficit != "9" || incs[0].MaxDeficitBlock != 103 || incs[0].ClosedAt == nil {
		t.Fatalf("%+v", incs)
	}
	st, _ := r.LoadStates(ctx, []string{"t"})
	if st["t"].Status != model.StatusGreen || st["t"].OpenIncidentID != 0 || st["t"].LastBlock != 104 {
		t.Fatalf("%+v", st["t"])
	}
	if n, err := alert.NewRelay(alert.RelayConfig{}, r, q).Flush(ctx); err != nil || n != 3 {
		t.Fatalf("relay moved %d: %v", n, err)
	}
	var kinds []string
	for {
		d, _ := q.Dequeue(ctx, 0)
		if d == nil {
			break
		}
		kinds = append(kinds, string(d.Alert.Kind))
	}
	if strings.Join(kinds, ",") != "turned_red,incident_update,recovered" {
		t.Fatalf("alerts %v", kinds)
	}
}

func TestKV(t *testing.T) {
	r, ctx := testRepo(t)
	if _, err := r.GetKV(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key: %v", err)
	}
	if err := r.SetKV(ctx, "k", "1"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetKV(ctx, "k", "2"); err != nil {
		t.Fatal(err)
	}
	if v, err := r.GetKV(ctx, "k"); err != nil || v != "2" {
		t.Fatalf("kv = %q %v", v, err)
	}
}

// TestInTxRollsBackIncidentStateAndOutbox checks the atomicity the alert
// engine relies on: a failure after the incident write undoes it, the state
// and the outbox rows together.
func TestInTxRollsBackIncidentStateAndOutbox(t *testing.T) {
	r, ctx := testRepo(t)
	boom := errors.New("boom")
	err := r.InTx(ctx, func(tx alert.Tx) error {
		if _, err := tx.OpenIncident(ctx, model.Incident{TokenID: "t", StartBlock: 1, RedBlock: 2, MaxDeficit: "5", MaxDeficitBlock: 2}); err != nil {
			return err
		}
		if err := tx.SaveStates(ctx, []model.TokenState{{TokenID: "t", Status: model.StatusRed, OpenIncidentID: 1}}); err != nil {
			return err
		}
		if err := tx.AddOutbox(ctx, []alert.Alert{{Kind: alert.TurnedRed, TokenID: "t"}}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	incs, _ := r.ListIncidents(ctx, IncidentFilter{})
	st, _ := r.LoadStates(ctx, nil)
	pend, _ := r.PendingOutbox(ctx, 10)
	if len(incs) != 0 || len(st) != 0 || len(pend) != 0 {
		t.Fatalf("rollback left %d incidents, %d states, %d outbox rows", len(incs), len(st), len(pend))
	}
	if err := r.InTx(ctx, func(tx alert.Tx) error {
		return tx.AddOutbox(ctx, []alert.Alert{{Kind: alert.TurnedRed, TokenID: "t", Gap: "-1", At: base}, {Kind: alert.Recovered, TokenID: "t"}})
	}); err != nil {
		t.Fatal(err)
	}
	pend, _ = r.PendingOutbox(ctx, 10)
	if len(pend) != 2 || pend[0].Alert.Kind != alert.TurnedRed || pend[0].Alert.Gap != "-1" || pend[0].ID >= pend[1].ID {
		t.Fatalf("%+v", pend)
	}
	if err := r.MarkOutboxEnqueued(ctx, []int64{pend[0].ID}); err != nil {
		t.Fatal(err)
	}
	if pend, _ = r.PendingOutbox(ctx, 10); len(pend) != 1 || pend[0].Alert.Kind != alert.Recovered {
		t.Fatalf("%+v", pend)
	}
	if n, err := r.PruneOutbox(ctx, time.Now().Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("prune %d %v", n, err)
	}
}

// failingOutboxRunner makes the outbox insert fail inside real transactions.
type failingOutboxRunner struct {
	*Repo
	fail bool
}

func (f *failingOutboxRunner) InTx(ctx context.Context, fn func(alert.Tx) error) error {
	return f.Repo.InTx(ctx, func(tx alert.Tx) error {
		return fn(failTx{Tx: tx, fail: f.fail})
	})
}

type failTx struct {
	alert.Tx
	fail bool
}

func (f failTx) AddOutbox(ctx context.Context, a []alert.Alert) error {
	if f.fail {
		return errors.New("outbox insert failed")
	}
	return f.Tx.AddOutbox(ctx, a)
}

// Regression for review finding 1 on the real store: K=2, gaps -5 -5 with the
// alert write failing, then +10 must leave no open incident behind.
func TestEngineOutboxFailureLeavesNoOpenIncident(t *testing.T) {
	r, ctx := testRepo(t)
	_ = r.UpsertTokens(ctx, []model.Token{{TokenID: "t", Symbol: "TT", Decimals: 6}})
	fr := &failingOutboxRunner{Repo: r}
	e := alert.NewEngine(alert.EngineConfig{Alert: alert.Config{K: 2}}, r, fr, r)
	step := func(b uint64, gap int64) error {
		return e.Process(ctx, []model.Snapshot{mkSnap("t", b, big.NewInt(100), big.NewInt(100+gap))}, nil)
	}
	if err := step(1, -5); err != nil {
		t.Fatal(err)
	}
	fr.fail = true
	if err := step(2, -5); err == nil {
		t.Fatal("want error")
	}
	fr.fail = false
	if err := step(3, 10); err != nil {
		t.Fatal(err)
	}
	open, _ := r.ListIncidents(ctx, IncidentFilter{Status: "open"})
	if len(open) != 0 {
		t.Fatalf("open incidents left behind: %+v", open)
	}
	if pend, _ := r.PendingOutbox(ctx, 10); len(pend) != 0 {
		t.Fatalf("outbox: %+v", pend)
	}
}

// Regression for review finding 3: two items of one log line on the same
// token (alice -100 / bob +100) must both be stored, and re-attaching is
// still a no-op.
func TestIncidentEventsKeepDistinctItemsOfOneLog(t *testing.T) {
	r, ctx := testRepo(t)
	id, err := r.OpenIncident(ctx, model.Incident{TokenID: "t", StartBlock: 1, RedBlock: 2, MaxDeficit: "1", MaxDeficitBlock: 2})
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"standard":"dip4","event":"token_diff","data":[{"account_id":"alice.near","diff":{"nep141:wrap.near":"-100"}},{"account_id":"bob.near","diff":{"nep141:wrap.near":"100"}}]}`
	mint := `{"standard":"nep245","event":"mt_mint","data":[{"owner_id":"a","token_ids":["x"],"amounts":["5"]},{"owner_id":"b","token_ids":["x"],"amounts":["5"]}]}`
	evs := []model.IncidentEvent{
		{IncidentID: id, BlockHeight: 10, ReceiptID: "r1", Standard: "dip4", Event: "token_diff", TokenID: "nep141:wrap.near", Amount: "-100", Raw: raw, ItemIndex: 0},
		{IncidentID: id, BlockHeight: 10, ReceiptID: "r1", Standard: "dip4", Event: "token_diff", TokenID: "nep141:wrap.near", Amount: "100", Raw: raw, ItemIndex: 1},
		{IncidentID: id, BlockHeight: 10, ReceiptID: "r2", Standard: "nep245", Event: "mt_mint", TokenID: "x", Amount: "5", Raw: mint, ItemIndex: 0},
		{IncidentID: id, BlockHeight: 10, ReceiptID: "r2", Standard: "nep245", Event: "mt_mint", TokenID: "x", Amount: "5", Raw: mint, ItemIndex: 1},
		{IncidentID: id, BlockHeight: 10, ReceiptID: "r2", Standard: "nep245", Event: "mt_mint", TokenID: "x", Amount: "5", Raw: mint, LogIndex: 1, ItemIndex: 0},
	}
	for i := 0; i < 2; i++ { // second pass: idempotent
		if err := r.AddIncidentEvents(ctx, evs); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.IncidentEvents(ctx, id)
	if err != nil || len(got) != 5 {
		t.Fatalf("stored %d events (want 5): %v %+v", len(got), err, got)
	}
}

// Finding 2: a token with an open incident is never deactivated.
func TestDeactivateTokensExceptKeepsOpenIncidents(t *testing.T) {
	r, ctx := testRepo(t)
	_ = r.UpsertTokens(ctx, []model.Token{{TokenID: "a"}, {TokenID: "b"}, {TokenID: "c"}})
	id, _ := r.OpenIncident(ctx, model.Incident{TokenID: "b", StartBlock: 1, RedBlock: 2, MaxDeficit: "1", MaxDeficitBlock: 2})
	_ = r.SaveStates(ctx, []model.TokenState{{TokenID: "b", Status: model.StatusRed, OpenIncidentID: id}})
	n, err := r.DeactivateTokensExcept(ctx, []string{"a"})
	if err != nil || n != 1 {
		t.Fatalf("deactivated %d: %v", n, err)
	}
	toks, _ := r.ListTokens(ctx)
	if len(toks) != 2 || toks[0].TokenID != "a" || toks[1].TokenID != "b" {
		t.Fatalf("%+v", toks)
	}
}

// StaleSince round-trips through token_states.
func TestStateStaleSinceRoundTrip(t *testing.T) {
	r, ctx := testRepo(t)
	since := base.Add(time.Minute)
	if err := r.SaveStates(ctx, []model.TokenState{{TokenID: "a", Status: model.StatusGreen, Stale: true, StaleSince: since}, {TokenID: "b", Status: model.StatusGreen}}); err != nil {
		t.Fatal(err)
	}
	got, _ := r.LoadStates(ctx, nil)
	if !got["a"].StaleSince.Equal(since) || !got["b"].StaleSince.IsZero() {
		t.Fatalf("%+v", got)
	}
}

// Review finding 10: a long incident used to return every negative row (~86k
// points a day). Negative rows are now downsampled too, but the sign
// transitions and the deepest deficit of each run always survive.
func TestHistoryDownsamplesLongNegativeRun(t *testing.T) {
	r, ctx := testRepo(t)
	_ = r.UpsertTokens(ctx, []model.Token{{TokenID: "t", Symbol: "T"}})
	// blocks 1..100 healthy; 101..2600 negative (deepest at 1777); 2601..2700 healthy;
	// 2701..2900 negative (deepest at 2850); 2901..3000 healthy.
	const limit = 200
	var snaps []model.Snapshot
	for b := uint64(1); b <= 3000; b++ {
		held := int64(100)
		switch {
		case b >= 101 && b <= 2600:
			held = 100 - 1 - int64(b%7)
			if b == 1777 {
				held = 100 - 50
			}
		case b >= 2701 && b <= 2900:
			held = 100 - 2
			if b == 2850 {
				held = 100 - 30
			}
		}
		snaps = append(snaps, mkSnap("t", b, big.NewInt(100), big.NewInt(held)))
	}
	if _, err := r.InsertSnapshots(ctx, snaps); err != nil {
		t.Fatal(err)
	}
	got, err := r.History(ctx, "t", Range{}, limit)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 2*limit {
		t.Fatalf("a long negative run was not downsampled: %d rows for limit %d", len(got), limit)
	}
	seen := map[uint64]bool{}
	var prev uint64
	for _, s := range got {
		if s.BlockHeight <= prev {
			t.Fatal("not ordered")
		}
		prev = s.BlockHeight
		seen[s.BlockHeight] = true
	}
	must := map[string]uint64{
		"range start": 1, "range end": 3000,
		"run 1 first": 101, "run 1 last": 2600, "run 1 max deficit": 1777,
		"run 2 first": 2701, "run 2 last": 2900, "run 2 max deficit": 2850,
	}
	for what, b := range must {
		if !seen[b] {
			t.Errorf("%s (block %d) dropped", what, b)
		}
	}
	// Below the limit every negative row is still returned.
	small, _ := r.History(ctx, "t", Range{FromBlock: 2701, ToBlock: 2900}, limit)
	if len(small) != 200 {
		t.Fatalf("short run must be returned in full: %d", len(small))
	}
}
