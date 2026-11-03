package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/store"
)

// snapshotIndex maps token id to its latest snapshot.
func snapshotIndex(snaps []model.Snapshot) map[string]*model.Snapshot {
	m := make(map[string]*model.Snapshot, len(snaps))
	for i := range snaps {
		m[snaps[i].TokenID] = &snaps[i]
	}
	return m
}

func maxBlock(snaps []model.Snapshot) (uint64, time.Time) {
	var h uint64
	var t time.Time
	for _, s := range snaps {
		if s.BlockHeight > h {
			h, t = s.BlockHeight, s.BlockTime
		}
	}
	return h, t.UTC()
}

// tokenViews loads every active token with state and latest snapshot,
// sorted by deficit risk.
func (s *Server) tokenViews(ctx context.Context) ([]tokenView, []model.Snapshot, error) {
	toks, err := s.store.ListTokens(ctx)
	if err != nil {
		return nil, nil, err
	}
	states, err := s.store.LoadStates(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	snaps, err := s.store.LatestSnapshots(ctx)
	if err != nil {
		return nil, nil, err
	}
	idx := snapshotIndex(snaps)
	views := make([]tokenView, 0, len(toks))
	for _, t := range toks {
		var stp *model.TokenState
		if st, ok := states[t.TokenID]; ok {
			stp = &st
		}
		views = append(views, buildToken(t, stp, idx[t.TokenID]))
	}
	sortTokens(views)
	return views, snaps, nil
}

type providerView struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Healthy   bool   `json:"healthy"`
	LastError string `json:"last_error"`
}

// redactURL keeps scheme and host only: provider URLs often carry API keys
// in the path or query.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	views, snaps, err := s.tokenViews(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	open, err := s.store.ListIncidents(ctx, store.IncidentFilter{Status: "open", Limit: 1000})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	round, rerr := s.store.LastRound(ctx)
	if rerr != nil && !isNotFound(rerr) {
		s.internalError(w, r, rerr)
		return
	}

	counts := map[model.Status]int{}
	for _, v := range views {
		counts[v.Status]++
	}
	byStatus := map[string]int{"green": 0, "amber": 0, "red": 0, "unknown": 0}
	for st, n := range counts {
		byStatus[string(st)] = n
	}
	overall := worstStatus(counts)

	providers := []providerView{}
	if s.src != nil {
		for _, p := range s.src.Health() {
			providers = append(providers, providerView{
				Name: p.Name, URL: redactURL(p.URL), Healthy: p.Healthy, LastError: sanitizeError(p.LastError),
			})
		}
	}

	var lastChecked, lastBlockTime *time.Time
	var lastBlock uint64
	stale := true
	if rerr == nil {
		fin := round.FinishedAt.UTC()
		lastChecked = &fin
		lastBlock = round.BlockHeight
		stale = s.now().Sub(round.FinishedAt) > s.cfg.StaleAfter
		if h, bt := maxBlock(snaps); h > 0 && !bt.IsZero() {
			lastBlockTime = &bt
		}
	}
	if stale {
		// A monitor that has stopped looking must not report "green".
		overall = model.StatusUnknown
	}
	cacheFor(w, 5)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":             overall,
		"monitor_stale":      stale,
		"last_block":         lastBlock,
		"last_block_time":    lastBlockTime,
		"last_checked_at":    lastChecked,
		"interval_seconds":   int(s.cfg.Interval.Seconds()),
		"debounce_snapshots": s.cfg.K,
		"tokens_total":       len(views),
		"tokens_by_status":   byStatus,
		"open_incidents":     len(open),
		"providers":          providers,
		"version":            s.cfg.Version,
	})
}

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	views, snaps, err := s.tokenViews(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	h, _ := maxBlock(snaps)
	cacheFor(w, 5)
	writeJSON(w, http.StatusOK, map[string]any{"block_height": h, "tokens": views})
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := pathID(r)
	tok, err := s.store.GetToken(ctx, id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		s.internalError(w, r, err)
		return
	}
	states, err := s.store.LoadStates(ctx, []string{id})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	snaps, err := s.store.LatestSnapshots(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var stp *model.TokenState
	if st, ok := states[id]; ok {
		stp = &st
	}
	out := tokenDetailView{tokenView: buildToken(tok, stp, snapshotIndex(snaps)[id])}
	if stp != nil && stp.OpenIncidentID != 0 {
		inc, err := s.store.GetIncident(ctx, stp.OpenIncidentID)
		switch {
		case err == nil:
			iv := newIncidentView(inc, map[string]model.Token{id: tok})
			out.OpenIncident = &iv
		case !isNotFound(err):
			s.internalError(w, r, err)
			return
		}
	}
	cacheFor(w, 5)
	writeJSON(w, http.StatusOK, out)
}

// parseBound parses a from/to value: digits are a block height, anything
// else must be RFC 3339.
func parseBound(v string) (block uint64, t time.Time, err error) {
	if n, perr := strconv.ParseUint(v, 10, 64); perr == nil {
		if n == 0 {
			return 0, time.Time{}, errors.New("block height must be positive")
		}
		return n, time.Time{}, nil
	}
	t, perr := time.Parse(time.RFC3339, v)
	if perr != nil {
		return 0, time.Time{}, errors.New("must be a block height or an RFC 3339 time")
	}
	return 0, t.UTC(), nil
}

// parseRange reads from/to. With neither, it returns the last 24 hours.
func (s *Server) parseRange(q url.Values) (store.Range, error) {
	var rg store.Range
	from, to := q.Get("from"), q.Get("to")
	if from == "" && to == "" {
		rg.FromTime = s.now().UTC().Add(-defaultWindow)
		return rg, nil
	}
	if from != "" {
		b, t, err := parseBound(from)
		if err != nil {
			return rg, errors.New("invalid from: " + err.Error())
		}
		rg.FromBlock, rg.FromTime = b, t
	}
	if to != "" {
		b, t, err := parseBound(to)
		if err != nil {
			return rg, errors.New("invalid to: " + err.Error())
		}
		rg.ToBlock, rg.ToTime = b, t
	}
	return rg, nil
}

func parseLimit(q url.Values, def, max int) (int, error) {
	v := q.Get("limit")
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, errors.New("invalid limit: must be a positive integer")
	}
	return min(n, max), nil
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := pathID(r)
	q := r.URL.Query()
	rg, err := s.parseRange(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := parseLimit(q, DefaultHistoryLimit, MaxHistoryLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tok, err := s.store.GetToken(ctx, id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "token not found")
			return
		}
		s.internalError(w, r, err)
		return
	}
	snaps, err := s.store.History(ctx, id, rg, limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	cacheFor(w, 10)
	writeJSON(w, http.StatusOK, map[string]any{
		"token":  map[string]any{"id": tok.TokenID, "symbol": tok.Symbol, "chain": chainLabel(tok), "decimals": tok.Decimals},
		"points": newPoints(snaps),
	})
}

func (s *Server) tokenMeta(ctx context.Context) (map[string]model.Token, error) {
	toks, err := s.store.ListTokens(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]model.Token, len(toks))
	for _, t := range toks {
		m[t.TokenID] = t
	}
	return m, nil
}

