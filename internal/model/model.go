// Package model holds the types shared across Argus packages. It has no
// dependencies on other internal packages so every package can import it.
//
// All token amounts are carried as base-10 integer strings in the token's
// smallest unit (yocto for wNEAR, 1e-6 for USDC, ...). Never use floats for
// amounts; convert with math/big at the edges.
package model

import "time"

// Standard is the token standard part of an Intents token id.
type Standard string

const (
	StandardNEP141 Standard = "nep141" // fungible token: <contract>.ft_balance_of
	StandardNEP245 Standard = "nep245" // multi token: <contract>.mt_balance_of(token_id=<inner>)
	StandardNEP171 Standard = "nep171" // NFT, out of scope for v1
)

// Token is one asset tracked on intents.near.
type Token struct {
	// TokenID is the full Intents token id, e.g. "nep141:wrap.near" or
	// "nep245:v2_1.omni.hot.tg:56_11111111111111111111".
	TokenID  string   `json:"id"`
	Standard Standard `json:"standard"`
	// Contract is the NEAR account that holds the real balance ledger
	// (the FT or MT contract).
	Contract string `json:"contract"`
	// InnerID is the token id inside an NEP-245 contract; empty for NEP-141.
	InnerID  string `json:"inner_id,omitempty"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
	// Chain is a human label for the network the asset comes from ("NEAR",
	// "Ethereum", "BNB Chain"...). Symbols repeat across chains, so show it
	// next to the symbol. Derived from the id by tokens.ChainOf; display only.
	Chain string `json:"chain,omitempty"`
	// Source records how the token was discovered: "enumeration",
	// "1click", or "config".
	Source    string    `json:"source"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Status is the traffic-light state of a token.
type Status string

const (
	StatusGreen   Status = "green"   // held >= owed
	StatusAmber   Status = "amber"   // gap < 0 at the latest snapshot, inside the settling window
	StatusRed     Status = "red"     // gap < 0 for K consecutive snapshots, or deficit above threshold
	StatusUnknown Status = "unknown" // no successful snapshot yet, or the last read failed
)

// Snapshot is one same-block reading of a token.
type Snapshot struct {
	TokenID     string    `json:"token_id"`
	BlockHeight uint64    `json:"block_height"`
	BlockHash   string    `json:"block_hash,omitempty"`
	BlockTime   time.Time `json:"block_time"`
	Owed        string    `json:"owed"` // intents.near mt_supply(token), smallest units
	Held        string    `json:"held"` // contract balance of intents.near, smallest units
	Gap         string    `json:"gap"`  // held - owed, may be negative
	// Provider is the RPC endpoint that served both reads (both sides are
	// always read from the same provider at the same block).
	Provider string    `json:"provider"`
	TakenAt  time.Time `json:"taken_at"`
}

// TokenState is the alert state machine's view of one token.
type TokenState struct {
	TokenID string `json:"token_id"`
	Status  Status `json:"status"`
	// NegativeStreak is the number of consecutive snapshots with gap < 0.
	NegativeStreak int `json:"negative_streak"`
	// FirstNegativeBlock is the block where the current negative streak began (0 if none).
	FirstNegativeBlock uint64    `json:"first_negative_block,omitempty"`
	OpenIncidentID     int64     `json:"open_incident_id,omitempty"`
	LastBlock          uint64    `json:"last_block"`
	UpdatedAt          time.Time `json:"updated_at"`

	// The fields below are used by the debounce state machine
	// (internal/alert) to keep Step a pure function of (state, snapshot).

	// MaxDeficit is the largest deficit (positive integer string) seen in the
	// current negative streak; empty when the streak is 0.
	MaxDeficit      string `json:"max_deficit,omitempty"`
	MaxDeficitBlock uint64 `json:"max_deficit_block,omitempty"`
	// RedBlock is the block at which the token turned red (0 unless red).
	RedBlock uint64 `json:"red_block,omitempty"`
	// Stale is true when the most recent read of this token failed; the
	// status and streak are deliberately left untouched in that case.
	Stale     bool   `json:"stale,omitempty"`
	LastError string `json:"last_error,omitempty"`
	// StaleSince is when reads of this token started failing (or stopped
	// advancing); zero while the token reads fine. A successful snapshot
	// clears it. The API reports a token whose StaleSince is older than the
	// staleness bound as unknown.
	StaleSince time.Time `json:"stale_since,omitempty"`
}

// Incident is a red period for a token.
type Incident struct {
	ID         int64      `json:"id"`
	TokenID    string     `json:"token_id"`
	StartBlock uint64     `json:"start_block"` // first negative snapshot of the streak
	RedBlock   uint64     `json:"red_block"`   // block at which it turned red
	EndBlock   uint64     `json:"end_block,omitempty"`
	OpenedAt   time.Time  `json:"opened_at"`
	ClosedAt   *time.Time `json:"closed_at,omitempty"`
	// MaxDeficit is the largest observed deficit as a positive integer string.
	MaxDeficit      string `json:"max_deficit"`
	MaxDeficitBlock uint64 `json:"max_deficit_block"`
	// Summary is optional AI-written text (P1). Never drives alert state.
	Summary string `json:"summary,omitempty"`
}

// IncidentEvent is an intents.near event (from the event stream) attached
// to an incident's block range.
type IncidentEvent struct {
	IncidentID  int64  `json:"incident_id"`
	BlockHeight uint64 `json:"block_height"`
	ReceiptID   string `json:"receipt_id"`
	TxHash      string `json:"tx_hash,omitempty"`
	Standard    string `json:"standard"` // "nep245" or "dip4"
	Event       string `json:"event"`    // "mt_mint", "mt_burn", "ft_withdraw", ...
	TokenID     string `json:"token_id,omitempty"`
	Amount      string `json:"amount,omitempty"`
	Raw         string `json:"raw"` // the EVENT_JSON payload as logged
	// LogIndex is the position of the log line within the receipt's execution
	// outcome and ItemIndex the position of the (token, amount) item within
	// that event. Together with the receipt id they identify the row, even
	// when two items of one log share a token (token_diff alice -100 / bob
	// +100) or an event repeats identical payloads.
	LogIndex  int `json:"log_index,omitempty"`
	ItemIndex int `json:"item_index,omitempty"`
}
