# StreamGo — every target runs in Docker; the host needs only Docker (and make).
# The only build target is linux/amd64; outputs go to dist/ (never committed).
# Portions adapted from MeetHub (MIT): Makefile

VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILDTIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PLATFORM  := linux/amd64
DIST      := dist

# The three are inserted into shell recipes: quoted there, and refused here when they hold anything else than these
# characters (a tag such as v0.4.0;id;# is a valid Git tag name).
# (the value reaches no shell here: every allowed character is removed with make's own functions, and what is left
# is the offence)
alnum := a b c d e f g h i j k l m n o p q r s t u v w x y z A B C D E F G H I J K L M N O P Q R S T U V W X Y Z 0 1 2 3 4 5 6 7 8 9
strip-chars = $(if $(1),$(call strip-chars,$(wordlist 2,$(words $(1)),$(1)),$(subst $(firstword $(1)),,$(2))),$(2))
only = $(if $(filter-out 1,$(words $($(1))))$(strip $(call strip-chars,$(alnum) $(2),$($(1)))),$(error $(1) may hold only letters, digits and $(2)))
$(call only,VERSION,. _ -)
$(call only,COMMIT,. _ -)
$(call only,BUILDTIME,. _ - : +)
BUILD_ARGS := --platform $(PLATFORM) \
	--build-arg 'VERSION=$(VERSION)' --build-arg 'COMMIT=$(COMMIT)' --build-arg 'BUILDTIME=$(BUILDTIME)'

.PHONY: all help build single test-single image init clean tools check check-go test test-go test-db check-db up down logs dev

all: build

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-21s %s\n", $$1, $$2}'

build: ## Build dist/streamgo (static linux/amd64, web app embedded) inside Docker
	docker build $(BUILD_ARGS) --target export --output type=local,dest=$(DIST) .
	@ls -l $(DIST)/streamgo

single: ## Build dist/streamgo-single: the executable packed with UPX, LiveKit and MediaMTX appended (streamgo up)
	docker build $(BUILD_ARGS) --target single-export --output type=local,dest=$(DIST) .
	@ls -l $(DIST)/streamgo-single

# when dist/packages holds the .tar.gz of this very version (make package), it also checks the single file is smaller
# than it; packages of another version left in dist/ are ignored here (the workflow always gives the directory, and
# a missing archive fails there)
PACKAGE_VERSION := $(subst -,.,$(patsubst v%,%,$(VERSION)))
test-single: single tools ## Smoke test the single file in a clean Debian: ready, sign-in, a studio, the key kept, SIGKILL, damage
	packaging/single-smoke.sh $(DIST)/streamgo-single '$(VERSION)' $(if $(wildcard $(DIST)/packages/streamgo_$(PACKAGE_VERSION)_linux_amd64.tar.gz),$(DIST)/packages)

IMAGE ?= streamgo
image: ## Build the runtime image $(IMAGE):$(VERSION) (non-root, with ffmpeg)
	docker build $(BUILD_ARGS) --target runtime -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

# init never replaces an existing file or value: a new secret key makes every stored destination and invite link
# unreadable. A variable missing from an existing .env (added by a later version) is appended.
init: ## Create config.toml and .env (secret key, LiveKit, MediaMTX and database credentials) when missing
	@test -f config.toml || { cp config.toml.example config.toml && echo "created config.toml"; }
	@test -f .env || { (umask 077 && touch .env) && echo "created .env (back it up with the database: the secret key cannot be recreated)"; }
	@grep -q '^STREAMGO_SECRET_KEY=' .env || printf 'STREAMGO_SECRET_KEY=%s\n' "$$(head -c32 /dev/urandom | base64)" >> .env
	@grep -q '^STREAMGO_LIVEKIT_API_KEY=' .env || echo 'STREAMGO_LIVEKIT_API_KEY=streamgo' >> .env
	@grep -q '^STREAMGO_LIVEKIT_API_SECRET=' .env || printf 'STREAMGO_LIVEKIT_API_SECRET=%s\n' "$$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')" >> .env
	@grep -q '^STREAMGO_MEDIAMTX_USER=' .env || echo 'STREAMGO_MEDIAMTX_USER=streamgo' >> .env
	@grep -q '^STREAMGO_MEDIAMTX_SECRET=' .env || printf 'STREAMGO_MEDIAMTX_SECRET=%s\n' "$$(od -An -tx1 -N24 /dev/urandom | tr -d ' \n')" >> .env
	@grep -q '^STREAMGO_DATABASE_PASSWORD=' .env || printf 'STREAMGO_DATABASE_PASSWORD=%s\n' "$$(od -An -tx1 -N24 /dev/urandom | tr -d ' \n')" >> .env

