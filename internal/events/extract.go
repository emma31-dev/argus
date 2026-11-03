package events

import (
	"math/big"

	"github.com/emma31-dev/argus/internal/model"
)

// DefaultAccount is the verifier contract whose events Argus follows.
const DefaultAccount = "intents.near"

// Extract returns the incident-event rows for one block: one row per
// (token, amount) pair of every NEP-245/DIP-4 event logged by a successful
// receipt executed on account (DefaultAccount if empty). Events with no
// token amounts (account_locked, fee_changed, ...) yield one row without
// TokenID/Amount. A nil block yields nil. Malformed logs are skipped.
func Extract(b *Block, account string) []model.IncidentEvent {
	if b == nil {
		return nil
	}
	if account == "" {
		account = DefaultAccount
	}
	height := b.Block.Header.Height
	var out []model.IncidentEvent
	for _, sh := range b.Shards {
		for _, ro := range sh.ReceiptExecutionOutcomes {
			oc := ro.ExecutionOutcome.Outcome
			if oc.ExecutorID != account || !oc.Succeeded() {
				continue
			}
			for _, log := range oc.Logs {
				ev, ok, err := ParseLog(log)
				if err != nil || !ok {
					continue
				}
				base := model.IncidentEvent{
					BlockHeight: height,
					ReceiptID:   ro.ReceiptID(),
					TxHash:      ro.TxHash,
					Standard:    ev.Standard,
					Event:       ev.Name,
					Raw:         ev.Raw,
				}
				tas := ev.TokenAmounts()
				if len(tas) == 0 {
					out = append(out, base)
					continue
				}
				for _, ta := range tas {
					row := base
					row.TokenID, row.Amount = ta.TokenID, ta.Amount
					out = append(out, row)
				}
			}
		}
	}
	return out
}

// Deltas sums the supply change per token over NEP-245 rows from intents.near:
// mt_mint adds, mt_burn subtracts, mt_transfer leaves the token's entry
// unchanged (it is still present, at zero if it had no other rows). Rows with
// unparseable amounts are ignored. DIP-4 rows never count: withdrawals show
// up as mt_burn too.
func Deltas(events []model.IncidentEvent) map[string]*big.Int {
	out := map[string]*big.Int{}
	for _, e := range events {
		if e.Standard != StandardNEP245 || e.TokenID == "" {
			continue
		}
		var sign int
		switch e.Event {
		case "mt_mint":
			sign = 1
		case "mt_burn":
			sign = -1
		case "mt_transfer":
		default:
			continue
		}
		d := out[e.TokenID]
		if d == nil {
			d = new(big.Int)
			out[e.TokenID] = d
		}
		if sign == 0 {
			continue
		}
		v, ok := new(big.Int).SetString(e.Amount, 10)
		if !ok {
			continue
		}
		if sign > 0 {
			d.Add(d, v)
		} else {
			d.Sub(d, v)
		}
	}
	return out
}
