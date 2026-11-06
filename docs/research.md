# Argus research notes

Written 2026-11-03 as background for the monitor design.

Every fact below has a source. Anything that could not be confirmed against a primary source is marked **UNVERIFIED**. Where something was worked out from code and not from a document, it says **INFERRED**.

## 0. How this research was done, and what could not be reached

The sandbox limited what could be checked, and that affects how much weight each section can bear.

| Access path | Result |
|---|---|
| Shell `curl` to NEAR hosts (`rpc.mainnet.fastnear.com`, `rpc.mainnet.near.org`, `mainnet.neardata.xyz`, `docs.near-intents.org`, `1click.chaindefuser.com`) | Blocked (proxy 403), as the brief said. |
| `WebFetch` (any host tried: `docs.near-intents.org`, `docs.near.org`, `mainnet.neardata.xyz`, `rekt.news`, `www.kucoin.com`, `thecoding.substack.com`) | **All failed** with `getaddrinfo ENOTFOUND`: these hosts are outside the session's network allowlist. **No live endpoint was called.** |
| `WebSearch` | Worked. It returns search-engine summaries, not page text, so anything taken only from a search summary is labelled "(search summary)". |
| `api.github.com` and `github.com` web pages (PRs, releases) via curl | Blocked (403: "GitHub access to this repository is not enabled for this session"). PR descriptions could not be read. |
| `git clone` from github.com and `raw.githubusercontent.com` | **Worked.** Most of the hard facts below come from source code and docs repositories cloned locally. |

Repositories read (commit = HEAD at the time of reading):

- `near/intents` @ `7fe27bf7c77adf657b652e0555a4aef660b489fc` (2026-11-03), history fetched back to 2025-06, plus all `defuse/v*` tags.
- `near/docs` @ `e2d2ea173cf462bcf518ecae5650984a221d5183` (2026-11-01). This is the source of docs.near.org.
- `fastnear/neardata-server` @ `7324d9131e7e8285306606ba05822b007e44c53a` (2026-07-22).
- `fastnear/builder-docs` @ `240ab2d80466774f1e6993c3b8b967af1b26fa91` (2026-11-02). This appears to be the source of docs.fastnear.com.
- `near/NEPs` @ `095fe98218d2272a0a285008f72bdb3e8957e613`.
- `defuse-protocol/one-click-sdk-go` @ `42ad1e6c8172b552c243270d3f5318d50c209813` (2026-04-06), which contains the 1Click OpenAPI spec.
- `defuse-protocol/defuse-frontend` @ `594085d3c7e60ce912fb62b768b12f46c4917510` (2026-10-06), which contains the NEAR Intents production token list.

---

## 1. Official postmortem

### Findings

