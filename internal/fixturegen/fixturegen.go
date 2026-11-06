// Package fixturegen writes the SYNTHETIC fixture set that cmd/verify replays.
//
// Nothing here is mainnet data. The generator invents token balances for a
// small scenario, writes them as recorded JSON-RPC exchanges shaped like
// NEAR responses (the same files rpc.WithRecorder produces), and writes an
// expected.json computed by a small independent oracle (plain big.Int
// counting, not the alert package) so that cmd/verify is a real cross-check
// of snapshot + alert code.
//
// The output is deterministic: running it twice gives identical bytes.
package fixturegen

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Scenario constants. Heights are deliberately small and round so nobody
// mistakes them for mainnet heights.
const (
	Rounds     = 12
	K          = 4
	BaseHeight = 1_000_000
	BlockStep  = 30 // blocks between rounds (about 30 s on NEAR)
	Provider   = "synthetic"
	Intents    = "intents.near"
)

var baseTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type token struct {
	ID       string
	Symbol   string
	Decimals int
	Standard string // nep141 | nep245
	Contract string
	Inner    string
	// series returns owed and held for round r (1-based).
	series func(r int) (owed, held *big.Int)
	Note   string
}

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }
func units(whole int64, dec int) *big.Int {
	return new(big.Int).Mul(big.NewInt(whole), pow10(dec))
}
func add(xs ...*big.Int) *big.Int {
	s := new(big.Int)
	for _, x := range xs {
		s.Add(s, x)
	}
	return s
}

// scale returns r * 10^exp.
func scale(r, exp int) *big.Int  { return new(big.Int).Mul(big.NewInt(int64(r)), pow10(exp)) }
func sub(a, b *big.Int) *big.Int { return new(big.Int).Sub(a, b) }
func mustInt(s string) *big.Int {
	v, _ := new(big.Int).SetString(s, 10)
	return v
}

func tokens() []token {
	// wNEAR: 24 decimals, about 4.2e30 smallest units, with a non-trivial
	// fractional part. Always a small surplus.
	wnearOwed := func(r int) *big.Int {
		return add(units(4_200_000, 24), mustInt("123456789012345678901234"), units(int64(r)*3/2, 24))
	}
	return []token{
		{ID: "nep141:wnear.synthetic.near", Symbol: "wNEAR", Decimals: 24, Standard: "nep141", Contract: "wnear.synthetic.near",
			Note: "healthy, 24-decimal values around 4.2e30",
			series: func(r int) (*big.Int, *big.Int) {
				o := wnearOwed(r)
				return o, add(o, units(2, 24), big.NewInt(7))
			}},
		{ID: "nep141:usdc.synthetic.near", Symbol: "USDC", Decimals: 6, Standard: "nep141", Contract: "usdc.synthetic.near",
			Note: "healthy, small growing surplus",
			series: func(r int) (*big.Int, *big.Int) {
				o := add(units(12_000_000, 6), big.NewInt(int64(r)*1_000_000))
				return o, add(o, big.NewInt(int64(r)*5_000))
			}},
		{ID: "nep141:usdt.synthetic.near", Symbol: "USDT", Decimals: 6, Standard: "nep141", Contract: "usdt.synthetic.near",
			Note: "healthy, held exactly equals owed",
			series: func(r int) (*big.Int, *big.Int) {
				o := add(units(9_500_000, 6), big.NewInt(int64(r)*250_000))
				return o, new(big.Int).Set(o)
			}},
		{ID: "nep141:eth.synthetic.near", Symbol: "ETH", Decimals: 18, Standard: "nep141", Contract: "eth.synthetic.near",
			Note: "SUSTAINED DEFICIT: gap < 0 from round 5 to the end (K+1 = 5 or more rounds); deficit grows at round 9",
			series: func(r int) (*big.Int, *big.Int) {
				o := add(units(8_500, 18), scale(r, 17))
				switch {
				case r < 5:
					return o, add(o, big.NewInt(5e17))
				case r < 9:
					return o, sub(o, units(2, 18))
				}
				return o, sub(o, units(5, 18))
			}},
		{ID: "nep141:wbtc.synthetic.near", Symbol: "WBTC", Decimals: 8, Standard: "nep141", Contract: "wbtc.synthetic.near",
			Note: "deficit for rounds 2-6 (turns red), then fully recovers",
			series: func(r int) (*big.Int, *big.Int) {
				o := add(units(1_200, 8), big.NewInt(int64(r)*10_000_000))
				if r >= 2 && r <= 6 {
					return o, sub(o, big.NewInt(25_000_000))
				}
				return o, add(o, big.NewInt(100))
			}},
		{ID: "nep141:dai.synthetic.near", Symbol: "DAI", Decimals: 18, Standard: "nep141", Contract: "dai.synthetic.near",
			Note: "FALSE ALARM: a single in-flight negative snapshot at round 6 that recovers at round 7",
			series: func(r int) (*big.Int, *big.Int) {
				o := add(units(3_000_000, 18), scale(r, 18))
				if r == 6 {
					return o, sub(o, units(3, 18))
				}
				return o, add(o, big.NewInt(1e15))
			}},
		{ID: "nep245:hot.synthetic.near:56_SYNTHETIC", Symbol: "hUSDT", Decimals: 18, Standard: "nep245", Contract: "hot.synthetic.near", Inner: "56_SYNTHETIC",
			Note: "the one NEP-245 token (multi-token standard), healthy",
			series: func(r int) (*big.Int, *big.Int) {
				o := add(units(750_000, 18), scale(r, 18))
				return o, add(o, units(10, 18))
			}},
	}
}

