# Install the binary

The `.tar.gz` archive, with StreamGo and the pinned LiveKit and MediaMTX binaries, arrives with the packaging step;
this page will then describe laying it out by hand on any linux/amd64 distribution, with the systemd units, the
settings and an ffmpeg with libx264 from the distribution. Until it exists, run StreamGo with Docker:
[Install with Docker](install-docker.md).

To run a binary you built yourself from a checkout (`make build` writes `dist/streamgo`), the
[root README](../README.md#quick-start) shows the commands; it still needs a LiveKit server, MediaMTX and ffmpeg,
which is what the Compose stack and the packages provide.
