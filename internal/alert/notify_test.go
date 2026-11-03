package alert

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emma31-dev/argus/internal/model"
)

func sampleAlert(kind Kind) Alert {
	return Alert{
		Kind: kind, TokenID: "nep141:a_b.near", Symbol: "A<B>&C", Decimals: 6,
		Gap: "-2500000", MaxDeficit: "3000000", StartBlock: 10, Block: 14, IncidentID: 5,
		DashboardURL: "https://argus.example/token/nep141%3Aa?x=1&y=\"2\"", At: t0, DurationSeconds: 125,
	}
}

func TestFormatTelegram(t *testing.T) {
	red := FormatTelegram(sampleAlert(TurnedRed))
	for _, want := range []string{
		"<b>RED: A&lt;B&gt;&amp;C</b>", "Deficit: <b>2.5 A&lt;B&gt;&amp;C</b>",
		"<code>nep141:a_b.near</code>", "First negative block: 10, current block: 14", "Incident #5",
		`<a href="https://argus.example/token/nep141%3Aa?x=1&amp;y=&quot;2&quot;">`,
	} {
		if !strings.Contains(red, want) {
			t.Errorf("red message missing %q:\n%s", want, red)
		}
	}
	if strings.Contains(red, "A<B>") {
		t.Error("unescaped symbol")
	}
	upd := FormatTelegram(sampleAlert(IncidentUpdate))
	if !strings.Contains(upd, "UPDATE") || !strings.Contains(upd, "Max deficit: 3 A&lt;B&gt;&amp;C") {
		t.Errorf("update:\n%s", upd)
	}
	rec := FormatTelegram(sampleAlert(Recovered))
	if !strings.Contains(rec, "RECOVERED") || !strings.Contains(rec, "Duration: 2m5s") || !strings.Contains(rec, "recovered at block 14") {
		t.Errorf("recovered:\n%s", rec)
	}
	if EscapeHTML(`a&b<c>"d"`) != `a&amp;b&lt;c&gt;"d"` {
		t.Error("EscapeHTML")
	}
}

type tgServer struct {
	*httptest.Server
	mu    sync.Mutex
	sent  []map[string]any
	paths []string
	fail  map[string]int // chat id -> http-style error code to return
}

func newTGServer(t *testing.T) *tgServer {
	s := &tgServer{fail: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.paths = append(s.paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			chat, _ := m["chat_id"].(string)
			if code := s.fail[chat]; code != 0 {
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": code, "description": "nope"})
				return
			}
			s.sent = append(s.sent, m)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []any{}})
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *tgServer) chats() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, m := range s.sent {
		out = append(out, m["chat_id"].(string))
	}
	return out
}

func TestTelegramNotifyRoutingThrottleAndTokenSafety(t *testing.T) {
	ctx := context.Background()
	srv := newTGServer(t)
	subs := &MemorySubscriptions{}
	_, _ = subs.AddSubscription(ctx, Subscription{Channel: ChannelTelegram, Target: "111", TokenID: "nep141:a_b.near"})
	_, _ = subs.AddSubscription(ctx, Subscription{Channel: ChannelTelegram, Target: "222", TokenID: AllTokens})
	_, _ = subs.AddSubscription(ctx, Subscription{Channel: ChannelTelegram, Target: "333", TokenID: "nep141:other"})
	_, _ = subs.AddSubscription(ctx, Subscription{Channel: ChannelWebhook, Target: "http://x", TokenID: AllTokens})
	now := t0
	tg, err := NewTelegram(TelegramConfig{Token: "SECRET:TOKEN", BaseURL: srv.URL, DefaultChatID: "999", Now: func() time.Time { return now }}, subs)
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.Notify(ctx, sampleAlert(TurnedRed)); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range srv.chats() {
		got[c] = true
	}
	if len(got) != 3 || !got["999"] || !got["111"] || !got["222"] {
		t.Fatalf("recipients: %v", srv.chats())
	}
	if srv.sent[0]["parse_mode"] != "HTML" {
		t.Error("parse_mode")
	}
	if !strings.HasPrefix(srv.paths[0], "/botSECRET:TOKEN/") {
		t.Errorf("path %s", srv.paths[0])
	}
	// Retrying the same alert does not re-send.
	n := len(srv.sent)
	_ = tg.Notify(ctx, sampleAlert(TurnedRed))
	if len(srv.sent) != n {
		t.Error("duplicate send on retry")
	}

	// IncidentUpdate throttle: first goes out, next within 10 min is dropped, later one goes out.
	u1 := sampleAlert(IncidentUpdate)
	u1.Block = 20
	_ = tg.Notify(ctx, u1)
	n = len(srv.sent)
	u2 := u1
	u2.Block = 21
	_ = tg.Notify(ctx, u2)
	if len(srv.sent) != n {
		t.Error("update not throttled")
	}
	now = now.Add(11 * time.Minute)
	u3 := u1
	u3.Block = 22
	_ = tg.Notify(ctx, u3)
	if len(srv.sent) != n+3 {
		t.Errorf("update after interval not sent: %d vs %d", len(srv.sent), n+3)
	}
}

func TestTelegramErrorsNeverLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused; the error would normally embed the URL
	tg, _ := NewTelegram(TelegramConfig{Token: "SECRET:TOKEN", BaseURL: url, DefaultChatID: "1"}, nil)
	err := tg.SendMessage(context.Background(), "1", "hi")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
}

func TestTelegramPermanentErrorDropsRecipientTransientRetries(t *testing.T) {
	ctx := context.Background()
	srv := newTGServer(t)
	subs := &MemorySubscriptions{}
	_, _ = subs.AddSubscription(ctx, Subscription{Channel: ChannelTelegram, Target: "111", TokenID: AllTokens})
	_, _ = subs.AddSubscription(ctx, Subscription{Channel: ChannelTelegram, Target: "222", TokenID: AllTokens})
	srv.fail["111"] = 403 // blocked the bot
	srv.fail["222"] = 500
	tg, _ := NewTelegram(TelegramConfig{Token: "T", BaseURL: srv.URL}, subs)
	err := tg.Notify(ctx, sampleAlert(TurnedRed))
	if err == nil {
		t.Fatal("want error for 500 recipient")
	}
	if l, _ := subs.ListSubscriptions(ctx, ChannelTelegram, "111"); len(l) != 0 {
		t.Error("blocked user's subscription should be removed")
	}
	srv.mu.Lock()
	delete(srv.fail, "222")
	srv.mu.Unlock()
	if err := tg.Notify(ctx, sampleAlert(TurnedRed)); err != nil {
		t.Fatal(err)
	}
	if c := srv.chats(); len(c) != 1 || c[0] != "222" {
		t.Fatalf("chats %v", c)
	}
}

func TestNewTelegramRequiresToken(t *testing.T) {
	if _, err := NewTelegram(TelegramConfig{}, nil); err == nil {
		t.Fatal("want error")
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		in   string
		cmd  string
		args []string
		ok   bool
	}{
		{"/start", "start", nil, true},
		{"/Subscribe wNEAR", "subscribe", []string{"wNEAR"}, true},
		{"/subscribe@ArgusBot  nep141:wrap.near ", "subscribe", []string{"nep141:wrap.near"}, true},
		{"  /list", "list", nil, true},
		{"hello /start", "", nil, false},
		{"", "", nil, false},
		{"/", "", nil, false},
		{"/@bot", "", nil, false},
	}
	for _, tc := range tests {
		cmd, args, ok := ParseCommand(tc.in)
		if cmd != tc.cmd || ok != tc.ok || strings.Join(args, ",") != strings.Join(tc.args, ",") {
			t.Errorf("ParseCommand(%q) = %q %v %v", tc.in, cmd, args, ok)
		}
	}
}

type multiTokens []model.Token

func (m multiTokens) ListTokens(context.Context) ([]model.Token, error) { return m, nil }

