# Argus build, run and verify tooling. Run `make` or `make help` to list targets.

export GOTOOLCHAIN ?= local

SHELL := /bin/bash
.DEFAULT_GOAL := help

BIN_DIR := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: help verify test test-race lint build run down logs \
	web-install web-dev web-build web-check checks replay clean

help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-13s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

verify: ## Offline check: unit tests, then snapshot math on committed fixtures (no network, no keys)
	go test ./...
	go run ./cmd/verify

test: ## Run Go unit tests
	go test ./...

test-race: ## Run Go unit tests with the race detector
	go test -race ./...

lint: ## go vet, plus a gofmt check that fails if any file is unformatted
	go vet ./...
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt: these files need formatting (run gofmt -w):"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

build: ## Build bin/argus, bin/replay and bin/verify (trimmed, stripped, version-stamped)
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/argus ./cmd/argus
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/replay ./cmd/replay
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/verify ./cmd/verify

run: ## Start Postgres, Redis, Argus and the dashboard with docker compose (mainnet RPC)
	docker compose up --build

down: ## Stop the docker compose stack
	docker compose down

logs: ## Follow docker compose logs
	docker compose logs -f --tail=200

web-install: ## Install dashboard dependencies (frozen lockfile)
	pnpm --dir web install --frozen-lockfile

web-dev: ## Run the SvelteKit dev server
	pnpm --dir web dev

web-build: ## Build the static dashboard into web/build
	pnpm --dir web build

web-check: ## Run dashboard svelte-check and tests
	pnpm --dir web check
	pnpm --dir web test

checks: ## Run capability checks checks/01..06 in order (needs network to NEAR RPC and neardata)
	@for d in checks/0[1-6]-*/; do \
		echo "==> $${d%/}"; \
		go run ./$${d%/} || exit 1; \
	done

replay: ## Run the offline incident replay (pass extra flags with ARGS="...")
	go run ./cmd/replay $(ARGS)

clean: ## Remove build outputs and caches
	rm -rf $(BIN_DIR) dist coverage.out web/build web/.svelte-kit
