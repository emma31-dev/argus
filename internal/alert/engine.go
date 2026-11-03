package alert

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/emma31-dev/argus/internal/model"
)

// StateStore persists per-token alert state.
type StateStore interface {
	// LoadStates returns the stored state of the given tokens; tokens
	// without stored state are absent from the map.
	LoadStates(ctx context.Context, tokenIDs []string) (map[string]model.TokenState, error)
	SaveStates(ctx context.Context, states []model.TokenState) error
}

// IncidentStore persists incidents. OpenIncident must be idempotent per
// token: if the token already has an open incident it returns that id.
type IncidentStore interface {
	OpenIncident(ctx context.Context, inc model.Incident) (int64, error)
	UpdateIncident(ctx context.Context, id int64, maxDeficit string, maxDeficitBlock uint64) error
	CloseIncident(ctx context.Context, id int64, endBlock uint64, closedAt time.Time, maxDeficit string, maxDeficitBlock uint64) error
	GetIncident(ctx context.Context, id int64) (model.Incident, error)
}

// TokenLister provides token metadata (symbol, decimals) for alerts and bot
// commands.
type TokenLister interface {
	ListTokens(ctx context.Context) ([]model.Token, error)
}

// EngineConfig configures an Engine.
type EngineConfig struct {
	Alert        Config
	DashboardURL string // base URL of the dashboard
	// LinkFormat builds the per-token link from DashboardURL. {base} is the
	// base URL without a trailing slash, {id} the URL-escaped token id.
	// Empty selects the legacy
	// "{base}/token/{id}" (path-escaped id) is used. The SvelteKit dashboard
	// uses DashboardLinkFormat.
	LinkFormat string
	Logger     *slog.Logger
	Now        func() time.Time
	// OnTransition, when set, is called for every transition once its side
	// effects (incident open/update/close, queueing) have succeeded. For
	// TurnedRed the Transition carries the new IncidentID. It runs on the
	// alert path, so it must return quickly and must not fail the round:
	// start a goroutine for slow work. Panics are recovered and logged.
	OnTransition func(Transition)
}

// ErrBlockNotAdvancing is the read error recorded for a token whose snapshot
// in a new round is not newer than the last one processed (a provider stuck
// on an old block).
var ErrBlockNotAdvancing = errors.New("block not advancing")

// Engine wires Step to persistence and the alert outbox.
type Engine struct {
	cfg    EngineConfig
	states StateStore
	runner TxRunner
	tokens TokenLister // optional

	falseAlarms atomic.Uint64
}

// NewEngine builds an Engine. states is used for reads and for tokens whose
// state changes without side effects; runner makes the incident write, the
// state save and the outbox insert of one token atomic. tokens may be nil
// (alerts then carry the token id as symbol and 0 decimals).
func NewEngine(cfg EngineConfig, states StateStore, runner TxRunner, tokens TokenLister) *Engine {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Engine{cfg: cfg, states: states, runner: runner, tokens: tokens}
}

// FalseAlarms returns how many amber streaks recovered without going red
// since the process started (for metrics).
func (e *Engine) FalseAlarms() uint64 { return e.falseAlarms.Load() }

