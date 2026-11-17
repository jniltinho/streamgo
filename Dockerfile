# syntax=docker/dockerfile:1
# StreamGo — every build runs here; the host needs only Docker. Target: linux/amd64 only.
#   make build  →  docker build --target export --output dist .   (dist/streamgo)
#   make image  →  docker build --target runtime -t streamgo:<version> .
#   make package  →  docker build --target packages --output dist/packages .   (.deb, .rpm, .tar.gz)
#   make image-allinone  →  docker build --target allinone -t streamgo:<version>-allinone .
# Portions adapted from MeetHub (MIT): Dockerfile

ARG NODE_IMAGE=node:24.16-bookworm-slim@sha256:2c87ef9bd3c6a3bd4b472b4bec2ce9d16354b0c574f736c476489d09f560a203
ARG GO_IMAGE=golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf
ARG RUNTIME_IMAGE=alpine:3.23@sha256:469c9ea410b9215eaee7976ba6c0639039b444be8e5d638f7abbada189971eb2
# the packages: Debian's dpkg-deb and rpm
ARG DEBIAN_IMAGE=debian:13-slim@sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2
# the media servers of the Compose stack; the SHA-256 of each binary is checked when it is copied out
ARG LIVEKIT_IMAGE=livekit/livekit-server:v1.13.10@sha256:89ca9ce0791e18d6e03bf6e2c49ceb27e1c754eae8bd50c989386c1a5963791a
ARG LIVEKIT_SHA256=5153d7ce95fb2f2ffb455a0e4ac464be2c94a38c77c99fd728f9c61748a11381
ARG MEDIAMTX_IMAGE=bluenviron/mediamtx:1.21.4@sha256:c730bccdbc8739f8f0c13cefe9bea8b61926f599b88fa4d0d229baa91846c7bc
ARG MEDIAMTX_SHA256=e64868f1cfedc3d4d3d009021216c8d1b5d4dcac94562e07ce1665a04f76c72c

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

# ── media: the pinned LiveKit and MediaMTX binaries, from their official images (by digest), checked by SHA-256 ─
# The same versions as the Compose stack (docs/spike-media-path.md); THIRD_PARTY lists them with these checksums.
FROM ${LIVEKIT_IMAGE} AS livekit
FROM ${MEDIAMTX_IMAGE} AS mediamtx
FROM ${DEBIAN_IMAGE} AS media
ARG LIVEKIT_SHA256
ARG MEDIAMTX_SHA256
COPY --from=livekit /livekit-server /out/livekit-server
COPY --from=mediamtx /mediamtx /out/mediamtx
RUN printf '%s  %s\n' "$LIVEKIT_SHA256" /out/livekit-server "$MEDIAMTX_SHA256" /out/mediamtx | sha256sum -c -

# ── packages: .deb, .rpm and .tar.gz under /opt/streamgo with three systemd units (packaging/build.sh) ─────────
# Only with --target packages; the stage root holds the three files and SHA256SUMS. No ffmpeg: the .deb depends on
# the distribution's, the .rpm recommends it.
FROM ${DEBIAN_IMAGE} AS package-build
RUN apt-get update && apt-get install -y --no-install-recommends rpm && rm -rf /var/lib/apt/lists/*
WORKDIR /work
COPY packaging/ packaging/
COPY config.toml.example LICENSE NOTICE THIRD_PARTY ./
COPY --from=build /out/streamgo ./streamgo
COPY --from=media /out/livekit-server /out/mediamtx ./
ARG VERSION=dev
RUN sh packaging/build.sh "${VERSION}" /out

FROM scratch AS packages
COPY --from=package-build /out/ /

# ── allinone: StreamGo, LiveKit, MediaMTX and Alpine's ffmpeg under supervisord, as a non-root user ─────────────
# Only with --target allinone (docker/allinone/); state in /data, secrets generated there on the first start.
# The same Alpine base and ffmpeg package as the runtime image (libx264, aac; GPL-2.0-or-later, recorded in the
# package's metadata); Debian's ffmpeg pulls in LLVM and Mesa and made the image 741 MB.
FROM ${RUNTIME_IMAGE} AS allinone
LABEL org.opencontainers.image.source="https://github.com/jniltinho/streamgo" \
      org.opencontainers.image.description="StreamGo all-in-one: the studio server, LiveKit, MediaMTX and ffmpeg in one container, SQLite on /data" \
      org.opencontainers.image.licenses="MIT"
# the same uid as the runtime image, so a /data volume can move between the two
RUN apk add --no-cache ffmpeg supervisor \
 && addgroup -S -g 65532 streamgo \
 && adduser -S -D -H -u 65532 -G streamgo -h /data -s /sbin/nologin streamgo \
 && mkdir -p /data && chown 65532:65532 /data
COPY --from=build /out/streamgo /usr/local/libexec/streamgo
COPY --chmod=755 docker/allinone/streamgo.sh /usr/local/bin/streamgo
COPY --chmod=755 packaging/init-env.sh /usr/local/bin/streamgo-init-env
COPY --from=media /out/livekit-server /out/mediamtx /usr/local/bin/
COPY packaging/livekit.yaml packaging/mediamtx.yml /etc/streamgo/
COPY LICENSE NOTICE THIRD_PARTY /usr/share/doc/streamgo/
COPY docker/allinone/supervisord.conf /etc/supervisord.conf
COPY --chmod=755 docker/allinone/entrypoint.sh /usr/local/bin/allinone-entrypoint
# NODE_IP: the address browsers reach the media ports at (LiveKit's --node-ip, MediaMTX's advertised host)
ENV STREAMGO_SERVER_ADDR=:8080 \
    STREAMGO_SERVER_DATA_DIR=/data \
    STREAMGO_LIVEKIT_URL= \
    STREAMGO_LIVEKIT_API_URL=http://127.0.0.1:7880 \
    STREAMGO_MEDIAMTX_WHIP_URL=http://127.0.0.1:8889 \
    STREAMGO_MEDIAMTX_RTSP_URL=rtsp://127.0.0.1:8554 \
    STREAMGO_MEDIAMTX_API_URL=http://127.0.0.1:9997 \
    MTX_WEBRTCIPSFROMINTERFACES=false \
    NODE_IP=127.0.0.1
USER 65532:65532
WORKDIR /data
VOLUME ["/data"]
EXPOSE 8080 7881 7882/udp 8189 8189/udp
HEALTHCHECK --interval=15s --timeout=6s --start-period=30s --retries=3 CMD ["streamgo", "healthcheck"]
ENTRYPOINT ["allinone-entrypoint"]
CMD []

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
