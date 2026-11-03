package tokens

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/emma31-dev/argus/internal/model"
)

// ErrNotSupported is returned by a Source that cannot work in this
// environment (for example the contract lacks the enumeration method). The
// chain treats it as "try the next source", not as a failure.
var ErrNotSupported = errors.New("tokens: discovery source not supported")

// Source names recorded in model.Token.Source.
const (
	SourceEnumeration = "enumeration"
	SourceOneClick    = "1click"
	SourceConfig      = "config"
)

// Source is one way of finding the token list.
type Source interface {
	// Name is the value stored in model.Token.Source.
	Name() string
	// Fetch returns tokens. Enumeration returns ids only (no symbol or
	// decimals). Out-of-scope standards must already be filtered out.
	Fetch(ctx context.Context) ([]model.Token, error)
}

// Discoverer finds the tokens to track. The daily refresh is the caller's job.
type Discoverer interface {
	// Discover returns the token list and the name of the source that
	// provided the token ids.
	Discover(ctx context.Context) ([]model.Token, string, error)
}

// Chain implements the PRD 5.1 discovery order: enumeration, then the 1Click
// list, then the committed config. The first source that yields tokens
// provides the id set; 1Click metadata fills in symbol and decimals when
// enumeration was used; config entries always override symbol and decimals.
type Chain struct {
	Enumeration Source // may be nil
	OneClick    Source // may be nil
	Config      Source // may be nil
	Now         func() time.Time
}

// Discover implements Discoverer. A failing source is skipped; the error is
// returned only if every source failed or produced nothing.
func (c *Chain) Discover(ctx context.Context) ([]model.Token, string, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	var (
		base    []model.Token
		source  string
		errs    []error
		oneList []model.Token
	)
	try := func(s Source) []model.Token {
		if s == nil {
			return nil
		}
		ts, err := s.Fetch(ctx)
		if err != nil {
			if !errors.Is(err, ErrNotSupported) {
				errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
			}
			return nil
		}
		return ts
	}
	if ts := try(c.Enumeration); len(ts) > 0 {
		base, source = ts, SourceEnumeration
	}
	if ts := try(c.OneClick); len(ts) > 0 {
		oneList = ts
		if base == nil {
			base, source = ts, SourceOneClick
		}
	}
	cfg := try(c.Config)
	if base == nil && len(cfg) > 0 {
		base, source = cfg, SourceConfig
	}
	if len(base) == 0 {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("tokens: no discovery source produced tokens: %w", errors.Join(errs...))
	}

	byID := map[string]model.Token{}
	for _, t := range base {
		byID[t.TokenID] = t
	}
	if source == SourceEnumeration { // enrich bare ids with 1Click metadata
		for _, o := range oneList {
			if t, ok := byID[o.TokenID]; ok {
				t.Symbol, t.Decimals = o.Symbol, o.Decimals
				byID[o.TokenID] = t
			}
		}
	}
	for _, cf := range cfg { // config overrides
		if t, ok := byID[cf.TokenID]; ok {
			if cf.Symbol != "" {
				t.Symbol = cf.Symbol
			}
			t.Decimals = cf.Decimals
			byID[cf.TokenID] = t
		}
	}
	out := make([]model.Token, 0, len(byID))
	ts := now().UTC()
	for _, t := range byID {
		t.Source, t.UpdatedAt = source, ts
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TokenID < out[j].TokenID })
	return out, source, nil
}