clean: ## Remove build outputs
	rm -rf $(DIST)
	find web/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +

# ── checks and tests (tools image; repository mounted, caches in a volume) ────
TOOLS_IMAGE := streamgo-tools
TOOLS_RUN    = docker run --rm -u $(shell id -u):$(shell id -g) -v $(CURDIR):/src -v streamgo-cache:/cache \
	$(1) $(TOOLS_IMAGE) sh -c '$(2)'
NPM_CI      := cd frontend && npm ci --no-audit --no-fund >/dev/null
# (hidden directories hold other checkouts, such as agents' worktrees: the go tool skips them, so gofmt does too)
GO_FILES    := find . -name "*.go" -not -path "./frontend/*" -not -path "./spike/*" -not -path "./.*/*"

tools: ## Build the tools image (Go + gcc, golangci-lint, govulncheck, Node, ffmpeg)
	@docker build -q -f docker/tools.Dockerfile -t $(TOOLS_IMAGE) docker >/dev/null
	@docker volume create streamgo-cache >/dev/null
	@docker run --rm -v streamgo-cache:/cache $(TOOLS_IMAGE) sh -c 'mkdir -p /cache/home && chmod -R 1777 /cache'

check: tools ## gofmt, go vet, golangci-lint (with the layering rule), govulncheck, frontend check, build and size check
	$(call TOOLS_RUN,,set -e; \
	  test -z "$$(gofmt -l $$($(GO_FILES)))" || { gofmt -l $$($(GO_FILES)); echo "gofmt: the files above need formatting"; exit 1; }; \
	  go vet ./...; golangci-lint run ./...; govulncheck ./...; \
	  $(NPM_CI); npm run check; SIZE_CHECK=1 npm run build; node scripts/size-check.mjs)

check-go: tools ## The Go half of check: gofmt, go vet, golangci-lint, govulncheck (no frontend install)
	$(call TOOLS_RUN,,set -e; \
	  test -z "$$(gofmt -l $$($(GO_FILES)))" || { gofmt -l $$($(GO_FILES)); echo "gofmt: the files above need formatting"; exit 1; }; \
	  go vet ./...; golangci-lint run ./...; govulncheck ./...)

test: tools ## Go tests with -race (SQLite) and frontend unit tests
	$(call TOOLS_RUN,,set -e; go test -race -count=1 ./...; $(NPM_CI); npm test -- --run --passWithNoTests)

test-go: tools ## The Go half of test (no frontend install)
	$(call TOOLS_RUN,,set -e; go test -race -count=1 ./...)

# test-db starts throwaway MariaDB and PostgreSQL (no published ports) and runs the Go tests on the three databases;
# the containers are removed on exit, failure or interruption.
# TEST_PROJECT names the Compose project, so two checkouts can run test-db at the same time.
TEST_PROJECT ?= streamgo-test
COMPOSE_TEST := docker compose -p $(TEST_PROJECT) -f deployments/compose/compose.test.yml
test-db: tools ## Go tests with -race against SQLite, MariaDB and PostgreSQL
	@trap '$(COMPOSE_TEST) down -v --remove-orphans >/dev/null 2>&1' EXIT INT TERM; \
	$(COMPOSE_TEST) up -d --wait --quiet-pull >/dev/null || { echo "the test databases did not start"; exit 1; }; \
	$(call TOOLS_RUN,--network $(TEST_PROJECT)_default -e STREAMGO_TEST_MARIADB_ADDR=mariadb:3306 \
	  -e STREAMGO_TEST_POSTGRES_ADDR=postgres:5432 -e STREAMGO_TEST_LIVEKIT_URL=http://livekit:7880 \
	  -e STREAMGO_TEST_MEDIAMTX=mediamtx -e STREAMGO_TEST_SINK=sink -e CI,go test -race -count=1 ./...)

