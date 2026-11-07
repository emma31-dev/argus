package tokens

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/rpc"
)

// Caller is the slice of the RPC client that discovery needs.
type Caller interface {
	LatestFinalBlock(ctx context.Context) (rpc.Block, string, error)
	CallFunction(ctx context.Context, provider, account, method string, args any, blockHeight uint64) ([]byte, error)
}

// defaultEnumPage is large on purpose. intents.near lists every token with a
// non-zero supply, and on mainnet nearly all of the ~88k entries are NFTs
// (nep171) that Argus drops. Small pages would cost hundreds of RPC calls per
// discovery; 5000 is also what the Intents team's own listing script uses.
const defaultEnumPage = 5000

// maxEnumPages bounds pagination so a misbehaving node cannot loop us forever.
const maxEnumPages = 200

// Enumeration lists tokens through NEP-245 enumeration on the Intents
// contract:
//
//	intents.near.mt_tokens({"from_index": "<u128 string>", "limit": <u32>})
//	  -> [{"token_id": "nep141:wrap.near"}, ...]
//
// (contracts/defuse/src/contract/tokens/nep245/enumeration.rs in
// github.com/near/intents iterates total_supplies.) Pages are read from one
// provider at one block so the list is consistent. Paging ends on an empty
// page, not a short one, so a provider that caps limit cannot silently
// truncate the list.
type Enumeration struct {
	Caller   Caller
	Account  string // defaults to DefaultIntentsAccount
	PageSize int    // defaults to 5000
}

// Name implements Source.
func (e *Enumeration) Name() string { return SourceEnumeration }

// Fetch implements Source. It returns ErrNotSupported when the contract does
// not expose mt_tokens.
func (e *Enumeration) Fetch(ctx context.Context) ([]model.Token, error) {
	account := e.Account
	if account == "" {
		account = DefaultIntentsAccount
	}
	page := e.PageSize
	if page <= 0 {
		page = defaultEnumPage
	}
	blk, provider, err := e.Caller.LatestFinalBlock(ctx)
	if err != nil {
		return nil, fmt.Errorf("latest block: %w", err)
	}
	var out []model.Token
	from := 0
	for i := 0; i < maxEnumPages; i++ {
		raw, err := e.Caller.CallFunction(ctx, provider, account, "mt_tokens",
			map[string]any{"from_index": strconv.Itoa(from), "limit": page}, blk.Height)
		if err != nil {
			if errors.Is(err, rpc.ErrMethodNotFound) {
				return nil, fmt.Errorf("%w: %s has no mt_tokens", ErrNotSupported, account)
			}
			return nil, fmt.Errorf("mt_tokens from %d: %w", from, err)
		}
		var items []struct {
			TokenID string `json:"token_id"`
		}
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("mt_tokens: decode: %w", err)
		}
		for _, it := range items {
			if t, err := Parse(it.TokenID); err == nil && InScope(t) {
				t.Source = SourceEnumeration
				out = append(out, t)
			}
		}
		if len(items) == 0 {
			return out, nil
		}
		from += len(items)
	}
	return nil, fmt.Errorf("mt_tokens: more than %d pages", maxEnumPages)
}

var _ Caller = (*rpc.Client)(nil)
