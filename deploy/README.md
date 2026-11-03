# Deploying Argus

Argus has three runtime parts:

| Part | What it is | Where it can run |
|---|---|---|
| Monitor + API | Go binary (`cmd/argus`), listens on `:8080`, exposes `/healthz` and `/v1/*` | Fly.io, or any VM with Docker |
| Postgres | History of snapshots | Neon or Supabase (hosted), or the compose `postgres` service |
| Redis | Alert queue (optional) | Upstash (hosted), or the compose `redis` service, or nothing (in-memory queue, single process only) |
| Dashboard | Static SvelteKit site in `web/` | Cloudflare Pages, or the compose `dashboard` service (nginx) |

Free-tier terms, limits and pricing change often. Check each provider's current page before you sign up. This guide does not state prices.

## Option A: Fly.io for the monitor, hosted free-tier data stores, Cloudflare Pages for the dashboard

This is the layout the PRD targets: the monitor runs continuously, the dashboard is static.

### 1. Postgres (Neon or Supabase)

1. Create a free project on Neon or Supabase.
2. Copy the connection string. Use `sslmode=require` (both providers require TLS). Example shape:
   `postgres://USER:PASSWORD@HOST/DBNAME?sslmode=require`
3. Keep it as a Fly secret (step 3), not in any file.

### 2. Redis (Upstash, optional)

1. Create a free Redis database on Upstash.
2. Copy the TLS connection URL that Upstash shows. It uses the `rediss://` scheme.
3. Set it as the `REDIS_URL` secret. Leave `REDIS_URL` unset to use the in-memory queue instead. That is fine for one monitor process.

### 3. Monitor on Fly.io

The app config is `deploy/fly/fly.toml` (app name `argus-monitor`, region `ams`, one machine kept running, health check on `/healthz`). Run everything from the repository root:

```sh
fly auth login
fly config validate -c deploy/fly/fly.toml
fly apps create argus-monitor            # skip if it already exists, or change app in fly.toml
fly secrets set -c deploy/fly/fly.toml \
  DATABASE_URL='postgres://...?sslmode=require' \
  REDIS_URL='rediss://...'               # omit if not using Redis
fly deploy -c deploy/fly/fly.toml
curl https://argus-monitor.fly.dev/healthz
```

Notes:

- `auto_stop_machines = "off"` keeps the monitor running. Machines that sleep stop the snapshot loop, so that setting matters for the PRD's "running continuously" goal.
- Telegram alerts need `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` as secrets, so set them the same way.
- The Docker image does not include `config/` (it is mounted in compose). If the monitor needs `config/tokens.yaml` on Fly, either add a `COPY` for it in the Dockerfile or set `ARGUS_TOKENS_FILE` to a path that exists in the image. Check this before the first deploy.
- The health check runs on `/healthz`. Fly marks the machine unhealthy if it fails.

### 4. Dashboard on Cloudflare Pages

Create a Pages project connected to this repository with these settings:

| Setting | Value |
|---|---|
| Root directory | `web` |
| Build command | `pnpm install --frozen-lockfile && pnpm build` |
| Build output directory | `build` (relative to the root directory, so the output is `web/build`) |
| Environment variable | `PUBLIC_API_BASE=https://argus-monitor.fly.dev` (use your Fly app URL) |

Set the Node version to 22 in the Pages environment variables (`NODE_VERSION=22`) and pnpm to 10 (`PNPM_VERSION=10.28.0`).

For the SPA fallback and security headers, copy the two files into the build output:

- `deploy/cloudflare/_redirects` gives unknown paths the dashboard shell.
- `deploy/cloudflare/_headers` sets the CSP. Its `connect-src` must name your Fly host, and it must match `PUBLIC_API_BASE`.

The simplest way is to copy both into `web/static/` so the SvelteKit build includes them at the output root. Check the copied file's location in the deployed output after your first build.

The API sends `Access-Control-Allow-Origin` for `ARGUS_CORS_ORIGINS`. The default `*` works. To tighten it, set `ARGUS_CORS_ORIGINS` on the Fly app to your Pages origin.

## Option B: docker compose on a small VM

Any Linux VM with Docker and the compose plugin works, including the free-tier VMs from Oracle Cloud or Google Cloud. Check each provider's current free-tier eligibility, region and shape before you rely on it. Free-tier VMs often have small memory, so watch the first few snapshot cycles.

```sh
git clone <this repo> argus && cd argus
cp .env.example .env          # edit: set POSTGRES_PASSWORD, and TELEGRAM_* if you want alerts
docker compose up -d --build  # postgres, redis, argus on :8080, dashboard on :3000
docker compose ps
curl http://localhost:8080/healthz
```

Put a reverse proxy with TLS in front of ports 3000 and 8080 if the VM is public, and change `POSTGRES_PASSWORD` from the default. Keep Postgres and Redis bound to `127.0.0.1` (the compose file already does this).

The compose stack reads `config/` and `evidence/` read-only. Keep `.env` on the VM only.

## Checklist before going public

- No secrets in git. `.env` and `*.env` are ignored, and `.env.example` holds defaults only.
- `POSTGRES_PASSWORD` is not the default.
- `ARGUS_RPC_URLS` only lists public providers that the PRD names, and the request rate (`ARGUS_RPC_RPS`) respects their free-tier limits.
- `GET /healthz` returns 200 from the public URL.
- `ARGUS_PUBLIC_URL` points at the dashboard, so alert links work.
