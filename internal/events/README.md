# internal/events

Reads `intents.near` events from FastNEAR neardata (default
`https://mainnet.neardata.xyz`) and turns them into `model.IncidentEvent`
rows: incident context (PRD 5.3/5.4) and the per-block mint/burn deltas for the
`owed` fallback.

## What it relies on
- `GET /v0/block/:h` returns a BlockDocument, or `null` for a skipped height.
  May redirect (followed).
- `GET /v0/last_block/final` redirects to `/v0/block/<height>`; the height is
  read from `Location` (body fallback).
- Fields decoded: `block.header.{height,hash,timestamp}`,
  `shards[].receipt_execution_outcomes[]` with
  `execution_outcome.{id,outcome.{executor_id,logs,status}}`,
  `receipt.receipt_id`, `tx_hash`.
- Only logs of receipts executed by `intents.near` with status `SuccessValue`
  or `SuccessReceiptId` count; `Failure` is skipped.
- Logs are `EVENT_JSON:{standard,version,event,data}`. `version` is ignored.
  Supported: NEP-245 `mt_mint|mt_burn|mt_transfer` (strict: mismatched
  token_ids/amounts is an error) and every DIP-4 event (typed fields for
  transfer, token_diff, intents_executed, ft/nft/mt/native_withdraw,
  storage_deposit; the rest, e.g. public_key_added, fee_changed,
  account_locked, salt_rotation, imt_*, are kept with raw JSON only).
  Other standards are ignored.

## Limits
- 180 req/min/IP is neardata's documented limit; the client defaults to 2 rps
  and backs off on 429/5xx (honours `Retry-After`). Following the head needs
  about 1.7 blocks/s, so a 2 rps default leaves little catch-up headroom:
  raise `ARGUS_NEARDATA_RPS` towards 2.5 if needed.
- Backfill costs one request per block. `Attacher` rejects ranges over 3000
  blocks (`ErrRangeTooLarge`); extend open incidents incrementally.
- Withdrawals appear as both a DIP-4 `*_withdraw` and an NEP-245 `mt_burn`;
  `Deltas` counts only NEP-245 rows, so nothing is double counted.

## Verified vs assumed
Verified: parsing and serialization against the Rust serde definitions in
near/intents (`crates/near/nep245/src/events.rs`,
`contracts/defuse/core/src/events/mod.rs`, `intents/{tokens,account}.rs`);
client/follower/backfill behaviour against `httptest` servers.

Assumed until capability check 4 runs live (fixtures in `testdata/` are
SYNTHETIC, see its README): the exact neardata block JSON (shape taken from
the OpenAPI/README via docs/research.md section 7), that `last_block/final`
redirects with a `Location` header, the Retry-After behaviour on 429, that
neardata includes receipt outcome logs for intents.near on current blocks,
the serialization of `storage_deposit`/`min_gas`/`NearToken` (decoded
leniently as string or number), and the derived Intents ids for DIP-4
withdraw events (`nep141:<token>`, `nep245:<token>:<id>`, `nep171:<token>:<id>`).