func (s *Server) handleIncidents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	switch status {
	case "", "open", "closed", "all":
	default:
		writeError(w, http.StatusBadRequest, "invalid status: use open, closed or all")
		return
	}
	limit, err := parseLimit(q, 100, 1000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	incs, err := s.store.ListIncidents(r.Context(), store.IncidentFilter{Status: status, Limit: limit})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	meta, err := s.tokenMeta(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]incidentView, 0, len(incs))
	for _, i := range incs {
		out = append(out, newIncidentView(i, meta))
	}
	cacheFor(w, 10)
	writeJSON(w, http.StatusOK, map[string]any{"incidents": out})
}

func (s *Server) handleIncident(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid incident id")
		return
	}
	inc, err := s.store.GetIncident(ctx, id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "incident not found")
			return
		}
		s.internalError(w, r, err)
		return
	}
	events, err := s.store.IncidentEvents(ctx, id)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if events == nil {
		events = []model.IncidentEvent{}
	}
	rg := store.Range{FromBlock: inc.StartBlock}
	if inc.ClosedAt != nil {
		rg.ToBlock = inc.EndBlock
	}
	snaps, err := s.store.History(ctx, inc.TokenID, rg, DefaultHistoryLimit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	meta := map[string]model.Token{}
	if t, err := s.store.GetToken(ctx, inc.TokenID); err == nil {
		meta[t.TokenID] = t
	}
	cacheFor(w, 10)
	writeJSON(w, http.StatusOK, struct {
		incidentView
		Events    []model.IncidentEvent `json:"events"`
		Snapshots []pointView           `json:"snapshots"`
	}{newIncidentView(inc, meta), events, newPoints(snaps)})
}

var csvHeader = []string{"token_id", "symbol", "decimals", "block_height", "block_time", "owed", "held", "gap", "provider"}

// csvText neutralises spreadsheet formula injection in text columns that
// originate from on-chain metadata. Numeric columns are never passed here.
func csvText(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rg, err := s.parseRange(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tokenID := unescapeID(q.Get("token"))
	rc := http.NewResponseController(w)
	extend := func() { _ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second)) }
	extend()

	var cw *csv.Writer
	start := func() {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		name := "argus-export.csv"
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Header().Set("Cache-Control", "no-store")
		cw = csv.NewWriter(w)
		_ = cw.Write(csvHeader)
	}
	n := 0
	err = s.store.ExportRows(r.Context(), tokenID, rg, MaxExportRows, func(e store.ExportRow) error {
		if cw == nil {
			start()
		}
		n++
		if err := cw.Write([]string{
			csvText(e.TokenID), csvText(e.Symbol), strconv.Itoa(e.Decimals),
			strconv.FormatUint(e.BlockHeight, 10), e.BlockTime.UTC().Format(time.RFC3339),
			e.Owed, e.Held, e.Gap, csvText(e.Provider),
		}); err != nil {
			return err
		}
		if n%1000 == 0 {
			cw.Flush()
			if err := cw.Error(); err != nil {
				return err
			}
			if err := rc.Flush(); err != nil {
				return err
			}
			extend()
		}
		return nil
	})
	if err != nil {
		if cw == nil {
			s.internalError(w, r, err)
			return
		}
		s.log.Error("api: export aborted", "rows", n, "err", err)
		return
	}
	if cw == nil {
		start()
	}
	cw.Flush()
}

func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	const notAvailable = "replay not available"
	if s.cfg.ReplayPath == "" {
		writeError(w, http.StatusNotFound, notAvailable)
		return
	}
	b, err := os.ReadFile(s.cfg.ReplayPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.log.Error("api: read replay", "path", s.cfg.ReplayPath, "err", err)
		}
		writeError(w, http.StatusNotFound, notAvailable)
		return
	}
	if !json.Valid(b) {
		s.log.Error("api: replay file is not valid JSON", "path", s.cfg.ReplayPath)
		writeError(w, http.StatusNotFound, notAvailable)
		return
	}
	cacheFor(w, 300)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}
