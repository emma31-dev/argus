# Claims ledger

Every factual claim in the README and the docs, with the code that implements it and the proof that it ran. Status values:

- **VERIFIED**: a committed test or log shows it.
- **PARTIAL**: the code and tests exist, but the claim was not exercised in the environment it describes.
- **NOT RUN**: the code path exists and has not been executed.
- **OUT OF SCOPE**: deliberately not built.
- **OBSERVED**: a one-off measurement, not a reproducible test.

Reproduce the main checks with:

```sh
go test ./...
go run ./cmd/verify
```

## Product claims

| # | Claim | Status | Code | Proof |
|---|---|---|---|---|
| 1 | Ledger owed is read from `intents.near.mt_batch_supply`, 50 ids per call | VERIFIED | [`internal/snapshot/reader.go:355`](../internal/snapshot/reader.go#L355) | `TestRoundBatching`, `TestRoundCustomBatchSize`; live round found 790 ids |
| 2 | Held balances are read with `ft_balance_of` and `mt_batch_balance_of` for `intents.near` | VERIFIED | [`internal/tokens/held.go:26`](../internal/tokens/held.go#L26), [`held.go:41`](../internal/tokens/held.go#L41) | `TestHeldMethod`, `TestRoundNEP245Held`, `TestRoundBatchesNEP245HeldPerContract` |
| 3 | Owed and held come from one final block | VERIFIED | [`internal/snapshot/reader.go:166`](../internal/snapshot/reader.go#L166), [`reader.go:355`](../internal/snapshot/reader.go#L355) | `TestRoundBatching` asserts one height and hash; live round header names block 224512508 once |
| 4 | The gap is exact big-integer subtraction | VERIFIED | [`internal/snapshot/math.go:57`](../internal/snapshot/math.go#L57) | `TestGap`, `TestRoundHugeValues` |
| 5 | Discovery keeps NEP-141 and NEP-245 and skips NFTs | VERIFIED | [`internal/tokens/parse.go:64`](../internal/tokens/parse.go#L64), [`internal/tokens/discover.go`](../internal/tokens/discover.go) | `TestParse`, `TestEnumeration`, `TestEnumerationSurvivesCappedPages`; live round: 790 in scope |
| 6 | A token goes amber first and red after 4 consecutive negative snapshots | VERIFIED | [`internal/alert/state.go:12`](../internal/alert/state.go#L12), [`cmd/argus/config.go:155`](../cmd/argus/config.go#L155) | `TestStepScenarios`, `TestEngineIncidentLifecycle`; [`evidence/verify-synthetic-2026-11-14.txt`](../evidence/verify-synthetic-2026-11-14.txt) |
| 7 | A failed read marks the token stale and does not reset its streak | VERIFIED | [`internal/alert/state.go`](../internal/alert/state.go) | `TestEngineReadErrorsKeepStreak`, `TestStepErrorKeepsStreak`, `TestStepIgnoresStaleBlocks` |
| 8 | A red token opens an incident, and the alert is queued atomically through an outbox | VERIFIED | [`internal/alert/engine.go:108`](../internal/alert/engine.go#L108), [`internal/alert/outbox.go`](../internal/alert/outbox.go) | `TestEngineIncidentLifecycle`, `TestEngineAtomicOutboxFailureRedThenRecoverThenDeficit`, `TestRelayDeliversOutboxOnceQueueRecovers` |
| 9 | Telegram delivery is implemented | PARTIAL | [`internal/alert/telegram.go:167`](../internal/alert/telegram.go#L167) | Unit tests against a local fake server; no live bot token was used |
| 10 | Webhook delivery is HMAC-SHA256 signed | PARTIAL | [`internal/alert/webhook.go:25`](../internal/alert/webhook.go#L25) | Unit tests against local servers; no live endpoint was called |
| 11 | The HTTP API exposes status, tokens, history, incidents, CSV, replay and a badge | VERIFIED | [`internal/api/api.go:109`](../internal/api/api.go#L109) | `TestStatus`, `TestTokensSortAndFields`, `TestHistory`, `TestIncidents`, `TestExportCSV` |
| 12 | The dashboard reads the `/v1` API shape | PARTIAL | [`web/src/lib/api.ts:227`](../web/src/lib/api.ts#L227), [`web/src/routes`](../web/src/routes) | `pnpm check` 0 errors, 36 tests, build passes; screenshots and tests use the mock API, not a live monitor |
| 13 | A full round ran against NEAR mainnet | VERIFIED | [`cmd/argus/once.go`](../cmd/argus/once.go) | [`evidence/live-round-2026-11-14.txt`](../evidence/live-round-2026-11-14.txt) |
| 14 | 12 recorded rounds replay offline and match expectations | VERIFIED | [`cmd/verify`](../cmd/verify) | [`evidence/verify-synthetic-2026-11-14.txt`](../evidence/verify-synthetic-2026-11-14.txt) |
| 15 | A Postgres store holds snapshots, incidents, subscriptions and the outbox | PARTIAL | [`internal/store`](../internal/store) | Package tests pass; the 25 database tests skip without `ARGUS_TEST_DATABASE_URL` and `ARGUS_TEST_REDIS_URL` |
| 16 | Incident events are attached from a block range | PARTIAL | [`internal/events/attach.go:36`](../internal/events/attach.go#L36) | Unit tests pass; no live incident exercised the path |
| 17 | An optional LLM incident summary exists | PARTIAL | [`internal/summary/summary.go:66`](../internal/summary/summary.go#L66) | Unit tests pass; disabled by default and not exercised with a real model key |
| 18 | 236 Go tests pass, 25 skip, 0 fail | VERIFIED | `go test ./... -count=1` | [`evidence/go-test-2026-11-14.txt`](../evidence/go-test-2026-11-14.txt) |
| 19 | `go vet`, a CGO-disabled build, `go mod tidy -diff` and gofmt are clean | VERIFIED | `go vet ./...`, `CGO_ENABLED=0 go build ./...`, `go mod tidy -diff`, `gofmt -l .` | [`evidence/static-checks-2026-11-14.txt`](../evidence/static-checks-2026-11-14.txt) |
| 20 | The dashboard passes check, 36 tests and a production build | VERIFIED | [`web/package.json`](../web/package.json) | [`evidence/dashboard-checks-2026-11-14.txt`](../evidence/dashboard-checks-2026-11-14.txt) |
| 21 | The live round took about 9.5 minutes at 1 request per second | OBSERVED | [`cmd/argus/config.go:25`](../cmd/argus/config.go#L25) | [`evidence/live-round-2026-11-14.md`](../evidence/live-round-2026-11-14.md); the log has start and block time but no end timestamp |

## Not run or not built

| Item | Status | Why |
|---|---|---|
| Docker Compose stack | NOT RUN | Docker is not installed on the build machine. The compose file and Dockerfiles are in the repository. |
| 25 Postgres and Redis tests | NOT RUN | No database or Redis service, and no `ARGUS_TEST_*` URLs were available. |
| `cmd/verify -capture` | NOT RUN | The flag and the recording path exist in [`cmd/verify/main.go`](../cmd/verify/main.go) and [`cmd/verify/capture.go`](../cmd/verify/capture.go). |
| October replay | NOT RUN | Needs Postgres plus an archival RPC endpoint. The plan and example file are in [`data/replay`](../data/replay). |
| Live Telegram and webhook delivery | NOT RUN | No bot token or public endpoint was used. |
| Hosted dashboard | NOT RUN | No deployment. The screenshots are from the mock API. |
| 24/7 uptime | OUT OF SCOPE | One live round is not an uptime claim. |
| Origin-chain backing of bridged tokens | OUT OF SCOPE | Argus reads the NEAR side only. |
| NFT and `imt:` backing | OUT OF SCOPE | Skipped by [`internal/tokens/parse.go:64`](../internal/tokens/parse.go#L64). |
| BigQuery and the 1Click Explorer API | NOT USED | Argus reads NEAR RPC directly. No BigQuery or Explorer JWT is required. |
| A claim that Argus would have prevented the October incident | NOT MADE | Argus is a detector, not a control. It compares ledger totals with held balances after the fact. |

## Evidence map

| File | What it proves |
|---|---|
| [`live-round-2026-11-14.txt`](../evidence/live-round-2026-11-14.txt) | One mainnet round: 790 discovered, 720 read, 70 stale, 705 green, 15 amber, 0 red |
| [`live-round-2026-11-14.md`](../evidence/live-round-2026-11-14.md) | The round summary, the 15 amber groups and the 70 stale reasons |
| [`go-test-2026-11-14.txt`](../evidence/go-test-2026-11-14.txt) | 236 passed, 25 skipped, 0 failed |
| [`static-checks-2026-11-14.txt`](../evidence/static-checks-2026-11-14.txt) | vet, build, tidy and gofmt results |
| [`dashboard-checks-2026-11-14.txt`](../evidence/dashboard-checks-2026-11-14.txt) | Dashboard check, 36 tests and build |
| [`verify-synthetic-2026-11-14.txt`](../evidence/verify-synthetic-2026-11-14.txt) | 12 synthetic rounds, 7 transitions, 4 alerts, all matching `expected.json` |

## How the live numbers break down

The clean log has 790 token rows. 70 rows are followed by an error line and are counted as stale, not green:

- 59 contract methods not found.
- 9 accounts or contract code not found.
- 2 contract execution failures.
- 55 of the 70 are one test contract, `nep245:test1779579480756.near`.

The 15 amber rows are:

- 8 HOT Omni ids on chain ids 1010 and 4444119 with zero NEAR-side held balance.
- 3 `mt.nfts.tg` NFT ids.
- 2 test ids with a negative gap.
- 2 `clearing.stg.baseload.near` staging ids with zero held.

No row is red. Red needs 4 consecutive negative snapshots, and this was one round. The synthetic replay exercises the red path.
