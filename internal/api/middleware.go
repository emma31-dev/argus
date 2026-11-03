package api

import (
	"context"
	"math"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// statusWriter records the status and size of a response and still supports
// http.Flusher / http.ResponseController through Unwrap.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// recoverPanics turns a handler panic into a 500, unless the response has
// already started. It wraps the writer itself so it knows, whatever the inner
// middleware do: once headers or body bytes are out, a JSON error would be
// appended to a half-written body (a corrupt CSV that looks complete), so the
// connection is aborted instead and the client sees a truncated response.
func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				s.log.Error("api: panic in handler", "path", r.URL.Path, "panic", rec, "response_started", sw.status != 0)
				if sw.status != 0 {
					panic(http.ErrAbortHandler)
				}
				writeError(sw, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(sw, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		level := s.log.Info
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			level = s.log.Debug
		}
		level("http request",
			"method", r.Method, "path", r.URL.Path, "status", status,
			"bytes", sw.bytes, "duration_ms", s.now().Sub(start).Milliseconds(),
			"ip", clientIP(r, s.cfg.TrustProxy))
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	origins := s.cfg.CORSOrigins
	any := len(origins) == 0 || slices.Contains(origins, "*")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		if any {
			h.Set("Access-Control-Allow-Origin", "*")
		} else if o := r.Header.Get("Origin"); o != "" && slices.Contains(origins, o) {
			h.Set("Access-Control-Allow-Origin", o)
			h.Add("Vary", "Origin")
		}
		h.Set("Access-Control-Expose-Headers", "Retry-After")
		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			h.Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// timeout bounds the request context of every endpoint except the CSV
// export, which streams and manages its own deadlines.
func (s *Server) timeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/export.csv" {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// clientIP is the rate-limit key. Behind a trusted reverse proxy the proxy
// appends the peer address it saw to X-Forwarded-For, so the last entry is
// the one we can trust; earlier entries are client-supplied.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type bucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter is a per-IP token bucket: capacity and refill are both derived
// from the per-minute limit (a full minute of requests may burst).
type rateLimiter struct {
	limit      int
	trustProxy bool
	now        func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

func newRateLimiter(perMinute int, trustProxy bool, now func() time.Time) *rateLimiter {
	return &rateLimiter{limit: perMinute, trustProxy: trustProxy, now: now, buckets: map[string]*bucket{}}
}

// allow takes one token for ip; when it is refused it returns the wait.
func (l *rateLimiter) allow(ip string) (bool, time.Duration) {
	now := l.now()
	rate := float64(l.limit) / 60.0 // tokens per second
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) > time.Minute {
		for k, b := range l.buckets { // full buckets carry no information
			if now.Sub(b.last).Seconds()*rate+b.tokens >= float64(l.limit) {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	b, ok := l.buckets[ip]
	if !ok {
		b = &bucket{tokens: float64(l.limit), last: now}
		l.buckets[ip] = b
	}
	b.tokens = math.Min(float64(l.limit), b.tokens+now.Sub(b.last).Seconds()*rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / rate * float64(time.Second))
}

func (l *rateLimiter) middleware(next http.Handler) http.Handler {
	if l.limit <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health probes come from the platform, not from users.
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		ok, wait := l.allow(clientIP(r, l.trustProxy))
		if !ok {
			secs := int(math.Ceil(wait.Seconds()))
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
