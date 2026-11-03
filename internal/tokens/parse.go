// Package tokens parses NEAR Intents token ids, builds the view calls that
// read how much of each token intents.near holds, and discovers which tokens
// to track.
//
// Token id grammar (from the Intents contract, crates/primitives/token-id):
//
//	nep141:<contract>
//	nep171:<contract>:<token_id>
//	nep245:<contract>:<inner_id>
//
// The contract source splits on the first ':' after the standard and, for
// nep171/nep245, on the first ':' of the remainder. The inner id may
// therefore contain further colons.
package tokens

import (
	"errors"
	"fmt"
	"strings"

	"github.com/emma31-dev/argus/internal/model"
)

var (
	// ErrInvalidID means the string is not a well-formed Intents token id.
	ErrInvalidID = errors.New("tokens: invalid token id")
	// ErrUnsupportedStandard means the standard prefix is not one Argus knows
	// (for example the contract's "imt" minter tokens).
	ErrUnsupportedStandard = errors.New("tokens: unsupported token standard")
)

// maxAccountLen is the NEAR account id length limit.
const maxAccountLen = 64

// Parse splits an Intents token id into a model.Token. Only TokenID,
// Standard, Contract and InnerID are filled in. nep171 ids parse
// successfully but are out of scope; see InScope.
func Parse(id string) (model.Token, error) {
	std, rest, ok := strings.Cut(id, ":")
	if !ok || rest == "" {
		return model.Token{}, fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	t := model.Token{TokenID: id}
	switch model.Standard(std) {
	case model.StandardNEP141:
		if !validAccount(rest) {
			return model.Token{}, fmt.Errorf("%w: bad contract in %q", ErrInvalidID, id)
		}
		t.Standard, t.Contract = model.StandardNEP141, rest
	case model.StandardNEP245, model.StandardNEP171:
		contract, inner, ok := strings.Cut(rest, ":")
		if !ok || inner == "" || !validAccount(contract) {
			return model.Token{}, fmt.Errorf("%w: %q", ErrInvalidID, id)
		}
		t.Standard, t.Contract, t.InnerID = model.Standard(std), contract, inner
	default:
		return model.Token{}, fmt.Errorf("%w: %q in %q", ErrUnsupportedStandard, std, id)
	}
	return t, nil
}

// InScope reports whether Argus tracks tokens of this standard in v1
// (NEP-141 and NEP-245; NFTs are out of scope).
func InScope(t model.Token) bool {
	return t.Standard == model.StandardNEP141 || t.Standard == model.StandardNEP245
}

// validAccount is a deliberately light NEAR account id check: lowercase
// letters, digits and the separators . _ - with a sane length.
func validAccount(s string) bool {
	if len(s) < 2 || len(s) > maxAccountLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}