# ── Compose stack (deployments/compose; guide: docs/deployment.md) ───────────
# Every published port comes from the environment or .env: HTTP_PORT, LIVEKIT_PORT, LIVEKIT_TCP_PORT,
# LIVEKIT_UDP_PORT, MEDIAMTX_WEBRTC_PORT and, for make dev, DEV_PORT; NODE_IP is what the media servers advertise.
DB          ?= sqlite
DBS         := sqlite mariadb postgres
COMPOSE_DIR := deployments/compose
COMPOSE      = docker compose --env-file .env -f $(COMPOSE_DIR)/compose.yml $(if $(filter sqlite,$(DB)),,-f $(COMPOSE_DIR)/compose.$(DB).yml)
# Unless the environment or .env sets NODE_IP, make up and make dev advertise this host's network address (the
# source of its default route): it serves every browser on this host and on the network. Without a network it is
# 127.0.0.1, which Firefox does not reach before the site may use the camera.
LAN_IP       = $(shell ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p')
# .env may spell it "NODE_IP=", "NODE_IP = " or "export NODE_IP=": Compose accepts the three
NODE_ENV     = $(if $(NODE_IP)$(shell grep -Eqs '^[[:space:]]*(export[[:space:]]+)?NODE_IP[[:space:]]*=' .env && echo set),,NODE_IP=$(or $(LAN_IP),127.0.0.1))
STACK_ENV    = VERSION=$(VERSION) COMMIT=$(COMMIT) BUILDTIME=$(BUILDTIME) $(NODE_ENV)

check-db:
	@$(if $(filter $(DB),$(DBS)),true,{ echo "DB must be one of: $(DBS) (got '$(DB)')"; exit 1; })

up: check-db init ## Start StreamGo, LiveKit and MediaMTX (DB=sqlite|mariadb|postgres) on http://localhost:8080
	$(STACK_ENV) $(COMPOSE) up -d --build --wait
	@echo "StreamGo is up: http://localhost:$$($(COMPOSE) port streamgo 8080 | sed 's/.*://')"
	@$(if $(NODE_ENV),echo "Media address: $(patsubst NODE_IP=%,%,$(NODE_ENV)) (run make up again after the network changes; NODE_IP sets another)",true)

down: check-db ## Stop the stack (volumes are kept; add V=1 to delete them)
	$(COMPOSE) down $(if $(V),-v,)

logs: check-db ## Follow the server log
	$(COMPOSE) logs -f streamgo

dev: check-db init ## Vite dev server with hot reload on http://localhost:5173 (and the stack behind it)
	$(STACK_ENV) DEV_UID=$(shell id -u) DEV_GID=$(shell id -g) $(COMPOSE) -f $(COMPOSE_DIR)/compose.dev.yml up --build

# ── Linux packages: StreamGo, LiveKit and MediaMTX under /opt/streamgo with three systemd units (packaging/) ──
.PHONY: package test-package test-package-systemd image-allinone test-allinone

package: ## Build dist/packages: the .deb, .rpm, .tar.gz, single file and small archive of this version, and SHA256SUMS
	rm -rf $(DIST)/packages
	docker build $(BUILD_ARGS) --target packages --output type=local,dest=$(DIST)/packages .
	@ls -l $(DIST)/packages

test-package: package ## Install the packages on Debian, Ubuntu, AlmaLinux (and Rocky Linux with RPM Fusion): files, user, units, a real start, removal
	packaging/smoke.sh $(DIST)/packages

test-package-systemd: package ## The .deb under a real systemd (privileged Debian 13 and Ubuntu 26.04 containers)
	packaging/systemd.sh $(DIST)/packages

# ── all-in-one image: StreamGo, LiveKit, MediaMTX and Alpine's ffmpeg under supervisord (docker/allinone/) ──
image-allinone: ## Build the all-in-one image $(IMAGE):$(VERSION)-allinone
	docker build $(BUILD_ARGS) --target allinone -t $(IMAGE):$(VERSION)-allinone -t $(IMAGE):allinone .

test-allinone: image-allinone ## Smoke test the all-in-one image: healthy, ready, sign-in, data and key kept across a new container
	docker/allinone/smoke.sh $(IMAGE):$(VERSION)-allinone
