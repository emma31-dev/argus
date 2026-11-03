package alert

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Subscription channels.
const (
	ChannelTelegram = "telegram"
	ChannelWebhook  = "webhook"
	// AllTokens is the token_id of a subscription to every token.
	AllTokens = "*"
)

// Subscription routes alerts for a token (or AllTokens) to a target: a
// Telegram chat id or a webhook URL. Secret signs webhook bodies.
type Subscription struct {
	ID        int64     `json:"id"`
	Channel   string    `json:"channel"`
	Target    string    `json:"target"`
	TokenID   string    `json:"token_id"`
	Secret    string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// SubscriptionStore persists subscriptions.
type SubscriptionStore interface {
	// AddSubscription is idempotent on (channel, target, token_id); on a
	// duplicate it keeps the existing row (updating the secret if non-empty).
	AddSubscription(ctx context.Context, s Subscription) (Subscription, error)
	// RemoveSubscription deletes one subscription and reports whether it existed.
	// tokenID == "" removes every subscription of the target on that channel.
	RemoveSubscription(ctx context.Context, channel, target, tokenID string) (bool, error)
	ListSubscriptions(ctx context.Context, channel, target string) ([]Subscription, error)
	// SubscriptionsForToken returns the subscriptions on a channel that match
	// the token, including AllTokens ("*") subscriptions.
	SubscriptionsForToken(ctx context.Context, channel, tokenID string) ([]Subscription, error)
}

// MemorySubscriptions is an in-memory SubscriptionStore for tests and for
// running without a database.
type MemorySubscriptions struct {
	mu   sync.Mutex
	next int64
	subs []Subscription
}

// AddSubscription implements SubscriptionStore.
func (m *MemorySubscriptions) AddSubscription(_ context.Context, s Subscription) (Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.subs {
		if e.Channel == s.Channel && e.Target == s.Target && e.TokenID == s.TokenID {
			if s.Secret != "" {
				m.subs[i].Secret = s.Secret
			}
			return m.subs[i], nil
		}
	}
	m.next++
	s.ID, s.CreatedAt = m.next, time.Now().UTC()
	m.subs = append(m.subs, s)
	return s, nil
}

// RemoveSubscription implements SubscriptionStore.
func (m *MemorySubscriptions) RemoveSubscription(_ context.Context, channel, target, tokenID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept, removed := m.subs[:0], false
	for _, e := range m.subs {
		if e.Channel == channel && e.Target == target && (tokenID == "" || e.TokenID == tokenID) {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	m.subs = kept
	return removed, nil
}

// ListSubscriptions implements SubscriptionStore.
func (m *MemorySubscriptions) ListSubscriptions(_ context.Context, channel, target string) ([]Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Subscription
	for _, e := range m.subs {
		if e.Channel == channel && e.Target == target {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TokenID < out[j].TokenID })
	return out, nil
}

// SubscriptionsForToken implements SubscriptionStore.
func (m *MemorySubscriptions) SubscriptionsForToken(_ context.Context, channel, tokenID string) ([]Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Subscription
	for _, e := range m.subs {
		if e.Channel == channel && (e.TokenID == tokenID || e.TokenID == AllTokens) {
			out = append(out, e)
		}
	}
	return out, nil
}
