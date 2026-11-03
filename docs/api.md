# Argus HTTP API (v1)

Read-only, no auth, JSON unless noted. All amounts are **base-10 integer strings in the token's smallest unit**; clients format them using `decimals`. `gap = held - owed` and may be negative. Timestamps are RFC 3339 UTC. Rate-limited per IP (default 60 req/min); a limited request gets `429` with `Retry-After`.

Errors: `{"error": "<message>"}` with a 4xx/5xx status.

CORS: `Access-Control-Allow-Origin: *` (data is public).

## `GET /v1/status`

Overall monitor health plus a summary.

```json
{
  "status": "green",                 // worst token status: red > amber > unknown > green
  "monitor_stale": false,            // true when no round completed recently (see below)
  "last_block": 171234567,           // 0 before the first round
  "last_block_time": "2026-11-03T12:00:00Z",   // null before the first round
  "last_checked_at": "2026-11-03T12:00:04Z",   // null before the first round
  "interval_seconds": 30,
  "debounce_snapshots": 4,
  "tokens_total": 112,
  "tokens_by_status": {"green": 110, "amber": 0, "red": 0, "unknown": 2},
  "open_incidents": 0,
  "providers": [
    {"name": "fastnear", "url": "https://free.rpc.fastnear.com", "healthy": true, "last_error": ""}
  ],
  "version": "0.1.0"
}
```

`status` is the riskiest token status. A monitor that has stopped looking must not report green, so `monitor_stale` is `true` and `status` is `unknown` when there has been no completed round, when the last completed round is older than `max(5 x interval, 2 min)` ("staleAfter"), or when the newest block time among the latest snapshots is older than staleAfter (rounds keep completing, but the provider is stuck on an old block). Provider `url` shows scheme and host only (provider URLs may embed API keys) and `last_error` has URLs reduced the same way.

## `GET /v1/tokens`

Every tracked token with its latest snapshot, sorted by deficit risk: `red`, then `amber`, then `unknown`, then `green`; within a status by gap relative to owed, most negative first.

```json
{
  "block_height": 171234567,
  "tokens": [
    {
      "id": "nep141:wrap.near",
      "standard": "nep141",
      "contract": "wrap.near",
      "inner_id": "",
      "symbol": "wNEAR",
      "chain": "NEAR",               // display label for the asset's network, see below
      "decimals": 24,
      "status": "green",
      "negative_streak": 0,
      "stale": false,                // the most recent read of this token failed
      "stale_since": "2026-11-03T12:00:00Z", // only while stale: when reads started failing
      "last_error": "",              // reason when stale (URLs reduced to scheme + host)
      "latest": {
        "block_height": 171234567,
        "block_time": "2026-11-03T12:00:00Z",
        "owed": "123456789000000000000000000000",
        "held": "123456789500000000000000000000",
        "gap":  "500000000000000000000",
        "provider": "fastnear"
      },
      "usd_price": "3.21"            // optional (P1), omitted when unknown
    }
  ]
}
```

`latest` is `null` for a token with no successful snapshot yet. A token without alert state or without any snapshot has status `unknown`. When `stale` is true the status and streak are those of the last successful read (a failed read never changes them), with one exception: a `green` token whose reads have been failing for longer than staleAfter (`stale_since` older than `max(5 x interval, 2 min)`) is reported as `unknown`, because its last green is no longer evidence. That includes a token whose snapshot stopped advancing: a snapshot not newer than the last processed block in a new round counts as a failed read (`last_error` "block not advancing"). Amber and red tokens keep their status while stale (a deficit already seen is never hidden by silence). A stale token counts as `unknown` in `tokens_by_status`, the overall `status` and the badges.

`chain` is derived from the token id (display only, never used for alerting) because symbols repeat across chains: `NEAR` for plain NEAR contracts, `*.omft.near` by prefix (`Ethereum`, `Base`, `Arbitrum`, `Solana`, `Tron`, `Bitcoin`, `Zcash`, `Dogecoin`, `XRP Ledger`...), `Ethereum (Rainbow)` / `Bitcoin (Omni)` for `*.bridge.near` assets, and for HOT Omni `nep245:v2_1.omni.hot.tg:<chainId>_...` the chain id mapped to a name (`BNB Chain` for 56, `Polygon` for 137, `TON` for 1117...; unknown ids render as `HOT Omni <id>`). `usd_price` is not implemented yet and is always absent.

Sorting: status rank (`red`, `amber`, `unknown`, `green`), then `gap / owed` ascending, exact (big rational). A token with `owed == 0` sorts as most negative if its gap is negative and last otherwise; tokens with no snapshot sort last in their status; ties by symbol then id.

## `GET /v1/tokens/{id}`

Same object as one element of `/v1/tokens`, plus `"open_incident": <Incident|null>`. `{id}` is the URL-escaped full token id (`nep141%3Awrap.near`).

## `GET /v1/tokens/{id}/history?from=&to=&limit=`

