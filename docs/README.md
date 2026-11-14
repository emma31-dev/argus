# docs

Design notes for Argus. The code is in [`../internal`](../internal), [`../cmd`](../cmd) and [`../web`](../web).

## Decisions and tradeoffs

- Direct NEAR RPC instead of an indexer. The check needs one final block and the exact raw strings, not a derived table. The tradeoff is that a slow public endpoint sets the round time, and archival queries need a separate endpoint.
- Both sides at the same block. Reading owed and held at different heights would turn ordinary deposits and withdrawals into fake gaps. The tradeoff is that every call in a round must carry the same block height, which the reader enforces.
- Big-integer math on decimal strings. Token amounts exceed float64 and JavaScript's safe integer range. The tradeoff is that every amount stays a string until formatting.
- Postgres is the history store, not a requirement for a one-off round. `cmd/argus -once` runs without a database, which is what makes the judge path and the live evidence possible. The tradeoff is that the dashboard and incidents need the full stack.
- A static SvelteKit dashboard. It reads `/v1` from the browser and has no server of its own, so it can be hosted on any static host. The tradeoff is that it needs `PUBLIC_API_BASE` when the API is on another origin.

## Map

- To find the HTTP contract visit [api.md](api.md).
- To find deployment steps visit [deploy.md](deploy.md).
- To find the day-0 sponsor research and contract quirks visit [research.md](research.md).
- To find every claim mapped to code and proof visit [CLAIMS.md](CLAIMS.md).
- The Mermaid source for the README architecture diagram can be found in [architecture.mmd](architecture.mmd).

## Evidence

The committed test and live-run output is in [`../evidence`](../evidence), with an index in [`../evidence/README.md`](../evidence/README.md).
