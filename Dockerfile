# syntax=docker/dockerfile:1
# StreamGo — every build runs here; the host needs only Docker. Target: linux/amd64 only.
#   make build  →  docker build --target export --output dist .   (dist/streamgo)
# The runtime image (non-root, with ffmpeg) is added by the deployment package (task 3.H).
# Portions adapted from MeetHub (MIT): Dockerfile

ARG NODE_IMAGE=node:24.16-bookworm-slim@sha256:2c87ef9bd3c6a3bd4b472b4bec2ce9d16354b0c574f736c476489d09f560a203
ARG GO_IMAGE=golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf

# ── frontend: Vue web app → web/dist ──────────────────────────────────────────
FROM ${NODE_IMAGE} AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# ── build: static Go binary with the web app embedded ─────────────────────────
FROM ${GO_IMAGE} AS build
RUN apk add --no-cache file
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=frontend /src/web/dist ./web/dist
ARG VERSION=dev
ARG COMMIT=none
ARG BUILDTIME=unknown
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w \
      -X github.com/jniltinho/streamgo/internal/buildinfo.Version=${VERSION} \
      -X github.com/jniltinho/streamgo/internal/buildinfo.Commit=${COMMIT} \
      -X github.com/jniltinho/streamgo/internal/buildinfo.Date=${BUILDTIME}" \
    -o /out/streamgo . \
 && file /out/streamgo | tee /dev/stderr | grep -q 'ELF 64-bit LSB executable, x86-64.*statically linked'

FROM scratch AS export
COPY --from=build /out/streamgo /streamgo
