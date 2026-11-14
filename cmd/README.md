# cmd

Four entry points. Each is a `main` package. Shared logic lives in [`../internal`](../internal).

| Command | Run | What it does |
|---|---|---|
| `argus` | `go run ./cmd/argus` | Long-running monitor and HTTP API. Needs `DATABASE_URL`. `-once` runs one read-only round and needs no database. |
| `probe` | `go run ./cmd/probe all` | Live mainnet checks for owed values, held values, discovery and archival access. |
| `verify` | `go run ./cmd/verify` | Replays recorded fixtures through the real reader and alert engine. `-capture` records a live round. |
| `replay` | `go run ./cmd/replay` | Archival replay of the October incident against a plan file. |

To find the one-round path visit [argus/once.go](argus/once.go).
To find the monitor loop visit [argus/app.go](argus/app.go).
To find environment parsing visit [argus/config.go](argus/config.go).
To find the offline replay visit [verify/main.go](verify/main.go).
To find the live capture path visit [verify/capture.go](verify/capture.go).
To find the archival replay visit [replay/main.go](replay/main.go).

The commands share the RPC client, snapshot reader, alert engine and store from [`../internal`](../internal).
