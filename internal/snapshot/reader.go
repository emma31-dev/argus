package snapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/rpc"
	"github.com/emma31-dev/argus/internal/tokens"
)

// ErrOwedUnsupported means intents.near has no mt_batch_supply view method,
// so owed must be derived from mint/burn events instead.
var ErrOwedUnsupported = errors.New("snapshot: mt_batch_supply not supported by the contract")

// Defaults for Reader.
const (
	DefaultBatchSize   = 50
	DefaultConcurrency = 8

	// minRateLimitBudget is the fewest rate-limited reads a round tolerates
	// before giving up on its provider (it is 20% of the reads for big rounds).
	minRateLimitBudget = 5
)

// Caller is the part of the RPC client the Reader needs. *rpc.Client
// implements it.
type Caller interface {
	LatestFinalBlock(ctx context.Context) (rpc.Block, string, error)
	// LatestFinalBlockOn is LatestFinalBlock pinned to one provider; used to
	// retry a whole round on the next provider.
	LatestFinalBlockOn(ctx context.Context, provider string) (rpc.Block, error)
	Providers() []string
	CallFunction(ctx context.Context, provider, account, method string, args any, blockHeight uint64) ([]byte, error)
}

// TokenError is a failure to read one token. The rest of the round is
// unaffected.
type TokenError struct {
	TokenID string
	Err     error
}

func (e TokenError) Error() string { return e.TokenID + ": " + e.Err.Error() }
func (e TokenError) Unwrap() error { return e.Err }

// Result is the outcome of one round.
type Result struct {
	Block     rpc.Block
	Provider  string
	Snapshots []model.Snapshot
	Errors    []TokenError
}

// Reader performs same-block snapshot rounds. The zero value is not usable:
// set Caller. Other fields default as documented.
type Reader struct {
	Caller         Caller
	IntentsAccount string           // default "intents.near"
	BatchSize      int              // mt_batch_supply chunk size, default 50
	Concurrency    int              // parallel held reads, default 8
	Now            func() time.Time // default time.Now
}

// Round reads owed and held for every token at one final block and one
// provider. If that provider fails part-way, the entire round is repeated
// on the next provider, so the two sides of a token are never mixed across
// providers. Per-token problems land in Result.Errors; the returned error is
// non-nil only if the round as a whole could not be completed
// (ErrOwedUnsupported, all providers failing, context cancellation).
func (r *Reader) Round(ctx context.Context, toks []model.Token) (Result, error) {
	var errs []error
	res, provider, err := r.attempt(ctx, toks, "")
	tried := map[string]bool{}
	for {
		if err == nil {
			return res, nil
		}
		if provider == "" {
			// The latest-block lookup already fell back through every provider.
			return Result{}, err
		}
		tried[provider] = true
		if !rpc.IsProviderFailure(err) || ctx.Err() != nil {
			return Result{}, err
		}
		errs = append(errs, err)
		next := ""
		for _, name := range r.Caller.Providers() {
			if !tried[name] {
				next = name
				break
			}
		}
		if next == "" {
			return Result{}, fmt.Errorf("snapshot: round failed on every provider: %w", errors.Join(errs...))
		}
		res, provider, err = r.attempt(ctx, toks, next)
	}
}