func height(r int) uint64 { return uint64(BaseHeight + BlockStep*r) }

// Fixture is the descriptor written to fixture.json.
type Fixture struct {
	Kind           string `json:"kind"`
	Provider       string `json:"provider"`
	Rounds         int    `json:"rounds"`
	K              int    `json:"k"`
	IntentsAccount string `json:"intents_account"`
	Description    string `json:"description"`
}

// Expected is the shape of expected.json.
type Expected struct {
	Note        string                `json:"note"`
	Rounds      int                   `json:"rounds"`
	Final       map[string]FinalState `json:"final"`
	Transitions []Transition          `json:"transitions"`
	Alerts      []AlertRec            `json:"alerts"`
}

// FinalState is a token's status and gap after the last round.
type FinalState struct {
	Status string `json:"status"`
	Gap    string `json:"gap"`
}

// Transition is a status change worth reporting.
type Transition struct {
	Token string `json:"token"`
	Kind  string `json:"kind"` // amber | red | recovered | false_alarm_cleared
	Block uint64 `json:"block"`
}

// AlertRec is an alert the engine must have queued.
type AlertRec struct {
	Kind  string `json:"kind"` // turned_red | incident_update | recovered
	Token string `json:"token"`
	Block uint64 `json:"block"`
}

// Oracle computes the expected outcome with plain counting.
func oracle(toks []token) Expected {
	exp := Expected{
		Note:   "Computed by internal/fixturegen's independent oracle from the scenario definition, not by running the alert code.",
		Rounds: Rounds, Final: map[string]FinalState{},
	}
	for _, t := range toks {
		status, streak := "green", 0
		maxDef := new(big.Int)
		for r := 1; r <= Rounds; r++ {
			owed, held := t.series(r)
			gap := sub(held, owed)
			blk := height(r)
			prev := status
			if gap.Sign() < 0 {
				streak++
				def := new(big.Int).Neg(gap)
				if streak >= K {
					status = "red"
				} else {
					status = "amber"
				}
				if streak == 1 || def.Cmp(maxDef) > 0 {
					grew := status == "red" && prev == "red" && streak > 1 && def.Cmp(maxDef) > 0
					maxDef = def
					if grew {
						exp.Alerts = append(exp.Alerts, AlertRec{"incident_update", t.ID, blk})
					}
				}
			} else {
				streak, status = 0, "green"
				maxDef = new(big.Int)
			}
			if status != prev {
				switch {
				case status == "amber":
					exp.Transitions = append(exp.Transitions, Transition{t.ID, "amber", blk})
				case status == "red":
					exp.Transitions = append(exp.Transitions, Transition{t.ID, "red", blk})
					exp.Alerts = append(exp.Alerts, AlertRec{"turned_red", t.ID, blk})
				case prev == "red":
					exp.Transitions = append(exp.Transitions, Transition{t.ID, "recovered", blk})
					exp.Alerts = append(exp.Alerts, AlertRec{"recovered", t.ID, blk})
				default:
					exp.Transitions = append(exp.Transitions, Transition{t.ID, "false_alarm_cleared", blk})
				}
			}
			if r == Rounds {
				exp.Final[t.ID] = FinalState{Status: status, Gap: gap.String()}
			}
		}
	}
	SortExpected(&exp)
	return exp
}

