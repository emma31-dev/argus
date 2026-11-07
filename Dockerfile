# syntax=docker/dockerfile:1

# ---- build: all three Go binaries, static and stripped ----
FROM golang:1.27-alpine AS builder
ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0
WORKDIR /src

# Dependencies first, so source edits don't invalidate the module download layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && \
    go build -trimpath -ldflags "-s -w" -o /out/argus ./cmd/argus && \
    go build -trimpath -ldflags "-s -w" -o /out/replay ./cmd/replay && \
    go build -trimpath -ldflags "-s -w" -o /out/verify ./cmd/verify

# ---- runtime: no shell, no package manager, non-root ----
# Migrations are embedded in the binary. config/ and data/ are baked in as
# defaults so the image runs standalone (e.g. on Fly); compose mounts over them.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/argus /out/replay /out/verify /app/
COPY config /app/config
COPY data /app/data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/argus"]
