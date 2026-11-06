// Package summary writes optional plain-English incident summaries with any
// OpenAI-compatible chat-completions endpoint (off by default).
//
// The model only ever writes text. It never sees live data beyond the
// incident record, it cannot influence alert state, and every failure is
// logged and dropped: the Notifier always reports success to the alert
// dispatcher, so a broken LLM can never delay, retry or lose an alert.
package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/model"
)

const (
	maxSummaryChars = 1200
	maxResponseSize = 1 << 20
	maxInFlight     = 4
)

// Store is the part of the repository the summariser needs.
type Store interface {
	GetIncident(ctx context.Context, id int64) (model.Incident, error)
	SetIncidentSummary(ctx context.Context, id int64, summary string) error
}

// Config configures a Summarizer.
type Config struct {
	BaseURL string // e.g. https://cloud-api.near.ai/v1 (no trailing /chat/completions)
	APIKey  string
	Model   string
	Timeout time.Duration // per request, default 30s
	HTTP    *http.Client
	Logger  *slog.Logger
}

// Summarizer generates and stores incident summaries. It implements
// alert.Notifier so it can ride the alert dispatcher: on a Recovered alert
// (the incident is closed and its record is complete) it writes a summary in
// the background.
type Summarizer struct {
	cfg   Config
	store Store
	http  *http.Client
	log   *slog.Logger
	base  context.Context
	sem   chan struct{}
	wg    sync.WaitGroup
}

var _ alert.Notifier = (*Summarizer)(nil)

// New builds a Summarizer. ctx bounds the background work: cancelling it
// aborts in-flight requests.
func New(ctx context.Context, cfg Config, st Store) (*Summarizer, error) {
	if cfg.BaseURL == "" || cfg.Model == "" {
		return nil, fmt.Errorf("summary: LLM_BASE_URL and LLM_MODEL are required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	hc := cfg.HTTP
	if hc == nil {
		hc = &http.Client{}
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Summarizer{cfg: cfg, store: st, http: hc, log: log, base: ctx, sem: make(chan struct{}, maxInFlight)}, nil
}

// Name implements alert.Notifier.
func (s *Summarizer) Name() string { return "llm-summary" }

// Notify implements alert.Notifier. It never returns an error.
func (s *Summarizer) Notify(_ context.Context, a alert.Alert) error {
	if a.Kind != alert.Recovered || a.IncidentID == 0 {
		return nil
	}
	select {
	case s.sem <- struct{}{}:
	default:
		s.log.Warn("llm summary skipped: too many in flight", "incident", a.IncidentID)
		return nil
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.sem }()
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("llm summary panicked", "incident", a.IncidentID, "panic", r)
			}
		}()
		if err := s.Summarize(s.base, a); err != nil {
			s.log.Warn("llm summary failed", "incident", a.IncidentID, "err", err)
		}
	}()
	return nil
}

// Wait blocks until background summaries finish (tests, shutdown).
func (s *Summarizer) Wait() { s.wg.Wait() }

// Summarize generates and stores the summary for the incident of a. It skips
// incidents that already have one.
func (s *Summarizer) Summarize(ctx context.Context, a alert.Alert) error {
	inc, err := s.store.GetIncident(ctx, a.IncidentID)
	if err != nil {
		return fmt.Errorf("get incident: %w", err)
	}
	if inc.Summary != "" {
		return nil
	}
	text, err := s.complete(ctx, BuildPrompt(inc, a.Symbol, a.Decimals))
	if err != nil {
		return err
	}
	if err := s.store.SetIncidentSummary(ctx, inc.ID, text); err != nil {
		return fmt.Errorf("store summary: %w", err)
	}
	s.log.Info("llm summary stored", "incident", inc.ID, "chars", len(text))
	return nil
}

const systemPrompt = "You write short, factual summaries of solvency incidents for a public monitor of NEAR Intents. " +
	"Use only the facts in the user message. Do not guess a cause, do not assign blame, do not give financial advice, " +
	"and do not mention anything that is not provided. Write 2 to 3 plain sentences."

// BuildPrompt renders the incident record, and nothing else, as the user
// message. Amounts are formatted exactly with the token's decimals.
func BuildPrompt(inc model.Incident, symbol string, decimals int) string {
	if symbol == "" {
		symbol = inc.TokenID
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Incident record\n")
	fmt.Fprintf(&b, "- token: %s (%s), %d decimals\n", symbol, inc.TokenID, decimals)
	fmt.Fprintf(&b, "- invariant: the contract must hold at least as much of the token as the ledger owes\n")
	fmt.Fprintf(&b, "- first block with held < owed: %d\n", inc.StartBlock)
	fmt.Fprintf(&b, "- block at which it was confirmed (red): %d\n", inc.RedBlock)
	fmt.Fprintf(&b, "- largest deficit: %s %s at block %d\n", alert.FormatUnits(inc.MaxDeficit, decimals), symbol, inc.MaxDeficitBlock)
	fmt.Fprintf(&b, "- opened at: %s\n", inc.OpenedAt.UTC().Format(time.RFC3339))
	if inc.ClosedAt != nil {
		fmt.Fprintf(&b, "- closed at: %s (end block %d), duration %s\n", inc.ClosedAt.UTC().Format(time.RFC3339), inc.EndBlock,
			inc.ClosedAt.Sub(inc.OpenedAt).Round(time.Second))
	} else {
		fmt.Fprintf(&b, "- status: still open\n")
	}
	return b.String()
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (s *Summarizer) complete(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model: s.cfg.Model, Temperature: 0.2, MaxTokens: 300,
		Messages: []chatMessage{{Role: "system", Content: systemPrompt}, {Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	url := strings.TrimRight(s.cfg.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.cfg.APIKey)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm request: %w", redactKey(err, s.cfg.APIKey))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return "", fmt.Errorf("llm read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 200 {
			msg = msg[:200] + "..."
		}
		return "", fmt.Errorf("llm: HTTP %d: %s", resp.StatusCode, msg)
	}
	var out struct {
		Choices []struct {
			Message chatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("llm: undecodable response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm: response has no choices")
	}
	text := strings.TrimSpace(out.Choices[0].Message.Content)
	if text == "" {
		return "", fmt.Errorf("llm: empty completion")
	}
	if r := []rune(text); len(r) > maxSummaryChars {
		text = string(r[:maxSummaryChars]) + "..."
	}
	return text, nil
}

// redactKey makes sure an API key can never reach the logs through a
// transport error.
func redactKey(err error, key string) error {
	if key == "" || !strings.Contains(err.Error(), key) {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), key, "[redacted]"))
}