// attempt runs one round. only == "" means "whatever provider serves the
// latest block"; otherwise the round is pinned to that provider. It returns
// the provider used so the caller can exclude it on retry.
func (r *Reader) attempt(ctx context.Context, toks []model.Token, only string) (Result, string, error) {
	var (
		blk      rpc.Block
		provider = only
		err      error
	)
	if only == "" {
		blk, provider, err = r.Caller.LatestFinalBlock(ctx)
	} else {
		blk, err = r.Caller.LatestFinalBlockOn(ctx, only)
	}
	if err != nil {
		return Result{}, provider, err
	}
	owed, tokErrs, err := r.readOwed(ctx, provider, blk.Height, toks)
	if err != nil {
		return Result{}, provider, err
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	res := Result{Block: blk, Provider: provider}
	res.Errors = append(res.Errors, tokErrs...)

	var jobs []heldJob
	for i, t := range toks {
		if _, ok := owed[t.TokenID]; ok {
			jobs = append(jobs, heldJob{i, t})
		}
	}
	units := r.heldUnits(jobs)
	workers := r.Concurrency
	if workers <= 0 {
		workers = DefaultConcurrency
	}
	if workers > len(units) {
		workers = len(units)
	}
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	unitCh := make(chan []heldJob)
	outCh := make(chan heldOut, len(jobs))
	var wg sync.WaitGroup
	account := r.account()
	// A 429 that outlasted the client's retries only costs that token its
	// read: no wrong data was returned and nothing was mixed across
	// providers. Past rateLimitBudget the provider is treated as failed.
	var rateLimited atomic.Int32
	rateLimitBudget := int32(max(minRateLimitBudget, len(units)/5))
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range unitCh {
				outs := r.readHeldUnit(cctx, account, provider, blk.Height, u)
				var failure error
				for _, o := range outs {
					if o.err != nil && rpc.IsProviderFailure(o.err) {
						failure = o.err
						break
					}
				}
				fatal := failure != nil
				if fatal && rpc.IsRateLimited(failure) && rateLimited.Add(1) <= rateLimitBudget {
					fatal = false
				}
				if fatal {
					cancel() // abandon the round; it will be retried elsewhere
				}
				for _, o := range outs {
					o.fatal = fatal && o.err != nil && rpc.IsProviderFailure(o.err)
					outCh <- o
				}
			}
		}()
	}
	go func() {
		defer close(unitCh)
		for _, u := range units {
			select {
			case unitCh <- u:
			case <-cctx.Done():
				return
			}
		}
	}()
	wg.Wait()
	close(outCh)

	heldByIdx := map[int]heldOut{}
	var providerErr error
	for o := range outCh {
		if o.fatal && providerErr == nil {
			providerErr = o.err
		}
		heldByIdx[o.idx] = o
	}
	if providerErr != nil {
		return Result{}, provider, providerErr
	}
	if err := ctx.Err(); err != nil {
		return Result{}, provider, err
	}
	taken := now().UTC()
	for _, j := range jobs {
		o := heldByIdx[j.idx]
		if o.err != nil {
			res.Errors = append(res.Errors, TokenError{j.tok.TokenID, fmt.Errorf("held: %w", o.err)})
			continue
		}
		o2 := owed[j.tok.TokenID]
		gap, err := Gap(o.held, o2)
		if err != nil {
			res.Errors = append(res.Errors, TokenError{j.tok.TokenID, err})
			continue
		}
		res.Snapshots = append(res.Snapshots, model.Snapshot{
			TokenID: j.tok.TokenID, BlockHeight: blk.Height, BlockHash: blk.Hash, BlockTime: blk.Time,
			Owed: o2, Held: o.held, Gap: gap, Provider: provider, TakenAt: taken,
		})
	}
	return res, provider, nil
}

// heldJob is one token to read, with its position in the round's token list.
type heldJob struct {
	idx int
	tok model.Token
}

// heldOut is the held balance (or failure) of one token. fatal marks a
// provider failure that abandons the round.
type heldOut struct {
	idx   int
	held  string
	err   error
	fatal bool
}

// heldUnits groups jobs into the RPC work items of a round. NEP-141 tokens
// each need their own call (every token has its own contract). NEP-245 tokens
// of the same contract share one mt_batch_balance_of call per BatchSize
// tokens, which turns hundreds of calls into a handful on mainnet.
func (r *Reader) heldUnits(jobs []heldJob) [][]heldJob {
	size := r.BatchSize
	if size <= 0 {
		size = DefaultBatchSize
	}
	var units [][]heldJob
	byContract := map[string][]heldJob{}
	var order []string
	for _, j := range jobs {
		if j.tok.Standard != model.StandardNEP245 {
			units = append(units, []heldJob{j})
			continue
		}
		if _, seen := byContract[j.tok.Contract]; !seen {
			order = append(order, j.tok.Contract)
		}
		byContract[j.tok.Contract] = append(byContract[j.tok.Contract], j)
	}
	for _, c := range order {
		js := byContract[c]
		for start := 0; start < len(js); start += size {
			units = append(units, js[start:min(start+size, len(js))])
		}
	}
	return units
}

