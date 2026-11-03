package tokens

import "github.com/emma31-dev/argus/internal/model"

// DefaultIntentsAccount is the NEAR Intents verifier contract.
const DefaultIntentsAccount = "intents.near"

// HeldMethod returns the view call that reads how much of t the default
// intents account holds. See HeldMethodFor.
func HeldMethod(t model.Token) (contract, method string, args any) {
	return HeldMethodFor(DefaultIntentsAccount, t)
}

// HeldMethodFor returns the contract, view method and JSON arguments that
// read how much of t the given holder account owns:
//
//	nep141: <contract>.ft_balance_of({"account_id": holder})
//	nep245: <contract>.mt_balance_of({"account_id": holder, "token_id": inner})
//
// Both return the balance as a decimal string. For out-of-scope standards
// (nep171) it returns an empty contract and method.
func HeldMethodFor(holder string, t model.Token) (contract, method string, args any) {
	switch t.Standard {
	case model.StandardNEP141:
		return t.Contract, "ft_balance_of", map[string]string{"account_id": holder}
	case model.StandardNEP245:
		return t.Contract, "mt_balance_of", map[string]string{"account_id": holder, "token_id": t.InnerID}
	default:
		return "", "", nil
	}
}
