# Design: one file to run StreamGo

## Context

StreamGo needs two media servers beside it: LiveKit hosts the studio rooms and MediaMTX receives the program by
WHIP and serves it to ffmpeg over RTSP. Today three things start them: systemd (the packages), Compose, and
supervisord in the all-in-one image, whose entrypoint also creates the secrets the three share. `streamgo up`
does in Go what that entrypoint and supervisord do, so that one executable is enough.

It is one downloadable file, not one running process: StreamGo supervises the two official media servers.

Linux on amd64 only, as the rest of the project. The plan was reviewed with Codex; the corrections of that review
are in the decisions below.

## Decisions

### B1. The official binaries are embedded; their code is not compiled in

The single file stores the `livekit-server` and `mediamtx` executables the packages already ship (the same pinned
images and SHA-256 of the `media` stage of the `Dockerfile`).

Linking the two servers into StreamGo as libraries was rejected:

- MediaMTX keeps its whole server under `internal/`, which Go does not let another module import. It would need a
  maintained fork.
- LiveKit and MediaMTX depend on different versions of the same WebRTC libraries; one module graph would pick one
  version for both, which neither project tests.
- Every upgrade of either server would become a build problem of StreamGo instead of a new pinned digest.

### B2. One build; the single file is that build with a payload; where the media servers come from

There is one Go build. The single file is the `streamgo` executable, packed with UPX, with a payload appended
after it: the two media servers, compressed, and a trailer that gives their names, versions, offsets and sizes
and ends with a fixed mark. The kernel and the UPX loader ignore what follows the program; `up` opens its own
executable, finds the trailer at the end and reads each program straight from the file to the data directory.
Nothing of the payload is held in memory, and a command that does not unpack (`user add`, `version`) never reads
it. An executable without the mark has no payload.

The SHA-256 of the two media servers is compiled into every build from the `LIVEKIT_SHA256` and
`MEDIAMTX_SHA256` the `Dockerfile` already pins, so a payload is checked against the program, not against
itself.

`up` chooses each program once, in this order: the payload of its own executable, the file beside the running
executable, `PATH`.
The choice never falls through on a failure: a program of the payload that cannot be unpacked, verified or run
fails the start, and so does a file beside the executable that is not executable. The start line says where each
program came from and its version. The plain executable therefore runs `up` on a host with the package installed,
and the Go tests exercise `up` with small fake servers and with a payload they append themselves.

### B3. Compression

Measured on the 0.3.0 binaries (bytes): StreamGo 37,277,856, LiveKit 81,133,440, MediaMTX 56,853,466.

| | StreamGo | LiveKit | MediaMTX |
|---|---|---|---|
| `upx --lzma` | 10,007,944 | 31,339,564 | 24,492,136 |
| `zstd -19` | not measured | 32.5 MB | 25.4 MB |

UPX gains on StreamGo's own executable and nothing worth having on the two servers, so:

- the **executable of the single file is packed with `upx --lzma`** in the Docker build stage (the distribution's
  `upx-ucl`), tested with `upx -t` before the payload is appended. It unpacks itself in memory at each start
  (`streamgo version` took 0.4 s packed against 7 ms unpacked on the build host); the packages, the images and
  the small archive keep the unpacked executable.
- the **two media servers stay the official files**, stored in the payload with zstd (level 19, written by the
  `zstd` tool, read with `github.com/klauspost/compress/zstd`, already in the module graph: no new module). What
  `up` writes to disk is byte for byte the binary the packages ship.

The single file is then about 68 MB instead of 95 MB.

### B4. Unpacking and verification

`up` owns `<data_dir>/media/`: a directory it creates with mode 0700 and refuses to use when it is a symbolic
link or belongs to another user. Each program of the payload goes to `<name>-<first 12 hex digits of its SHA-256>` as a
regular file of mode 0700, written to a temporary file in the same directory and renamed.

At every start it compares the SHA-256 of the file on disk with the digest compiled into the program and writes
the file again
when it is missing, differs or is not a regular file; a correct file with a wrong mode gets its mode back.
Cleanup removes only files in that directory whose name is one of the two programs with another digest.

A program that cannot be executed fails the start. The message names `--data-dir` and says `noexec` only when the
file system is mounted that way; otherwise it reports the permission error as it is. A media server already
started is stopped before `up` exits.

### B5. Supervision

`internal/mediaservers` starts LiveKit and MediaMTX, each in its own process group
(`SysProcAttr{Setpgid: true, Pdeathsig: SIGKILL}`).

The parent-death signal belongs to the OS thread that created the child. Every start and restart therefore
happens on one launcher goroutine that calls `runtime.LockOSThread()` before its first `Cmd.Start` and stays
alive and locked until every child has been stopped and waited for. The signal reaches the direct child only, not
processes that child might fork: the two servers do not fork.

