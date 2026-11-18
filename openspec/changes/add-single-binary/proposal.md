# One file to run StreamGo

## Why

A release is three programs: StreamGo, LiveKit and MediaMTX. The packages and the `.tar.gz` carry the three with
three systemd units and two configuration files, and the all-in-one image runs them under supervisord. Someone who
wants to try StreamGo on a Linux host without Docker and without installing a package has to unpack an archive,
create the secrets, write the units or start three processes by hand. The owner asked for a single binary whose
options are command-line flags, and noticed how large a release's files are.

## What Changes

- **`streamgo up`**: a new command that runs the whole studio from one process tree. It creates the secrets on the
  first start, starts LiveKit and MediaMTX as child processes with the configuration they need, keeps them running,
  and runs the StreamGo server itself. Its options are flags (data directory, HTTP address, public URL, the
  address and the ports browsers reach for media, HTTPS).
- **A single file**: `streamgo_X.Y.Z_linux_amd64`, the StreamGo executable packed with UPX, with the official
  LiveKit and MediaMTX binaries appended to it, compressed. `up` unpacks them into the data directory, checks them
  and runs them. The download is one file; `chmod +x` and `./streamgo up` is the whole install (ffmpeg comes from
  the distribution, as always).
- **A small download**: `streamgo-server_X.Y.Z_linux_amd64.tar.gz`, StreamGo alone (the executable, the example
  configuration and the notices), for a host that already has LiveKit and MediaMTX.
- **`--data-dir` on every command** that reads the configuration, and the secrets of
  `<data_dir>/streamgo.env` read by the binary itself.
- **`streamgo licenses`**: prints the third-party notices, which a single file cannot ship beside itself.
- The plain build gains `up` too: there it runs the `livekit-server` and `mediamtx` beside the executable (the
  packages' `/opt/streamgo/bin`) or in `PATH`.
- **The README and the guides are brought up to date**: the downloads with their sizes and what each is for, the
  single file as the quickest way to try StreamGo without Docker, and the "Good to know" section, which still
  speaks of the first version.

Nothing changes for the packages, the Compose stack or the all-in-one image in this change.

**Sizes**, measured on the release files `make package` built from this change (bytes): the single file
68,104,047 (StreamGo packed with UPX, about 10.2 MB of it, then LiveKit and MediaMTX with zstd -19), against
79,044,877 for the `.tar.gz` of the same build; the small download 13,542,411. The two media servers are most of
every download and do not shrink further.

## Capabilities

### New Capabilities

- `single-binary`: the `up` command, the embedded media servers, their supervision, the secrets of a first start,
  the flags, the release files and the notices.

### Modified Capabilities

None formally (`add-live-studio` is not archived yet); `server-cli`, `configuration`, `build-and-deploy` and
`documentation` gain what this capability describes.

## Impact

- A new adapter `internal/mediaservers` (find, unpack, verify, run and supervise the two servers), `cmd/up.go`,
  `cmd/licenses.go`; the instance lock moves out of `runServer` so `up` can hold it for its whole life;
  `internal/config` gains a bootstrap step (data directory, then the env file, then validation) and a `[media]`
  section. `internal/core` is untouched.
- `AGENTS.md` rule 5 names the one file the binary reads secrets from.
- `Dockerfile`: a stage that packs the executable with UPX and appends the two verified binaries, compressed;
  the pinned digests compiled into every build; `Makefile`: `make single`, `make test-single`;
  `packaging/build.sh` writes the small archive.
- `.github/workflows/packages.yml` builds, smoke tests and publishes the two new files and checks a release by
  its expected file names.
- Docs: a new `docs/install-single.md`, `README.md` quick start, `docs/README.md`, `docs/install-binary.md`,
  `config.toml.example`, `CHANGELOG.md`, `THIRD_PARTY`, the release skill.
