# StreamGo — every target runs in Docker; the host needs only Docker (and make).
# The only build target is linux/amd64; outputs go to dist/ (never committed).
# Portions adapted from MeetHub (MIT): Makefile

VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILDTIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PLATFORM  := linux/amd64
DIST      := dist

BUILD_ARGS := --platform $(PLATFORM) \
	--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILDTIME=$(BUILDTIME)

.PHONY: all help build init clean tools check test test-db up down dev

all: build

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

build: ## Build dist/streamgo (static linux/amd64, web app embedded) inside Docker
	docker build $(BUILD_ARGS) --target export --output type=local,dest=$(DIST) .
	@ls -l $(DIST)/streamgo

# init never replaces an existing file: a new secret key makes every stored destination and invite link unreadable.
init: ## Create config.toml and .env (secret key, LiveKit and MediaMTX credentials) when missing
	@test -f config.toml || { cp config.toml.example config.toml && echo "created config.toml"; }
	@test -f .env || { umask 077 && { \
	  printf 'STREAMGO_SECRET_KEY=%s\n' "$$(head -c32 /dev/urandom | base64)"; \
	  echo 'STREAMGO_LIVEKIT_API_KEY=streamgo'; \
	  printf 'STREAMGO_LIVEKIT_API_SECRET=%s\n' "$$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')"; \
	  echo 'STREAMGO_MEDIAMTX_USER=streamgo'; \
	  printf 'STREAMGO_MEDIAMTX_SECRET=%s\n' "$$(od -An -tx1 -N24 /dev/urandom | tr -d ' \n')"; \
	} > .env && echo "created .env (back it up with the database: the secret key cannot be recreated)"; }

clean: ## Remove build outputs
	rm -rf $(DIST)
	find web/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +

# ── checks and tests (tools image; repository mounted, caches in a volume) ────
TOOLS_IMAGE := streamgo-tools
TOOLS_RUN    = docker run --rm -u $(shell id -u):$(shell id -g) -v $(CURDIR):/src -v streamgo-cache:/cache \
	$(1) $(TOOLS_IMAGE) sh -c '$(2)'
NPM_CI      := cd frontend && npm ci --no-audit --no-fund >/dev/null
GO_FILES    := find . -name "*.go" -not -path "./frontend/*" -not -path "./spike/*"

tools: ## Build the tools image (Go + gcc, golangci-lint, govulncheck, Node, ffmpeg)
	@docker build -q -f docker/tools.Dockerfile -t $(TOOLS_IMAGE) docker >/dev/null
	@docker volume create streamgo-cache >/dev/null
	@docker run --rm -v streamgo-cache:/cache $(TOOLS_IMAGE) sh -c 'mkdir -p /cache/home && chmod -R 1777 /cache'

check: tools ## gofmt, go vet, golangci-lint (with the layering rule), govulncheck, frontend check and build
	$(call TOOLS_RUN,,set -e; \
	  test -z "$$(gofmt -l $$($(GO_FILES)))" || { gofmt -l $$($(GO_FILES)); echo "gofmt: the files above need formatting"; exit 1; }; \
	  go vet ./...; golangci-lint run ./...; govulncheck ./...; \
	  $(NPM_CI); npm run check; npm run build)

test: tools ## Go tests with -race (SQLite) and frontend unit tests
	$(call TOOLS_RUN,,set -e; go test -race -count=1 ./...; $(NPM_CI); npm test -- --run --passWithNoTests)

# test-db starts throwaway MariaDB and PostgreSQL (no published ports) and runs the Go tests on the three databases;
# the containers are removed on exit, failure or interruption.
COMPOSE_TEST := docker compose -f deployments/compose/compose.test.yml
test-db: tools ## Go tests with -race against SQLite, MariaDB and PostgreSQL
	@trap '$(COMPOSE_TEST) down -v --remove-orphans >/dev/null 2>&1' EXIT INT TERM; \
	$(COMPOSE_TEST) up -d --wait --quiet-pull >/dev/null || { echo "the test databases did not start"; exit 1; }; \
	$(call TOOLS_RUN,--network streamgo-test_default -e STREAMGO_TEST_MARIADB_ADDR=mariadb:3306 \
	  -e STREAMGO_TEST_POSTGRES_ADDR=postgres:5432,go test -race -count=1 ./...)

# ── Compose stack (deployments/compose, task 3.H) ─────────────────────────────
DB          ?= sqlite
COMPOSE_DIR := deployments/compose
COMPOSE      = docker compose --env-file .env -f $(COMPOSE_DIR)/compose.yml $(if $(filter sqlite,$(DB)),,-f $(COMPOSE_DIR)/compose.$(DB).yml)

up: init ## Start StreamGo, LiveKit and MediaMTX (DB=sqlite|mariadb|postgres) on http://localhost:8080
	VERSION=$(VERSION) COMMIT=$(COMMIT) BUILDTIME=$(BUILDTIME) $(COMPOSE) up -d --build --wait

down: ## Stop the stack (volumes are kept; add V=1 to delete them)
	$(COMPOSE) down $(if $(V),-v,)

dev: tools ## Vite dev server with hot reload on http://localhost:5173 (API proxied to the server of make up)
	$(call TOOLS_RUN,-it -p 5173:5173 --add-host=host.docker.internal:host-gateway -e STREAMGO_API_URL=http://host.docker.internal:8080,$(NPM_CI); npm run dev)
