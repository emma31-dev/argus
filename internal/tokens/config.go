package tokens

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/emma31-dev/argus/internal/model"
)

// ConfigFile is the on-disk shape of config/tokens.yaml.
type ConfigFile struct {
	Tokens []ConfigEntry `yaml:"tokens"`
}

// ConfigEntry is one fallback token.
type ConfigEntry struct {
	ID       string `yaml:"id"`
	Symbol   string `yaml:"symbol"`
	Decimals int    `yaml:"decimals"`
}

// Config is the committed fallback token list.
type Config struct {
	Path string
}

// Name implements Source.
func (c *Config) Name() string { return SourceConfig }

// Fetch implements Source.
func (c *Config) Fetch(ctx context.Context) ([]model.Token, error) {
	b, err := os.ReadFile(c.Path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", c.Path, err)
	}
	return ParseConfig(b)
}

// ParseConfig parses tokens.yaml content. Unknown fields and invalid or
// duplicate ids are errors: this file is hand-maintained, so be strict.
func ParseConfig(b []byte) ([]model.Token, error) {
	var f ConfigFile
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("tokens config: %w", err)
	}
	seen := map[string]bool{}
	var out []model.Token
	for i, e := range f.Tokens {
		t, err := Parse(e.ID)
		if err != nil {
			return nil, fmt.Errorf("tokens config entry %d: %w", i, err)
		}
		if !InScope(t) {
			return nil, fmt.Errorf("tokens config entry %d: %s is out of scope", i, e.ID)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("tokens config entry %d: duplicate id %s", i, e.ID)
		}
		if e.Decimals < 0 || e.Decimals > 77 {
			return nil, fmt.Errorf("tokens config entry %d: bad decimals %d", i, e.Decimals)
		}
		seen[e.ID] = true
		t.Symbol, t.Decimals, t.Source = e.Symbol, e.Decimals, SourceConfig
		out = append(out, t)
	}
	return out, nil
}