// SortExpected puts transitions and alerts in a canonical order.
func SortExpected(e *Expected) {
	sort.Slice(e.Transitions, func(i, j int) bool {
		a, b := e.Transitions[i], e.Transitions[j]
		if a.Block != b.Block {
			return a.Block < b.Block
		}
		if a.Token != b.Token {
			return a.Token < b.Token
		}
		return a.Kind < b.Kind
	})
	sort.Slice(e.Alerts, func(i, j int) bool {
		a, b := e.Alerts[i], e.Alerts[j]
		if a.Block != b.Block {
			return a.Block < b.Block
		}
		if a.Token != b.Token {
			return a.Token < b.Token
		}
		return a.Kind < b.Kind
	})
}

func jsonBytes(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}

func quoteBytes(s string) []int {
	out := make([]int, len(s))
	for i := 0; i < len(s); i++ {
		out[i] = int(s[i])
	}
	return out
}

// Generate writes the synthetic fixture set into dir.
func Generate(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	toks := tokens()
	seq := 0
	write := func(method string, params, result any) error {
		seq++
		id := fmt.Sprint(seq)
		ex := map[string]any{
			"provider": Provider,
			"request":  map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params},
			"response": map[string]any{"jsonrpc": "2.0", "id": id, "result": result},
		}
		return os.WriteFile(filepath.Join(dir, fmt.Sprintf("rpc-%04d-%s.json", seq, method)), jsonBytes(ex), 0o644)
	}
	call := func(h uint64, account, method string, args any, value any) error {
		argBytes, _ := json.Marshal(args)
		valBytes, _ := json.Marshal(value)
		params := map[string]any{
			"request_type": "call_function", "block_id": h, "account_id": account,
			"method_name": method, "args_base64": base64.StdEncoding.EncodeToString(argBytes),
		}
		return write("query", params, map[string]any{
			"result": quoteBytes(string(valBytes)), "logs": []string{},
			"block_height": h, "block_hash": fmt.Sprintf("SYNTHETIC-HASH-%07d", h),
		})
	}
	ids := make([]string, len(toks))
	for i, t := range toks {
		ids[i] = t.ID
	}
	for r := 1; r <= Rounds; r++ {
		h := height(r)
		ns := baseTime.Add(time.Duration(r) * 30 * time.Second).UnixNano()
		if err := write("block", map[string]any{"finality": "final"}, map[string]any{
			"author": "synthetic.near",
			"header": map[string]any{
				"height": h, "hash": fmt.Sprintf("SYNTHETIC-HASH-%07d", h),
				"timestamp": ns, "timestamp_nanosec": fmt.Sprint(ns),
			},
			"chunks": []any{},
		}); err != nil {
			return err
		}
		owedVals := make([]string, len(toks))
		for i, t := range toks {
			o, _ := t.series(r)
			owedVals[i] = o.String()
		}
		if err := call(h, Intents, "mt_batch_supply", map[string]any{"token_ids": ids}, owedVals); err != nil {
			return err
		}
		for _, t := range toks {
			_, held := t.series(r)
			var args any
			var method string
			if t.Standard == "nep245" {
				method, args = "mt_balance_of", map[string]string{"account_id": Intents, "token_id": t.Inner}
			} else {
				method, args = "ft_balance_of", map[string]string{"account_id": Intents}
			}
			if err := call(h, t.Contract, method, args, held.String()); err != nil {
				return err
			}
		}
	}

	var y strings.Builder
	y.WriteString("# SYNTHETIC token list for the synthetic fixture set. Not mainnet data.\ntokens:\n")
	for _, t := range toks {
		fmt.Fprintf(&y, "  - id: %q\n    symbol: %s\n    decimals: %d   # %s\n", t.ID, t.Symbol, t.Decimals, t.Note)
	}
	files := map[string][]byte{
		"tokens.yaml": []byte(y.String()),
		"fixture.json": jsonBytes(Fixture{
			Kind: "synthetic", Provider: Provider, Rounds: Rounds, K: K, IntentsAccount: Intents,
			Description: "Invented balances shaped like NEAR JSON-RPC responses. NOT mainnet data.",
		}),
		"expected.json": jsonBytes(oracle(toks)),
	}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}
