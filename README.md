# argus

Argus watches NEAR Intents (`intents.near`) and checks, token by token, that the contract really holds at least what its internal ledger says users own. If that stops being true for more than a couple of minutes, it raises an alert.

![dashboard overview](web/docs/screenshots/overview-1440-light.png)

*Rendered from the dev mock API. The striped banner marks the numbers as made up.*

## Why

On 2026-10-01 a failing deposit callback let an attacker keep ledger credit for deposits that had been refunded. Across 23 deposits that added up to roughly $3.8M of balances with nothing behind them, which were withdrawn as USDT on BNB Chain and returned about 36 hours later. NEAR Intents' monitoring compared bridge backing with NEAR-side totals, but not ledger totals with what the contract actually held. The deposit path has since been fixed ([near/intents@16d04f6](https://github.com/near/intents/commit/16d04f6ec4401263f8914638710260da50bb46b0)) and the team now runs that check internally.

Argus runs the same comparison in public, so anyone can see it, re-run it and subscribe to it.

The official postmortem couldn't be opened from the build environment, so the figures come from coverage that quotes it: [The Defiant](https://thedefiant.io/news/security/near-intents-postmortem-links-3-8-million-exploit-to-deposit-bug), [Crypto Times](https://www.cryptotimes.io/2026/10/17/near-intents-traces-3-8m-exploit-to-smart-contract-deposit-bug/), [KuCoin News](https://www.kucoin.com/news/flash/near-intents-releases-post-mortem-report-on-3-8m-vulnerability), [The Block](https://theblock.co/news/ecosystems/2026-10-01-near-intents-halts-services-after-3-8-million-exploit-promises-full-compensation-417404). More in [docs/research.md](docs/research.md).

## How it works

Every `ARGUS_INTERVAL` (30 seconds by default), at one final block and through one RPC provider:

- owed: `intents.near.mt_batch_supply` for every token, 50 ids per call
- held: `ft_balance_of` (NEP-141) on each token contract, or `mt_batch_balance_of` (NEP-245) for up to 50 tokens of one contract, always for `intents.near`
- gap = held - owed, in big-integer math on the raw strings

Both sides come from the same block, otherwise ordinary deposits and withdrawals would show up as fake gaps.

`intents.near` lists about 88,000 token ids, almost all NFTs that Argus skips. About 700 are NEP-141 or NEP-245 tokens and all of them are tracked. A round is then roughly 500 RPC calls. FastNEAR's free endpoint sustained about 1 request a second in a live test and answered 429 for a third of the calls at 2, so the default `ARGUS_RPC_RPS` is 1. At that rate a full mainnet round took about 8 minutes with no 429s, and rounds follow each other back to back. A keyed or self-hosted endpoint with a higher `ARGUS_RPC_RPS` gets the 30 second cadence.

A negative gap turns a token amber first, because a transfer in flight can take a few blocks. If it stays negative for 4 rounds in a row (about 2 minutes at the 30 second cadence), or passes an absolute limit set in `ARGUS_RED_THRESHOLDS`, the token goes red. Argus then opens an incident, sends Telegram and webhook alerts, and attaches the `intents.near` events from that block range. A failed read marks a token stale and never resets its streak.

Not covered: origin-chain backing of bridged tokens, NFTs, and `imt:` tokens. Argus only reads. It never signs anything and holds no keys.

## Running it

Offline, no network or keys:

```sh
go test ./...
go run ./cmd/verify
```

`cmd/verify` pushes recorded RPC responses through the real snapshot reader and alert engine for 12 rounds, then checks the outcome against `expected.json`. The recorded set in `data/fixtures/synthetic` is invented (see `internal/fixturegen`). `go run ./cmd/verify -capture` records a real mainnet round, which then gets replayed first.

Full stack with Docker:

```sh
cp .env.example .env
docker compose up --build    # postgres, redis, argus on :8080, dashboard on :3000
```

Without Docker you need Go 1.26.10+, Postgres 16, and optionally Redis 7. The dashboard needs Node 22 and pnpm 10.

```sh
go build -o bin/argus ./cmd/argus
DATABASE_URL=postgres://... ./bin/argus
cd web && pnpm install && pnpm dev     # :5173, proxies /v1 to :8080
```

Live checks against mainnet (read-only):

```sh
go run ./cmd/probe all
go run ./cmd/probe archival -date 2026-10-01
```

Replaying October:

```sh
go run ./cmd/replay -find-heights 2026-09-29T16:00:00Z,2026-10-01T06:00:00Z
cp data/replay/plan.example.yaml data/replay/plan.yaml    # fill in heights and token ids
go run ./cmd/replay
```

Every setting is an environment variable, listed in [.env.example](.env.example). For Telegram, create a bot with `@BotFather`, set `TELEGRAM_BOT_TOKEN`, then send it `/subscribe all`. API reference: [docs/api.md](docs/api.md). Hosting: [docs/deploy.md](docs/deploy.md).

## Layout

- `cmd/argus`: the monitor and its HTTP API
- `cmd/verify`: offline replay of recorded rounds
- `cmd/replay`: archival replay of the October incident
- `cmd/probe`: live mainnet checks
- `internal/`: RPC client, token discovery, snapshot math, alerting, event stream, Postgres store, API handlers
- `web/`: the SvelteKit dashboard
- `data/`: fixtures and replay inputs

## Status

This was built where every NEAR host was blocked. The Go tests (including the Postgres and Redis ones), `cmd/verify` and the dashboard checks pass. The probes, a live round and the October replay haven't run against mainnet yet. Method names and argument shapes were checked against the near/intents source instead. The best guess for the replayed token is HOT Omni USDT on BNB Chain (`nep245:v2_1.omni.hot.tg:56_2CMMyVTGZkeyNZTSvS5sarzfir6g`), still unconfirmed.

Known limits: free public RPC is rate-limited (a provider that answers 429 is put on a cooldown and the round carries on or moves to the next provider), and non-archival nodes only go back about 36 hours. Tokens without metadata show raw amounts. A fee-on-transfer or rebasing token could stay red without anything being wrong.

MIT, see [LICENSE](LICENSE).
