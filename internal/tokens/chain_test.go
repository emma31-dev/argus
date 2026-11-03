package tokens

import "testing"

func TestChainOf(t *testing.T) {
	cases := map[string]string{
		"nep141:wrap.near":              "NEAR",
		"nep141:usdt.tether-token.near": "NEAR",
		"nep141:eth.omft.near":          "Ethereum",
		"nep141:btc.omft.near":          "Bitcoin",
		"nep141:sol.omft.near":          "Solana",
		"nep141:zec.omft.near":          "Zcash",
		"nep141:doge.omft.near":         "Dogecoin",
		"nep141:xrp.omft.near":          "XRP Ledger",
		"nep141:tron.omft.near":         "Tron",
		"nep141:base.omft.near":         "Base",
		"nep141:arb.omft.near":          "Arbitrum",
		"nep141:eth-0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48.omft.near":       "Ethereum",
		"nep141:base-0x833589fcd6edb6e08f4c7c32d4f71b54bda02913.omft.near":      "Base",
		"nep141:arb-0xaf88d065e77c8cc2239327c5edb3a432268e5831.omft.near":       "Arbitrum",
		"nep141:sol-5ce3bf3a31af18be40ba30f721101b4341690186.omft.near":         "Solana",
		"nep141:tron-d28a265909efecdcee7c5028585214ea0b96f015.omft.near":        "Tron",
		"nep141:newchain-abc.omft.near":                                         "NEWCHAIN (omft)",
		"nep141:eth.bridge.near":                                                "Ethereum (Rainbow)",
		"nep141:nbtc.bridge.near":                                               "Bitcoin (Omni)",
		"nep141:0xdac17f958d2ee523a2206206994597c13d831ec7.factory.bridge.near": "Ethereum (Rainbow)",
		"nep245:v2_1.omni.hot.tg:56_2CMMyVTGZkeyNZTSvS5sarzfir6g":               "BNB Chain",
		"nep245:v2_1.omni.hot.tg:137_3hpYoaLtt8MP1Z2GH1U473DMRKgr":              "Polygon",
		"nep245:v2_1.omni.hot.tg:1117_":                                         "TON",
		"nep245:v2_1.omni.hot.tg:99999_abc":                                     "HOT Omni 99999",
		"nep245:other.near:1":                                                   "NEAR",
		"nep171:nft.near:1":                                                     "",
		"imt:something":                                                         "",
		"garbage":                                                               "",
	}
	for id, want := range cases {
		if got := ChainOf(id); got != want {
			t.Errorf("ChainOf(%q) = %q, want %q", id, got, want)
		}
	}
}
