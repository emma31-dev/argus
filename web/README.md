# web

Static SvelteKit dashboard for Argus. It reads the [API](../docs/api.md) from the browser and has no server of its own.

```sh
pnpm install
pnpm dev:mock    # mock API on :8080 plus vite on :5173, no backend needed
pnpm dev         # vite only, proxies /v1 to the Go server on :8080
pnpm check
pnpm test
pnpm build       # writes build/
```

The mock in `dev/mock-api.mjs` serves made-up numbers in the real API shape, and every screenshot in `docs/screenshots` comes from it. Nothing in `src/` imports it. To switch states, use `curl 'localhost:8080/__mock?scenario=ok|incident&replay=0|1&stale=0|1&down=0|1'`.

Build-time settings: `PUBLIC_API_BASE` (API origin, default same origin) and `BASE_PATH` (serve under a sub-path). `ARGUS_API_PROXY` changes the dev proxy target.

Amounts arrive as integer strings in the token's smallest unit and stay `BigInt` until chart drawing (`src/lib/format.ts`).
