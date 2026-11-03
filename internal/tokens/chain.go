package tokens

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/emma31-dev/argus/internal/model"
)

// omftChains maps the chain prefix of a *.omft.near token (the bare form
// "eth.omft.near" or the prefixed form "eth-0xabc....omft.near") to a label.
var omftChains = map[string]string{
	"eth": "Ethereum", "btc": "Bitcoin", "sol": "Solana", "zec": "Zcash", "doge": "Dogecoin",
	"xrp": "XRP Ledger", "tron": "Tron", "base": "Base", "arb": "Arbitrum", "bsc": "BNB Chain",
	"pol": "Polygon", "op": "Optimism", "avax": "Avalanche", "bera": "Berachain", "gnosis": "Gnosis",
	"ton": "TON", "sui": "Sui", "aptos": "Aptos", "cardano": "Cardano", "ltc": "Litecoin",
	"bch": "Bitcoin Cash", "stellar": "Stellar", "xlayer": "X Layer",
}

// hotChains maps the numeric chain id at the start of a HOT Omni inner id
// ("56_2CMM...") to a label. Ids not listed here render as "HOT Omni <id>".
var hotChains = map[string]string{
	"1": "Ethereum", "10": "Optimism", "56": "BNB Chain", "100": "Gnosis", "137": "Polygon",
	"8453": "Base", "42161": "Arbitrum", "43114": "Avalanche",
	"1100": "Stellar", "1117": "TON",
}

var rainbowERC20 = regexp.MustCompile(`^0x[0-9a-f]{40}\.factory\.bridge\.near$`)

// hotOmniContract is the HOT Omni multi-token contract.
const hotOmniContract = "v2_1.omni.hot.tg"

// ChainOf returns a human label for the network a token comes from. It is
// derived from the id alone, is for display only (symbols repeat across
// chains) and never influences alert state. Plain NEAR contracts are "NEAR".
// Unknown ids return "".
func ChainOf(tokenID string) string {
	t, err := Parse(tokenID)
	if err != nil {
		return ""
	}
	switch t.Standard {
	case model.StandardNEP141:
		return nep141Chain(t.Contract)
	case model.StandardNEP245:
		if t.Contract == hotOmniContract {
			id, _, _ := strings.Cut(t.InnerID, "_")
			if label, ok := hotChains[id]; ok {
				return label
			}
			return fmt.Sprintf("HOT Omni %s", id)
		}
		return "NEAR"
	}
	return ""
}

func nep141Chain(contract string) string {
	if prefix, ok := strings.CutSuffix(contract, ".omft.near"); ok {
		chain, _, _ := strings.Cut(prefix, "-")
		if label, ok := omftChains[chain]; ok {
			return label
		}
		return strings.ToUpper(chain) + " (omft)"
	}
	switch {
	case contract == "eth.bridge.near", rainbowERC20.MatchString(contract):
		return "Ethereum (Rainbow)"
	case contract == "nbtc.bridge.near":
		return "Bitcoin (Omni)"
	}
	return "NEAR"
}
