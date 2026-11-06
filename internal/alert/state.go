package alert

import (
	"math/big"
	"time"

	"github.com/emma31-dev/argus/internal/model"
)

// DefaultK is the default number of consecutive negative snapshots that turns
// a token red (about two minutes at the default 30 s interval).
const DefaultK = 4

// Config tunes the debounce state machine.
type Config struct {
	// K is the number of consecutive negative snapshots needed to go red.
	// Values <= 0 mean DefaultK.
	K int
	// AbsThreshold maps a token id to a deficit (smallest units) at or above
	// which the token goes red immediately. Optional.
	AbsThreshold map[string]*big.Int
	// DefaultAbsThreshold applies to tokens without an AbsThreshold entry.
	DefaultAbsThreshold *big.Int
}

func (c Config) k() int {
	if c.K <= 0 {
		return DefaultK
	}
	return c.K
}

// threshold returns the absolute red threshold for a token, or nil if none.
func (c Config) threshold(tokenID string) *big.Int {
	if t, ok := c.AbsThreshold[tokenID]; ok && t != nil && t.Sign() > 0 {
		return t
	}
	if c.DefaultAbsThreshold != nil && c.DefaultAbsThreshold.Sign() > 0 {
		return c.DefaultAbsThreshold
	}
	return nil
}

// Kind identifies a state transition (and, for the first three, an alert).
type Kind string

const (
	TurnedAmber       Kind = "turned_amber"
	TurnedRed         Kind = "turned_red"
	IncidentUpdate    Kind = "incident_update"
	Recovered         Kind = "recovered"
	FalseAlarmCleared Kind = "false_alarm_cleared"
)

// Alertable reports whether a transition of this kind is sent to users.
// Amber and cleared false alarms are recorded only.
func (k Kind) Alertable() bool {
	return k == TurnedRed || k == IncidentUpdate || k == Recovered
}

// Transition describes one change produced by Step.
type Transition struct {
	Kind      Kind
	TokenID   string
	Block     uint64    // block of the snapshot that caused the transition
	BlockTime time.Time // its block time
	Gap       string    // held - owed at Block (negative while in deficit)
	// MaxDeficit is the largest deficit of the streak/incident so far, as a
	// positive integer string, and MaxDeficitBlock the block it occurred at.
	MaxDeficit      string
	MaxDeficitBlock uint64
	StartBlock      uint64 // first negative block of the streak
	RedBlock        uint64 // block at which the token turned red (0 if never)
	// IncidentID is the open incident the transition belongs to (set by Step
	// only for IncidentUpdate and Recovered; the Engine fills it for TurnedRed).
	IncidentID int64
	Streak     int
}

func parseInt(s string) (*big.Int, bool) {
	if s == "" {
		return new(big.Int), true
	}
	return new(big.Int).SetString(s, 10)
}