`from`/`to` are RFC 3339 times or block heights (integers). Default: last 24 h. `limit` default 2000, max 10000. When the range has more points than `limit`, the server downsamples evenly. While the snapshots with `gap < 0` number at most `limit`, every one of them is kept (so the response can exceed `limit`). When negative snapshots alone exceed `limit` (a long incident) they are downsampled too, but the first and last point of the range, the first and last point of every negative run (the sign transitions) and the deepest-deficit point of every run are always kept.

```json
{
  "token": {"id": "nep141:wrap.near", "symbol": "wNEAR", "chain": "NEAR", "decimals": 24},
  "points": [
    {"block_height": 171234500, "block_time": "2026-11-03T11:59:30Z", "owed": "...", "held": "...", "gap": "..."}
  ]
}
```

## `GET /v1/incidents?status=open|closed|all&limit=`

```json
{
  "incidents": [
    {
      "id": 7,
      "token_id": "nep141:eth.omft.near",
      "symbol": "ETH",
      "chain": "Ethereum",
      "decimals": 18,
      "start_block": 170001000,
      "red_block": 170001004,
      "end_block": 170001100,            // null while open
      "opened_at": "2026-10-01T03:12:00Z",
      "closed_at": "2026-10-01T04:00:00Z",
      "duration_seconds": 2880,          // null while the incident is open
      "max_deficit": "1000000000000000000",
      "max_deficit_block": 170001090,
      "summary": ""
    }
  ]
}
```

`closed_at`, `end_block` and `duration_seconds` are `null` while the incident is open. Status filter values other than `open`, `closed`, `all` give `400`; `limit` defaults to 100, max 1000.

## `GET /v1/incidents/{id}`

The incident plus `"events": [IncidentEvent...]` (see `internal/model`; always an array, filled when the event stream is enabled) and `"snapshots": [point...]` for its block range (capped at 2000 points).

## `GET /v1/export.csv?token=&from=&to=`

`text/csv`. Header: `token_id,symbol,decimals,block_height,block_time,owed,held,gap,provider`. Without `token`, all tokens. Same `from`/`to` rules as history including the 24 h default, so pass an explicit range for more; no downsampling, rows are ordered by token then block and capped at 100000 (narrow the range or pick a token if you hit the cap). The response is streamed. Text columns that start with `=`, `+`, `-`, `@`, tab or CR are prefixed with `'` to defuse spreadsheet formula injection; numeric columns are never altered (a negative `gap` stays `-123`).

## `GET /v1/replay`

The October 2026 incident replay result, loaded from `evidence/replay-2026-10-01.json`. `404 {"error":"replay not available"}` if that file doesn't exist.

```json
{
  "title": "NEAR Intents, 2026-10-01",
  "generated_at": "...",
  "rpc_provider": "...",
  "notes": "...",
  "debounce_k": 4,
  "cadence_seconds": 30,
  "tokens": [
    {
      "id": "nep141:...", "symbol": "...", "decimals": 18,
      "points": [
        {"block_height": 1, "block_time": "...", "owed": "...", "held": "...", "gap": "...", "label": "before exploit", "status": "green",
         "owed_query": "query call_function intents.near.mt_batch_supply({...}) at block_id 1", "held_query": "..."}
      ],
      "chain": "BNB Chain",
      "first_negative_block": 0,         // first sampled height with gap < 0, 0 if none
      "detected_red_block": 0,           // height at which alert.Step turns red on these samples, 0 if never
      "max_deficit": "0",
      "time_to_detect_seconds": null,    // (K-1) x 30 s when the sampled deficit lasted that long, else null
      "detection_note": "..."
    }
  ]
}
```

## `GET /v1/badge/{id}.svg` (P1)

Shields-style flat SVG badge (`image/svg+xml`, `Cache-Control: public, max-age=60`): `argus | green` for one token (`{id}` is the URL-escaped token id followed by `.svg`), or `/v1/badge/all.svg` for the overall status. Both show `unknown` when the monitor is stale (same rule as `monitor_stale`); a per-token badge also shows `unknown` for a green token that has been stale for longer than staleAfter. Colours: green, amber, red, grey for unknown. An unknown token id gets a grey `argus | not found` badge with status `404`; a path without the `.svg` suffix is a JSON `404`.

## `GET /healthz`

`200 ok` when the process is up; `GET /readyz` is `200` once at least one snapshot round has completed, otherwise `503`. Both are plain text and exempt from the rate limit.

## Operational behaviour

- Replay points are sparse; `detected_red_block` means "the debounce would have fired given snapshots at these heights" (see the `notes` field).
- Rate limit: token bucket per client IP, `ARGUS_API_RATE_LIMIT` requests per minute (burst equals one minute of allowance); `429` with `Retry-After` seconds. The IP is the TCP peer unless `ARGUS_TRUST_PROXY=true`, in which case the last `X-Forwarded-For` entry (the one our own proxy appended) is used.
- CORS follows `ARGUS_CORS_ORIGINS` (`*` or a list; a matching `Origin` is echoed). `OPTIONS` preflight answers `204`.
- Unknown paths give JSON `404`; non-GET methods give JSON `405` with `Allow`.
- Store failures give `500 {"error":"internal error"}` with details only in the server log.
- History/export `from` and `to`: an integer is a block height, anything else must be RFC 3339; `limit` must be a positive integer (invalid values give `400`).
