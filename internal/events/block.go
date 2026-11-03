package events

import (
	"encoding/json"
)

// Block is the subset of a neardata BlockDocument (GET /v0/block/:height)
// that Argus decodes. Everything else in the document is ignored.
type Block struct {
	Block struct {
		Header Header `json:"header"`
	} `json:"block"`
	Shards []Shard `json:"shards"`
}

// Header is the block header subset.
type Header struct {
	Height    uint64 `json:"height"`
	Hash      string `json:"hash"`
	Timestamp uint64 `json:"timestamp"` // nanoseconds since the epoch
}

// Shard holds one shard's receipt execution outcomes.
type Shard struct {
	ShardID                  uint64           `json:"shard_id"`
	ReceiptExecutionOutcomes []ReceiptOutcome `json:"receipt_execution_outcomes"`
}

// ReceiptOutcome is one executed receipt with its outcome. TxHash is added
// by neardata: the hash of the transaction that originated the receipt.
type ReceiptOutcome struct {
	ExecutionOutcome struct {
		ID      string  `json:"id"`
		Outcome Outcome `json:"outcome"`
	} `json:"execution_outcome"`
	Receipt *struct {
		ReceiptID string `json:"receipt_id"`
	} `json:"receipt"`
	TxHash string `json:"tx_hash"`
}

// Outcome is the execution outcome subset.
type Outcome struct {
	ExecutorID string          `json:"executor_id"`
	Logs       []string        `json:"logs"`
	Status     json.RawMessage `json:"status"`
}

// ReceiptID returns the receipt id, preferring receipt.receipt_id and
// falling back to execution_outcome.id (the two are equal in NEAR data).
func (r ReceiptOutcome) ReceiptID() string {
	if r.Receipt != nil && r.Receipt.ReceiptID != "" {
		return r.Receipt.ReceiptID
	}
	return r.ExecutionOutcome.ID
}

// Succeeded reports whether the outcome status is SuccessValue or
// SuccessReceiptId. Failure, Unknown and unparseable statuses are false: a
// failed receipt's state changes are reverted, so its events never happened.
func (o Outcome) Succeeded() bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(o.Status, &m); err != nil {
		return false
	}
	_, a := m["SuccessValue"]
	_, b := m["SuccessReceiptId"]
	return a || b
}

// Height returns the block height, 0 for a nil block.
func (b *Block) Height() uint64 {
	if b == nil {
		return 0
	}
	return b.Block.Header.Height
}
