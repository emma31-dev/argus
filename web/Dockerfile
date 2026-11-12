# syntax=docker/dockerfile:1

# ---- build: static SvelteKit output in /app/build ----
FROM node:22-alpine AS builder
RUN corepack enable && corepack prepare pnpm@10.28.0 --activate
WORKDIR /app

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile

COPY . .
RUN pnpm build

# ---- serve: nginx with SPA fallback, /v1 proxied to the Argus API ----
FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/build /usr/share/nginx/html
EXPOSE 80
