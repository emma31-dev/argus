package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/fixturegen"
	"github.com/emma31-dev/argus/internal/model"
)

// memStates is an in-memory alert.StateStore that also derives the status
// transitions the engine produced, by diffing consecutive saved states.
type memStates struct {
	mu          sync.Mutex
	m           map[string]model.TokenState
	transitions []fixturegen.Transition
}

func newMemStates() *memStates { return &memStates{m: map[string]model.TokenState{}} }

func (s *memStates) LoadStates(_ context.Context, ids []string) (map[string]model.TokenState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]model.TokenState{}
	for _, id := range ids {
		if st, ok := s.m[id]; ok {
			out[id] = st
		}
	}
	return out, nil
}

func (s *memStates) SaveStates(_ context.Context, states []model.TokenState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range states {
		old, ok := s.m[st.TokenID]
		prev := model.StatusUnknown
		if ok {
			prev = old.Status
		}
		if st.Status != prev {
			if kind := transitionKind(prev, st.Status); kind != "" {
				s.transitions = append(s.transitions, fixturegen.Transition{Token: st.TokenID, Kind: kind, Block: st.LastBlock})
			}
		}
		s.m[st.TokenID] = st
	}
	return nil
}

// transitionKind names a status change; changes into green from unknown
// (the first reading) are not reported.
func transitionKind(from, to model.Status) string {
	switch {
	case to == model.StatusAmber:
		return "amber"
	case to == model.StatusRed:
		return "red"
	case from == model.StatusRed && to == model.StatusGreen:
		return "recovered"
	case from == model.StatusAmber && to == model.StatusGreen:
		return "false_alarm_cleared"
	}
	return ""
}

type memIncidents struct {
	mu   sync.Mutex
	next int64
	m    map[int64]*model.Incident
}

func newMemIncidents() *memIncidents { return &memIncidents{m: map[int64]*model.Incident{}} }

func (s *memIncidents) OpenIncident(_ context.Context, inc model.Incident) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.m {
		if e.TokenID == inc.TokenID && e.ClosedAt == nil {
			return id, nil
		}
	}
	s.next++
	inc.ID = s.next
	s.m[inc.ID] = &inc
	return inc.ID, nil
}

func (s *memIncidents) get(id int64) (*model.Incident, error) {
	i, ok := s.m[id]
	if !ok {
		return nil, fmt.Errorf("incident %d not found", id)
	}
	return i, nil
}

func (s *memIncidents) UpdateIncident(_ context.Context, id int64, maxDeficit string, block uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.get(id)
	if err != nil {
		return err
	}
	i.MaxDeficit, i.MaxDeficitBlock = maxDeficit, block
	return nil
}

func (s *memIncidents) CloseIncident(_ context.Context, id int64, end uint64, at time.Time, maxDeficit string, block uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.get(id)
	if err != nil {
		return err
	}
	i.EndBlock, i.ClosedAt, i.MaxDeficit, i.MaxDeficitBlock = end, &at, maxDeficit, block
	return nil
}

func (s *memIncidents) GetIncident(_ context.Context, id int64) (model.Incident, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.get(id)
	if err != nil {
		return model.Incident{}, err
	}
	return *i, nil
}

type memTokens []model.Token

func (t memTokens) ListTokens(context.Context) ([]model.Token, error) { return t, nil }

// memOutbox collects the alerts the engine records.
type memOutbox struct {
	mu     sync.Mutex
	alerts []alert.Alert
}

func (o *memOutbox) AddOutbox(_ context.Context, a []alert.Alert) error {
	o.mu.Lock()
	o.alerts = append(o.alerts, a...)
	o.mu.Unlock()
	return nil
}

// memTx bundles the in-memory stores into the alert.Tx / alert.TxRunner the
// engine needs. Nothing here fails, so InTx has no rollback to perform.
type memTx struct {
	*memStates
	*memIncidents
	*memOutbox
}

func (t memTx) InTx(_ context.Context, fn func(alert.Tx) error) error { return fn(t) }
