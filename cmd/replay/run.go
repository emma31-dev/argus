package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/emma31-dev/argus/internal/alert"
	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/snapshot"
	"github.com/emma31-dev/argus/internal/tokens"
)

// CadenceSeconds is the snapshot interval assumed for time-to-detect.
const CadenceSeconds = 30

// Caller is the part of the RPC client the replay needs.
type Caller interface {
	BlockAt(ctx context.Context, height uint64) (rpc.Block, string, error)
	CallFunction(ctx context.Context, provider, account, method string, args any, blockHeight uint64) ([]byte, error)
}

// Query describes one view call exactly, so a reader can re-run it.
type Query struct {
	Account string `json:"account_id"`
	Method  string `json:"method_name"`
	Args    string `json:"args"`
	Block   uint64 `json:"block_id"`
}

func (q Query) String() string {
	return fmt.Sprintf("query call_function %s.%s(%s) at block_id %d", q.Account, q.Method, q.Args, q.Block)
}

// ReplayPoint is one reading in the /v1/replay shape.
type ReplayPoint struct {
	BlockHeight uint64       `json:"block_height"`
	BlockTime   time.Time    `json:"block_time"`
	Owed        string       `json:"owed"`
	Held        string       `json:"held"`
	Gap         string       `json:"gap"`
	Label       string       `json:"label"`
	Status      model.Status `json:"status"`
	OwedQuery   string       `json:"owed_query"`
	HeldQuery   string       `json:"held_query"`
}

// ReplayToken is one token's result.
type ReplayToken struct {
	ID                  string        `json:"id"`
	Symbol              string        `json:"symbol"`
	Chain               string        `json:"chain"`
	Decimals            int           `json:"decimals"`
	Points              []ReplayPoint `json:"points"`
	FirstNegativeBlock  uint64        `json:"first_negative_block"`
	DetectedRedBlock    uint64        `json:"detected_red_block"`
	MaxDeficit          string        `json:"max_deficit"`
	TimeToDetectSeconds *int64        `json:"time_to_detect_seconds"`
	DetectionNote       string        `json:"detection_note"`
}

// ReplayFile is the whole /v1/replay document.
type ReplayFile struct {
	Title          string        `json:"title"`
	GeneratedAt    time.Time     `json:"generated_at"`
	RPCProvider    string        `json:"rpc_provider"`
	Notes          string        `json:"notes"`
	DebounceK      int           `json:"debounce_k"`
	CadenceSeconds int           `json:"cadence_seconds"`
	Tokens         []ReplayToken `json:"tokens"`
}

const sparseNote = "Replay points are sparse, not one snapshot every 30 s. \"detected_red_block\" means the " +
	"debounce state machine (internal/alert.Step, K consecutive negative snapshots) turns red when fed exactly " +
	"these snapshots, i.e. it would have fired given snapshots at these heights. \"time_to_detect_seconds\" " +
	"assumes a 30 s snapshot cadence starting at the first negative sample: (K-1) x 30 s, and is only reported " +
	"when the sampled deficit demonstrably lasted that long."

// Run reads every point of the plan and evaluates the debounce.
func Run(ctx context.Context, c Caller, plan *Plan, k int, now time.Time, logf func(string, ...any)) (*ReplayFile, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	ids := make([]string, len(plan.Tokens))
	metas := make([]model.Token, len(plan.Tokens))
	for i, pt := range plan.Tokens {
		t, err := tokens.Parse(pt.ID)
		if err != nil {
			return nil, err
		}
		t.Symbol, t.Decimals, t.Chain = pt.Symbol, pt.Decimals, tokens.ChainOf(pt.ID)
		metas[i], ids[i] = t, pt.ID
	}
	out := &ReplayFile{Title: plan.Title, GeneratedAt: now.UTC(), DebounceK: k, CadenceSeconds: CadenceSeconds}
	res := make([]ReplayToken, len(metas))
	for i, t := range metas {
		res[i] = ReplayToken{ID: t.TokenID, Symbol: t.Symbol, Chain: t.Chain, Decimals: t.Decimals}
	}
	providers := map[string]bool{}

	for pi, pt := range plan.Points {
		logf("point %d/%d: block %d (%s)", pi+1, len(plan.Points), pt.BlockHeight, pt.Label)
		blk, provider, err := c.BlockAt(ctx, pt.BlockHeight)
		if err != nil {
			return nil, fmt.Errorf("block %d: %w", pt.BlockHeight, err)
		}
		providers[provider] = true
		supplyArgs := map[string]any{"token_ids": ids}
		raw, err := c.CallFunction(ctx, provider, plan.IntentsAccount, "mt_batch_supply", supplyArgs, pt.BlockHeight)
		if err != nil {
			return nil, fmt.Errorf("mt_batch_supply at %d: %w", pt.BlockHeight, err)
		}
		var supplies []json.RawMessage
		if err := json.Unmarshal(raw, &supplies); err != nil || len(supplies) != len(ids) {
			return nil, fmt.Errorf("mt_batch_supply at %d: unexpected response %q", pt.BlockHeight, truncate(string(raw)))
		}
		sq := queryString(plan.IntentsAccount, "mt_batch_supply", supplyArgs, pt.BlockHeight)
		for i, t := range metas {
			owed, err := amount(supplies[i])
			if err != nil {
				return nil, fmt.Errorf("owed of %s at %d: %w", t.TokenID, pt.BlockHeight, err)
			}
			contract, method, args := tokens.HeldMethodFor(plan.IntentsAccount, t)
			hraw, err := c.CallFunction(ctx, provider, contract, method, args, pt.BlockHeight)
			if err != nil {
				return nil, fmt.Errorf("%s.%s at %d: %w", contract, method, pt.BlockHeight, err)
			}
			held, err := amount(hraw)
			if err != nil {
				return nil, fmt.Errorf("held of %s at %d: %w", t.TokenID, pt.BlockHeight, err)
			}
			gap, err := snapshot.Gap(held, owed)
			if err != nil {
				return nil, err
			}
			res[i].Points = append(res[i].Points, ReplayPoint{
				BlockHeight: pt.BlockHeight, BlockTime: blk.Time.UTC(), Owed: owed, Held: held, Gap: gap, Label: pt.Label,
				OwedQuery: sq, HeldQuery: queryString(contract, method, args, pt.BlockHeight),
			})
		}
	}
	for i := range res {
		evaluate(&res[i], k)
	}
	names := make([]string, 0, len(providers))
	for p := range providers {
		names = append(names, p)
	}
	sort.Strings(names)
	out.RPCProvider = strings.Join(names, ", ")
	out.Notes = strings.TrimSpace(plan.Notes + "\n\n" + sparseNote)
	out.Tokens = res
	return out, nil
}