Output is read line by line into `slog` with a `component` attribute, with a bounded line length and a bounded
tail (the last 20 lines of each server, for a failure message). A server cannot block by writing.

A server that exits is started again after a delay that doubles from 1 s to 30 s and returns to 1 s after a
minute of running. Restarts stop when shutdown begins. The existing readiness checks of `/readyz` keep reporting
each server.

Order: the two media servers first; the StreamGo server starts once both answer their existing checks (30 s at
most, then the start fails with the tail of the server that did not come up). Any failure after a child started,
of the second server or of StreamGo itself, stops the children before `up` exits. On SIGINT or SIGTERM: the
StreamGo server stops first (its existing 25 s), then each process group gets SIGTERM and, after 10 s, SIGKILL,
and is waited for.

### B6. Internal ports are chosen at start; only the media ports are options

Four listeners are reached by StreamGo alone, on the loopback address: LiveKit's signalling and API port (`port`),
MediaMTX's WHIP (`webrtcAddress`), RTSP (`rtspAddress`) and API (`apiAddress`). `up` picks four distinct free TCP
ports at each start and gives them to the servers and to StreamGo's own settings. With the packaging templates no
other listener is open: LiveKit's Prometheus, debug and TURN are off by default, and the MediaMTX template turns
off RTMP, HLS, SRT, MoQ, metrics, pprof and playback and uses RTSP over TCP without encryption.

In managed mode `up` sets `livekit.url` to empty (browsers use the `/livekit/` proxy; the default of that setting
is not empty), `livekit.api_url` and the three `mediamtx.*_url` to the chosen ports, whatever the configuration
says, and logs one line when it replaced a value the operator had set.

Browsers reach three ports directly: LiveKit media on 7881/tcp and 7882/udp, MediaMTX WebRTC on 8189 (UDP and
TCP). Those are settings with flags. `up` checks them before starting anything, both the TCP and the UDP side of
8189, and fails naming the flag of a port in use. A host that already runs a LiveKit or a MediaMTX on its usual
media ports needs these flags; only the internal ports move by themselves.

### B7. Generated configuration and a controlled environment

The two configuration files are written under `<data_dir>/media/` at every start, from the templates of
`packaging/livekit.yaml` and `packaging/mediamtx.yml` with the ports of B6. They carry a first line saying they
are generated and hold no secret. An operator who needs more than the flags uses the packages or Compose.

Each child gets an environment built for it, not the parent's: `PATH`, `HOME`, `TZ` and the locale variables, and

| Child | Handed over | How |
|---|---|---|
| LiveKit | the API key and secret | `LIVEKIT_KEYS` |
| LiveKit | the node address | `--node-ip`, with `rtc.use_external_ip: false` |
| MediaMTX | the user and password | `MTX_AUTHINTERNALUSERS_0_USER`, `MTX_AUTHINTERNALUSERS_0_PASS` |
| MediaMTX | the node address | `MTX_WEBRTCADDITIONALHOSTS`, `MTX_WEBRTCIPSFROMINTERFACES=false` |

No `LIVEKIT_*`, `MTX_*` or `RTSP_*` variable of the parent reaches a child, so nothing in the operator's shell
can undo the generated configuration. StreamGo's secret key and database DSN reach neither child. Credentials are
never in a command line or in a generated file. The values of the secrets are replaced in every line before it is
logged or kept in the tail.

The credentials are visible in `/proc/<pid>/environ` of the two children to the same user and to root, as they
are under systemd and supervisord today. The guide says so.

### B8. The data directory, the secrets and the configuration bootstrap

`--data-dir` becomes a flag of the root command, for every command that reads the configuration. `version`,
`licenses` and `--help` read no configuration.

`config.Load` validates the secret key as soon as it has read the file and the environment, so a first start
cannot ask it where the data directory is. The loader gains a bootstrap step:

1. defaults, `config.toml`, the environment and the flags the user gave, without validation;
2. the data directory of that result; a relative path stays relative to the working directory, as today;
3. `<data_dir>/streamgo.env`, when it exists, read as lines of `NAME=value` (the format of
   `packaging/streamgo.env`; it is data, never run by a shell), for variables the environment lacks;
4. validation.

A line that is not an assignment fails the start with the file and the line number.

On the first start `up` creates `<data_dir>/streamgo.env` (mode 0600) with the five variables of
`packaging/init-env.sh`. For each one it lacks it stores the value the environment gives, when there is one, and
generates a value only otherwise: a secret key passed in the environment on the first start is the one kept, so a
later start without that variable reads the same destinations and invite links. A value already in the file is
never replaced, and a variable in the environment wins over the file for that run; overriding the stored secret
key does not re-encrypt anything. The file is written to a temporary file and renamed.

