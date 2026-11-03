package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/api"
	"github.com/emma31-dev/argus/internal/events"
	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/snapshot"
	"github.com/emma31-dev/argus/internal/store"
	"github.com/emma31-dev/argus/internal/tokens"
)

// App is the running monitor: discovery, the snapshot loop, the alert
// engine and the HTTP API, all over one Postgres repository.
type App struct {
	cfg    Config
	log    *slog.Logger
	repo   *store.Repo
	rpc    *rpc.Client
	reader *snapshot.Reader
	disc   tokens.Discoverer
	engine *alert.Engine

	mu   sync.RWMutex
	toks []model.Token

	// P1 event stream (off unless ARGUS_EVENTS_ENABLED); set by EnableEvents.
	incEvents atomic.Pointer[incidentEvents]
	follower  *events.Follower

	// backoff bounds, overridable in tests
	minBackoff, maxBackoff time.Duration
}

// errNoTokens means a round was skipped because discovery has not produced
// a token list yet.
var errNoTokens = errors.New("no tokens discovered yet")

// NewApp wires the monitor. The caller owns the repository, RPC client and
// alert queue.
func NewApp(cfg Config, log *slog.Logger, repo *store.Repo, client *rpc.Client, queue alert.Queue) *App {
	a := &App{
		cfg: cfg, log: log, repo: repo, rpc: client,
		reader: &snapshot.Reader{
			Caller: client, IntentsAccount: cfg.IntentsAccount,
			BatchSize: cfg.BatchSize, Concurrency: cfg.Concurrency,
		},
		disc:       buildDiscoverer(cfg, client),
		minBackoff: 5 * time.Second, maxBackoff: 5 * time.Minute,
	}
	a.engine = alert.NewEngine(alert.EngineConfig{
		Alert:        alert.Config{K: cfg.K},
		DashboardURL: cfg.PublicURL,
		LinkFormat:   alert.DashboardLinkFormat,
		Logger:       log,
		OnTransition: func(t alert.Transition) {
			if ie := a.incEvents.Load(); ie != nil {
				ie.OnTransition(t)
			}
		},
	}, repo, repo, queue, repo)
	return a
}

// buildDiscoverer assembles the PRD 5.1 chain: enumeration, 1Click, config.
func buildDiscoverer(cfg Config, c tokens.Caller) tokens.Discoverer {
	chain := &tokens.Chain{
		Enumeration: &tokens.Enumeration{Caller: c, Account: cfg.IntentsAccount},
		OneClick:    &tokens.OneClick{URL: cfg.OneClickURL, Client: &http.Client{Timeout: 30 * time.Second}},
	}
	if cfg.TokensFile != "" {
		chain.Config = &tokens.Config{Path: cfg.TokensFile}
	}
	return chain
}

