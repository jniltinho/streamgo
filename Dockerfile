# syntax=docker/dockerfile:1
# StreamGo — every build runs here; the host needs only Docker. Target: linux/amd64 only.
#   make build  →  docker build --target export --output dist .   (dist/streamgo)
#   make image  →  docker build --target runtime -t streamgo:<version> .
# Portions adapted from MeetHub (MIT): Dockerfile

ARG NODE_IMAGE=node:24.16-bookworm-slim@sha256:2c87ef9bd3c6a3bd4b472b4bec2ce9d16354b0c574f736c476489d09f560a203
ARG GO_IMAGE=golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf
ARG RUNTIME_IMAGE=alpine:3.23@sha256:469c9ea410b9215eaee7976ba6c0639039b444be8e5d638f7abbada189971eb2

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

# ── runtime: the binary and Alpine's ffmpeg (libx264, aac, rtsp, rtmp/rtmps) as a non-root user ─────────────
# The server checks at startup that ffmpeg offers everything the live output needs (design D4).
# Alpine's ffmpeg is built with libx264 and so is GPL-licensed; its license files ship in the package.
FROM ${RUNTIME_IMAGE} AS runtime
RUN apk add --no-cache ffmpeg \
 && addgroup -S -g 65532 streamgo \
 && adduser -S -D -H -u 65532 -G streamgo -h /data -s /sbin/nologin streamgo \
 && mkdir -p /data /etc/streamgo && chown 65532:65532 /data
COPY --from=build /out/streamgo /usr/local/bin/streamgo
ENV STREAMGO_SERVER_ADDR=:8080 \
    STREAMGO_SERVER_DATA_DIR=/data
USER 65532:65532
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=6s --start-period=30s --retries=3 CMD ["streamgo", "healthcheck"]
ENTRYPOINT ["streamgo"]
CMD ["server"]