func TestBotCommands(t *testing.T) {
	ctx := context.Background()
	subs := &MemorySubscriptions{}
	tg, _ := NewTelegram(TelegramConfig{Token: "T"}, subs)
	tokens := multiTokens{
		{TokenID: "nep141:wrap.near", Symbol: "wNEAR"},
		{TokenID: "nep141:usdc.a", Symbol: "USDC"},
		{TokenID: "nep141:usdc.b", Symbol: "usdc"},
	}
	b := NewBot(tg, subs, tokens, StatusFunc(func(context.Context) (string, error) { return "all <green>", nil }))
	say := func(s string) string {
		r, ok := b.HandleMessage(ctx, "42", s)
		if !ok {
			t.Fatalf("no reply to %q", s)
		}
		return r
	}
	if r := say("/start"); !strings.Contains(r, "/subscribe") {
		t.Error(r)
	}
	if r := say("/subscribe wnear"); !strings.Contains(r, "Subscribed to <b>wnear</b>") {
		t.Error(r)
	}
	if r := say("/subscribe USDC"); !strings.Contains(r, "2 tokens") {
		t.Error(r)
	}
	if r := say("/subscribe nope"); !strings.Contains(r, "No token matches") {
		t.Error(r)
	}
	if r := say("/subscribe"); !strings.Contains(r, "Usage") {
		t.Error(r)
	}
	l := say("/list")
	if !strings.Contains(l, "wNEAR") || strings.Count(l, "- ") != 3 {
		t.Error(l)
	}
	if r := say("/subscribe all"); !strings.Contains(r, "all tokens") {
		t.Error(r)
	}
	if got, _ := subs.SubscriptionsForToken(ctx, ChannelTelegram, "nep141:anything"); len(got) != 1 || got[0].Target != "42" {
		t.Errorf("wildcard sub: %v", got)
	}
	if r := say("/unsubscribe nep141:wrap.near"); r != "Unsubscribed." {
		t.Error(r)
	}
	if r := say("/unsubscribe nep141:wrap.near"); !strings.Contains(r, "not subscribed") {
		t.Error(r)
	}
	if r := say("/status"); r != "all &lt;green&gt;" {
		t.Error(r)
	}
	if r := say("/frobnicate"); !strings.Contains(r, "Unknown") {
		t.Error(r)
	}
	if _, ok := b.HandleMessage(ctx, "42", "just chatting"); ok {
		t.Error("non-command should not reply")
	}
	// Subscriptions are per chat.
	if l, _ := subs.ListSubscriptions(ctx, ChannelTelegram, "7"); len(l) != 0 {
		t.Error("leak across chats")
	}
}

func TestBotRunPollsAndReplies(t *testing.T) {
	var mu sync.Mutex
	var replies []string
	var offsets []float64
	served := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			offsets = append(offsets, m["offset"].(float64))
			if !served {
				served = true
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []any{
					map[string]any{"update_id": 10, "message": map[string]any{"text": "/start", "chat": map[string]any{"id": 5}}},
				}})
				return
			}
			time.Sleep(20 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []any{}})
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			replies = append(replies, m["chat_id"].(string))
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{}})
		}
	}))
	defer srv.Close()
	subs := &MemorySubscriptions{}
	tg, _ := NewTelegram(TelegramConfig{Token: "T", BaseURL: srv.URL}, subs)
	b := NewBot(tg, subs, multiTokens{}, nil)
	b.PollSeconds = 1
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := b.Run(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(replies) != 1 || replies[0] != "5" {
		t.Errorf("replies %v", replies)
	}
	if len(offsets) < 2 || offsets[0] != 0 || offsets[1] != 11 {
		t.Errorf("offsets %v", offsets)
	}
}

func TestWebhookSigningAndDelivery(t *testing.T) {
	ctx := context.Background()
	var gotSig, gotEvent string
	var gotBody []byte
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		gotSig, gotEvent = r.Header.Get(SignatureHeader), r.Header.Get("X-Argus-Event")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	subs := &MemorySubscriptions{}
	_, _ = subs.AddSubscription(ctx, Subscription{Channel: ChannelWebhook, Target: srv.URL, TokenID: AllTokens, Secret: "s3cret"})
	w := NewWebhook(WebhookConfig{AllowPrivate: true}, subs)
	a := sampleAlert(TurnedRed)
	if err := w.Notify(ctx, a); err != nil {
		t.Fatal(err)
	}
	if gotSig != SignPayload("s3cret", gotBody) || !strings.HasPrefix(gotSig, "sha256=") || len(gotSig) != len("sha256=")+64 {
		t.Errorf("signature %q", gotSig)
	}
	if gotEvent != "turned_red" {
		t.Error(gotEvent)
	}
	var back Alert
	if err := json.Unmarshal(gotBody, &back); err != nil || back.TokenID != a.TokenID || back.Gap != a.Gap {
		t.Errorf("body %s", gotBody)
	}
	// Known-answer HMAC (RFC 4231-style sanity: different secret differs).
	if SignPayload("a", []byte("x")) == SignPayload("b", []byte("x")) {
		t.Error("secret ignored")
	}
	_ = w.Notify(ctx, a)
	if calls.Load() != 1 {
		t.Error("duplicate webhook on retry")
	}
}

func TestWebhookFailureAndPrivateBlocking(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	subs := &MemorySubscriptions{}
	_, _ = subs.AddSubscription(ctx, Subscription{Channel: ChannelWebhook, Target: srv.URL + "/hook?key=abc", TokenID: AllTokens})
	w := NewWebhook(WebhookConfig{AllowPrivate: true}, subs)
	err := w.Notify(ctx, sampleAlert(TurnedRed))
	if err == nil || strings.Contains(err.Error(), "key=abc") {
		t.Fatalf("err %v", err)
	}
	// Default config refuses loopback targets (SSRF guard).
	w2 := NewWebhook(WebhookConfig{}, subs)
	if err := w2.Notify(ctx, sampleAlert(TurnedRed)); err == nil || strings.Contains(err.Error(), "key=abc") {
		t.Fatalf("loopback should be blocked: %v", err)
	}
}

