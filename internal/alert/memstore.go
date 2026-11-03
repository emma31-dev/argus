package alert

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/emma31-dev/argus/internal/model"
)

// MemStore is an in-memory StateStore, IncidentStore, Outbox, OutboxStore
// and TxRunner for tests and offline replay. InTx is genuinely atomic: a
// failing callback restores everything it wrote. It is safe for concurrent
// use, but transactions are serialised.
type MemStore struct {
	mu        sync.Mutex
	txMu      sync.Mutex
	states    map[string]model.TokenState
	incidents map[int64]*model.Incident
	nextInc   int64
	outbox    []OutboxEntry
	enqueued  map[int64]bool
	nextOut   int64
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{
		states: map[string]model.TokenState{}, incidents: map[int64]*model.Incident{},
		enqueued: map[int64]bool{},
	}
}

var (
	_ StateStore    = (*MemStore)(nil)
	_ IncidentStore = (*MemStore)(nil)
	_ Outbox        = (*MemStore)(nil)
	_ OutboxStore   = (*MemStore)(nil)
	_ TxRunner      = (*MemStore)(nil)
)

// InTx implements TxRunner. fn must not call methods of the MemStore itself
// (use the Tx it is given).
func (m *MemStore) InTx(_ context.Context, fn func(Tx) error) error {
	m.txMu.Lock()
	defer m.txMu.Unlock()
	m.mu.Lock()
	savedStates := make(map[string]model.TokenState, len(m.states))
	for k, v := range m.states {
		savedStates[k] = v
	}
	savedInc := make(map[int64]model.Incident, len(m.incidents))
	for k, v := range m.incidents {
		savedInc[k] = *v
	}
	savedNextInc, savedOut, savedNextOut := m.nextInc, len(m.outbox), m.nextOut
	m.mu.Unlock()

	if err := fn(m); err != nil {
		m.mu.Lock()
		m.states = savedStates
		m.incidents = make(map[int64]*model.Incident, len(savedInc))
		for k, v := range savedInc {
			v := v
			m.incidents[k] = &v
		}
		m.nextInc, m.outbox, m.nextOut = savedNextInc, m.outbox[:savedOut], savedNextOut
		m.mu.Unlock()
		return err
	}
	return nil
}

// LoadStates implements StateStore; no ids returns every state.
func (m *MemStore) LoadStates(_ context.Context, ids []string) (map[string]model.TokenState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]model.TokenState{}
	if len(ids) == 0 {
		for k, v := range m.states {
			out[k] = v
		}
		return out, nil
	}
	for _, id := range ids {
		if s, ok := m.states[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

// SaveStates implements StateStore.
func (m *MemStore) SaveStates(_ context.Context, states []model.TokenState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range states {
		m.states[s.TokenID] = s
	}
	return nil
}

// State returns the stored state of a token.
func (m *MemStore) State(id string) model.TokenState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.states[id]
}

// OpenIncident implements IncidentStore (idempotent per token).
func (m *MemStore) OpenIncident(_ context.Context, inc model.Incident) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.incidents {
		if e.TokenID == inc.TokenID && e.ClosedAt == nil {
			return id, nil
		}
	}
	m.nextInc++
	inc.ID = m.nextInc
	m.incidents[inc.ID] = &inc
	return inc.ID, nil
}

// UpdateIncident implements IncidentStore.
func (m *MemStore) UpdateIncident(_ context.Context, id int64, maxDeficit string, block uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.incidents[id]
	if !ok {
		return fmt.Errorf("incident %d not found", id)
	}
	i.MaxDeficit, i.MaxDeficitBlock = maxDeficit, block
	return nil
}

// CloseIncident implements IncidentStore (a no-op on a closed incident).
func (m *MemStore) CloseIncident(_ context.Context, id int64, end uint64, at time.Time, maxDeficit string, block uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.incidents[id]
	if !ok {
		return fmt.Errorf("incident %d not found", id)
	}
	if i.ClosedAt != nil {
		return nil
	}
	i.EndBlock, i.ClosedAt, i.MaxDeficit, i.MaxDeficitBlock = end, &at, maxDeficit, block
	return nil
}

// GetIncident implements IncidentStore.
func (m *MemStore) GetIncident(_ context.Context, id int64) (model.Incident, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.incidents[id]
	if !ok {
		return model.Incident{}, fmt.Errorf("incident %d not found", id)
	}
	return *i, nil
}

// Incidents returns a copy of all incidents ordered by id.
func (m *MemStore) Incidents() []model.Incident {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []model.Incident
	for id := int64(1); id <= m.nextInc; id++ {
		if i, ok := m.incidents[id]; ok {
			out = append(out, *i)
		}
	}
	return out
}

// AddOutbox implements Outbox.
func (m *MemStore) AddOutbox(_ context.Context, alerts []Alert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range alerts {
		m.nextOut++
		m.outbox = append(m.outbox, OutboxEntry{ID: m.nextOut, Alert: a})
	}
	return nil
}

// OutboxAlerts returns every alert ever added to the outbox, in id order.
func (m *MemStore) OutboxAlerts() []Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Alert, 0, len(m.outbox))
	for _, e := range m.outbox {
		out = append(out, e.Alert)
	}
	return out
}

// PendingOutbox implements OutboxStore.
func (m *MemStore) PendingOutbox(_ context.Context, limit int) ([]OutboxEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []OutboxEntry
	for _, e := range m.outbox {
		if m.enqueued[e.ID] {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// MarkOutboxEnqueued implements OutboxStore.
func (m *MemStore) MarkOutboxEnqueued(_ context.Context, ids []int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range ids {
		m.enqueued[id] = true
	}
	return nil
}

// PruneOutbox implements OutboxStore (enqueued entries are dropped
// regardless of age).
func (m *MemStore) PruneOutbox(_ context.Context, _ time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	keep := m.outbox[:0:0]
	var n int64
	for _, e := range m.outbox {
		if m.enqueued[e.ID] {
			delete(m.enqueued, e.ID)
			n++
			continue
		}
		keep = append(keep, e)
	}
	m.outbox = keep
	return n, nil
}
