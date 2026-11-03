package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// LogPrefix starts every NEP-297 event log.
const LogPrefix = "EVENT_JSON:"

// Standards handled by ParseLog.
const (
	StandardNEP245 = "nep245"
	StandardDIP4   = "dip4"
)

// Directions reported by TokenAmounts.
const (
	DirMint     = "mint"
	DirBurn     = "burn"
	DirTransfer = "transfer"
	DirWithdraw = "withdraw" // DIP-4 *_withdraw events
	DirDiff     = "diff"     // DIP-4 token_diff entries (signed amounts)
)

// Num is a decimal quantity. The contract serialises amounts as strings
// (DisplayFromStr); Num also accepts a bare JSON number, and keeps any other
// JSON value (e.g. an object) as its raw text, so one odd field never fails
// a whole log line.
type Num string

// UnmarshalJSON implements json.Unmarshaler.
func (n *Num) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if string(b) == "null" {
		*n = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*n = Num(s)
		return nil
	}
	*n = Num(b)
	return nil
}

// Item is one element of an event's data array. It is a union of the fields
// of all supported events; fields an event does not have stay empty. Raw is
// always the item's original JSON.
type Item struct {
	Raw  json.RawMessage `json:"-"`
	Memo string          `json:"memo,omitempty"`

	// NEP-245 (crates/near/nep245/src/events.rs).
	OwnerID      string `json:"owner_id,omitempty"`
	AuthorizedID string `json:"authorized_id,omitempty"`
	OldOwnerID   string `json:"old_owner_id,omitempty"`
	NewOwnerID   string `json:"new_owner_id,omitempty"`
	TokenIDs     []string
	Amounts      []Num

	// DIP-4 common (contracts/defuse/core/src/events/mod.rs).
	IntentHash string `json:"intent_hash,omitempty"`
	AccountID  string `json:"account_id,omitempty"`
	ReceiverID string `json:"receiver_id,omitempty"`

	// transfer / token_diff
	Tokens        map[string]Num `json:"tokens,omitempty"`
	Diff          map[string]Num `json:"diff,omitempty"`
	FeesCollected map[string]Num `json:"fees_collected,omitempty"`
	Referral      string         `json:"referral,omitempty"`

	// *_withdraw
	Token          string `json:"token,omitempty"`
	NFTTokenID     string
	Amount         Num
	Msg            string
	StorageDeposit Num
	MinGas         Num

	// storage_deposit
	ContractID          string
	DepositForAccountID string

	// intents_executed
	Nonce string
}

// itemJSON mirrors Item with explicit JSON names (Item keeps Go-friendly
// field names for the ones that are not simple).
type itemJSON struct {
	Memo                string         `json:"memo"`
	OwnerID             string         `json:"owner_id"`
	AuthorizedID        string         `json:"authorized_id"`
	OldOwnerID          string         `json:"old_owner_id"`
	NewOwnerID          string         `json:"new_owner_id"`
	TokenIDs            []string       `json:"token_ids"`
	Amounts             []Num          `json:"amounts"`
	IntentHash          string         `json:"intent_hash"`
	AccountID           string         `json:"account_id"`
	ReceiverID          string         `json:"receiver_id"`
	Tokens              map[string]Num `json:"tokens"`
	Diff                map[string]Num `json:"diff"`
	FeesCollected       map[string]Num `json:"fees_collected"`
	Referral            string         `json:"referral"`
	Token               string         `json:"token"`
	NFTTokenID          string         `json:"token_id"`
	Amount              Num            `json:"amount"`
	Msg                 string         `json:"msg"`
	StorageDeposit      Num            `json:"storage_deposit"`
	MinGas              Num            `json:"min_gas"`
	ContractID          string         `json:"contract_id"`
	DepositForAccountID string         `json:"deposit_for_account_id"`
	Nonce               string         `json:"nonce"`
}

func decodeItem(raw json.RawMessage) (Item, error) {
	var j itemJSON
	err := json.Unmarshal(raw, &j)
	return Item{
		Raw: raw, Memo: j.Memo, OwnerID: j.OwnerID, AuthorizedID: j.AuthorizedID,
		OldOwnerID: j.OldOwnerID, NewOwnerID: j.NewOwnerID, TokenIDs: j.TokenIDs, Amounts: j.Amounts,
		IntentHash: j.IntentHash, AccountID: j.AccountID, ReceiverID: j.ReceiverID,
		Tokens: j.Tokens, Diff: j.Diff, FeesCollected: j.FeesCollected, Referral: j.Referral,
		Token: j.Token, NFTTokenID: j.NFTTokenID, Amount: j.Amount, Msg: j.Msg,
		StorageDeposit: j.StorageDeposit, MinGas: j.MinGas,
		ContractID: j.ContractID, DepositForAccountID: j.DepositForAccountID, Nonce: j.Nonce,
	}, err
}

// Event is one decoded EVENT_JSON log.
type Event struct {
	Standard string
	Version  string // informational only; never filtered on
	Name     string // "mt_mint", "ft_withdraw", ...
	Items    []Item // data array (a single object becomes one item)
	Raw      string // the JSON after the EVENT_JSON: prefix, as logged
}

type envelope struct {
	Standard string          `json:"standard"`
	Version  string          `json:"version"`
	Event    string          `json:"event"`
	Data     json.RawMessage `json:"data"`
}

