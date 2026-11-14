# internal

Shared packages for the monitor, the API, the replay tools and the tests.

| Package | Role |
|---|---|
| [`alert`](alert) | Debounce state machine, incident engine, outbox relay, Telegram, webhook, Redis queue |
| [`api`](api) | `/v1/*` HTTP handlers and the CSV export |
| [`events`](events) | neardata client and incident event attachment |
| [`fixturegen`](fixturegen) | Generates the synthetic fixture set |
| [`model`](model) | Shared token, snapshot, incident and event types |
| [`report`](report) | Status tables and gap formatting |
| [`rpc`](rpc) | JSON-RPC client, rate limit, provider failover, replay recorder |
| [`snapshot`](snapshot) | Same-block reader and big-integer gap math |
| [`store`](store) | Postgres repository and embedded migrations |
| [`summary`](summary) | Optional LLM incident summary |
| [`tokens`](tokens) | Discovery, token id parsing, held-balance method selection |

The read path is `tokens` to `snapshot` to `alert` to `store` to `api`.

To find the ledger and held reads visit [snapshot/reader.go](snapshot/reader.go).
To find the gap subtraction visit [snapshot/math.go](snapshot/math.go).
To find token discovery visit [tokens/discover.go](tokens/discover.go).
To find the amber/red rules visit [alert/state.go](alert/state.go).
To find incident handling visit [alert/engine.go](alert/engine.go).
To find the HTTP routes visit [api/api.go](api/api.go).
To find the schema runner visit [store/store.go](store/store.go).
