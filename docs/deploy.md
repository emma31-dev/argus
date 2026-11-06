# Deploying

Runtime pieces: the Go monitor (`cmd/argus`, port 8080, `/healthz` and `/v1/*`), Postgres for history, an optional Redis for the alert queue (without it the queue lives in memory, which is fine for one process), and the static dashboard in `web/`.

Free-tier terms change often, so check each provider before signing up.

## Fly.io + hosted Postgres/Redis + Cloudflare Pages

**Postgres.** Create a free Neon or Supabase project and copy the connection string with `sslmode=require`, e.g. `postgres://USER:PASSWORD@HOST/DBNAME?sslmode=require`. It goes in a Fly secret, never in a file.

**Redis (optional).** Create an Upstash database and copy its `rediss://` URL.

**Monitor.** The config is `deploy/fly/fly.toml` (app `argus-monitor`, region `ams`, one machine that never sleeps, health check on `/healthz`). From the repo root:

```sh
fly auth login
fly apps create argus-monitor
fly secrets set -c deploy/fly/fly.toml \
  DATABASE_URL='postgres://...?sslmode=require' \
  REDIS_URL='rediss://...'
fly deploy -c deploy/fly/fly.toml
curl https://argus-monitor.fly.dev/healthz
```

Keep `auto_stop_machines = "off"`, because a sleeping machine stops the snapshot loop. Telegram alerts need `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` as secrets too. The image already includes `config/` and `data/`.

**Dashboard.** Connect a Cloudflare Pages project to the repo:

- root directory `web`
- build command `pnpm install --frozen-lockfile && pnpm build`
- output directory `build`
- env: `PUBLIC_API_BASE=https://argus-monitor.fly.dev`, `NODE_VERSION=22`, `PNPM_VERSION=10.28.0`

Copy `deploy/cloudflare/_redirects` (SPA fallback) and `deploy/cloudflare/_headers` (CSP) into `web/static/` so they end up at the root of the build. The `connect-src` in `_headers` has to name your Fly host. CORS defaults to `*`. Set `ARGUS_CORS_ORIGINS` to the Pages origin to tighten it.

## docker compose on a VM

```sh
git clone https://github.com/emma31-dev/argus && cd argus
cp .env.example .env           # set POSTGRES_PASSWORD, and TELEGRAM_* for alerts
docker compose up -d --build   # postgres, redis, argus on :8080, dashboard on :3000
curl http://localhost:8080/healthz
```

Put a TLS reverse proxy in front of 3000 and 8080 if the VM is public. Postgres and Redis stay bound to `127.0.0.1`. The stack mounts `config/` and `data/` read-only, and `.env` stays on the VM.

Before going public: change `POSTGRES_PASSWORD` from the default, keep `ARGUS_RPC_RPS` within the providers' free limits, and point `ARGUS_PUBLIC_URL` at the dashboard so alert links work.
