package summary

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/model"
)

type fakeStore struct {
	mu      sync.Mutex
	inc     model.Incident
	summary string
	setErr  error
}

func (f *fakeStore) GetIncident(context.Context, int64) (model.Incident, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.inc
	i.Summary = f.summary
	return i, nil
}
func (f *fakeStore) SetIncidentSummary(_ context.Context, _ int64, s string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	f.summary = s
	return nil
}

func incident() model.Incident {
	t := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	c := t.Add(time.Hour)
	return model.Incident{ID: 5, TokenID: "nep141:eth.omft.near", StartBlock: 100, RedBlock: 104, EndBlock: 200,
		OpenedAt: t, ClosedAt: &c, MaxDeficit: "1500000000000000000", MaxDeficitBlock: 150}
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestSummarizeOK(t *testing.T) {
	var gotAuth, gotPath string
	var req chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&req)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"  ETH was short by 1.5 for an hour.  "}}]}`))
	}))
	defer srv.Close()
	st := &fakeStore{inc: incident()}
	s, _ := New(context.Background(), Config{BaseURL: srv.URL + "/v1/", APIKey: "k3y", Model: "m", Logger: quiet}, st)
	if err := s.Notify(context.Background(), alert.Alert{Kind: alert.Recovered, IncidentID: 5, Symbol: "ETH", Decimals: 18}); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if st.summary != "ETH was short by 1.5 for an hour." {
		t.Fatalf("summary = %q", st.summary)
	}
	if gotPath != "/v1/chat/completions" || gotAuth != "Bearer k3y" || req.Model != "m" || len(req.Messages) != 2 {
		t.Fatalf("path=%s auth=%s req=%+v", gotPath, gotAuth, req)
	}
	user := req.Messages[1].Content
	for _, want := range []string{"ETH", "1.5 ETH at block 150", "first block with held < owed: 100", "duration 1h0m0s"} {
		if !strings.Contains(user, want) {
			t.Errorf("prompt lacks %q:\n%s", want, user)
		}
	}
}

func TestOnlyRecoveredTriggers(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	s, _ := New(context.Background(), Config{BaseURL: srv.URL, Model: "m", Logger: quiet}, &fakeStore{inc: incident()})
	for _, k := range []alert.Kind{alert.TurnedRed, alert.IncidentUpdate, alert.TurnedAmber} {
		_ = s.Notify(context.Background(), alert.Alert{Kind: k, IncidentID: 5})
	}
	s.Wait()
	if calls != 0 {
		t.Fatalf("LLM called %d times", calls)
	}
}

func TestFailuresNeverReturnErrors(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"500":     func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nope", 500) },
		"garbage": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
		"empty":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"choices":[]}`)) },
		"blank": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":" "}}]}`))
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()
			st := &fakeStore{inc: incident()}
			s, _ := New(context.Background(), Config{BaseURL: srv.URL, Model: "m", Logger: quiet}, st)
			if err := s.Notify(context.Background(), alert.Alert{Kind: alert.Recovered, IncidentID: 5}); err != nil {
				t.Fatalf("Notify returned %v; summaries must never affect alerts", err)
			}
			s.Wait()
			if st.summary != "" {
				t.Fatalf("summary stored on failure: %q", st.summary)
			}
			if err := s.Summarize(context.Background(), alert.Alert{IncidentID: 5}); err == nil {
				t.Fatal("Summarize should report the failure")
			}
		})
	}
}

func TestTimeoutAndStoreError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()
	s, _ := New(context.Background(), Config{BaseURL: srv.URL, Model: "m", Timeout: 50 * time.Millisecond, Logger: quiet}, &fakeStore{inc: incident()})
	start := time.Now()
	if err := s.Summarize(context.Background(), alert.Alert{IncidentID: 5}); err == nil || time.Since(start) > time.Second {
		t.Fatalf("timeout not enforced: %v after %v", err, time.Since(start))
	}

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"x"}}]}`))
	}))
	defer ok.Close()
	s2, _ := New(context.Background(), Config{BaseURL: ok.URL, Model: "m", Logger: quiet}, &fakeStore{inc: incident(), setErr: errors.New("db")})
	if err := s2.Summarize(context.Background(), alert.Alert{IncidentID: 5}); err == nil {
		t.Fatal("expected store error")
	}
}

func TestExistingSummaryNotOverwritten(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer srv.Close()
	st := &fakeStore{inc: incident(), summary: "already"}
	s, _ := New(context.Background(), Config{BaseURL: srv.URL, Model: "m", Logger: quiet}, st)
	if err := s.Summarize(context.Background(), alert.Alert{IncidentID: 5}); err != nil || calls != 0 || st.summary != "already" {
		t.Fatalf("err=%v calls=%d summary=%q", err, calls, st.summary)
	}
}

func TestNewValidates(t *testing.T) {
	if _, err := New(context.Background(), Config{}, nil); err == nil {
		t.Fatal("expected error for empty config")
	}
}

func TestRedactKey(t *testing.T) {
	err := redactKey(errors.New("Get https://x/?key=sekret failed"), "sekret")
	if strings.Contains(err.Error(), "sekret") {
		t.Fatal(err)
	}
}
