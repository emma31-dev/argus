# Capability checks

Small programs that verify, against real NEAR mainnet, the assumptions the
design rests on (PRD section 7). Each prints `PASS` / `FAIL` / `INFO` lines,
exits non-zero on failure, and writes the raw JSON-RPC exchanges plus summaries
to `evidence/checks/<check>/` (run from the repository root). No keys or
credentials are needed. They only read.

| Check | What it verifies |
|---|---|
| `01-owed` | `intents.near` implements `mt_supply` and `mt_batch_supply` (wNEAR, USDC, bridged ETH, plus an unknown id to see the null/zero behaviour). If `mt_batch_supply` is missing, the PRD says to stop and switch `owed` to the event-delta method. |
| `02-held` | The held side: `ft_balance_of({"account_id":"intents.near"})` on wNEAR, USDC and a `*.omft.near` token, and `mt_balance_of` for one NEP-245 token (`-nep245 <id>`, default: found by discovery). |
| `03-same-block` | Owed and held read at the same recent `block_id` from one pinned provider; re-reading gives identical values; probes how far back a non-archival provider accepts `block_id`. |
| `04-events` | FastNEAR neardata (`https://mainnet.neardata.xyz`) serves recent blocks without a key, with receipt execution outcomes and logs, and `intents.near` `EVENT_JSON` logs parse. `-blocks N` sets the window (default 100). |
| `05-discovery` | Which token discovery sources work (NEP-245 `mt_tokens` enumeration, the 1Click list, `config/tokens.yaml`) and how many tokens each finds. `-oneclick-url` overrides the endpoint. |
| `06-archival` | Archival RPC can serve view calls at a block from 2026-10-01. Needs `-block <height>` (from the postmortem or an explorer), or `-date 2026-10-01` to bisect the first block of that UTC day (about 25 archival requests). |

## Running

```sh
go run ./checks/01-owed
go run ./checks/02-held
go run ./checks/03-same-block
go run ./checks/04-events
go run ./checks/05-discovery
go run ./checks/06-archival -block <height>
```

Environment:

- `ARGUS_RPC_URLS`: comma-separated RPC URLs for checks 1, 2, 3 and 5. Default:
  `https://rpc.mainnet.fastnear.com`, `https://rpc.mainnet.near.org`.
- `ARGUS_ARCHIVAL_RPC_URLS`: comma-separated archival URLs for check 6. Default:
  `https://archival-rpc.mainnet.fastnear.com`, `https://archival-rpc.mainnet.near.org`.

The checks use the same `internal/rpc` client as the monitor, at a conservative
2 requests per second per provider. If the network is unreachable they print
the underlying error and a hint that it is a connectivity problem rather than
a contract result.
