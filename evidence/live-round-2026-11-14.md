# Live round, 2026-11-14

One read-only round against NEAR mainnet. No database, no keys, no wallet.

- Command: `ARGUS_RPC_RPS=1 go run ./cmd/argus -once`
- Started: 2026-11-14T12:13:51Z
- Block: 224512508 (2026-11-14T12:14:49Z)
- Provider: free.rpc.fastnear.com
- Tokens in scope: 790, from enumeration discovery
- Read: 720
- Stale: 70
- Green: 705
- Amber: 15
- Red: 0
- Wall time: about 9.5 minutes from process start to exit

The full table is in [`live-round-2026-11-14.txt`](live-round-2026-11-14.txt). SHA256 of that file:

```
336CBAB91D4BADB8AF378371B71F44EBDD018645E506179B323C50B546BC53AC
```

## The 15 amber tokens

| Group | Count | Why it is amber |
|---|---|---|
| HOT Omni, chain ids 1010 and 4444119 | 8 | Owed is positive and the NEAR-side held balance is zero. Backing for these ids sits on the origin chain, which Argus does not cover. |
| `mt.nfts.tg` NFT ids (`5`, `data-babies.nfts.tg`, `ping3.nfts.tg`) | 3 | Owed is positive and held is zero or lower. NFT backing is out of scope. |
| Test ids (`ninu-4.nearlytrade.near`, `nvdastest.optimal-bone.near`) | 2 | Test contracts with a negative gap. |
| Staging ids (`clearing.stg.baseload.near`) | 2 | Staging contracts with a zero held balance. |

No token went red. Red needs 4 consecutive negative snapshots, and this was one round. The synthetic replay in `cmd/verify` exercises the red path.

## The 70 stale tokens

Stale means the read failed. It is not green, and the negative streak is not reset.

| Reason | Count |
|---|---|
| Contract method not found | 59 |
| Account or contract code not found | 9 |
| Contract execution failed | 2 |

55 of the 70 are one test contract, `nep245:test1779579480756.near`. These ids are in the `intents.near` list but are not readable token contracts. Argus reports them as stale instead of pretending they are healthy.

## What this proves

The same-block reader, the big-integer gap, token discovery, and the amber/stale classification all ran against NEAR mainnet. The run was read-only and used no keys.

It does not prove a 24/7 deployment, the October replay, or origin-chain backing. Those are listed as not built or out of scope in the README.
