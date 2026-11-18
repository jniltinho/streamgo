# StreamGo documentation

**StreamGo** is a live-streaming studio that runs on your own server. The host opens a studio in the browser,
invites guests with a link, chooses who is on stage and in which layout, adds a logo and banners, and broadcasts
the result to several RTMP destinations at once (YouTube, Twitch, your own RTMP server). It is one Go binary with
the web app inside (also published as a single file that carries the media servers and runs them with
`streamgo up`), a database (SQLite, MariaDB or PostgreSQL), [LiveKit](https://livekit.io) for the studio
room, [MediaMTX](https://github.com/bluenviron/mediamtx) to receive the program, and ffmpeg to encode it and send
it out.

## Where to start

| I want to… | Read |
|---|---|
| Try it or run it from one file, without Docker or a package | [Install the single file](install-single.md) |
| **Install it on Ubuntu or Debian, or on the RHEL family (a package, systemd services)** | [Install from the Linux packages](install-linux.md) |
| Try it in one container | [Install with Docker: the all-in-one image](install-docker.md#the-all-in-one-image) |
| Run it with Docker Compose, on SQLite, MariaDB or PostgreSQL | [Install with Docker](install-docker.md) |
| Install the packages' files by hand, on any distribution with systemd | [Install the binary](install-binary.md) |
| Serve HTTPS from StreamGo itself, self-signed or with my certificate, and know which ports to open | [Standalone HTTPS](standalone-https.md) |
| Put it behind nginx or Caddy | [Behind a reverse proxy](reverse-proxy.md) |
| Configure the Compose stack: ports, addresses behind NAT, backups | [Deployment](deployment.md) |
| Understand how it works inside | [Architecture](architecture.md) |
| Know how it is secured, and what it does not protect | [Security](security.md) |
| See what each release brings | [Changelog](../CHANGELOG.md) |

## Run it your way

| | For | What runs |
|---|---|---|
| **Single file** | One host without Docker or a package, a first look | One executable that holds StreamGo, LiveKit and MediaMTX; `streamgo up` unpacks the two media servers into the data directory and runs the three, the distribution's ffmpeg |
| **Linux package** | A server without Docker | A `.deb` or `.rpm`: StreamGo, LiveKit and MediaMTX in `/opt/streamgo` as three systemd services, the distribution's ffmpeg |
| **All-in-one image** | One container on one host, a first look | StreamGo, LiveKit, MediaMTX and ffmpeg under supervisord, state in `/data` |
| **Compose stack** | A server with Docker, a checkout of the repository | StreamGo, LiveKit and MediaMTX in containers; SQLite, MariaDB or PostgreSQL |
| **Binary** | Any other distribution with systemd | The files of the packages, laid out by hand |
| **StreamGo alone** | A host that already runs LiveKit and MediaMTX | The StreamGo executable of the small archive, with `streamgo server` or `streamgo up` |

Every release publishes on [GitHub](https://github.com/jniltinho/streamgo/releases), with their `SHA256SUMS`:

| File | Size | What it holds |
|---|---|---|
| `streamgo_<version>_linux_amd64` | 68 MB | The single file: StreamGo, packed with UPX, with the official LiveKit and MediaMTX executables of the release appended, compressed |
| `streamgo_<version>_amd64.deb` | 66 MB | StreamGo, LiveKit and MediaMTX in `/opt/streamgo`, three systemd units |
| `streamgo-<version>-1.x86_64.rpm` | 79 MB | The same, for the RHEL family |
| `streamgo_<version>_linux_amd64.tar.gz` | 79 MB | The files of the packages |
| `streamgo-server_<version>_linux_amd64.tar.gz` | 14 MB | StreamGo alone: the executable, the example configuration and the notices |

and the image as `ghcr.io/jniltinho/streamgo:<version>-allinone` (about 150 MB to pull). The single file and the
StreamGo-only archive come with 0.4.0. All are linux/amd64 only.

### `streamgo up`

`streamgo up` runs the whole studio from one process tree, as the all-in-one image's supervisor does for its
container. It creates the secret key and the credentials the three programs share in `<data_dir>/streamgo.env` on
the first start, starts LiveKit and MediaMTX as child processes with a configuration it writes for them, waits for
both, runs the StreamGo server, starts a media server again when it exits, and stops the three in order on a
signal. It takes each media server from the single file's payload, else from the file beside the executable (the
packages' `/opt/streamgo/bin`), else from `PATH`; a payload's files run only when their SHA-256 is the one
compiled into StreamGo. It refuses to run as root and to start without ffmpeg.

The media servers' own listeners are on the loopback address, on ports chosen free at every start. Browsers reach
three ports, which are flags: `--livekit-tcp-port` (7881/tcp), `--livekit-udp-port` (7882/udp) and
`--webrtc-port` (8189, TCP and UDP); `--node-ip` is the address both advertise. The other flags are `--addr`,
`--public-url`, `--self-signed`, `--cert-file`, `--key-file` and `--no-migrate`. `--data-dir` is a flag of every
command that reads the configuration, and each of them reads `<data_dir>/streamgo.env` for the variables the
environment lacks. [Install the single file](install-single.md) has the details.

The packages, the Compose stack and the all-in-one image do not use `up`: they keep their own units and
supervisor.

![The pieces of StreamGo: browsers reach StreamGo over HTTPS and LiveKit over WebRTC; the owner's browser sends the program to MediaMTX; ffmpeg reads it and sends it to the RTMP destinations](images/diagram-overview.svg)

What every install needs, whichever way you run it:

- **HTTPS**, except for a browser on the server itself: browsers give a page the camera and the microphone only
  on a secure origin. Either StreamGo serves HTTPS itself ([Standalone HTTPS](standalone-https.md)) or a reverse
  proxy does ([Behind a reverse proxy](reverse-proxy.md)).
- **The media ports open to browsers**: LiveKit's and MediaMTX's WebRTC ports are not HTTP and never go through a
  proxy. There is no TURN server in this version, so a network that allows only web traffic cannot take part.
  The list is in [Standalone HTTPS: ports](standalone-https.md#ports).
- **A desktop Chromium-based browser for the host** (Chrome, Edge, Brave and the like). Guests can use any
  current browser.

## Where it stands

StreamGo is published in tagged releases; what each one changed is in the [changelog](../CHANGELOG.md). This
documentation describes what the code does; where a behaviour was measured rather than only designed, the
[architecture guide](architecture.md) says so and gives the numbers from the [media spike](spike-media-path.md).

Three things are not yet checked on real hardware: a host whose browser window is minimized (it passed only in an
emulation), a server behind NAT reached from another network, and a long broadcast to a public platform. The live
output is tested against an RTMP server in the test stack.

Not in the current version, so you can plan around it: recordings, output above 1440p, more than one StreamGo
process, a TURN server, hosts other than linux/amd64, and composing the program on the server (the broadcast
depends on the host's browser staying open; see [the limits](architecture.md#limits)).

## For developers

StreamGo is Go at the repository root (Cobra, Echo, GORM) with a Vue 3 and TypeScript web app in `frontend/`,
embedded in the binary. Everything builds and tests in Docker; the host needs Docker and `make`. The rules are in
[AGENTS.md](../AGENTS.md), and the plan with its specifications in [`openspec/`](../openspec/).

### Build and run from source

```bash
make init                      # config.toml and .env (secret key and credentials), only when missing
make build                     # dist/streamgo, a static linux/amd64 binary
make single                    # dist/streamgo-single, the single file (packed, with the two media servers)
dist/streamgo version
```

Run the server directly (SQLite under `./data`, an ffmpeg with libx264 on the `PATH`, and a LiveKit and a MediaMTX
to talk to):

```bash
set -a; . ./.env; set +a       # secrets come only from the environment
dist/streamgo server           # http://localhost:8080; GET /healthz and /readyz; applies pending migrations
```

`dist/streamgo --help` lists the commands. Every setting is documented with its default in
[`config.toml.example`](../config.toml.example) and can be overridden by `STREAMGO_<SECTION>_<KEY>`. The secret key,
the database connection string and the LiveKit and MediaMTX credentials are read only from the environment.

### The whole stack

```bash
make up                        # StreamGo, LiveKit and MediaMTX with Docker Compose (DB=sqlite|mariadb|postgres)
make dev                       # the same stack with the Vite dev server in front, on http://localhost:5173
make down                      # stop it (volumes are kept)
```

`make up` advertises the host's network address for media unless `NODE_IP` is set; the published ports come from
`HTTP_PORT`, `LIVEKIT_PORT`, `LIVEKIT_TCP_PORT`, `LIVEKIT_UDP_PORT` and `MEDIAMTX_WEBRTC_PORT`, in the environment
or in `.env`. [Deployment](deployment.md) has the details.

### Check and test

```bash
make check          # gofmt, go vet, golangci-lint (with the layering rule), govulncheck, frontend check, build and size check
make test           # Go tests with -race on SQLite, frontend unit tests
make test-db        # Go tests on SQLite, MariaDB and PostgreSQL, with LiveKit, MediaMTX and ffmpeg (throwaway containers)
make test-package   # build the release files and install the .deb and .rpm in clean distribution containers
make test-single    # build the single file and smoke test it in a clean Debian container (packaging/single-smoke.sh)
make test-allinone  # build and smoke test the all-in-one image
```

The studio's browser suite (`frontend/tests/e2e/20-studio.spec.ts`) runs against a running stack; its header says
how.

### UI SDK in the web app

The studio's device menus, its tooltips and the microphone meter, every modal, the confirmation and alert dialogs
and the toasts are built on [UI SDK](https://github.com/vercel-labs/ui) (`@ui-sdk/vue` and `@ui-sdk/core`,
Apache-2.0): unstyled components that bring the keyboard, focus and screen-reader behaviour, styled by StreamGo's own
recipes on the theme roles. It is a young library (a beta), so it is held at one exact version and used only inside
the wrappers of `frontend/src/components/ui/` (`UiMenu`, `UiTooltip`, `UiMeter`, `DialogHost`, `ToastStack`) and
`AppModal.vue`; a lint rule fails `make check` on an import anywhere else. Its default stylesheet is not used.

Two things in the recipes depend on what the library renders, and are the first to check after an upgrade: a dialog
is a `div` in the top layer whose browser box the library resets, so `.pnl` in `style.css` gives it back the box a
native modal `<dialog>` had (in the `base` layer, under the components' own sizes); and the toast region lists the
newest toast first, so `#toasts` reverses it. `feedback/toasts.ts` stays the one clock and limit of the toasts: the
library's toaster only mirrors its list, with no timer of its own.

An upgrade is one commit, never a version bump alone:

1. both packages to the same new exact version: `npm install --save-exact @ui-sdk/vue@X @ui-sdk/core@X` in
   `frontend/`, with `package-lock.json`; read its changelog (https://ui-sdk.dev/docs/core/changelog) and the
   installed docs (`frontend/node_modules/@ui-sdk/vue/docs/llms.txt`);
2. the wrappers adjusted to what changed, and the version in `THIRD_PARTY`;
3. `make check` and `make test`, the browser suites (`02-themes`, `03-feedback`, `07-ui-wrappers`, `10-app-pages`
   and the studio suite on a stack);
4. the size numbers: `make check` prints the gzip sizes of the entry and of the studio and guest routes and fails
   above their ceilings, which are in `frontend/scripts/size-check.mjs`; a growth that is wanted raises them there,
   10 % above the new measure, in the same commit.

A new UI SDK component enters the same way: a `Ui*.vue` wrapper, a recipe on the role tokens, an entry in the test
gallery and its contrast recipes in `frontend/src/themes/matrix.json`. Releases are cut from a tag: the Packages workflow builds and publishes the files, and the image workflow
pushes the all-in-one image.
