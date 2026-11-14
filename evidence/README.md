# Evidence

Committed output from real runs on 2026-11-14. Every file here was produced on this machine, not copied from a template. The SHA256 values let a reader check that a file was not edited after the run.

The raw `live-round-2026-11-14.log` is not committed. PowerShell wrote it in mixed encodings (an ASCII header followed by UTF-16LE output), so the clean UTF-8 table is the committed artefact. The log stays local at 444,858 bytes.

## Files

| File | Command | What it proves | SHA256 |
|---|---|---|---|
| [`live-round-2026-11-14.txt`](live-round-2026-11-14.txt) | `ARGUS_RPC_RPS=1 go run ./cmd/argus -once` | One mainnet round at block 224512508: 790 tokens discovered, 720 read, 70 stale, 705 green, 15 amber, 0 red | `336CBAB91D4BADB8AF378371B71F44EBDD018645E506179B323C50B546BC53AC` |
| [`live-round-2026-11-14.md`](live-round-2026-11-14.md) | Summary of the run above | The 15 amber groups, the 70 stale reasons, and what the round does not prove | `CCD6A6E0EAF874B84E9E193B842B8F2E2AAC5D875B6325C1A42642F7ABBB05F4` |
| [`go-test-2026-11-14.txt`](go-test-2026-11-14.txt) | `go test ./... -count=1` | 236 tests passed, 25 skipped, 0 failed, with per-package results | `75E6C9B1FC0F31BCFC318ECC7CAFE54D7CB6451419E8226349E07F102D331E3A` |
| [`static-checks-2026-11-14.txt`](static-checks-2026-11-14.txt) | `go vet ./...`, `CGO_ENABLED=0 go build ./...`, `go mod tidy -diff`, `gofmt -l .` | All four checks pass | `BF5C97B8AE7CC5E3605171BD29A33909EE30921E7759856B81809489D1839BAF` |
| [`dashboard-checks-2026-11-14.txt`](dashboard-checks-2026-11-14.txt) | `pnpm check`, `pnpm test`, `pnpm build` | 0 Svelte errors, 36 Vitest tests pass, static build completes | `3D400FB7C0E4F9600739A1152D2F6677172E70EE9C452D9203B1FCC229CDBB42` |
| [`verify-synthetic-2026-11-14.txt`](verify-synthetic-2026-11-14.txt) | `go run ./cmd/verify` | 12 synthetic rounds replay offline; 7 transitions and 4 alerts match `data/fixtures/synthetic/expected.json` | `B21DC12BA30BCBC6CAE6D20A00EA0B72F7903071803A1EFCE919DC4CCFBE4578` |

The dashboard log is raw terminal output, so it contains ANSI colour codes. The test counts and the final `EXIT=0` lines are still readable.

## What the live round does and does not prove

It proves that token discovery, the same-block reader, big-integer gap math, and the green/amber/stale classification ran against NEAR mainnet with no keys. It does not prove a 24/7 deployment, the October replay, origin-chain backing, or that any of the 15 amber tokens is actually insolvent. Amber means a negative gap right now. On bridged and NFT ids that can be expected because the backing sits on another chain or is out of scope.

## Reproduce the checks

```sh
go test ./... -count=1
go run ./cmd/verify
```

The live round needs network access and about 10 minutes at the default rate:

```sh
$env:ARGUS_RPC_RPS='1'
go run ./cmd/argus -once
```

The dashboard checks:

```sh
cd web
pnpm install --frozen-lockfile
pnpm check
pnpm test
pnpm build
```

## Verify a committed file

```sh
Get-FileHash evidence\live-round-2026-11-14.txt -Algorithm SHA256
```

The output must match the value in the table above. On Linux or macOS use `sha256sum`.