// Process runs one snapshot round: it steps every token's state with its new
// snapshot (or the read error), opens/updates/closes incidents, records
// alerts in the outbox and persists the states.
//
// Per token, the incident writes, the state save and the outbox insert of its
// alerts commit in one transaction (TxRunner). If any of them fails nothing
// is written for that token, it keeps its previous state, and the same
// transition re-fires on the next round. Other tokens are unaffected and the
// errors are joined. A Relay moves committed alerts to the Queue.
//
// One Process call is one round, and at most one snapshot per token counts
// (the highest block). A snapshot at or below the token's LastBlock is
// therefore a new round that did not advance: it is recorded as a failed read
// (ErrBlockNotAdvancing) instead of being silently ignored, so a provider
// stuck on an old block makes the token stale.
func (e *Engine) Process(ctx context.Context, snaps []model.Snapshot, errs map[string]error) error {
	bySnap := make(map[string]model.Snapshot, len(snaps))
	for _, s := range snaps {
		if old, ok := bySnap[s.TokenID]; !ok || s.BlockHeight > old.BlockHeight {
			bySnap[s.TokenID] = s
		}
	}
	ids := make([]string, 0, len(bySnap)+len(errs))
	for id := range bySnap {
		ids = append(ids, id)
	}
	for id := range errs {
		if _, ok := bySnap[id]; !ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)

	prev, err := e.states.LoadStates(ctx, ids)
	if err != nil {
		return fmt.Errorf("load states: %w", err)
	}
	meta := e.tokenMeta(ctx)
	now := e.cfg.Now()

	var (
		plain  []model.TokenState // no side effects: saved in one batch
		joined []error
	)
	for _, id := range ids {
		p, ok := prev[id]
		if !ok {
			p = model.TokenState{TokenID: id, Status: model.StatusUnknown}
		}
		var next model.TokenState
		var trs []Transition
		snap, haveSnap := bySnap[id]
		if haveSnap && p.LastBlock != 0 && snap.BlockHeight <= p.LastBlock {
			haveSnap = false
			errs = withErr(errs, id, fmt.Errorf("%w: block %d, last processed %d", ErrBlockNotAdvancing, snap.BlockHeight, p.LastBlock))
		}
		if haveSnap {
			next, trs = Step(p, snap, e.cfg.Alert)
		} else {
			next = StepError(p, errs[id], e.cfg.Alert, now)
			next.UpdatedAt = now
		}
		if len(trs) == 0 {
			plain = append(plain, next)
			continue
		}
		if terr := e.commit(ctx, next, trs, meta, now); terr != nil {
			joined = append(joined, fmt.Errorf("%s: %w", id, terr))
		}
	}
	if len(plain) > 0 {
		if err := e.states.SaveStates(ctx, plain); err != nil {
			joined = append(joined, fmt.Errorf("save states: %w", err))
		}
	}
	return errors.Join(joined...)
}

