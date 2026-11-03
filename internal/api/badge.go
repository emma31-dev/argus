package api

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/emma31-dev/argus/internal/model"
)

var badgeColors = map[string]string{
	"green":   "#4c1",
	"amber":   "#dfb317",
	"red":     "#e05d44",
	"unknown": "#9f9f9f",
}

// textWidth estimates the rendered width of s in 11px Verdana, as
// shields.io does without a font: about 6.6 px per character.
func textWidth(s string) int { return len([]rune(s))*66/10 + 1 }

// renderBadge draws a shields.io flat-style badge.
func renderBadge(label, message, color string) []byte {
	lw := textWidth(label) + 10
	rw := textWidth(message) + 10
	w := lw + rw
	l, m := html.EscapeString(label), html.EscapeString(message)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">`, w, l, m)
	fmt.Fprintf(&b, `<title>%s: %s</title>`, l, m)
	b.WriteString(`<linearGradient id="s" x2="0" y2="100%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient>`)
	fmt.Fprintf(&b, `<clipPath id="r"><rect width="%d" height="20" rx="3" fill="#fff"/></clipPath>`, w)
	fmt.Fprintf(&b, `<g clip-path="url(#r)"><rect width="%d" height="20" fill="#555"/><rect x="%d" width="%d" height="20" fill="%s"/><rect width="%d" height="20" fill="url(#s)"/></g>`, lw, lw, rw, color, w)
	b.WriteString(`<g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" text-rendering="geometricPrecision" font-size="11">`)
	fmt.Fprintf(&b, `<text x="%d" y="15" fill="#010101" fill-opacity=".3">%s</text><text x="%d" y="14">%s</text>`, lw/2, l, lw/2, l)
	fmt.Fprintf(&b, `<text x="%d" y="15" fill="#010101" fill-opacity=".3">%s</text><text x="%d" y="14">%s</text>`, lw+rw/2, m, lw+rw/2, m)
	b.WriteString(`</g></svg>`)
	return []byte(b.String())
}

func writeBadge(w http.ResponseWriter, code int, message, color string) {
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.WriteHeader(code)
	_, _ = w.Write(renderBadge("argus", message, color))
}

// handleBadge serves /v1/badge/{id}.svg for one token, or all.svg for the
// overall status.
func (s *Server) handleBadge(w http.ResponseWriter, r *http.Request) {
	raw, ok := strings.CutSuffix(r.PathValue("id"), ".svg")
	if !ok || raw == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	ctx := r.Context()
	// One staleness rule for every badge: a monitor that has stopped
	// looking, or whose newest block is old, must not show a status.
	stale, err := s.monitorStaleNow(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if raw == "all" {
		views, _, err := s.tokenViews(ctx)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		counts := map[model.Status]int{}
		for _, v := range views {
			counts[v.Status]++
		}
		st := worstStatus(counts)
		if stale {
			st = model.StatusUnknown
		}
		writeBadge(w, http.StatusOK, string(st), badgeColors[string(st)])
		return
	}
	id := unescapeID(raw)
	if _, err := s.store.GetToken(ctx, id); err != nil {
		if isNotFound(err) {
			writeBadge(w, http.StatusNotFound, "not found", badgeColors["unknown"])
			return
		}
		s.internalError(w, r, err)
		return
	}
	st := model.StatusUnknown
	states, err := s.store.LoadStates(ctx, []string{id})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if ts, ok := states[id]; ok && !stale {
		st = effectiveStatus(&ts, ts.LastBlock > 0, s.now(), s.cfg.StaleAfter)
	}
	writeBadge(w, http.StatusOK, string(st), badgeColors[string(st)])
}