// discoverScoped runs the discovery chain and keeps only in-scope tokens.
func discoverScoped(ctx context.Context, d tokens.Discoverer) ([]model.Token, string, error) {
	all, source, err := d.Discover(ctx)
	if err != nil {
		return nil, "", err
	}
	var out []model.Token
	for _, t := range all {
		if tokens.InScope(t) {
			t.Chain = tokens.ChainOf(t.TokenID)
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, source, errors.New("discovery returned no in-scope tokens")
	}
	return out, source, nil
}

// Tokens returns the current token list.
func (a *App) Tokens() []model.Token {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return append([]model.Token(nil), a.toks...)
}

func (a *App) setTokens(ts []model.Token) {
	a.mu.Lock()
	a.toks = ts
	a.mu.Unlock()
}

// Discover runs one discovery pass and persists the result. On failure the
// previous token list stays in place.
func (a *App) Discover(ctx context.Context) error {
	ts, source, err := discoverScoped(ctx, a.disc)
	if err != nil {
		return err
	}
	if err := a.repo.UpsertTokens(ctx, ts); err != nil {
		return fmt.Errorf("upsert tokens: %w", err)
	}
	ids := make([]string, len(ts))
	for i, t := range ts {
		ids[i] = t.TokenID
	}
	removed, err := a.repo.DeactivateTokensExcept(ctx, ids)
	if err != nil {
		return fmt.Errorf("deactivate tokens: %w", err)
	}
	a.setTokens(ts)
	a.log.Info("token discovery complete", "tokens", len(ts), "source", source, "deactivated", removed)
	return nil
}

// discoveryLoop discovers at start and then every DiscoveryInterval. A
// failing pass keeps the previous list and retries with exponential backoff.
func (a *App) discoveryLoop(ctx context.Context) {
	// A restart with failing discovery can still use the stored list.
	if stored, err := a.repo.ListTokens(ctx); err == nil && len(stored) > 0 {
		a.setTokens(stored)
		a.log.Info("loaded stored token list", "tokens", len(stored))
	}
	failures := 0
	for ctx.Err() == nil {
		wait := a.cfg.DiscoveryInterval
		if err := a.Discover(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			failures++
			wait = min(a.minBackoff<<min(failures-1, 10), a.maxBackoff, a.cfg.DiscoveryInterval)
			a.log.Error("token discovery failed; keeping the previous list", "err", err,
				"tokens_kept", len(a.Tokens()), "retry_in", wait.String())
		} else {
			failures = 0
		}
		if !sleep(ctx, wait) {
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// RunRound performs one snapshot round: read, store, record, evaluate
// alerts. A non-nil error means the round as a whole did not complete (the
// tokens are then flagged stale).
func (a *App) RunRound(ctx context.Context) error {
	toks := a.Tokens()
	if len(toks) == 0 {
		return errNoTokens
	}
	started := time.Now().UTC()
	res, err := a.reader.Round(ctx, toks)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, snapshot.ErrOwedUnsupported) {
			a.log.Error("intents contract has no mt_batch_supply; all tokens marked stale. "+
				"The event-delta fallback for owed (PRD 5.4) is NOT implemented because the contract source "+
				"confirms mt_batch_supply exists, so this is an unexpected state: check ARGUS_INTENTS_ACCOUNT and the RPC provider",
				"account", a.cfg.IntentsAccount, "err", err)
		} else {
			a.log.Error("snapshot round failed; tokens marked stale", "err", err)
		}
		a.markAllStale(ctx, toks, err)
		return err
	}

	if _, err := a.repo.InsertSnapshots(ctx, res.Snapshots); err != nil {
		a.log.Error("storing snapshots failed", "err", err)
	}
	errs := make(map[string]error, len(res.Errors))
	for _, te := range res.Errors {
		errs[te.TokenID] = te.Err
		a.log.Warn("token read failed", "token", te.TokenID, "err", te.Err)
	}
	if err := a.repo.RecordRound(ctx, store.Round{
		BlockHeight: res.Block.Height, StartedAt: started, FinishedAt: time.Now().UTC(),
		TokensOK: len(res.Snapshots), TokensFailed: len(res.Errors), Provider: res.Provider,
	}); err != nil {
		a.log.Error("recording round failed", "err", err)
	}
	if err := a.engine.Process(ctx, res.Snapshots, errs); err != nil {
		a.log.Error("alert processing reported errors", "err", err)
	}
	a.log.Info("snapshot round complete", "block", res.Block.Height, "provider", res.Provider,
		"tokens_ok", len(res.Snapshots), "tokens_failed", len(res.Errors), "duration_ms", time.Since(started).Milliseconds())
	return nil
}

func (a *App) markAllStale(ctx context.Context, toks []model.Token, cause error) {
	errs := make(map[string]error, len(toks))
	for _, t := range toks {
		errs[t.TokenID] = cause
	}
	if err := a.engine.Process(ctx, nil, errs); err != nil {
		a.log.Error("marking tokens stale failed", "err", err)
	}
}

// snapshotLoop runs a round every Interval until ctx ends. Rounds never
// overlap; when one overruns, the ticks that elapsed are skipped.
func (a *App) snapshotLoop(ctx context.Context) {
	t := time.NewTicker(a.cfg.Interval)
	defer t.Stop()
	roundTimeout := max(3*a.cfg.Interval, 2*time.Minute)
	for {
		start := time.Now()
		rctx, cancel := context.WithTimeout(ctx, roundTimeout)
		err := a.RunRound(rctx)
		cancel()
		if err != nil && ctx.Err() == nil && !errors.Is(err, snapshot.ErrOwedUnsupported) {
			a.log.Warn("snapshot round did not complete", "err", err)
		}
		if ctx.Err() != nil {
			return
		}
		if el := time.Since(start); el > a.cfg.Interval {
			skipped := 0
			for drained := false; !drained; {
				select {
				case <-t.C:
					skipped++
				default:
					drained = true
				}
			}
			a.log.Warn("snapshot round overran the interval; skipping ticks",
				"duration", el.Round(time.Millisecond).String(), "interval", a.cfg.Interval.String(), "skipped_ticks", skipped)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// prune deletes history older than the retention window (negative-gap
// snapshots are kept forever: they are the evidence).
func (a *App) prune(ctx context.Context) {
	cutoff := time.Now().Add(-a.cfg.Retention)
	n, err := a.repo.PruneSnapshots(ctx, cutoff, true)
	if err != nil {
		a.log.Error("pruning snapshots failed", "err", err)
		return
	}
	r, err := a.repo.PruneRounds(ctx, cutoff)
	if err != nil {
		a.log.Error("pruning rounds failed", "err", err)
		return
	}
	a.log.Info("pruned history", "snapshots", n, "rounds", r, "older_than", cutoff.UTC().Format(time.RFC3339))
}

func (a *App) pruneLoop(ctx context.Context) {
	for sleep(ctx, 24*time.Hour) {
		a.prune(ctx)
	}
}

// StatusText is the short plain-text summary for the Telegram /status
// command.
func (a *App) StatusText(ctx context.Context) (string, error) {
	toks, err := a.repo.ListTokens(ctx)
	if err != nil {
		return "", err
	}
	states, err := a.repo.LoadStates(ctx, nil)
	if err != nil {
		return "", err
	}
	counts := map[model.Status]int{}
	var bad []string
	for _, t := range toks {
		st := model.StatusUnknown
		if s, ok := states[t.TokenID]; ok && s.LastBlock > 0 && s.Status != "" {
			st = s.Status
		}
		counts[st]++
		if st == model.StatusRed || st == model.StatusAmber {
			bad = append(bad, fmt.Sprintf("%s %s (streak %d)", strings.ToUpper(string(st)), orID(t), states[t.TokenID].NegativeStreak))
		}
	}
	sort.Strings(bad)
	var b strings.Builder
	fmt.Fprintf(&b, "Argus: %d tokens, %d green, %d amber, %d red, %d unknown.", len(toks),
		counts[model.StatusGreen], counts[model.StatusAmber], counts[model.StatusRed], counts[model.StatusUnknown])
	if round, err := a.repo.LastRound(ctx); err == nil {
		fmt.Fprintf(&b, "\nLast round: block %d, %s ago.", round.BlockHeight, time.Since(round.FinishedAt).Round(time.Second))
	} else {
		b.WriteString("\nNo snapshot round has completed yet.")
	}
	for i, l := range bad {
		if i == 10 {
			fmt.Fprintf(&b, "\n...and %d more", len(bad)-10)
			break
		}
		b.WriteString("\n" + l)
	}
	if a.cfg.PublicURL != "" {
		b.WriteString("\n" + strings.TrimRight(a.cfg.PublicURL, "/"))
	}
	return b.String(), nil
}

func orID(t model.Token) string {
	if t.Symbol != "" {
		return t.Symbol
	}
	return t.TokenID
}

// APIServer builds the HTTP server for the read-only API.
func (a *App) APIServer(version string) *http.Server {
	srv := api.New(api.Config{
		Interval: a.cfg.Interval, K: a.cfg.K, Version: version,
		ReplayPath: a.cfg.ReplayFile, RateLimit: a.cfg.APIRateLimit,
		TrustProxy: a.cfg.TrustProxy, CORSOrigins: a.cfg.CORSOrigins, Logger: a.log,
	}, a.repo, a.rpc)
	return api.NewHTTPServer(a.cfg.HTTPAddr, srv.Handler())
}

// EnableEvents turns on the neardata event stream: a follower that keeps a
// persisted cursor, and attachment of intents.near events to incidents.
// ctx bounds the background attachments.
func (a *App) EnableEvents(ctx context.Context, src events.BlockSource) {
	acct := a.cfg.IntentsAccount
	att := &events.Attacher{Source: src, Account: acct, Store: a.repo}
	a.incEvents.Store(newIncidentEvents(ctx, att, att.MaxBlocks, a.log))
	a.follower = &events.Follower{
		Client: src, Account: acct, Sink: debugSink{a.log}, Cursor: newKVCursor(a.repo, acct), Logger: a.log,
	}
}

// newNeardata builds the neardata client from the configuration.
func newNeardata(cfg Config) *events.Client {
	return &events.Client{BaseURL: cfg.NeardataURL, RPS: cfg.NeardataRPS, APIKey: cfg.NeardataAPIKey}
}

// WaitEvents waits for in-flight incident event attachments.
func (a *App) WaitEvents() {
	if ie := a.incEvents.Load(); ie != nil {
		ie.Wait()
	}
}
