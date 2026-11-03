# Synthetic fixtures

Everything in this directory is SYNTHETIC. None of it was captured from
mainnet or from a neardata server (those hosts are unreachable from the
development sandbox). The files are hand-written to follow the neardata
`BlockDocument` shape described in docs/research.md section 7, and the event
log lines follow the serde definitions in the near/intents contract source
(see docs/research.md section 8). Hashes, receipt ids and account names are
made up.

They must be replaced or supplemented with real captured blocks once
capability check 4 can run against a live neardata server.

- `block_events.json`: a block with a successful intents.near mint and
  token_diff, a Failure receipt carrying a mint (must be ignored), a receipt on
  another contract carrying an EVENT_JSON log (must be ignored), and a
  SuccessReceiptId receipt with a burn and a non-event log.
- `block_null.json`: the literal body for a skipped height.