func withErr(m map[string]error, id string, err error) map[string]error {
	out := make(map[string]error, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	out[id] = err
	return out
}

func (e *Engine) tokenMeta(ctx context.Context) map[string]model.Token {
	m := map[string]model.Token{}
	if e.tokens == nil {
		return m
	}
	list, err := e.tokens.ListTokens(ctx)
	if err != nil {
		e.cfg.Logger.Warn("alert: list tokens failed", "err", err)
		return m
	}
	for _, t := range list {
		m[t.TokenID] = t
	}
	return m
}

// commit performs the side effects of one token's transitions and saves its
// state, atomically. Hooks and counters run only after the commit.
func (e *Engine) commit(ctx context.Context, st model.TokenState, trs []Transition, meta map[string]model.Token, now time.Time) error {
	var done []Transition
	var falseAlarms uint64
	err := e.runner.InTx(ctx, func(tx Tx) error {
		done, falseAlarms = done[:0], 0
		next, alerts, applied, err := e.apply(ctx, tx, st, trs, meta, now)
		if err != nil {
			return err
		}
		if err := tx.AddOutbox(ctx, alerts); err != nil {
			return fmt.Errorf("outbox: %w", err)
		}
		if err := tx.SaveStates(ctx, []model.TokenState{next}); err != nil {
			return fmt.Errorf("save state: %w", err)
		}
		done = applied
		for _, t := range applied {
			if t.Kind == FalseAlarmCleared {
				falseAlarms++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	e.falseAlarms.Add(falseAlarms)
	for _, t := range done {
		e.notifyTransition(t)
	}
	return nil
}

// apply performs the incident writes of the transitions of one token through
// tx and returns the state to persist (with OpenIncidentID set/cleared), the
// alerts to record and the transitions as applied (TurnedRed carries its
// incident id).
func (e *Engine) apply(ctx context.Context, tx Tx, st model.TokenState, trs []Transition, meta map[string]model.Token, now time.Time) (model.TokenState, []Alert, []Transition, error) {
	var alerts []Alert
	applied := make([]Transition, 0, len(trs))
	for _, t := range trs {
		log := e.cfg.Logger.With("token", t.TokenID, "kind", string(t.Kind), "block", t.Block)
		a := e.newAlert(t, meta, now)
		switch t.Kind {
		case TurnedAmber:
			log.Info("token turned amber", "gap", t.Gap)
		case FalseAlarmCleared:
			log.Info("false alarm cleared", "streak", t.Streak, "max_deficit", t.MaxDeficit)
		case TurnedRed:
			opened := t.BlockTime
			if opened.IsZero() {
				opened = now
			}
			id, err := tx.OpenIncident(ctx, model.Incident{
				TokenID: t.TokenID, StartBlock: t.StartBlock, RedBlock: t.RedBlock,
				OpenedAt: opened, MaxDeficit: t.MaxDeficit, MaxDeficitBlock: t.MaxDeficitBlock,
			})
			if err != nil {
				return st, nil, nil, fmt.Errorf("open incident: %w", err)
			}
			st.OpenIncidentID = id
			a.IncidentID = id
			t.IncidentID = id
			log.Warn("token turned red", "incident", id, "gap", t.Gap, "start_block", t.StartBlock)
		case IncidentUpdate:
			if t.IncidentID != 0 {
				if err := tx.UpdateIncident(ctx, t.IncidentID, t.MaxDeficit, t.MaxDeficitBlock); err != nil {
					return st, nil, nil, fmt.Errorf("update incident: %w", err)
				}
			}
			log.Info("incident deficit grew", "incident", t.IncidentID, "max_deficit", t.MaxDeficit)
		case Recovered:
			if t.IncidentID != 0 {
				closedAt := t.BlockTime
				if closedAt.IsZero() {
					closedAt = now
				}
				if inc, err := tx.GetIncident(ctx, t.IncidentID); err == nil && !inc.OpenedAt.IsZero() {
					a.DurationSeconds = int64(closedAt.Sub(inc.OpenedAt).Seconds())
				}
				if err := tx.CloseIncident(ctx, t.IncidentID, t.Block, closedAt, t.MaxDeficit, t.MaxDeficitBlock); err != nil {
					return st, nil, nil, fmt.Errorf("close incident: %w", err)
				}
			}
			log.Info("token recovered", "incident", t.IncidentID, "max_deficit", t.MaxDeficit)
		}
		if t.Kind.Alertable() {
			alerts = append(alerts, a)
		}
		applied = append(applied, t)
	}
	return st, alerts, applied, nil
}

func (e *Engine) notifyTransition(t Transition) {
	if e.cfg.OnTransition == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			e.cfg.Logger.Error("alert: OnTransition panicked", "panic", r)
		}
	}()
	e.cfg.OnTransition(t)
}

func (e *Engine) newAlert(t Transition, meta map[string]model.Token, now time.Time) Alert {
	a := Alert{
		Kind: t.Kind, TokenID: t.TokenID, Symbol: t.TokenID,
		Gap: t.Gap, MaxDeficit: t.MaxDeficit, StartBlock: t.StartBlock, Block: t.Block,
		IncidentID: t.IncidentID, At: t.BlockTime,
	}
	if a.At.IsZero() {
		a.At = now
	}
	if tok, ok := meta[t.TokenID]; ok {
		a.Symbol, a.Decimals = tok.Symbol, tok.Decimals
	}
	if e.cfg.DashboardURL != "" {
		a.DashboardURL = e.tokenLink(t.TokenID)
	}
	return a
}

// DashboardLinkFormat is the token route of the bundled web/ dashboard
// (ids travel in the query string; see web/src/lib/links.ts).
const DashboardLinkFormat = "{base}/token/?id={id}"

func (e *Engine) tokenLink(id string) string {
	base := trimSlash(e.cfg.DashboardURL)
	if e.cfg.LinkFormat == "" {
		return fmt.Sprintf("%s/token/%s", base, url.PathEscape(id))
	}
	return strings.NewReplacer("{base}", base, "{id}", url.QueryEscape(id)).Replace(e.cfg.LinkFormat)
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
