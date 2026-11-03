// Package api is Argus's read-only public HTTP API (see docs/api.md).
//
// It depends on two small interfaces, Store (a subset of *store.Repo) and
// StatusSource (RPC provider health), so every handler is unit-testable with
// fakes. All amounts are base-10 integer strings in the token's smallest
// unit; nothing here ever converts them to floats.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/store"
)

// Store is the subset of *store.Repo the API reads.
type Store interface {
	ListTokens(ctx context.Context) ([]model.Token, error)
	GetToken(ctx context.Context, id string) (model.Token, error)
	LatestSnapshots(ctx context.Context) ([]model.Snapshot, error)
	LoadStates(ctx context.Context, tokenIDs []string) (map[string]model.TokenState, error)
	History(ctx context.Context, tokenID string, rg store.Range, limit int) ([]model.Snapshot, error)
	ExportRows(ctx context.Context, tokenID string, rg store.Range, maxRows int, fn func(store.ExportRow) error) error
	ListIncidents(ctx context.Context, f store.IncidentFilter) ([]model.Incident, error)
	GetIncident(ctx context.Context, id int64) (model.Incident, error)
	IncidentEvents(ctx context.Context, incidentID int64) ([]model.IncidentEvent, error)
	LastRound(ctx context.Context) (store.Round, error)
}

var _ Store = (*store.Repo)(nil)

// StatusSource reports RPC provider health. *rpc.Client implements it.
type StatusSource interface {
	Health() []rpc.ProviderHealth
}

var _ StatusSource = (*rpc.Client)(nil)

// Limits of the history and export endpoints.
const (
	DefaultHistoryLimit = 2000
	MaxHistoryLimit     = 10000
	MaxExportRows       = 100000
	defaultWindow       = 24 * time.Hour
)

// Config configures the API.
type Config struct {
	// Interval and K are reported by /v1/status.
	Interval time.Duration
	K        int
	Version  string
	// ReplayPath is the file served by /v1/replay.
	ReplayPath string
	// RateLimit is requests per minute per client IP; <= 0 disables it.
	RateLimit int
	// TrustProxy makes the client IP come from X-Forwarded-For (the last
	// entry, i.e. the one appended by our own reverse proxy). Only enable it
	// behind a proxy that sets the header.
	TrustProxy bool
	// CORSOrigins lists allowed origins; empty or "*" allows any.
	CORSOrigins []string
	// RequestTimeout bounds store calls of non-streaming endpoints (default 15s).
	RequestTimeout time.Duration
	// StaleAfter is how old the last completed round may be before the
	// monitor reports itself as stale and the overall status becomes
	// "unknown". Default: max(5*Interval, 2m).
	StaleAfter time.Duration
	Logger     *slog.Logger
	Now        func() time.Time
}

// Server is the HTTP API.
type Server struct {
	cfg     Config
	store   Store
	src     StatusSource
	log     *slog.Logger
	now     func() time.Time
	handler http.Handler
}

// New builds the API. src may be nil (no providers are reported).
func New(cfg Config, st Store, src StatusSource) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 15 * time.Second
	}
	if cfg.K <= 0 {
		cfg.K = 4
	}
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = max(5*cfg.Interval, 2*time.Minute)
	}
	s := &Server{cfg: cfg, store: st, src: src, log: cfg.Logger, now: cfg.Now}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	mux.HandleFunc("GET /v1/tokens", s.handleTokens)
	mux.HandleFunc("GET /v1/tokens/{id}", s.handleToken)
	mux.HandleFunc("GET /v1/tokens/{id}/history", s.handleHistory)
	mux.HandleFunc("GET /v1/incidents", s.handleIncidents)
	mux.HandleFunc("GET /v1/incidents/{id}", s.handleIncident)
	mux.HandleFunc("GET /v1/export.csv", s.handleExport)
	mux.HandleFunc("GET /v1/replay", s.handleReplay)
	mux.HandleFunc("GET /v1/badge/{id}", s.handleBadge)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("/", s.handleFallback)

	var h http.Handler = mux
	h = s.timeout(h)
	h = newRateLimiter(cfg.RateLimit, cfg.TrustProxy, cfg.Now).middleware(h)
	h = s.cors(h)
	h = s.logRequests(h)
	h = s.recoverPanics(h)
	s.handler = h
	return s
}

// Handler returns the full handler chain (recovery, logging, CORS, rate
// limit, timeout, routes).
func (s *Server) Handler() http.Handler { return s.handler }

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// NewHTTPServer wraps h in an http.Server with conservative timeouts. The
// export endpoint extends its own write deadline while streaming.
func NewHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

func (s *Server) handleFallback(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		writeError(w, http.StatusNotFound, "not found")
	default:
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := s.store.LastRound(r.Context()); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.Error("readyz: last round", "err", err)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready: no snapshot round has completed yet\n"))
		return
	}
	_, _ = w.Write([]byte("ready\n"))
}

// internalError logs err and answers 500 without leaking details.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil {
		return // client went away or timed out
	}
	s.log.Error("api: request failed", "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }

// pathID returns the unescaped token id of the {id} path value.
func pathID(r *http.Request) string {
	id := r.PathValue("id")
	return unescapeID(id)
}

func unescapeID(id string) string {
	if strings.Contains(id, "%") {
		if u, err := urlPathUnescape(id); err == nil {
			return u
		}
	}
	return id
}