// Step advances one token's state by one successful snapshot. It is pure: no
// clock, no I/O. Snapshots with a block at or below prev.LastBlock (duplicate
// or out of order) and snapshots whose gap does not parse are ignored here;
// the Engine, which knows a round's boundaries, turns a snapshot that fails
// to advance the block in a new round into a failed read (see Engine.Process).
//
// Rules: gap >= 0 is green. gap < 0 increments the negative streak;
// the token is amber while streak < K and red once streak >= K or the deficit
// reaches the absolute threshold. While red, a new maximum deficit yields
// IncidentUpdate. Recovery from red yields Recovered; recovery from amber
// yields FalseAlarmCleared.
//
// Invariant: whenever a token goes green while prev.OpenIncidentID != 0 the
// transition is Recovered (which closes that incident), whatever the previous
// status was. This repairs states in which an incident was left open next to
// a non-red status.
func Step(prev model.TokenState, snap model.Snapshot, cfg Config) (model.TokenState, []Transition) {
	if prev.LastBlock != 0 && snap.BlockHeight <= prev.LastBlock {
		return prev, nil
	}
	gap, ok := parseInt(snap.Gap)
	if !ok || snap.Gap == "" {
		return prev, nil
	}
	next := prev
	if next.TokenID == "" {
		next.TokenID = snap.TokenID
	}
	next.LastBlock = snap.BlockHeight
	next.Stale, next.LastError, next.StaleSince = false, "", time.Time{}
	if !snap.TakenAt.IsZero() {
		next.UpdatedAt = snap.TakenAt
	} else {
		next.UpdatedAt = snap.BlockTime
	}

	tr := Transition{
		TokenID: next.TokenID, Block: snap.BlockHeight, BlockTime: snap.BlockTime,
		Gap: gap.String(),
	}

	if gap.Sign() >= 0 {
		tr.StartBlock = prev.FirstNegativeBlock
		tr.RedBlock = prev.RedBlock
		tr.MaxDeficit = prev.MaxDeficit
		tr.MaxDeficitBlock = prev.MaxDeficitBlock
		tr.Streak = prev.NegativeStreak
		var out []Transition
		switch {
		case prev.Status == model.StatusRed || prev.OpenIncidentID != 0:
			tr.Kind = Recovered
			tr.IncidentID = prev.OpenIncidentID
			out = append(out, tr)
		case prev.NegativeStreak > 0:
			tr.Kind = FalseAlarmCleared
			out = append(out, tr)
		}
		next.Status = model.StatusGreen
		next.NegativeStreak = 0
		next.FirstNegativeBlock = 0
		next.MaxDeficit, next.MaxDeficitBlock = "", 0
		next.RedBlock = 0
		next.OpenIncidentID = 0
		return next, out
	}

	// gap < 0
	deficit := new(big.Int).Neg(gap)
	if prev.NegativeStreak <= 0 {
		next.NegativeStreak = 1
		next.FirstNegativeBlock = snap.BlockHeight
		next.MaxDeficit, next.MaxDeficitBlock = "", 0
	} else {
		next.NegativeStreak = prev.NegativeStreak + 1
	}
	prevMax, ok := parseInt(next.MaxDeficit)
	if !ok {
		prevMax = new(big.Int)
	}
	newMax := deficit.Cmp(prevMax) > 0
	if newMax {
		next.MaxDeficit = deficit.String()
		next.MaxDeficitBlock = snap.BlockHeight
	}

	tr.StartBlock = next.FirstNegativeBlock
	tr.MaxDeficit = next.MaxDeficit
	tr.MaxDeficitBlock = next.MaxDeficitBlock
	tr.Streak = next.NegativeStreak

	wasRed := prev.Status == model.StatusRed
	red := wasRed || next.NegativeStreak >= cfg.k()
	if th := cfg.threshold(next.TokenID); th != nil && deficit.Cmp(th) >= 0 {
		red = true
	}

	switch {
	case wasRed:
		next.Status = model.StatusRed
		tr.RedBlock = prev.RedBlock
		tr.IncidentID = prev.OpenIncidentID
		if newMax {
			tr.Kind = IncidentUpdate
			return next, []Transition{tr}
		}
	case red:
		next.Status = model.StatusRed
		next.RedBlock = snap.BlockHeight
		tr.RedBlock = snap.BlockHeight
		tr.Kind = TurnedRed
		return next, []Transition{tr}
	default:
		next.Status = model.StatusAmber
		if prev.Status != model.StatusAmber || prev.NegativeStreak == 0 {
			tr.Kind = TurnedAmber
			return next, []Transition{tr}
		}
	}
	return next, nil
}

// StepError records a failed read for a token. It never resets the negative
// streak and never changes the status of a token that has had a snapshot; it
// only flags the state as stale and records, in StaleSince, when reads first
// failed (kept while the failures continue; a successful Step clears it). A
// token that never had a snapshot is unknown. now is the time of the failed
// read.
func StepError(prev model.TokenState, err error, _ Config, now time.Time) model.TokenState {
	next := prev
	next.Stale = true
	if next.StaleSince.IsZero() {
		next.StaleSince = now
	}
	if err != nil {
		msg := err.Error()
		if len(msg) > 300 {
			msg = msg[:300]
		}
		next.LastError = msg
	}
	if next.Status == "" || (prev.LastBlock == 0 && next.Status != model.StatusRed) {
		next.Status = model.StatusUnknown
	}
	return next
}