// readHeldUnit reads every token of one unit. A batch that fails for a
// reason other than the provider (the contract lacks the method, or panics on
// one id) or that answers with the wrong shape is retried token by token, so
// one bad token cannot hide the rest of its batch. A provider failure is
// reported for every token and abandons the round.
func (r *Reader) readHeldUnit(ctx context.Context, account, provider string, height uint64, unit []heldJob) []heldOut {
	single := func() []heldOut {
		outs := make([]heldOut, 0, len(unit))
		for _, j := range unit {
			held, err := r.readHeld(ctx, account, provider, height, j.tok)
			outs = append(outs, heldOut{idx: j.idx, held: held, err: err})
		}
		return outs
	}
	if len(unit) == 1 {
		return single()
	}
	inner := make([]string, len(unit))
	for i, j := range unit {
		inner[i] = j.tok.InnerID
	}
	method, args := tokens.HeldBatchMethodFor(account, inner)
	raw, err := r.Caller.CallFunction(ctx, provider, unit[0].tok.Contract, method, args, height)
	if err != nil {
		if rpc.IsProviderFailure(err) || ctx.Err() != nil {
			outs := make([]heldOut, len(unit))
			for i, j := range unit {
				outs[i] = heldOut{idx: j.idx, err: err}
			}
			return outs
		}
		return single()
	}
	var vals []json.RawMessage
	if json.Unmarshal(raw, &vals) != nil || len(vals) != len(unit) {
		return single()
	}
	outs := make([]heldOut, len(unit))
	for i, j := range unit {
		held, err := parseAmount(vals[i])
		outs[i] = heldOut{idx: j.idx, held: held, err: err}
	}
	return outs
}

func (r *Reader) account() string {
	if r.IntentsAccount == "" {
		return tokens.DefaultIntentsAccount
	}
	return r.IntentsAccount
}

// readOwed batches mt_batch_supply over the in-scope tokens. It returns
// token id -> owed for tokens that read cleanly, and per-token errors for
// the rest. A null supply is an error for that token, never zero.
func (r *Reader) readOwed(ctx context.Context, provider string, height uint64, toks []model.Token) (map[string]string, []TokenError, error) {
	owed := map[string]string{}
	var tokErrs []TokenError
	var ids []string
	for _, t := range toks {
		if !tokens.InScope(t) {
			tokErrs = append(tokErrs, TokenError{t.TokenID, errors.New("token standard out of scope")})
			continue
		}
		ids = append(ids, t.TokenID)
	}
	size := r.BatchSize
	if size <= 0 {
		size = DefaultBatchSize
	}
	for start := 0; start < len(ids); start += size {
		end := min(start+size, len(ids))
		chunk := ids[start:end]
		raw, err := r.Caller.CallFunction(ctx, provider, r.account(), "mt_batch_supply",
			map[string]any{"token_ids": chunk}, height)
		if err != nil {
			if errors.Is(err, rpc.ErrMethodNotFound) {
				return nil, nil, ErrOwedUnsupported
			}
			if rpc.IsProviderFailure(err) || ctx.Err() != nil {
				return nil, nil, err
			}
			for _, id := range chunk {
				tokErrs = append(tokErrs, TokenError{id, fmt.Errorf("mt_batch_supply: %w", err)})
			}
			continue
		}
		var vals []json.RawMessage
		if err := json.Unmarshal(raw, &vals); err != nil || len(vals) != len(chunk) {
			e := fmt.Errorf("mt_batch_supply: unexpected response for %d ids (%d values, decode err %v)", len(chunk), len(vals), err)
			for _, id := range chunk {
				tokErrs = append(tokErrs, TokenError{id, e})
			}
			continue
		}
		for i, id := range chunk {
			s, err := parseAmount(vals[i])
			if err != nil {
				tokErrs = append(tokErrs, TokenError{id, fmt.Errorf("owed: %w", err)})
				continue
			}
			owed[id] = s
		}
	}
	return owed, tokErrs, nil
}

// readHeld calls the token's balance method for the intents account.
func (r *Reader) readHeld(ctx context.Context, account, provider string, height uint64, t model.Token) (string, error) {
	contract, method, args := tokens.HeldMethodFor(account, t)
	if contract == "" {
		return "", errors.New("token standard out of scope")
	}
	raw, err := r.Caller.CallFunction(ctx, provider, contract, method, args, height)
	if err != nil {
		return "", err
	}
	return parseAmount(raw)
}

// parseAmount reads a U128 as NEAR returns it: a JSON string of digits.
// A bare JSON number is tolerated; null and anything else is an error.
func parseAmount(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if string(trimmed) == "null" {
		return "", errors.New("null amount")
	}
	var s string
	if len(trimmed) > 0 && trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", fmt.Errorf("%w: %s", ErrInvalidAmount, truncate(string(raw)))
		}
	} else {
		s = string(trimmed)
	}
	n, err := parseUint(s)
	if err != nil {
		return "", err
	}
	return n.String(), nil
}

func truncate(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

var _ Caller = (*rpc.Client)(nil)
