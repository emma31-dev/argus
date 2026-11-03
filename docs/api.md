# Argus HTTP API (v1)

Read-only, no auth, JSON unless noted. All amounts are **base-10 integer strings in the token's smallest unit**; clients format them using `decimals`. `gap = held - owed` and may be negative. Timestamps are RFC 3339 UTC. Rate-limited per IP (default 60 req/min); a limited request gets `429` with `Retry-After`.

Errors: `{"error": "<message>"}` with a 4xx/5xx status.

CORS: `Access-Control-Allow-Origin: *` (data is public).

## `GET /v1/status`

Overall monitor health plus a summary.

```json
{
  "status": "green",                 // worst token status: green | amber | red | unknown
  "last_block": 171234567,
  "last_block_time": "2026-11-03T12:00:00Z",
  "last_checked_at": "2026-11-03T12:00:04Z",
  "interval_seconds": 30,
  "debounce_snapshots": 4,
  "tokens_total": 112,
  "tokens_by_status": {"green": 110, "amber": 1, "red": 0, "unknown": 1},
  "open_incidents": 0,
  "providers": [
    {"name": "fastnear", "url": "https://rpc.mainnet.fastnear.com", "healthy": true, "last_error": ""}
  ],
  "version": "0.1.0"
}
```

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
      "decimals": 24,
      "status": "green",
      "negative_streak": 0,
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

`latest` is `null` for a token with no successful snapshot yet.

## `GET /v1/tokens/{id}`

Same object as one element of `/v1/tokens`, plus `"open_incident": <Incident|null>`. `{id}` is the URL-escaped full token id (`nep141%3Awrap.near`).

## `GET /v1/tokens/{id}/history?from=&to=&limit=`

`from`/`to` are RFC 3339 times or block heights (integers). Default: last 24 h. `limit` default 2000, max 10000. When the range has more points than `limit`, the server downsamples evenly but always keeps every snapshot with `gap < 0`.

```json
{
  "token": {"id": "nep141:wrap.near", "symbol": "wNEAR", "decimals": 24},
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
      "decimals": 18,
      "start_block": 170001000,
      "red_block": 170001004,
      "end_block": 170001100,
      "opened_at": "2026-10-01T03:12:00Z",
      "closed_at": "2026-10-01T04:00:00Z",
      "duration_seconds": 2880,
      "max_deficit": "1000000000000000000",
      "max_deficit_block": 170001090,
      "summary": ""
    }
  ]
}
```

## `GET /v1/incidents/{id}`

The incident plus `"events": [IncidentEvent...]` (see `internal/model`) and `"snapshots": [point...]` for its block range.

## `GET /v1/export.csv?token=&from=&to=`

`text/csv`. Header: `token_id,symbol,decimals,block_height,block_time,owed,held,gap,provider`. Without `token`, all tokens. Same range rules as history (no downsampling, max 100000 rows).

## `GET /v1/replay`

The October 2026 incident replay result, loaded from `evidence/replay-2026-10-01.json`. `404 {"error":"replay not available"}` if that file doesn't exist.

```json
{
  "title": "NEAR Intents, 2026-10-01",
  "generated_at": "...",
  "rpc_provider": "...",
  "notes": "...",
  "tokens": [
    {
      "id": "nep141:...", "symbol": "...", "decimals": 18,
      "points": [
        {"block_height": 1, "block_time": "...", "owed": "...", "held": "...", "gap": "...", "label": "before exploit", "status": "green"}
      ],
      "first_negative_block": 0,
      "detected_red_block": 0,
      "max_deficit": "0"
    }
  ]
}
```

## `GET /v1/badge/{id}.svg` (P1)

Shields-style SVG badge: `argus | green` for one token, or `/v1/badge/all.svg` for the overall status.

## `GET /healthz`

`200 ok` when the process is up; `GET /readyz` is `200` once at least one snapshot round has completed.
