package events

import (
	"context"
	"fmt"

	"github.com/emma31-dev/argus/internal/model"
)

// EventStore is the slice of the store the attacher writes to; *store.Repo
// satisfies it.
type EventStore interface {
	AddIncidentEvents(ctx context.Context, events []model.IncidentEvent) error
}

// DefaultMaxAttachBlocks caps one AttachRange call (about 25 minutes at the
// default 2 rps). Callers extend an open incident with incremental ranges.
const DefaultMaxAttachBlocks = 3000

// ErrRangeTooLarge is returned when the range exceeds MaxBlocks.
var ErrRangeTooLarge = fmt.Errorf("events: block range too large")

// Attacher copies the events relevant to an incident into the store.
type Attacher struct {
	Source      BlockSource
	Account     string // default DefaultAccount
	Store       EventStore
	Concurrency int // default 4
	MaxBlocks   int // default DefaultMaxAttachBlocks
}

// AttachRange backfills [from, to], keeps the rows that touch tokenID plus
// DIP-4 events that mention it, tags them with incidentID, stores them and
// returns how many were stored. Storing is idempotent on the store side
// (ON CONFLICT DO NOTHING), so overlapping ranges are safe.
func (a *Attacher) AttachRange(ctx context.Context, incidentID int64, tokenID string, from, to uint64) (int, error) {
	if from > to {
		return 0, nil
	}
	max := a.MaxBlocks
	if max <= 0 {
		max = DefaultMaxAttachBlocks
	}
	if to-from+1 > uint64(max) {
		return 0, fmt.Errorf("%w: %d blocks (max %d)", ErrRangeTooLarge, to-from+1, max)
	}
	var keep []model.IncidentEvent
	err := Backfill(ctx, a.Source, a.Account, a.Concurrency, from, to, func(_ uint64, evs []model.IncidentEvent) error {
		for _, e := range evs {
			if relevant(e, tokenID) {
				e.IncidentID = incidentID
				keep = append(keep, e)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if len(keep) == 0 {
		return 0, nil
	}
	if err := a.Store.AddIncidentEvents(ctx, keep); err != nil {
		return 0, err
	}
	return len(keep), nil
}

func relevant(e model.IncidentEvent, tokenID string) bool {
	if e.TokenID == tokenID {
		return true
	}
	if e.Standard != StandardDIP4 {
		return false
	}
	ev, ok, err := ParseLog(LogPrefix + e.Raw)
	return err == nil && ok && ev.Mentions(tokenID)
}
