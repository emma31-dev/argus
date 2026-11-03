// Command 04-events is capability check 4 (PRD section 7): does FastNEAR's
// neardata service give us recent blocks without a key, including receipt
// execution outcomes with logs, and do intents.near EVENT_JSON logs parse?
//
// Run: go run ./checks/04-events [-blocks 100] [-base https://mainnet.neardata.xyz]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/emma31-dev/argus/checks/internal/checkutil"
)

const eventPrefix = "EVENT_JSON:"

// block is the part of the neardata block response this check reads.
type block struct {
	Block struct {
		Header struct {
			Height uint64 `json:"height"`
		} `json:"header"`
	} `json:"block"`
	Shards []struct {
		ReceiptExecutionOutcomes []struct {
			TxHash           string `json:"tx_hash"`
			ExecutionOutcome struct {
				ID      string `json:"id"`
				Outcome struct {
					ExecutorID string   `json:"executor_id"`
					Logs       []string `json:"logs"`
				} `json:"outcome"`
			} `json:"execution_outcome"`
		} `json:"receipt_execution_outcomes"`
	} `json:"shards"`
}

type event struct {
	Standard string          `json:"standard"`
	Version  string          `json:"version"`
	Event    string          `json:"event"`
	Data     json.RawMessage `json:"data"`
}

func main() {
	n := flag.Int("blocks", 100, "number of recent blocks to fetch")
	base := flag.String("base", "https://mainnet.neardata.xyz", "neardata base URL")
	delay := flag.Duration("delay", 150*time.Millisecond, "pause between requests")
	flag.Parse()

	env, err := checkutil.New("04-events", nil)
	if err != nil {
		checkutil.Fatalf("04-events", "setup: %v", err)
	}
	ctx, cancel := checkutil.Context(15 * time.Minute)
	defer cancel()
	hc := &http.Client{Timeout: 30 * time.Second}

	// /v0/last_block/final redirects to /v0/block/<height>.
	body, status, err := get(ctx, hc, *base+"/v0/last_block/final")
	if err != nil {
		env.Fail("GET last_block/final: %v", err)
		env.NetworkHint(err)
		env.Exit()
		return
	}
	if status != http.StatusOK {
		env.Fail("GET last_block/final: HTTP %d (a key or payment may be required): %.200s", status, body)
		env.Exit()
		return
	}
	var latest block
	if err := json.Unmarshal(body, &latest); err != nil || latest.Block.Header.Height == 0 {
		env.Fail("cannot parse latest block: %v", err)
		env.Exit()
		return
	}
	env.Pass("no key needed: HTTP 200 for the latest final block %d", latest.Block.Header.Height)
	env.Save("sample-block-latest.json", json.RawMessage(body))

	var (
		fetched, nullBlocks, outcomes, withLogs, intentsOutcomes, eventLogs, intentsEvents int
		eventNames                                                                         = map[string]int{}
		saved                                                                              bool
	)
	var samples []map[string]any
	for i := 0; i < *n; i++ {
		h := latest.Block.Header.Height - uint64(i)
		raw, status, err := get(ctx, hc, fmt.Sprintf("%s/v0/block/%d", *base, h))
		if err != nil {
			env.Fail("GET block %d: %v", h, err)
			env.NetworkHint(err)
			break
		}
		if status != http.StatusOK {
			env.Fail("GET block %d: HTTP %d: %.200s", h, status, raw)
			break
		}
		if strings.TrimSpace(string(raw)) == "null" {
			nullBlocks++ // skipped height
			continue
		}
		var b block
		if err := json.Unmarshal(raw, &b); err != nil {
			env.Fail("block %d: undecodable: %v", h, err)
			break
		}
		fetched++
		hasIntentsEvent := false
		for _, sh := range b.Shards {
			for _, o := range sh.ReceiptExecutionOutcomes {
				outcomes++
				if len(o.ExecutionOutcome.Outcome.Logs) > 0 {
					withLogs++
				}
				if o.ExecutionOutcome.Outcome.ExecutorID != "intents.near" {
					continue
				}
				intentsOutcomes++
				for _, l := range o.ExecutionOutcome.Outcome.Logs {
					if !strings.HasPrefix(l, eventPrefix) {
						continue
					}
					eventLogs++
					var ev event
					if err := json.Unmarshal([]byte(strings.TrimPrefix(l, eventPrefix)), &ev); err != nil {
						env.Fail("block %d receipt %s: EVENT_JSON does not parse: %v", h, o.ExecutionOutcome.ID, err)
						continue
					}
					intentsEvents++
					eventNames[ev.Standard+"/"+ev.Event]++
					hasIntentsEvent = true
					if len(samples) < 20 {
						samples = append(samples, map[string]any{"block": h, "receipt_id": o.ExecutionOutcome.ID, "tx_hash": o.TxHash, "event": ev})
					}
				}
			}
		}
		if hasIntentsEvent && !saved {
			env.Save(fmt.Sprintf("sample-block-%d-with-intents-events.json", h), json.RawMessage(raw))
			saved = true
		}
		time.Sleep(*delay)
	}

	env.Info("fetched %d blocks (%d skipped heights), %d receipt outcomes, %d with logs", fetched, nullBlocks, outcomes, withLogs)
	env.Info("intents.near: %d outcomes, %d EVENT_JSON logs, %d parsed; kinds: %v", intentsOutcomes, eventLogs, intentsEvents, eventNames)
	env.Save("summary.json", map[string]any{
		"base": *base, "blocks_requested": *n, "blocks_fetched": fetched, "skipped_heights": nullBlocks,
		"receipt_outcomes": outcomes, "outcomes_with_logs": withLogs, "intents_outcomes": intentsOutcomes,
		"intents_event_logs": eventLogs, "intents_events_parsed": intentsEvents, "event_kinds": eventNames,
		"sample_events": samples,
	})

	switch {
	case fetched == 0:
		env.Fail("no blocks fetched")
	case outcomes == 0:
		env.Fail("blocks contain no receipt_execution_outcomes")
	case withLogs == 0:
		env.Fail("receipt outcomes carry no logs in %d blocks (unexpected)", fetched)
	default:
		env.Pass("receipt execution outcomes with logs are included (%d of %d outcomes have logs)", withLogs, outcomes)
	}
	if intentsEvents > 0 {
		env.Pass("parsed %d intents.near EVENT_JSON events", intentsEvents)
	} else {
		env.Info("no intents.near events in this window (quiet period?): re-run with a larger -blocks to see some")
	}
	env.Exit()
}

func get(ctx context.Context, hc *http.Client, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return b, resp.StatusCode, err
}
