package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/emma31-dev/argus/internal/tokens"
)

// Plan describes what to replay.
type Plan struct {
	Title             string      `yaml:"title"`
	Notes             string      `yaml:"notes"`
	IntentsAccount    string      `yaml:"intents_account"`
	ArchivalProviders []string    `yaml:"archival_providers"`
	Tokens            []PlanToken `yaml:"tokens"`
	Points            []PlanPoint `yaml:"points"`
}

// PlanToken is one token to read at every point.
type PlanToken struct {
	ID       string `yaml:"id"`
	Symbol   string `yaml:"symbol"`
	Decimals int    `yaml:"decimals"`
}

// PlanPoint is one block height to read, with a human label.
type PlanPoint struct {
	BlockHeight uint64 `yaml:"block_height"`
	Label       string `yaml:"label"`
}

// LoadPlan reads and validates a plan file.
func LoadPlan(path string) (*Plan, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParsePlan(b)
}

// ParsePlan parses and validates plan YAML. Unknown fields are errors.
// Placeholder values (block height 0, ids that do not parse) are rejected
// with an explicit message, so the example plan can never be run by accident.
func ParsePlan(b []byte) (*Plan, error) {
	var p Plan
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}
	var errs []error
	if strings.TrimSpace(p.Title) == "" {
		errs = append(errs, errors.New("title is required"))
	}
	if p.IntentsAccount == "" {
		p.IntentsAccount = tokens.DefaultIntentsAccount
	}
	if len(p.Tokens) == 0 {
		errs = append(errs, errors.New("at least one token is required"))
	}
	seen := map[string]bool{}
	for i, t := range p.Tokens {
		tok, err := tokens.Parse(t.ID)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("tokens[%d] %q: not a valid token id (is this still a placeholder?): %v", i, t.ID, err))
		case !tokens.InScope(tok):
			errs = append(errs, fmt.Errorf("tokens[%d] %q: standard out of scope", i, t.ID))
		case seen[t.ID]:
			errs = append(errs, fmt.Errorf("tokens[%d] %q: duplicate", i, t.ID))
		}
		seen[t.ID] = true
		if t.Decimals < 0 || t.Decimals > 77 {
			errs = append(errs, fmt.Errorf("tokens[%d] %q: decimals %d out of range", i, t.ID, t.Decimals))
		}
	}
	if len(p.Points) == 0 {
		errs = append(errs, errors.New("at least one point is required"))
	}
	heights := map[uint64]bool{}
	for i, pt := range p.Points {
		if pt.BlockHeight == 0 {
			errs = append(errs, fmt.Errorf("points[%d] %q: block_height is 0 (placeholder?)", i, pt.Label))
		}
		if heights[pt.BlockHeight] && pt.BlockHeight != 0 {
			errs = append(errs, fmt.Errorf("points[%d]: duplicate block_height %d", i, pt.BlockHeight))
		}
		heights[pt.BlockHeight] = true
		if strings.TrimSpace(pt.Label) == "" {
			errs = append(errs, fmt.Errorf("points[%d]: label is required", i))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	sort.SliceStable(p.Points, func(i, j int) bool { return p.Points[i].BlockHeight < p.Points[j].BlockHeight })
	return &p, nil
}
