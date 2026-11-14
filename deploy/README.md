# deploy

Deployment configuration that is documented but not deployed.

- [`fly/fly.toml`](fly/fly.toml) describes the Go monitor on Fly.io: one process, port 8080, health check on `/healthz`.
- [`cloudflare/_headers`](cloudflare/_headers) and [`cloudflare/_redirects`](cloudflare/_redirects) are for hosting the static dashboard on Cloudflare Pages.

The full setup, including hosted Postgres and Redis, is in [`../docs/deploy.md`](../docs/deploy.md). Nothing in this folder has been applied to a live service.