func queryString(account, method string, args any, block uint64) string {
	b, _ := json.Marshal(args)
	return Query{Account: account, Method: method, Args: string(b), Block: block}.String()
}

func truncate(s string) string {
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

// amount parses a U128 returned as a JSON string of digits.
func amount(raw []byte) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("not a JSON string: %s", truncate(string(raw)))
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() < 0 || strings.ContainsAny(s, "+- ") {
		return "", fmt.Errorf("not a non-negative integer: %q", truncate(s))
	}
	return n.String(), nil
}

// evaluate fills the status of every point and the detection fields by
// feeding the points through alert.Step in block order.
func evaluate(t *ReplayToken, k int) {
	st := model.TokenState{TokenID: t.ID, Status: model.StatusUnknown}
	maxDef := new(big.Int)
	for i := range t.Points {
		p := &t.Points[i]
		next, trs := alert.Step(st, model.Snapshot{TokenID: t.ID, BlockHeight: p.BlockHeight, BlockTime: p.BlockTime,
			Owed: p.Owed, Held: p.Held, Gap: p.Gap}, alert.Config{K: k})
		st = next
		p.Status = st.Status
		for _, tr := range trs {
			if tr.Kind == alert.TurnedRed && t.DetectedRedBlock == 0 {
				t.DetectedRedBlock = tr.Block
			}
		}
		if g, ok := new(big.Int).SetString(p.Gap, 10); ok && g.Sign() < 0 {
			if t.FirstNegativeBlock == 0 {
				t.FirstNegativeBlock = p.BlockHeight
			}
			if d := new(big.Int).Neg(g); d.Cmp(maxDef) > 0 {
				maxDef = d
			}
		}
	}
	t.MaxDeficit = maxDef.String()
	t.TimeToDetectSeconds, t.DetectionNote = timeToDetect(t, k)
}

// timeToDetect applies the 30 s cadence assumption to the first contiguous
// run of negative samples.
func timeToDetect(t *ReplayToken, k int) (*int64, string) {
	if t.FirstNegativeBlock == 0 {
		return nil, "no negative gap at any sampled height"
	}
	var first, lastNeg time.Time
	for _, p := range t.Points {
		g, _ := new(big.Int).SetString(p.Gap, 10)
		neg := g != nil && g.Sign() < 0
		switch {
		case neg && first.IsZero():
			first, lastNeg = p.BlockTime, p.BlockTime
		case neg:
			lastNeg = p.BlockTime
		case !first.IsZero():
			goto done // the first run ended
		}
	}
done:
	need := time.Duration((k-1)*CadenceSeconds) * time.Second
	if lastNeg.Sub(first) >= need {
		secs := int64(need / time.Second)
		return &secs, fmt.Sprintf("negative from the first sample until at least %s later; with a %d s cadence the %dth consecutive negative snapshot lands %d s after the first",
			lastNeg.Sub(first).Round(time.Second), CadenceSeconds, k, secs)
	}
	return nil, fmt.Sprintf("the sampled negative run lasts only %s, shorter than the %s needed for %d snapshots at a %d s cadence; sparse points cannot show the debounce firing",
		lastNeg.Sub(first).Round(time.Second), need, k, CadenceSeconds)
}

var errNoProvider = errors.New("replay: no archival provider configured")