- **The official postmortem text could not be retrieved.** No search result gave a URL on a NEAR Intents domain or an X status URL for it, and WebFetch is blocked, so it can't be quoted directly. **UNVERIFIED: the primary URL.**
- Per secondary coverage (search summaries), it is titled **"Postmortem: NEAR Intents October 1 Exploit"**. It was published by **Alex Shevchenko** (NEAR Intents GM, X handle reported as `@AlexAuroraDev`) on **2026-10-16, around 15:56 UTC**. One outlet (TechFlow) dates it 2026-10-17. Sources: https://www.cryptotimes.io/2026/10/17/near-intents-traces-3-8m-exploit-to-smart-contract-deposit-bug/ , https://shattered.io/near-intents-3-8m-hack-postmortem-clears-omni-2026/ , https://www.techflowpost.com/en-US/newsletter/140312 , https://www.kucoin.com/news/insight/NEAR/6ad36dcdc31ba8000731f132
- It was most likely posted on X (the coverage attributes it to Shevchenko's account). **UNVERIFIED.** Linking it directly is still open; see Open questions.

### Key claims, as reported second-hand (search summaries; not verbatim quotes)

| Item | What secondary sources say | Sources |
|---|---|---|
| Root cause | A bug in the **NEAR Intents contract's deposit-notification feature**, not in Omni. The attacker deployed a contract built to make the notification fail, so "the token contract refunded the deposit while NEAR Intents retained the corresponding ledger credit". The postmortem reverses the initial 2026-10-01 statement that blamed the "Omni deposit and withdrawal infrastructure". | thedefiant.io, cryptotimes.io, shattered.io |
| Failed deposits | **23** failed deposit transactions | thedefiant.io, kucoin flash, chaincatcher https://www.chaincatcher.com/en/article/2296917 |
| Amount | **about $3.8M** of unbacked "ghost" internal balances. Bitquery's on-chain figure is **$3.865M / $3.87M USDT**. One KuCoin flash headline says "$380,000", which looks like a typo since every other outlet says $3.8M. | https://bitquery.io/investigations/near-intents-hack , https://www.kucoin.com/news/flash/near-intents-reveals-380k-usd-funds-fully-recovered-after-october-1-vulnerability-incident |
| Affected token / chain | Outflow was **USDT on BNB Chain (BSC)**: "isolated to USDT on BSC", in five withdrawals from a NEAR-Intents-linked BSC vault, after two test withdrawals of 10 and 11 USDT (Bitquery, via search summary). Unchained counts seven withdrawals. | bitquery.io (above), https://unchainedcrypto.com/near-intents-pledges-full-compensation-after-a-bug-lets-an-attacker-drain-3-8-million/ , https://coincentral.com/near-intents-hacked-for-3-8-million-in-latest-crypto-exploit |
| Attacker addresses | BSC address **`0x09fd1f5d9f185067a92493e43aa259ea4ab3ad37`** (Rekt; a French article attributes it to ZachXBT). **No NEAR attacker account id or attacker contract id was found in any source. UNVERIFIED.** | https://rekt.news/near-intents-rekt (search summary) |
| Time window | Exploit withdrawals ran **2026-09-29 16:14 UTC → 2026-10-01 05:54 UTC**. The team identified withdrawal problems at **08:04 UTC on Oct 1** and **paused the Intents contract at 08:33 UTC** (about 29 minutes later). It reopened on Oct 2. Other outlets say "began late Sept 30". **No block heights or tx hashes were found in any source.** | search summaries of cryptotimes / shattered / kucoin insight |
| Monitoring gap | Monitoring compared origin-chain (bridge vault) assets with NEAR-side token totals, but **did not compare aggregate users' internal balances with the tokens held by the Intents contract**. | kucoin flash, cryptotimes |
| Remediation | (a) The deposit-notification feature was **removed entirely on 2026-10-07**, reportedly after a separate critical report from a white-hat researcher (reported $300k bounty). (b) A new **invariant check comparing user balances with the contract's balances, which auto-pauses** the contract on mismatch. KuCoin insight also reports that the same callback feature had been reported and patched earlier through the bug bounty before it was exploited. | shattered.io, kucoin insight, coingabbar https://www.coingabbar.com/en/near-intents-exploit-deposit-flaw-3-8m-recovered |
| Recovery | Attacker returned all funds **within 36 hours of detection** (around Oct 2), and affected users were compensated. Some outlets say "24 hours" or "48-hour deadline". | https://www.cryptotimes.io/2026/10/03/near-intents-ends-exploit-probe-after-3-8-million-is-returned/ , https://shattered.io/near-intents-hack-3-8-million-returned-2026/ |

### Checking the secondary figures

| Reported figure | Finding |
|---|---|
| $3.8M | Consistent. On-chain tracing gives $3.865M USDT (Bitquery). |
| 23 failed deposits | Consistent across every outlet that gives a count. |
| Returned within 36h | Consistent with the postmortem summaries. Early outlets say 24h or 48h. |
| Mechanism ("a deposit path notified a third-party contract ... the token contract refunded the deposit while NEAR Intents kept the ledger credit") | Consistent with the summaries **and** with the contract code (section 2). |
| Monitoring gap and auto-pause | Consistent. |
| Not in the early coverage | The affected asset was **USDT on BSC**. The notify feature was **removed on 2026-10-07** (`defuse/v0.4.5`). The contract was **paused around 08:33 UTC on 2026-10-01**. |

### Secondary sources checked

The Defiant, KuCoin flash (both URLs above), KuCoin insight, The Block (not opened), Crypto Times (three articles), ChainCatcher, TechFlow, Shattered.io (two articles), Coingabbar, Defiprime https://defiprime.com/near-intents-recovers-3-8m-after-contract-exploit , Rekt, Bitquery, Unchained, Coincentral, Cointelegraph https://cointelegraph.com/news/near-intents-says-its-identified-the-hacker-gives-48-hour-ultimatum , and thecoding.substack.com https://thecoding.substack.com/p/near-intents-ghost-fund-postmortem (fetch blocked; search summary only).

**Takeaway:** The README can state the reported figures (23 deposits, about $3.8M, returned within 36h, monitoring gap, auto-pause) and cite at least two secondary sources plus the code evidence in section 2. Before relying on these, someone still needs to open the official postmortem on X and paste its exact URL. Search for "Postmortem: NEAR Intents October 1 Exploit", posted by `@AlexAuroraDev` on 2026-10-16. Add the affected asset (USDT on BSC) and the 2026-10-07 removal of the notify feature only after checking the official text.

---

## 2. Exploit on-chain details (for the replay)

### 2.1 Patch and incident commits in `github.com/near/intents`

From the local clone; `git log --since` covers the period, and tags were fetched with `git fetch --tags`.

| Commit | Date (author TZ) | Title | Relevance |
|---|---|---|---|
| `0c1a530c8da337008e28baa9987a816249d707e6` | 2026-10-01 20:02 +0200 | `chore: reproduce bug (#362)` | **Incident reproduction.** It adds `scripts/list_nep245_tokens.sh`, which compares `intents.near.mt_supply` with `<contract>.mt_balance_of(intents.near, inner_id)` for every `nep245:` token. That is exactly Argus's check. It also adds a regression test (`mt_resolve_deposit_caps_refunds_to_deposited_amounts`). Commit message bullets include "mismatch", "TEMP change that reflects deployed state", "revert temp changes to prove that its not an issue on latest main", and "refunded amount check in mt_resolve_deposit". https://github.com/near/intents/commit/0c1a530c8da337008e28baa9987a816249d707e6 |
| `b9e1a6a017321be55d151edc27ccf2d357b1fa9b` | 2026-10-01 23:03 +0400 | `chore!(verifier): disable state init (#361)` | Hardening that day. Its link to the exploit is unclear. |
| `b8bcf7dcf88a5968e1340f533d1759c33116bda4` | 2026-10-01 22:57 +0200 | `chore: limit number of batch tokens (#364)` | Adds `BATCH_MAX_TOKENS = 10` to `mt_on_transfer` (deposits), `mt_withdraw` and transfers. This caps the size of refund logs. |
| `cbe714b331d9565426dfa2c3affd2584c0f1e4a1` | 2026-10-02 01:29 +0400 | release `defuse/v0.4.3` (#365) | First post-incident release (tag `defuse/v0.4.3`). |
| `a2dd140892b68140bf7e70814604d3ba074d656c` | 2026-10-02 16:55 +0400 | release `defuse/v0.4.4` (#368) | Tag `defuse/v0.4.4`. |
| **`16d04f6ec4401263f8914638710260da50bb46b0`** | **2026-10-07 21:51 +0400** | **`fix(verifier): remove notifications on deposits (#374)`** | **The patch the postmortem describes ("feature removed on Oct 7").** It comments out `DepositAction::Notify` handling in the NEP-141, NEP-171 and NEP-245 deposit handlers. Tag `defuse/v0.4.5`. https://github.com/near/intents/commit/16d04f6ec4401263f8914638710260da50bb46b0 |
| `153cab96d9a06415cc93d93c48cf131d24c59836` | 2026-10-12 | `feat: withdrawals and deposits guardian (#326)` | POA-factory deposit/withdrawal tracking. Related hardening, not the fix. |

Earlier history that matters:

- `d51d980` (2025-11-24, #150): "call *_on_transfer on ... and respect the returned value". This **introduced deposit notifications** (`DepositAction::Notify`).
- `9e45ccc` (2026-01-29, #198): "check refund log length limits". It adds `check_refund()`, which **panics** if the refund `mt_burn` log would exceed the 16384-byte `TOTAL_LOG_LENGTH_LIMIT` (`crates/near/utils/src/event.rs`).
- `015fa81` (2026-03-23, #240): "**cap notify refunds by the original amounts**". This change is on `main` but **not in tag `defuse/v0.4.2` (2026-02-16)**, which was the last release before the incident.

Release tags: `defuse/v0.4.2` 2026-02-16 (`6dad94cf8a9c`), `v0.4.3` 2026-10-02, `v0.4.4` 2026-10-02, `v0.4.5` 2026-10-07.

### 2.2 Vulnerable code path (INFERRED from source; the official text could not be read)

In `defuse/v0.4.2`, which was most likely the deployed revision (**UNVERIFIED**; the reproduction commit says "TEMP change that reflects deployed state" and "not an issue on latest main"), here is what happens on an NEP-245 deposit. A token contract calls `intents.near.mt_on_transfer(sender_id, previous_owner_ids, token_ids, amounts, msg)` (`defuse/src/contract/tokens/nep245/deposit.rs`, https://github.com/near/intents/blob/defuse/v0.4.2/defuse/src/contract/tokens/nep245/deposit.rs#L20-L95). It **immediately credits the ledger**: `self.deposit(receiver_id, ..., Some("deposit"))` at L67 mints the internal balance, increases `total_supplies` and emits `mt_mint`. Then, if `msg` contains a `Notify` action (L81), it calls `notify_on_transfer` on the user-chosen `receiver_id` contract and chains the callback `mt_resolve_deposit` (L116). That callback calls `resolve_deposit_internal` (`defuse/src/contract/tokens/mod.rs` L152-L224, https://github.com/near/intents/blob/defuse/v0.4.2/defuse/src/contract/tokens/mod.rs#L152-L224). It reads the "refund" amounts the receiver returned and computes `refund_amount = balance_left.min(requested_refund)`. That is **not capped by the deposited amount**, despite the `//NOTE: refunds are capped by deposited amounts` comment. It then burns those amounts with an `mt_burn` "refund" event and finally runs `MtEvent::MtBurn(..).check_refund().unwrap_or_panic_display()`, which **panics** if the refund log exceeds `TOTAL_LOG_LENGTH_LIMIT` (16384 bytes).

A malicious receiver can therefore make `mt_resolve_deposit` fail, for example by returning oversized refund amounts across many token entries. The 2026-10-01 regression test describes exactly this: "a receiver with a large balance could request refunds bigger than the deposited amounts ... so the refund `mt_burn` event grew past `TOTAL_LOG_LENGTH_LIMIT` and the whole `mt_resolve_deposit` callback failed (as happened to the deployed revision)" (https://github.com/near/intents/blob/7fe27bf7c77adf657b652e0555a4aef660b489fc/tests/src/tests/defuse/tokens/nep245/mt_deposit_resolve_gas.rs#L443-L446). Under-provisioned static gas for the callback (`mt_resolve_deposit_gas`: 4 Tgas + 2 Tgas per token) is a second way to make it fail.

When the callback fails, the `mt_on_transfer` promise chain resolves as failed. NEP-245 requires the token contract's `mt_resolve_transfer` to "**revert token transfer** if promise chain failed" (https://github.com/near/NEPs/blob/095fe98218d2272a0a285008f72bdb3e8957e613/neps/nep-0245.md). The real tokens go back to the sender, but the `deposit` credit made in the earlier, successful `mt_on_transfer` receipt is never undone. Net result per failed deposit: `owed(T)` rises by the deposit while `held(T)` stays the same. That is the ghost balance. The same `Notify` path existed for NEP-141 (`ft_on_transfer` → `ft_resolve_deposit`) and NEP-171.

The fixes were #240 (cap by deposited amount, already on main), #364 (≤10 tokens per batch) and, finally, #374 (remove `Notify` on deposits entirely).

**Note:** whether the attack used exactly the log-overflow trigger, the gas trigger, or something else is **UNVERIFIED**. All three produce the same accounting symptom, and that symptom is what Argus detects.

### 2.3 Affected token id(s): best hypothesis (UNVERIFIED)

- Outflows were **USDT on BSC** (section 1). In the NEAR Intents production token list, BSC USDT is the HOT-Omni multi-token **`nep245:v2_1.omni.hot.tg:56_2CMMyVTGZkeyNZTSvS5sarzfir6g`** (symbol USDT, **18 decimals**, BSC contract `0x55d398326f99059fF775485246999027B3197955`). Source: `src/tokens/production.json` in https://github.com/defuse-protocol/defuse-frontend/blob/594085d3c7e60ce912fb62b768b12f46c4917510/src/tokens/production.json
- Supporting this: the incident-day script checks **only `nep245:` tokens**, and early reports blamed "Omni deposit and withdrawal infrastructure" (HOT Omni is HOT's bridge).
- Against it: the deficit sits on whichever token was *deposited* in the failed deposits, which need not be the withdrawn one, because ghost balances can be swapped through solvers before withdrawal. The regression test also mentions 173-character token ids "as long as the ones seen on mainnet" (`nep245:<64-hex implicit account>:<101 chars>`). That hints at other `nep245` tokens.
- **No NEAR attacker account, attacker contract, tx hash or block height was found in any source.**

### 2.4 Block range

Not published anywhere reachable. **UNVERIFIED.** Time anchors (UTC) from the secondary sources: first exploit activity around 2026-09-29 16:14, last withdrawal 2026-10-01 05:54, pause 2026-10-01 08:33, contract reopened 2026-10-02, funds returned by about 2026-10-02.

Mainnet epochs are 43,200 blocks, with a nominal 0.6 s block time (https://github.com/near/docs/blob/e2d2ea173cf462bcf518ecae5650984a221d5183/protocol/network/epoch.mdx). So the window is roughly 37.7 h, or about 2.2×10^5 blocks. **Do not hard-code estimated heights.**

**Replay plan:**
1. Convert the UTC anchors to heights by bisecting on `header.timestamp` (nanoseconds). Use either `https://mainnet.neardata.xyz/v0/block/{h}/headers`, which serves full history from genesis 9820210 and is free (section 7), or the archival `block` RPC. About 18 requests per anchor.
2. Check which contract version was live: run `query` / `view_account` on `intents.near` at the start height and compare the `code_hash` with a build of tag `defuse/v0.4.2`.
3. At about 12 heights across [anchor − 1 h, reopen + 1 h], call `intents.near.mt_batch_supply` for the candidate tokens and the matching held methods (section 3/4). Start with `nep245:v2_1.omni.hot.tg:56_2CMMyVTGZkeyNZTSvS5sarzfir6g` and the other BSC/HOT tokens. If none shows a deficit, run the full `mt_tokens` sweep at a single post-exploit, pre-pause height (2026-10-01 around 06:00 UTC) to find the deficit token. This is the same method as `scripts/list_nep245_tokens.sh`.
4. Bisect the first height where `gap < 0`, and fetch neardata blocks only around the deficit's step changes. Look for `intents.near` `mt_mint` "deposit" logs whose transaction also contains a failed `mt_resolve_deposit` receipt. That gives the 23 deposit tx hashes from first principles.
5. Cache everything. Budget: about 60 archival calls plus a few hundred neardata calls.

---

## 3. `intents.near` view methods (owed side)

Source of the trait: `crates/near/nep245/src/core.rs` and `enumeration.rs`. Implementation: `contracts/defuse/src/contract/tokens/nep245/core.rs` and `enumeration.rs` at `7fe27bf7c77adf657b652e0555a4aef660b489fc`. The same signatures exist at tags `defuse/v0.4.2` (paths without the `contracts/` prefix) and `defuse/v0.4.5`.

```rust
// crates/near/nep245/src/core.rs L45-L53
// https://github.com/near/intents/blob/7fe27bf7c77adf657b652e0555a4aef660b489fc/crates/near/nep245/src/core.rs#L45-L53
fn mt_token(&self, token_ids: Vec<TokenId>) -> Vec<Option<Token>>;
fn mt_balance_of(&self, account_id: AccountId, token_id: TokenId) -> U128;
fn mt_batch_balance_of(&self, account_id: AccountId, token_ids: Vec<TokenId>) -> Vec<U128>;
fn mt_supply(&self, token_id: TokenId) -> Option<U128>;
fn mt_batch_supply(&self, token_ids: Vec<TokenId>) -> Vec<Option<U128>>;

// crates/near/nep245/src/enumeration.rs L16, L27-L32
fn mt_tokens(&self, from_index: Option<U128>, limit: Option<u32>) -> Vec<Token>;
fn mt_tokens_for_owner(&self, account_id: AccountId, from_index: Option<U128>, limit: Option<u32>) -> Vec<Token>;
```

Implementation (https://github.com/near/intents/blob/7fe27bf7c77adf657b652e0555a4aef660b489fc/contracts/defuse/src/contract/tokens/nep245/core.rs#L138-L149):

```rust
fn mt_supply(&self, token_id: defuse_nep245::TokenId) -> Option<U128> {
    Some(U128(
        self.total_supplies.amount_for(&token_id.parse().ok()?),
    ))
}
fn mt_batch_supply(&self, token_ids: Vec<defuse_nep245::TokenId>) -> Vec<Option<U128>> {
    token_ids.into_iter().map(|token_id| self.mt_supply(token_id)).collect()
}
```

| Method | JSON args | JSON return |
|---|---|---|
| `mt_supply` | `{"token_id":"nep141:wrap.near"}` | `"123..."` (decimal string, u128). **`null` only if the id fails to parse** (bad prefix or bad account id). A well-formed but **unknown** token returns **`"0"`, not `null`** (`amount_for` → `unwrap_or_default`). |
| `mt_batch_supply` | **`{"token_ids":["nep141:wrap.near", "..."]}`** (yes, key `token_ids`) | `["123", null, "0", ...]`, positional |
| `mt_balance_of` | `{"account_id":"alice.near","token_id":"nep141:wrap.near"}` | `"123"` (unparseable id → `"0"`) |
| `mt_batch_balance_of` | `{"account_id":"alice.near","token_ids":[...]}` | `["123", ...]` |
| `mt_tokens` | `{"from_index":"0","limit":500}` (`from_index` is a **string** U128, `limit` a JSON number u32; both optional) | `[{"token_id":"nep141:wrap.near"}, ...]`. `owner_id` is omitted (always `None` here). |
| `mt_tokens_for_owner` | `{"account_id":"...","from_index":"0","limit":100}` | same `Token` shape |
| `mt_token` | `{"token_ids":[...]}` | `[{"token_id":"..."} or null, ...]` (`null` if not in `total_supplies`) |

Notes:
- **`mt_tokens` exists and enumerates `total_supplies`** (https://github.com/near/intents/blob/7fe27bf7c77adf657b652e0555a4aef660b489fc/contracts/defuse/src/contract/tokens/nep245/enumeration.rs#L8-L29). Zero-supply tokens are removed from the map automatically (`DefaultMap::entry_or_default` removes entries that return to default, `crates/map-utils/src/cleanup.rs` L16-L21), so `mt_tokens` lists tokens with **non-zero supply**. The NEAR Intents team pages it with `limit` 5000 in `scripts/list_nep245_tokens.sh`. The total count is **UNVERIFIED**. Order is storage-iteration order, so use `from_index` paging and do not assume sorting.
- `BATCH_MAX_TOKENS = 10` (`contracts/defuse/core/src/tokens.rs`) applies only to deposits, withdrawals and transfers, **not** to view methods. The batch size for `mt_batch_supply` is limited by view gas and response size only. Start with 50–100 per call, and measure.
- Token id parsing (`crates/primitives/token-id/src/lib.rs` L85-L95, `nep245.rs` L46-L51): split at the first `:` to get the standard. For `nep245`, the contract is everything up to the **next** `:` and the inner id is **the remainder, which may contain `:`**. Standards: `nep141`, `nep171`, `nep245`, `imt`.
- **`imt:` tokens ("intent mintable", `imt:<minter_id>:<token_id>`) have no external backing by design.** They are compiled in only with the `imt` / `far` feature. The default reproducible build is `--features=contract` (no `imt`), see `contracts/defuse/Cargo.toml`. Whether `intents.near` has them is **UNVERIFIED**. Argus must skip `imt:` (and `nep171:`) ids.
- `owed(T)` = `mt_supply(T)` includes all internal holders, including the fee collector. That is the right "owed" quantity.

**Takeaway:** `probe owed` should pass. Use `mt_batch_supply({"token_ids":[...]})`, and treat `"0"` and `null` differently: `null` means a malformed id, which is a bug to log. Use `mt_tokens` paging as discovery option 1. The event-delta fallback is unlikely to be needed.

---

## 4. Held-side methods

- **NEP-141:** `function ft_balance_of(account_id: string): string`. It "Returns the balance of an account in string form representing a value as an unsigned 128-bit integer. If the account doesn't exist must returns `"0"`." Source: https://github.com/near/NEPs/blob/095fe98218d2272a0a285008f72bdb3e8957e613/neps/nep-0141.md (View Methods). Call: `<contract>.ft_balance_of({"account_id":"intents.near"})` returns `"123"`.
- **NEP-245:** `function mt_balance_of(account_id: string, token_id: string): string` and `mt_batch_balance_of(account_id, token_ids): string[]`. Source: https://github.com/near/NEPs/blob/095fe98218d2272a0a285008f72bdb3e8957e613/neps/nep-0245.md . Call: `<contract>.mt_balance_of({"account_id":"intents.near","token_id":"<inner_id>"})`.
- **The NEAR Intents team uses exactly this held-side call.** `scripts/list_nep245_tokens.sh` (added 2026-10-01) runs `near contract call-function as-read-only "$CONTRACT" mt_balance_of json-args '{"account_id":"intents.near","token_id":"$TOKEN"}' ... at-block-height $BLOCK` and flags `mt_supply > balance` as `MISMATCH` (https://github.com/near/intents/blob/7fe27bf7c77adf657b652e0555a4aef660b489fc/scripts/list_nep245_tokens.sh).
- **HOT Omni contract:** `v2_1.omni.hot.tg`. HOT's docs describe it as the NEAR contract that "stores, mints, and burns omni-assets" (https://hot-labs.gitbook.io/hot-protocol/hot-omni-balance , search summary). In the Intents token list, every HOT asset id is `nep245:v2_1.omni.hot.tg:<chainId>_<base58-ish id>`. Examples from `production.json` (defuse-frontend @594085d):
  - `nep245:v2_1.omni.hot.tg:56_2CMMyVTGZkeyNZTSvS5sarzfir6g`: USDT on BSC (chain 56), 18 decimals. Contract `v2_1.omni.hot.tg`, inner id `56_2CMMyVTGZkeyNZTSvS5sarzfir6g`.
  - `nep245:v2_1.omni.hot.tg:56_11111111111111111111`: native BNB, 18 decimals.
  - `nep245:v2_1.omni.hot.tg:137_3hpYoaLtt8MP1Z2GH1U473DMRKgr`: USDT on Polygon, 6 decimals.
  - `nep245:v2_1.omni.hot.tg:1117_`: native TON, 9 decimals. **The inner id is literally `1117_`**, so the parser must allow an empty suffix.
  - `nep245:v2_1.omni.hot.tg:1100_111bzQBB5v7AhLyPMDwS8uJgQV24KaAPXtwyVWu2KXbbfQU6NXRCz`: XLM, 7 decimals.
- Whether `v2_1.omni.hot.tg` implements `mt_batch_balance_of` (to fetch all HOT tokens in one call) is **UNVERIFIED**; its source was not found. Try it in `probe held` and fall back to per-token `mt_balance_of`.
- Native NEAR is not a separate Intents token. Native deposits are wrapped into `nep141:wrap.near` (`contracts/defuse/src/contract/tokens/nep141/native.rs`), so `held(wNEAR) = wrap.near.ft_balance_of(intents.near)`.

**Takeaway:** Use `ft_balance_of` for `nep141:<c>` and `mt_balance_of(intents.near, <inner_id>)` on `<c>` for `nep245:<c>:<inner_id>`, both at the same `block_id`. Group per contract, and try `mt_batch_balance_of` on `v2_1.omni.hot.tg`.

---

## 5. Token ID formats and token discovery

### 5.1 Documented formats

From https://docs.near-intents.org/integration/verifier-contract/deposits-and-withdrawals/balances.md (search summary; direct fetch blocked):
- NEP-141: `nep141:<contract>`, e.g. `nep141:wrap.near`
- NEP-171: `nep171:<contract>:<token_id>`, e.g. `nep171:coolnfts.near:rock.near`
- NEP-245: `nep245:<contract>:<token_id>`, e.g. `nep245:mygame.near:shield.near`
- Query with `intents.near.mt_balance_of` / `mt_batch_balance_of`. Balances come back in the smallest unit.

The code adds a fourth prefix, `imt:` (section 3).

### 5.2 1Click supported-tokens endpoint

- **Endpoint:** `GET https://1click.chaindefuser.com/v0/tokens`. Sources: the 1Click OpenAPI spec (`servers: - url: https://1click.chaindefuser.com`, `paths: /v0/tokens: get: operationId getTokens`) in https://github.com/defuse-protocol/one-click-sdk-go/blob/42ad1e6c8172b552c243270d3f5318d50c209813/api/openapi.yaml , and the docs page https://docs.near-intents.org/integration/distribution-channels/1click-api/quickstart/making-a-request (search summary: "take the asset id from /v0/tokens").
- **Auth:** **none required.** In the spec, `/v0/tokens` has **no `security` entry**, while `/v0/quote`, `/v0/status` and the others list `JWT-auth` (bearer, "Enter JWT token (optional)").
- **Rate limit:** not documented in the spec. **UNVERIFIED.** Once a day is enough.
- **Response:** a JSON array of `TokenResponse`:

```yaml
# from the OpenAPI spec (example values are the spec's own, dated 2025-03-28)
TokenResponse:
  example: {symbol: wNEAR, blockchain: near, assetId: "nep141:wrap.near", price: 2.79,
            decimals: 24, priceUpdatedAt: "2025-03-28T12:23:00.07Z", contractAddress: wrap.near}
  properties:
    assetId:        string            # Intents token id, e.g. nep141:wrap.near
    decimals:       number
    blockchain:     string enum       # near, eth, base, arb, btc, sol, ton, dash, doge, xrp, zec, gnosis, bera, bsc, pol, tron, sui, op, avax, cardano, ltc, xlayer, monad, bch, adi, ...
    symbol:         string
    price:          number            # USD, float
    priceUpdatedAt: date-time
    contractAddress: string (optional) # origin-chain address; absent for natives
```

- **Live count and live sample: NOT obtained.** WebFetch and curl cannot reach the host. A third-party repository reports "`GET https://1click.chaindefuser.com/v0/tokens` — 186 assets across 35 chains" as of about 2026-09 (https://github.com/BonneVoyager/price-check-tool/blob/de47a8d056ce27892780e728c70eb4ea2f07d1f6/README.md , L1262). **UNVERIFIED** for today. The only real sample entry I can quote is the spec example above. I did not invent the other two.
- Caveats: 1Click lists *swappable* assets, not everything `intents.near` holds. `mt_tokens` is the ground truth. The frontend list also contains `1cs_v1:` ids (e.g. `1cs_v1:near:nep141:zec.omft.near`), which are **not** Intents token ids. Discovery must keep only `nep141:` and `nep245:` ids.

### 5.3 Candidate `config/tokens.yaml`

Source for every row: NEAR Intents' own production token list, `src/tokens/production.json` in https://github.com/defuse-protocol/defuse-frontend/blob/594085d3c7e60ce912fb62b768b12f46c4917510/src/tokens/production.json (146 entries; 111 `nep141`, 32 `nep245`, 3 `1cs_v1`). Rows were chosen by the list's `tvol:` (trading-volume rank) and `mc:` tags. `decimals` is the NEAR-side decimals from that file. **`probe held` should confirm each one** with `ft_metadata` / mt metadata; that was not done here.

```yaml
# config/tokens.yaml — source: defuse-protocol/defuse-frontend@594085d src/tokens/production.json (2026-10-06)
# held(T): nep141 -> <contract>.ft_balance_of(intents.near); nep245 -> <contract>.mt_balance_of(intents.near, <inner_id>)
tokens:
  - {id: "nep141:wrap.near", symbol: wNEAR, decimals: 24, standard: nep141, contract: wrap.near, inner_id: null}
  - {id: "nep141:17208628f84f5d6ad33f0da3bbbeb27ffcb398eac501a31bd6ad2011e36133a1", symbol: USDC, decimals: 6, standard: nep141, contract: 17208628f84f5d6ad33f0da3bbbeb27ffcb398eac501a31bd6ad2011e36133a1, inner_id: null}
  - {id: "nep141:usdt.tether-token.near", symbol: USDT, decimals: 6, standard: nep141, contract: usdt.tether-token.near, inner_id: null}
  - {id: "nep141:eth-0xdac17f958d2ee523a2206206994597c13d831ec7.omft.near", symbol: USDT, decimals: 6, standard: nep141, contract: eth-0xdac17f958d2ee523a2206206994597c13d831ec7.omft.near, inner_id: null}   # USDT (Ethereum)
  - {id: "nep141:eth-0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48.omft.near", symbol: USDC, decimals: 6, standard: nep141, contract: eth-0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48.omft.near, inner_id: null}   # USDC (Ethereum)
  - {id: "nep141:base-0x833589fcd6edb6e08f4c7c32d4f71b54bda02913.omft.near", symbol: USDC, decimals: 6, standard: nep141, contract: base-0x833589fcd6edb6e08f4c7c32d4f71b54bda02913.omft.near, inner_id: null}  # USDC (Base)
  - {id: "nep141:arb-0xaf88d065e77c8cc2239327c5edb3a432268e5831.omft.near", symbol: USDC, decimals: 6, standard: nep141, contract: arb-0xaf88d065e77c8cc2239327c5edb3a432268e5831.omft.near, inner_id: null}   # USDC (Arbitrum)
  - {id: "nep141:sol-5ce3bf3a31af18be40ba30f721101b4341690186.omft.near", symbol: USDC, decimals: 6, standard: nep141, contract: sol-5ce3bf3a31af18be40ba30f721101b4341690186.omft.near, inner_id: null}  # USDC (Solana)
  - {id: "nep141:sol-c800a4bd850783ccb82c2b2c7e84175443606352.omft.near", symbol: USDT, decimals: 6, standard: nep141, contract: sol-c800a4bd850783ccb82c2b2c7e84175443606352.omft.near, inner_id: null}  # USDT (Solana)
  - {id: "nep141:tron-d28a265909efecdcee7c5028585214ea0b96f015.omft.near", symbol: USDT, decimals: 6, standard: nep141, contract: tron-d28a265909efecdcee7c5028585214ea0b96f015.omft.near, inner_id: null}  # USDT (Tron)
  - {id: "nep141:eth.omft.near", symbol: ETH, decimals: 18, standard: nep141, contract: eth.omft.near, inner_id: null}
  - {id: "nep141:base.omft.near", symbol: ETH, decimals: 18, standard: nep141, contract: base.omft.near, inner_id: null}       # ETH (Base)
  - {id: "nep141:arb.omft.near", symbol: ETH, decimals: 18, standard: nep141, contract: arb.omft.near, inner_id: null}         # ETH (Arbitrum)
  - {id: "nep141:eth.bridge.near", symbol: ETH, decimals: 18, standard: nep141, contract: eth.bridge.near, inner_id: null}     # Rainbow-bridged ETH
  - {id: "nep141:nbtc.bridge.near", symbol: BTC, decimals: 8, standard: nep141, contract: nbtc.bridge.near, inner_id: null}   # BTC via NEAR Omni (current)
  - {id: "nep141:btc.omft.near", symbol: BTC, decimals: 8, standard: nep141, contract: btc.omft.near, inner_id: null}         # "BTC (LEGACY)", deprecated in UI but may still hold balances
  - {id: "nep141:eth-0x2260fac5e5542a773aa44fbcfedf7c193bc2c599.omft.near", symbol: WBTC, decimals: 8, standard: nep141, contract: eth-0x2260fac5e5542a773aa44fbcfedf7c193bc2c599.omft.near, inner_id: null}
  - {id: "nep141:sol.omft.near", symbol: SOL, decimals: 9, standard: nep141, contract: sol.omft.near, inner_id: null}
  - {id: "nep141:zec.omft.near", symbol: ZEC, decimals: 8, standard: nep141, contract: zec.omft.near, inner_id: null}
  - {id: "nep141:doge.omft.near", symbol: DOGE, decimals: 8, standard: nep141, contract: doge.omft.near, inner_id: null}
  - {id: "nep141:xrp.omft.near", symbol: XRP, decimals: 6, standard: nep141, contract: xrp.omft.near, inner_id: null}
  - {id: "nep141:tron.omft.near", symbol: TRX, decimals: 6, standard: nep141, contract: tron.omft.near, inner_id: null}
  - {id: "nep245:v2_1.omni.hot.tg:56_2CMMyVTGZkeyNZTSvS5sarzfir6g", symbol: USDT, decimals: 18, standard: nep245, contract: v2_1.omni.hot.tg, inner_id: "56_2CMMyVTGZkeyNZTSvS5sarzfir6g"}   # USDT (BSC), prime replay candidate
  - {id: "nep245:v2_1.omni.hot.tg:56_2w93GqMcEmQFDru84j3HZZWt557r", symbol: USDC, decimals: 18, standard: nep245, contract: v2_1.omni.hot.tg, inner_id: "56_2w93GqMcEmQFDru84j3HZZWt557r"}   # USDC (BSC)
  - {id: "nep245:v2_1.omni.hot.tg:56_11111111111111111111", symbol: BNB, decimals: 18, standard: nep245, contract: v2_1.omni.hot.tg, inner_id: "56_11111111111111111111"}
  - {id: "nep245:v2_1.omni.hot.tg:137_3hpYoaLtt8MP1Z2GH1U473DMRKgr", symbol: USDT, decimals: 6, standard: nep245, contract: v2_1.omni.hot.tg, inner_id: "137_3hpYoaLtt8MP1Z2GH1U473DMRKgr"}  # USDT (Polygon)
```

Symbols repeat across chains (USDT ×4, USDC ×5, ETH ×4). The dashboard needs a `chain` label as well as the symbol.

**Takeaway:** Discovery order: (1) `intents.near.mt_tokens` paging, which exists; (2) `GET https://1click.chaindefuser.com/v0/tokens` (no auth) for symbol, decimals and price metadata keyed by `assetId`; (3) the YAML above. Join on the Intents id. Tokens found by `mt_tokens` that have no metadata still get owed/held raw values, with decimals fetched from `ft_metadata` or marked unknown.

---

## 6. RPC providers

### 6.1 Mainnet endpoints (docs.near.org/api/rpc/providers)

Source: https://github.com/near/docs/blob/e2d2ea173cf462bcf518ecae5650984a221d5183/api/rpc/providers.mdx (rendered at https://docs.near.org/api/rpc/providers; last commit 2026-11-01). The table, verbatim, for providers with a URL:

| Provider | Endpoint | Public | Archival | Free tier limit |
|---|---|---|---|---|
| FASTNEAR | **`https://free.rpc.fastnear.com`** | ✅ | Paid only | Not published |
| NEAR | `https://archival-rpc.mainnet.near.org` | ✅ | ✅ | "Severely rate limited", not published |
| 1RPC | `https://1rpc.io/near` | ✅ | ❌ | 200 req/day |
| Ankr | `https://rpc.ankr.com/near` | ❌ (key) | ❌ | ~30 req/s |
| BlockPI | `https://near.blockpi.network/v1/rpc/public` | ✅ | ❌ | 20 req/s |
| dRPC | `https://near.drpc.org` | ✅ | ❌ | ~2,100 CU/s |
| GetBlock / NodeReal / NOWNodes | URL needs `{api-key}` | ❌ | ❌ | (key required) |
| Intear RPC | `https://rpc.intea.rs` | ✅ | ❌ | Not published |
| Shitzu | `https://rpc.shitzuapes.xyz` | ✅ | ❌ | Not published |
| ZAN | `https://api.zan.top/node/v1/near/mainnet/` | ✅ | ❌ | ~20 req/s |

Lava, All That Node, QuickNode, Tatum, Zeeve and node101 have no public URL listed ("N/A").

**Corrections to the original plan:**
- docs.near.org lists FastNEAR's public free endpoint as **`https://free.rpc.fastnear.com`**, not `https://rpc.mainnet.fastnear.com`. FastNEAR's own docs list **`https://rpc.mainnet.fastnear.com`** (regular) and **`https://archival-rpc.mainnet.fastnear.com`** (archival), used with an optional API key (`Authorization: Bearer` or `?apiKey=`) (https://github.com/fastnear/builder-docs/blob/240ab2d80466774f1e6993c3b8b967af1b26fa91/docs/rpc/index.md). Whether `rpc.mainnet.fastnear.com` and `archival-rpc.mainnet.fastnear.com` work **without a key** is **UNVERIFIED**. docs.near.org says FastNEAR archival is "Paid Only".
- `https://rpc.mainnet.near.org` now appears only in the docs' curl example ("Making requests"), not in the provider table. Its current status is **UNVERIFIED**.
- The only free archival endpoint documented is `https://archival-rpc.mainnet.near.org`, which is severely rate limited. Keep the replay small, as planned.
- **nearcore does not support JSON-RPC batch requests.** "If the body is a JSON array, a nearcore node rejects the whole request with HTTP 400 and error code -32700". Send one request object per HTTP request (same providers.mdx). "Batched" reads must use `mt_batch_supply`-style contract batching, not JSON-RPC batching.

### 6.2 History retention on non-archival nodes

- "A regular RPC node keeps approximately 36 hours of history - 5 epochs" and "History retention | 5 epochs (~36 hours) by default; configurable" (https://github.com/near/docs/blob/e2d2ea173cf462bcf518ecae5650984a221d5183/integrations/nodes.mdx L12, L20).
- "Nodes garbage collect blocks after 5 epochs (~1.5 days)". An epoch is 43,200 blocks, nominally 7.2 h at 0.6 s per block (https://github.com/near/docs/blob/e2d2ea173cf462bcf518ecae5650984a221d5183/protocol/network/epoch.mdx L10-L13).
- The earlier "~2.5 epochs / ~60h" estimate is **not what the docs say**. Per the docs, recent `block_id` queries work for about 5 epochs (about 36 h). Providers may configure this differently, so `probe sameblock` should measure it per provider.

### 6.3 `query` / `call_function` shapes

Source: https://github.com/near/docs/blob/e2d2ea173cf462bcf518ecae5650984a221d5183/api/rpc/contracts.mdx (section "Call a Contract Function" and "Error Handling").

Request (`finality` **or** `block_id`; `block_id` is a height (number) or a block hash (string)):
```json
{"jsonrpc":"2.0","id":"argus","method":"query",
 "params":{"request_type":"call_function","block_id":123456789,
           "account_id":"intents.near","method_name":"mt_batch_supply",
           "args_base64":"<base64 of {\"token_ids\":[\"nep141:wrap.near\"]}>"}}
```

Success: `result.result` is the return value as an **array of bytes**. "Decode those bytes as UTF-8 and parse as JSON" (`[34,104,101,108,108,111,34]` → `"hello"`). Alongside it are `result.logs`, `result.block_height` and `result.block_hash`. Store `block_hash` with each snapshot as proof. (The success-response field list is standard nearcore; the docs show the `result.result` decode and the `block_height`/`block_hash`/`logs` fields in the error example.)

Errors come back in four ways. Check `error` first, then `result.error`:
- Request errors: `error.name = "REQUEST_VALIDATION_ERROR"`, `error.cause.name = "PARSE_ERROR"`.
- Internal errors: `error.name = "INTERNAL_ERROR"`, message in `error.cause.info.error_message`.
- Handler errors: `error.name = "HANDLER_ERROR"`, `error.code = -32000`, with `error.cause.name` one of `UNKNOWN_ACCOUNT`, `INVALID_ACCOUNT`, **`UNKNOWN_BLOCK`** ("Block not found"), **`GARBAGE_COLLECTED_BLOCK`** ("Block too old: use archival node"), `NO_CONTRACT_CODE` (view_code), `TOO_LARGE_CONTRACT_STATE`, and others. Example:
  ```json
  {"jsonrpc":"2.0","error":{"name":"HANDLER_ERROR","cause":{"name":"TOO_LARGE_CONTRACT_STATE","info":{...,"block_height":187440904,"block_hash":"56xE..."}},"code":-32000,"message":"Server error","data":"..."},"id":"dontcare"}
  ```
- **Contract errors are HTTP 200 with `result.error` as a string:**
  ```json
  {"jsonrpc":"2.0","result":{"block_hash":"56xEo2LorUFVNbkFhCncFSWNiobdp1kzm14nZ47b5JVW","block_height":187440904,"error":"wasm execution failed with error: MethodResolveError(MethodNotFound)","logs":[]},"id":"dontcare"}
  ```
  Missing method: `...MethodResolveError(MethodNotFound)`. Account without a contract: the message starts with `wasm execution failed with error: CompilationError(CodeDoesNotExist`. Contract panics (for example a bad token id on some token contracts) also land here.

**Takeaway:** Provider list for `config`: primary `https://free.rpc.fastnear.com`; fallbacks `https://rpc.mainnet.fastnear.com` (test keyless), `https://near.blockpi.network/v1/rpc/public` (20 rps), `https://near.drpc.org`, `https://rpc.intea.rs`, `https://rpc.mainnet.near.org` (test). Archival: `https://archival-rpc.mainnet.near.org` (plus `https://archival-rpc.mainnet.fastnear.com` if a free key works). Fall back when the response has `GARBAGE_COLLECTED_BLOCK` or `UNKNOWN_BLOCK`. Never send JSON-RPC batch arrays. Treat `result.error` as a per-call failure, not a transport failure. To keep all reads at one block, take `H` from one provider and send `block_id: H` to all of them. If a fallback answers `UNKNOWN_BLOCK` for a fresh `H` (it is lagging), retry with backoff.

---

## 7. FastNEAR neardata

Sources: https://github.com/fastnear/neardata-server/blob/7324d9131e7e8285306606ba05822b007e44c53a/README.md , the OpenAPI spec `openapi/openapi.yaml` in the same repo, and https://github.com/fastnear/builder-docs/blob/240ab2d80466774f1e6993c3b8b967af1b26fa91/docs/neardata/index.md (source of https://docs.fastnear.com/neardata).

- **Base URL:** `https://mainnet.neardata.xyz` (testnet `https://testnet.neardata.xyz`).
- **Endpoints:**
  - `/v0/block/:block_height`: finalized block as one JSON object. Returns **`null`** if the block doesn't exist (a skipped height). If the height is just ahead of final, the server **waits** for it. May **redirect** to an archive host.
  - `/v0/block/:h/headers` (only the `block` object), `/v0/block/:h/chunk/:shard_id`, `/v0/block/:h/shard/:shard_id`.
  - `/v0/block_opt/:h`: optimistic block.
  - `/v0/last_block/final`, `/v0/last_block/optimistic`: **redirects** to the latest block. The HTTP client must follow redirects.
  - `/v0/first_block`: genesis is block 9820210 on mainnet.
- **Free, no key:** "The server is free to use and doesn't require any authentication." **Rate limit: "180 requests per minute per IP"**, bandwidth limited to 1 Gbps. An optional subscription key goes in `?apiKey=` (a Bearer header breaks across redirects). An invalid key returns 401 (README) or 403 (builder-docs).
- **History:** the README says historical indexing can start "from the genesis block (`9820210` for mainnet)". So neardata covers the October 2026 window, which helps the replay event scan.
- **Response shape** (OpenAPI `BlockDocument`):
  ```
  { "block": { "author", "header": {height, hash, timestamp(ns), ...}, "chunks": [...] },
    "shards": [ { "shard_id",
                  "chunk": { ..., "transactions": [...] },
                  "receipt_execution_outcomes": [
                    { "execution_outcome": { "id": <receipt_id>, "block_hash",
                                             "outcome": { "executor_id", "logs": [string],
                                                          "status": {SuccessValue|SuccessReceiptId|Failure},
                                                          "receipt_ids", "gas_burnt", "tokens_burnt", "metadata" },
                                             "proof" },
                      "receipt": { "receipt_id", "predecessor_id", "receiver_id", "receipt": {Action:{actions,...}|Data}, "priority" } | null,
                      "tx_hash": <hash of originating transaction> } ],
                  "state_changes": [...] } ] }
  ```
  neardata adds "the `tx_hash` for every receipt in the `receipt_execution_outcomes`" (README).
- **Finding `intents.near` logs:** for each `shards[].receipt_execution_outcomes[]`, keep entries where `execution_outcome.outcome.executor_id == "intents.near"`, then parse each `outcome.logs[]` string that starts with `EVENT_JSON:`. **Count only outcomes whose `status` is `SuccessValue` or `SuccessReceiptId`.** A failed receipt's state changes are reverted, so its events must not be counted. To fetch less, once you know the `intents.near` shard, `/v0/block/:h/shard/:shard_id` returns just that shard. Shard layout can change at resharding, so re-derive it and don't hard-code it.
- **Rate check:** NEAR produces about 1.67 blocks/s, roughly 100 blocks/min. Following every final block uses about 100 of the 180 req/min, which leaves about 80 req/min to catch up after downtime. That is fine for live following, and tight for backfill (a 37 h replay window is about 2.2×10^5 blocks, roughly 21 h at full rate). Use it for targeted ranges only.

**Takeaway:** `probe events` is expected to pass. Poll `/v0/block/{h}` sequentially from `/v0/last_block/final`, follow redirects, treat `null` as a skipped height, cap at ≤150 req/min with backoff on 429, filter by `executor_id == "intents.near"` and success status.

---

## 8. Intents events

Sources: contract source (`contracts/defuse/core/src/events/mod.rs`, `crates/near/nep245/src/events.rs` @7fe27bf; `core/src/events.rs` @`defuse/v0.4.2`), NEP-245, and https://docs.near-intents.org/integration/verifier-contract/events.md (search summary only: "Based on NEP-297 ... prefixed with the string EVENT_JSON. The Verifier contract emits events using the DIP-4 standard"; the docs say "current DIP-4 version is 0.3.0").

### 8.1 Envelope

Each log line is `EVENT_JSON:` followed by `{"standard": ..., "version": ..., "event": ..., "data": ...}` (NEP-297, emitted by near-sdk's `#[near(event_json(standard = ...))]`).

- NEP-245: `standard:"nep245"`, `version:"1.0.0"`, `event` ∈ `mt_mint|mt_burn|mt_transfer`, `data` = array.
- DIP-4: `standard:"dip4"`. **The version differs by event and by contract release.** At `defuse/v0.4.2` (live during the incident) most events were `"0.3.0"`, `intents_executed` was `"0.3.1"` and `salt_rotation` `"0.4.0"`. From `v0.4.3` on (current), `public_key_added/removed`, `transfer`, `token_diff`, `intents_executed`, `ft_withdraw`, `nft_withdraw`, `mt_withdraw`, `native_withdraw`, `storage_deposit`, `imt_mint/burn` and `set_auth_by_predecessor_id` are `"0.4.3"`, while `fee_changed`, `fee_collector_changed`, `account_locked/unlocked` stay `"0.3.0"` and `salt_rotation` `"0.4.0"`. **The parser must not filter on version.** The docs' "0.3.0" is out of date.

### 8.2 NEP-245 events (`crates/near/nep245/src/events.rs`)

Amounts are decimal strings (`DisplayFromStr`). Optional fields are omitted when absent.
- `mt_mint` data item: `{owner_id, token_ids[], amounts[], memo?}`
- `mt_burn` data item: `{owner_id, authorized_id?, token_ids[], amounts[], memo?}`
- `mt_transfer` data item: `{authorized_id?, old_owner_id, new_owner_id, token_ids[], amounts[], memo?}`
- Memos used by `intents.near`: `"deposit"` on deposit mints, `"withdraw"` on withdrawal burns, `"refund"` on refund burns (`mt_resolve_deposit`) and on re-mints after failed withdrawals (`crates/near/utils/src/event.rs`, `REFUND_MEMO`). For `owed` deltas: Δsupply(T) = Σ`mt_mint` − Σ`mt_burn` for T. `mt_transfer` does not change supply.

Doc-quoted examples (NEP-245 spec, https://github.com/near/NEPs/blob/095fe98218d2272a0a285008f72bdb3e8957e613/neps/nep-0245.md):
```
EVENT_JSON:{"standard":"nep245","version":"1.0.0","event":"mt_mint","data":[{"owner_id":"foundation.near","token_ids":["aurora","proximitylabs_ft"],"amounts":["1","100"]}]}
EVENT_JSON:{"standard":"nep245","version":"1.0.0","event":"mt_burn","data":[{"owner_id":"foundation.near","token_ids":["aurora","proximitylabs_ft"],"amounts":["1","100"]}]}
EVENT_JSON:{"standard":"nep245","version":"1.0.0","event":"mt_transfer","data":[{"old_owner_id":"user1.near","new_owner_id":"user2.near","token_ids":["meme"],"amounts":["1"],"memo":"have fun!"}]}
```
On `intents.near` the `token_ids` are full Intents ids (e.g. `nep141:wrap.near`).

### 8.3 DIP-4 events (`contracts/defuse/core/src/events/mod.rs`)

`DefuseEvent` variants (snake_case event names): `public_key_added`, `public_key_removed`, `fee_changed`, `fee_collector_changed`, `transfer`, `token_diff`, `intents_executed`, `ft_withdraw`, `nft_withdraw`, `mt_withdraw`, `native_withdraw`, `storage_deposit`, `imt_mint`*, `imt_burn`* (*`imt` feature only), `account_locked`, `account_unlocked`, `set_auth_by_predecessor_id`, `salt_rotation`.

Data items for the per-intent events are arrays of `MaybeIntentEvent<AccountEvent<T>>`, flattened:
`{"intent_hash": <base58, omitted for direct fn-calls>, "account_id": "...", ...fields of T}`. At v0.4.2 `intent_hash` was always present (`IntentEvent`).

| event | fields of T (flattened) |
|---|---|
| `transfer` | `receiver_id`, `tokens: {token_id: "amount"}`, `memo?` |
| `token_diff` | `diff: {token_id: "signed amount"}`, `memo?`, `referral?`, `fees_collected?: {token_id:"amount"}` (omitted when empty) |
| `intents_executed` | `nonce` (base64) |
| `ft_withdraw` | `token`, `receiver_id`, `amount` (string), `memo?`, `msg?`, `storage_deposit?`, `min_gas?` |
| `mt_withdraw` | `token`, `receiver_id`, `token_ids[]`, `amounts[]` (strings), `memo?`, `msg?`, `storage_deposit?`, `min_gas?` |
| `native_withdraw` | `receiver_id`, `amount` (yoctoNEAR) |
| `storage_deposit` | `contract_id`, `deposit_for_account_id`, `amount` |

Example lines. **These were built from the serde definitions and the repository's own test fixtures (`contracts/defuse/core/src/events/tests/mod.rs`); they are not captured mainnet logs**:
```
EVENT_JSON:{"standard":"dip4","version":"0.4.3","event":"token_diff","data":[{"intent_hash":"<base58>","account_id":"alice.near","diff":{"nep141:token.near":"100","nep245:token.near:abcd":"-200"},"memo":"test token diff","referral":"alice.near","fees_collected":{"nep141:token.near":"100"}}]}
EVENT_JSON:{"standard":"dip4","version":"0.4.3","event":"transfer","data":[{"intent_hash":"<base58>","account_id":"alice.near","receiver_id":"alice.near","tokens":{"nep141:token.near":"100"},"memo":"test transfer"}]}
EVENT_JSON:{"standard":"dip4","version":"0.4.3","event":"intents_executed","data":[{"intent_hash":"<base58>","account_id":"alice.near","nonce":"<base64>"}]}
EVENT_JSON:{"standard":"dip4","version":"0.4.3","event":"ft_withdraw","data":[{"intent_hash":"<base58>","account_id":"alice.near","token":"token.near","receiver_id":"alice.near","amount":"100","memo":"test ft withdraw","msg":"test message","storage_deposit":"100","min_gas":"10000000000000"}]}
EVENT_JSON:{"standard":"dip4","version":"0.4.3","event":"mt_withdraw","data":[{"intent_hash":"<base58>","account_id":"alice.near","token":"alice.near","receiver_id":"alice.near","token_ids":["nep141:token.near"],"amounts":["100"]}]}
EVENT_JSON:{"standard":"dip4","version":"0.4.3","event":"native_withdraw","data":[{"intent_hash":"<base58>","account_id":"alice.near","receiver_id":"alice.near","amount":"100000000000000000000000000"}]}
```
The serialization of `storage_deposit` and `min_gas` (string or number) is **UNVERIFIED**. Capture real mainnet lines in `probe events` and commit them as test fixtures.

**Takeaway:** Parse the envelope generically: prefix `EVENT_JSON:`, then switch on `standard` + `event`, and ignore `version`. For owed deltas, use only NEP-245 `mt_mint` / `mt_burn` from successful `intents.near` receipts. Use the DIP-4 events for incident context. Replace the built examples above with real captured lines as soon as neardata is reachable.

---

## 9. Free USD price source (P1)

- **1Click `/v0/tokens` includes `price` (USD, float) and `priceUpdatedAt` for each `assetId`, with no auth** (section 5.2). It is keyed directly by Intents token id, so no symbol mapping is needed. Its rate limit is undocumented (**UNVERIFIED**). Refreshing every 5–15 min should be modest. The 1Click service also charges 0.2% on unauthenticated *quotes* (third-party note, price-check-tool README L519), which doesn't matter for `/v0/tokens`.
- `price` is a float. Use it **only** for display or USD-threshold conversion, never in gap math. Keep gap math in `math/big` on raw units.
- Tokens that `mt_tokens` finds but 1Click doesn't list will have no USD value. Show "n/a".
- Alternative (not researched in depth): CoinGecko's public API, which needs a mapping and has its own free-tier rules. **UNVERIFIED.**

**Takeaway:** Use 1Click `/v0/tokens` as the P1 price source. Cache it and refresh at most every 5 minutes.

---

## Hosts to allowlist

Runtime (Argus itself):
- `free.rpc.fastnear.com`: FastNEAR free RPC (primary)
- `rpc.mainnet.fastnear.com`: FastNEAR regular RPC (fallback; keyless use unverified)
- `archival-rpc.mainnet.fastnear.com`: FastNEAR archival (replay; may need a key)
- `rpc.mainnet.near.org`: NEAR RPC (fallback; status unverified)
- `archival-rpc.mainnet.near.org`: NEAR archival (replay)
- `near.blockpi.network`: BlockPI public RPC (fallback)
- `near.drpc.org`: dRPC public RPC (fallback)
- `rpc.intea.rs`: Intear RPC (fallback)
- `mainnet.neardata.xyz`: block data / events. Redirects may point to other neardata hosts; **watch for redirect targets in check 4 and allowlist them too (UNVERIFIED names)**.
- `1click.chaindefuser.com`: token discovery and prices
- `api.telegram.org`: Telegram alerts (P0)

Research and dev only (to finish this document and to read docs):
- `docs.near-intents.org`, `docs.near.org`, `docs.fastnear.com`, `x.com` (official postmortem), `nearblocks.io` / `api.nearblocks.io` (explorer links for incidents, optional), `api.github.com` (PR descriptions for #362, #364 and #374)

---

## Open questions

1. **Official postmortem URL and verbatim text.** Someone still needs to open `@AlexAuroraDev` / `@near_intents` on X (2026-10-16) and confirm root cause, amounts, the asset and any block heights or tx hashes. Allowlisting `x.com` or `docs.near-intents.org` would let an agent do it.
2. **Affected Intents token id(s) and the 23 deposit tx hashes / block range.** Not public in any reachable source. Best hypothesis: `nep245:v2_1.omni.hot.tg:56_2CMMyVTGZkeyNZTSvS5sarzfir6g` (USDT on BSC via HOT Omni). Confirm with archival `mt_supply` vs held reads (section 2 plan).
3. **Which `intents.near` code version was live on 2026-09-29 to 2026-10-01?** Likely `defuse/v0.4.2`. Confirm via `code_hash` at those heights.
4. **Keyless access** to `rpc.mainnet.fastnear.com` and `archival-rpc.mainnet.fastnear.com`, and whether `rpc.mainnet.near.org` still serves the public.
5. **Real retention window** of each free non-archival provider (docs: about 5 epochs / about 36 h). Measure in check 3.
6. **Number of tokens** returned by `intents.near.mt_tokens` today. The team's script pages 5000 at a time, so it may be large, with many dust or spam `nep245` tokens. Decide whether to monitor all of them or only those with metadata or price.
7. Does `v2_1.omni.hot.tg` support `mt_batch_balance_of`? Do any `nep141` tokens held by `intents.near` have non-standard balance semantics (rebasing, fee-on-transfer) that would cause permanent benign gaps?
8. **1Click `/v0/tokens` live schema and count** (spec dated 2026-04; third party reported 186 assets in about 2026-09) and its rate limit.
9. The neardata redirect target hostnames (for the allowlist) and whether `/v0/block/{h}` for October 2026 heights is served without a key.
10. Exact JSON serialization of `NearToken` and `Gas` fields in DIP-4 withdraw events (string vs number). Capture real logs.