func TestWebhookTimeout(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer srv.Close()
	subs := &MemorySubscriptions{}
	_, _ = subs.AddSubscription(ctx, Subscription{Channel: ChannelWebhook, Target: srv.URL, TokenID: AllTokens})
	w := NewWebhook(WebhookConfig{AllowPrivate: true, Timeout: 50 * time.Millisecond}, subs)
	start := time.Now()
	if err := w.Notify(ctx, sampleAlert(TurnedRed)); err == nil || time.Since(start) > 250*time.Millisecond {
		t.Fatalf("timeout not enforced: %v", err)
	}
}

func TestValidateWebhookURL(t *testing.T) {
	for u, ok := range map[string]bool{"https://a.b/c": true, "http://a": true, "ftp://a": false, "/x": false, "": false, "https://": false} {
		if (ValidateWebhookURL(u) == nil) != ok {
			t.Errorf("%q", u)
		}
	}
}

type flaky struct {
	name  string
	fails int
	calls int
}

func (f *flaky) Name() string { return f.name }
func (f *flaky) Notify(context.Context, Alert) error {
	f.calls++
	if f.calls <= f.fails {
		return errors.New("boom")
	}
	return nil
}

func newTestDispatcher(q Queue, attempts int, ns ...Notifier) (*Dispatcher, *[]time.Duration) {
	d := NewDispatcher(DispatcherConfig{MaxAttempts: attempts, BaseBackoff: time.Second, MaxBackoff: 3 * time.Second}, q, ns...)
	var slept []time.Duration
	d.sleep = func(_ context.Context, dur time.Duration) error { slept = append(slept, dur); return nil }
	return d, &slept
}

func TestDispatcherRetriesWithBackoff(t *testing.T) {
	ctx := context.Background()
	q := NewMemoryQueue()
	_ = q.Enqueue(ctx, sampleAlert(TurnedRed))
	f := &flaky{name: "f", fails: 2}
	d, slept := newTestDispatcher(q, 5, f)
	if ok, err := d.ProcessOne(ctx, 0); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if f.calls != 3 || len(q.DeadLetters()) != 0 || len(q.inflight) != 0 {
		t.Fatalf("calls=%d dead=%d inflight=%d", f.calls, len(q.DeadLetters()), len(q.inflight))
	}
	if len(*slept) != 2 || (*slept)[0] != time.Second || (*slept)[1] != 2*time.Second {
		t.Errorf("backoff %v", *slept)
	}
	if ok, _ := d.ProcessOne(ctx, 0); ok {
		t.Error("queue should be empty")
	}
}

func TestDispatcherDeadLettersAndRetriesOnlyFailed(t *testing.T) {
	ctx := context.Background()
	q := NewMemoryQueue()
	_ = q.Enqueue(ctx, sampleAlert(TurnedRed))
	good := &flaky{name: "good"}
	bad := &flaky{name: "bad", fails: 100}
	d, slept := newTestDispatcher(q, 4, good, bad)
	if _, err := d.ProcessOne(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if good.calls != 1 || bad.calls != 4 {
		t.Fatalf("good=%d bad=%d", good.calls, bad.calls)
	}
	dead := q.DeadLetters()
	if len(dead) != 1 || !strings.Contains(dead[0].Reason, "bad: boom") || dead[0].Alert.TokenID != "nep141:a_b.near" {
		t.Fatalf("dead %+v", dead)
	}
	// 1s, 2s, then capped at 3s (MaxBackoff); no sleep after the final attempt.
	if len(*slept) != 3 || (*slept)[2] != 3*time.Second {
		t.Errorf("backoff %v", *slept)
	}
	if len(q.inflight) != 0 {
		t.Error("dead letter must leave in-flight set")
	}
}

func TestDispatcherRunAndRecover(t *testing.T) {
	q := NewMemoryQueue()
	ctx := context.Background()
	_ = q.Enqueue(ctx, sampleAlert(TurnedRed))
	if d, _ := q.Dequeue(ctx, 0); d == nil { // simulate crash mid-delivery
		t.Fatal("no delivery")
	}
	f := &flaky{name: "f"}
	disp := NewDispatcher(DispatcherConfig{PollWait: 10 * time.Millisecond}, q, f)
	rctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if err := disp.Run(rctx); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatalf("recovered alert delivered %d times", f.calls)
	}
}