// ParseLog decodes one log line. Lines that are not EVENT_JSON, or whose
// standard is neither nep245 nor dip4, return ok=false with no error.
// Malformed EVENT_JSON for a known standard returns an error.
//
// Version is deliberately ignored (it differs between contract releases).
func ParseLog(log string) (Event, bool, error) {
	if !strings.HasPrefix(log, LogPrefix) {
		return Event{}, false, nil
	}
	payload := strings.TrimSpace(log[len(LogPrefix):])
	var env envelope
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		return Event{}, false, fmt.Errorf("events: bad EVENT_JSON: %w", err)
	}
	if env.Standard != StandardNEP245 && env.Standard != StandardDIP4 {
		return Event{}, false, nil
	}
	if env.Event == "" {
		return Event{}, false, errors.New("events: EVENT_JSON without event name")
	}
	ev := Event{Standard: env.Standard, Version: env.Version, Name: env.Event, Raw: payload}

	var raws []json.RawMessage
	data := bytes.TrimSpace(env.Data)
	switch {
	case len(data) == 0 || string(data) == "null":
	case data[0] == '[':
		if err := json.Unmarshal(data, &raws); err != nil {
			return Event{}, false, fmt.Errorf("events: bad data array: %w", err)
		}
	default:
		raws = []json.RawMessage{data}
	}
	strict := env.Standard == StandardNEP245
	for _, r := range raws {
		it, err := decodeItem(r)
		if err != nil && strict {
			return Event{}, false, fmt.Errorf("events: bad %s item: %w", env.Event, err)
		}
		// DIP-4 items that do not fit (unknown shapes) stay raw-only.
		if err != nil {
			it = Item{Raw: r}
		}
		if strict && len(it.TokenIDs) != len(it.Amounts) {
			return Event{}, false, fmt.Errorf("events: %s has %d token_ids but %d amounts", env.Event, len(it.TokenIDs), len(it.Amounts))
		}
		ev.Items = append(ev.Items, it)
	}
	return ev, true, nil
}

// TokenAmount is one (token, amount) pair of an event.
type TokenAmount struct {
	TokenID   string
	Amount    string // decimal; negative for token_diff debits
	Owner     string // mint/burn owner, transfer sender, or DIP-4 account_id
	To        string // transfer/withdraw receiver, if any
	Direction string // mint, burn, transfer, withdraw, diff
}

// TokenAmounts flattens the event into per-token entries.
//
// NEP-245 events yield mint/burn/transfer entries with full Intents token
// ids. DIP-4 transfer, token_diff and *_withdraw events yield entries too
// (direction transfer, diff, withdraw); the Intents id of a withdrawn token
// is derived as nep141:<token>, nep245:<token>:<id> or nep171:<token>:<id>.
// native_withdraw has TokenID "native". Other events yield none.
func (e Event) TokenAmounts() []TokenAmount {
	var out []TokenAmount
	for _, it := range e.Items {
		switch e.Standard {
		case StandardNEP245:
			dir, owner, to := "", it.OwnerID, ""
			switch e.Name {
			case "mt_mint":
				dir = DirMint
			case "mt_burn":
				dir = DirBurn
			case "mt_transfer":
				dir, owner, to = DirTransfer, it.OldOwnerID, it.NewOwnerID
			default:
				continue
			}
			for i, id := range it.TokenIDs {
				if i >= len(it.Amounts) {
					break
				}
				out = append(out, TokenAmount{TokenID: id, Amount: string(it.Amounts[i]), Owner: owner, To: to, Direction: dir})
			}
		case StandardDIP4:
			switch e.Name {
			case "transfer":
				for _, k := range sortedKeys(it.Tokens) {
					out = append(out, TokenAmount{TokenID: k, Amount: string(it.Tokens[k]), Owner: it.AccountID, To: it.ReceiverID, Direction: DirTransfer})
				}
			case "token_diff":
				for _, k := range sortedKeys(it.Diff) {
					out = append(out, TokenAmount{TokenID: k, Amount: string(it.Diff[k]), Owner: it.AccountID, Direction: DirDiff})
				}
			case "ft_withdraw":
				out = append(out, TokenAmount{TokenID: "nep141:" + it.Token, Amount: string(it.Amount), Owner: it.AccountID, To: it.ReceiverID, Direction: DirWithdraw})
			case "nft_withdraw":
				out = append(out, TokenAmount{TokenID: "nep171:" + it.Token + ":" + it.NFTTokenID, Amount: "1", Owner: it.AccountID, To: it.ReceiverID, Direction: DirWithdraw})
			case "mt_withdraw":
				for i, id := range it.TokenIDs {
					if i >= len(it.Amounts) {
						break
					}
					out = append(out, TokenAmount{TokenID: "nep245:" + it.Token + ":" + id, Amount: string(it.Amounts[i]), Owner: it.AccountID, To: it.ReceiverID, Direction: DirWithdraw})
				}
			case "native_withdraw":
				out = append(out, TokenAmount{TokenID: "native", Amount: string(it.Amount), Owner: it.AccountID, To: it.ReceiverID, Direction: DirWithdraw})
			}
		}
	}
	return out
}

// Mentions reports whether any item of the event refers to tokenID (as a
// token amount, a fee, or anywhere in the raw item text for events without
// amounts).
func (e Event) Mentions(tokenID string) bool {
	for _, ta := range e.TokenAmounts() {
		if ta.TokenID == tokenID {
			return true
		}
	}
	for _, it := range e.Items {
		if _, ok := it.FeesCollected[tokenID]; ok {
			return true
		}
		if e.Standard == StandardDIP4 && bytes.Contains(it.Raw, []byte(`"`+tokenID+`"`)) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]Num) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// small maps; insertion sort keeps output deterministic
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