This is the one place the binary reads secrets from a file. It is what systemd's `EnvironmentFile` does for the
packages and what the wrapper script does in the all-in-one image; `config.toml` still refuses secrets. Rule 5 of
`AGENTS.md` names the file.

### B9. One instance

The instance lock of the data directory is taken today inside `runServer`. `up` takes it first, before it creates
secrets, unpacks, cleans up or starts a child, and releases it after every child has been waited for; `runServer`
receives the lock instead of taking it. A second `up` or `server` on the same data directory, of the same or of
another release, exits 1 without changing a file, and the running instance keeps working.

### B10. Settings and flags

New settings, used by `up` only, each with its environment name, a default, a range check and a line in
`config.toml.example`:

| Setting | Environment | Default | Flag |
|---|---|---|---|
| `media.node_ip` | `STREAMGO_MEDIA_NODE_IP` | empty: the IPv4 address of the default route, else `127.0.0.1` | `--node-ip` |
| `media.livekit_tcp_port` | `STREAMGO_MEDIA_LIVEKIT_TCP_PORT` | `7881` | `--livekit-tcp-port` |
| `media.livekit_udp_port` | `STREAMGO_MEDIA_LIVEKIT_UDP_PORT` | `7882` | `--livekit-udp-port` |
| `media.webrtc_port` | `STREAMGO_MEDIA_WEBRTC_PORT` | `8189` | `--webrtc-port` |

Flags of `up` for existing settings: `--addr` (`server.addr`), `--public-url` (`server.public_url`),
`--self-signed` (`server.self_signed`), `--cert-file` and `--key-file` (`server.cert_file`, `server.key_file`).
`--no-migrate` is a flag only, as on `streamgo server`.

Precedence: a flag the user gave (also an explicit `--self-signed=false`), the environment and the env file,
`config.toml`, the default. `--config` keeps working: a `config.toml` is optional, never required.

### B11. ffmpeg and root

ffmpeg is not embedded (the project never ships it). `up` keeps the server's contract: `streamgo server` refuses
to start without an ffmpeg that has the encoders it needs, and so does `up`, before it starts a child, with the
message that says how to install it.

`up` refuses to run as root: it would unpack and run programs as root in a directory that may belong to someone
else. The guide runs it as an unprivileged user and shows the systemd unit, with `AmbientCapabilities` for a port
below 1024.

### B12. The release files

`make single` builds `dist/streamgo-single` (the executable packed with UPX, then the payload) and
`packaging/build.sh` writes the small archive. The Packages workflow gathers every file of the release in one directory, writes `SHA256SUMS` after the
last build, and decides that a release is complete by its expected file names, not by a count of four.

The single file is smoke tested in a clean Debian container with the distribution's ffmpeg, as an unprivileged
user, from a script that stays alive to observe: `up` becomes ready, an account signs in, a browserless client
joins a room and a WHIP publish reaches RTSP, a second start keeps the secret key, an upgrade from the previous
release's single file keeps the account and an encrypted destination, and after SIGKILL of the parent the two
child process groups are gone within 5 s.

The Packages workflow already runs on every push to `main`: it builds the single file and runs its smoke test
there too, so the payload path is exercised before a release, not only at the tag.

### B13. Notices

`streamgo licenses` prints, from texts embedded in the executable: StreamGo's `LICENSE` and `NOTICE`,
`THIRD_PARTY` (which holds the Apache-2.0 and MIT texts of LiveKit and MediaMTX, and names UPX, whose licence
lets a packed program be distributed under its own terms), LiveKit's `NOTICE`, and the six font licence files of
`frontend/src/assets/fonts/` (five OFL texts and the Ubuntu Font Licence). It reads no configuration.

### B14. Layering

`internal/mediaservers` is an adapter (it uses `os/exec` and the file system) and `cmd/up.go` wires it;
`internal/core` does not change and does not import it. The `depguard` rule already forbids the reverse.

## Risks

- **Port choice race** (B6): another process can take an internal port between the choice and the server's bind.
  The media server then fails to start and `up` exits with its tail; the next start chooses again.
- **Two ways to run three programs** (supervisord and `up`): accepted for now, see below.
- **A second supervisor to maintain**: the restart, signal and thread rules of B5 are the part most likely to hide
  a bug; its tests run real processes.
- **A packed executable**: it starts a little slower and its code is not shared between processes; the smoke test
  measures the start of `streamgo version` (under 1 s) and runs every scenario on the packed file. If UPX ever
  breaks a release, the single file can ship unpacked with no change to the code.

## Not in this change

- The all-in-one image keeps supervisord and the packages keep three units. They reuse the same credential
  mappings; moving them to `up` is a follow-up once `up` has run in the field.
- Starting without ffmpeg.
- Windows, macOS and arm64.
- TURN, or any setting of the media servers beyond the flags above.
