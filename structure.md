# Structure

Folder map for Argus. The root [README.md](README.md) is the product overview. This file is the navigation aid for the code.

## Tree

```text
Argus/
├── cmd/
│   ├── argus/          monitor, HTTP API, one-round mode
│   ├── probe/          live mainnet checks
│   ├── replay/         archival replay of October
│   └── verify/         offline fixture replay and live capture
├── config/             fallback token list
├── data/
│   ├── fixtures/       synthetic rounds and the expected result
│   └── replay/         replay plan template and outputs
├── deploy/             Fly.io and Cloudflare configuration, not deployed
├── docs/               API, deploy, research, claims, architecture
├── evidence/           committed test and live-run output
├── internal/
│   ├── alert/          debounce, incidents, outbox, Telegram, webhook, Redis
│   ├── api/            /v1 handlers and CSV export
│   ├── events/         neardata client and incident attachment
│   ├── fixturegen/     synthetic fixture generator
│   ├── model/          shared types
│   ├── report/         tables and formatting
│   ├── rpc/            JSON-RPC client, rate limit, failover, recorder
│   ├── snapshot/       same-block reader and big-integer gap
│   ├── store/          Postgres repository and migration runner
│   ├── summary/        optional LLM incident summary
│   └── tokens/         discovery, parsing, held methods
├── migrations/         embedded Postgres schema
├── web/                SvelteKit dashboard
│   ├── dev/            mock API, screenshots
│   ├── docs/           screenshots
│   └── src/            routes, components, API client
├── docker-compose.yml
├── Dockerfile
├── go.mod
└── README.md
```

## Where the logic lives

| Logic | File |
|---|---|
| Ledger and held reads at one block | [`internal/snapshot/reader.go`](internal/snapshot/reader.go) |
| Big-integer gap subtraction | [`internal/snapshot/math.go`](internal/snapshot/math.go) |
| Token discovery and scope | [`internal/tokens/discover.go`](internal/tokens/discover.go), [`internal/tokens/parse.go`](internal/tokens/parse.go) |
| Held method selection | [`internal/tokens/held.go`](internal/tokens/held.go) |
| Amber and red state machine | [`internal/alert/state.go`](internal/alert/state.go) |
| Incident lifecycle | [`internal/alert/engine.go`](internal/alert/engine.go) |
| Atomic alert outbox | [`internal/alert/outbox.go`](internal/alert/outbox.go) |
| Telegram and webhook delivery | [`internal/alert/telegram.go`](internal/alert/telegram.go), [`internal/alert/webhook.go`](internal/alert/webhook.go) |
| Incident event attachment | [`internal/events/attach.go`](internal/events/attach.go) |
| HTTP routes | [`internal/api/api.go`](internal/api/api.go) |
| Postgres queries and migrations | [`internal/store`](internal/store), [`migrations`](migrations) |
| One-round command | [`cmd/argus/once.go`](cmd/argus/once.go) |
| Long-running monitor | [`cmd/argus/app.go`](cmd/argus/app.go) |
| Offline replay | [`cmd/verify/main.go`](cmd/verify/main.go) |
| Live capture | [`cmd/verify/capture.go`](cmd/verify/capture.go) |
| Archival replay | [`cmd/replay/main.go`](cmd/replay/main.go) |
| Dashboard routes | [`web/src/routes`](web/src/routes) |
| Dashboard API client | [`web/src/lib/api.ts`](web/src/lib/api.ts) |
| Synthetic fixture generator | [`internal/fixturegen`](internal/fixturegen) |

## Folder READMEs

- [README.md](README.md): product overview, judge check, measured results.
- [cmd/README.md](cmd/README.md): the four commands.
- [internal/README.md](internal/README.md): package map and read path.
- [web/README.md](web/README.md): dashboard commands and mock API.
- [docs/README.md](docs/README.md): decisions and document index.
- [evidence/README.md](evidence/README.md): what each committed log proves.
- [data/README.md](data/README.md): fixtures and replay inputs.
- [deploy/README.md](deploy/README.md): deployment configuration.
- [config/README.md](config/README.md): fallback token list.
- [migrations/README.md](migrations/README.md): schema files and runner.

## Related documents

- [docs/CLAIMS.md](docs/CLAIMS.md): every claim mapped to code and proof.
- [docs/api.md](docs/api.md): HTTP API reference.
- [docs/deploy.md](docs/deploy.md): deployment steps.
- [docs/research.md](docs/research.md): sponsor research and contract quirks.
- [docs/architecture.mmd](docs/architecture.mmd): Mermaid source for the README diagram.
