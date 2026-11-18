# Single binary

## ADDED Requirements

### Requirement: The up command
`streamgo up` SHALL run LiveKit, MediaMTX and the StreamGo server from one process tree and SHALL answer ready
when the three are.

#### Scenario: First start in an empty directory
- **GIVEN** an empty directory, the single-file build, ffmpeg installed and an unprivileged user
- **WHEN** `streamgo up` runs there
- **THEN** `./data` holds the database and `streamgo.env`, `GET /readyz` answers 200 with `database`, `livekit`
  and `mediamtx` ok, and an account created with `streamgo user add` in the same directory signs in

#### Scenario: A working studio
- **GIVEN** a ready `up`
- **WHEN** a client joins a room with a token of the server, and a WHIP publish is sent through StreamGo
- **THEN** the join succeeds and the published stream is readable at MediaMTX's RTSP address

#### Scenario: No configuration file
- **WHEN** `streamgo up` runs without a `config.toml`
- **THEN** it starts with the defaults and the flags it was given

#### Scenario: Managed media settings
- **GIVEN** a `config.toml` with `livekit.url`, `livekit.api_url` and `mediamtx.*_url` set to other addresses
- **WHEN** `streamgo up` runs
- **THEN** the server uses the media servers `up` started, `livekit.url` is empty, and one log line says which
  settings were replaced

#### Scenario: As root
- **WHEN** `streamgo up` runs as root
- **THEN** it exits 1 saying to run it as an unprivileged user, and creates nothing

#### Scenario: A failure after a media server started
- **WHEN** the second media server or the StreamGo server fails to start
- **THEN** `up` exits 1 and the process group of every child it started is gone within 15 s

### Requirement: Where the media servers come from
`up` SHALL take each media server from the embedded one, else the file beside the executable, else `PATH`, and
SHALL NOT try the next source when the chosen one fails.

#### Scenario: Plain build
- **GIVEN** an executable without a payload, and `livekit-server` and `mediamtx` both beside the executable and
  in `PATH`
- **WHEN** `streamgo up` runs
- **THEN** it runs the two beside the executable and its start line names their paths

#### Scenario: A path with spaces
- **GIVEN** the executable and the two programs in a directory whose name has a space
- **WHEN** `streamgo up` runs
- **THEN** it becomes ready

#### Scenario: A candidate that cannot run
- **GIVEN** a `mediamtx` beside the executable without execute permission and another in `PATH`
- **WHEN** `streamgo up` runs
- **THEN** it exits 1 naming the file beside the executable, and does not run the one in `PATH`

#### Scenario: No media servers anywhere
- **GIVEN** an executable without a payload and neither program beside the executable nor in `PATH`
- **WHEN** `streamgo up` runs
- **THEN** it exits 1 naming the program it did not find and the single-file download

### Requirement: Embedded media servers
The single file SHALL carry the LiveKit and MediaMTX executables of the release, and `up` SHALL unpack them into
a directory of its own under the data directory and run only files whose SHA-256 is the one compiled into the
program.

#### Scenario: A damaged payload
- **GIVEN** a single file whose payload was altered, or cut short
- **WHEN** `up` starts
- **THEN** it exits 1 saying the file is damaged, and runs no media server

#### Scenario: Commands that do not unpack
- **WHEN** `streamgo version` runs from the single file
- **THEN** it answers in under 1 s and nothing is written to disk

#### Scenario: Unpacked once
- **WHEN** `up` starts a second time
- **THEN** the two files under `<data_dir>/media/` have the inode and the modification time of the first start

#### Scenario: A changed file is replaced
- **GIVEN** an unpacked media server whose content was changed
- **WHEN** `up` starts
- **THEN** the file has the pinned SHA-256 again before it runs

#### Scenario: A lost permission is repaired
- **GIVEN** an unpacked media server with the right content and mode 0600
- **WHEN** `up` starts
- **THEN** the file has mode 0700 and runs

#### Scenario: An older version is removed
- **GIVEN** an unpacked media server of an earlier release and an unrelated file in `<data_dir>/media/`
- **WHEN** `up` of a newer release starts
- **THEN** the earlier media server is gone, the new one runs and the unrelated file is still there

#### Scenario: A directory that is not its own
- **GIVEN** `<data_dir>/media` that is a symbolic link, or a directory of another user
- **WHEN** `up` starts
- **THEN** it exits 1 naming the directory and writes nothing in it

#### Scenario: A data directory that cannot run programs
- **GIVEN** a data directory on a file system mounted `noexec`
- **WHEN** `up` starts
- **THEN** it exits 1 with a message that says `noexec` and names `--data-dir`

### Requirement: Supervision
`up` SHALL keep the two media servers running, SHALL stop the three programs in order on a signal, and SHALL never
leave a media server running after it exits.

#### Scenario: A media server that exits is started again
- **GIVEN** a running `up`
- **WHEN** the LiveKit process, or the MediaMTX process, is killed
- **THEN** `/readyz` reports that server failing, a new process of it starts, and `/readyz` is ready again

#### Scenario: Restart delay
- **WHEN** a media server exits at once every time it starts
- **THEN** the delays between its starts are 1, 2, 4, 8, 16, 30 and 30 s, each within 20 %

#### Scenario: The delay returns
- **GIVEN** a media server that was restarted with a delay of 8 s
- **WHEN** it has run for a minute and exits
- **THEN** it is started again after 1 s

#### Scenario: A stop during a delay
- **GIVEN** a media server waiting 30 s to be started again
- **WHEN** `up` receives SIGTERM
- **THEN** `up` exits within 5 s and the server is not started

#### Scenario: A media server that never comes up
- **WHEN** a media server does not answer within 30 s of the first start
- **THEN** `up` exits 1 with the last 20 lines that server printed

#### Scenario: Stop
- **WHEN** `up` receives SIGTERM
- **THEN** the StreamGo server stops first, then the two media servers, `up` exits 0, and the process group of each
  child is gone when it exits

#### Scenario: The parent is killed
- **GIVEN** a running `up` and an observer process that stays alive
- **WHEN** the `up` process is killed with SIGKILL
- **THEN** the two child processes it started are gone within 5 s

#### Scenario: Logs
- **WHEN** a media server prints a line, and a line of 1 MiB without a line break
- **THEN** the first appears in StreamGo's log with a `component` of `livekit` or `mediamtx`, the second is cut
  to a bounded length, and the server keeps running

### Requirement: One instance
`up` SHALL hold the instance lock of the data directory from before its first change until its last child has
stopped.

#### Scenario: A second start
- **GIVEN** a running `up`
- **WHEN** a second `streamgo up`, of the same release or of another one, starts on the same data directory
- **THEN** the second exits 1 naming the running instance, no file under the data directory changed, and the first
  is still ready

### Requirement: Ports
The ports only StreamGo uses SHALL be chosen free at each start; the ports browsers reach SHALL be settings with
flags.

#### Scenario: A host with the internal ports taken
- **GIVEN** another program listening on 127.0.0.1:7880, 8889, 8554 and 9997
- **WHEN** `streamgo up` runs
- **THEN** it becomes ready

#### Scenario: Nothing else listens
- **WHEN** `up` is ready with the default flags
- **THEN** the two child processes listen on four loopback TCP ports, on 7881/tcp, 7882/udp and 8189 (TCP and
  UDP), and on nothing else

#### Scenario: A media port in use
- **GIVEN** another program on 7881/tcp, or on 8189/udp
- **WHEN** `streamgo up` runs
- **THEN** it exits 1 naming `--livekit-tcp-port`, or `--webrtc-port`, before it starts a child

#### Scenario: Other media ports
- **WHEN** `streamgo up --livekit-tcp-port 17881 --livekit-udp-port 17882 --webrtc-port 18189` runs
- **THEN** the two child processes listen on those ports and on none of 7881, 7882 and 8189

### Requirement: The children's environment
Each media server SHALL get only the environment `up` builds for it, and no credential SHALL appear in a command
line, a generated file or a log.

#### Scenario: A variable of the shell does not reach a child
- **GIVEN** `MTX_RTSPADDRESS=:9999`, `RTSP_PROTOCOLS=udp` and `LIVEKIT_PORT=9998` in the environment of `up`
- **WHEN** `up` is ready
- **THEN** no child listens on 9999 or 9998 and RTSP is on the port `up` chose

#### Scenario: What a child receives
- **WHEN** `up` is ready
- **THEN** the LiveKit process has `LIVEKIT_KEYS` and neither process has `STREAMGO_SECRET_KEY` or
  `STREAMGO_DATABASE_DSN` in its environment

#### Scenario: Credentials stay out of sight
- **WHEN** `up` starts, a media server fails with its credentials in its output, and `up` exits
- **THEN** the LiveKit secret, the MediaMTX password and the secret key appear in no line of the log or of the
  failure message, in no command line of a child and in no file under `<data_dir>/media/`

### Requirement: First-start secrets
`up` SHALL create the secret key and the credentials the three programs share in `<data_dir>/streamgo.env` on the
first start and SHALL never replace them; every command that reads the configuration SHALL read that file for
variables the environment lacks.

#### Scenario: Created once
- **WHEN** `up` starts twice in the same directory
- **THEN** `streamgo.env` has mode 0600 and the same five values after the second start

#### Scenario: A key given on the first start is the one kept
- **GIVEN** `STREAMGO_SECRET_KEY` in the environment on the first start, and a destination saved during it
- **WHEN** `up` starts again without that variable
- **THEN** the destination's stream key is readable

#### Scenario: The environment wins
- **GIVEN** `STREAMGO_LIVEKIT_API_SECRET` in the environment and another value in `streamgo.env`
- **WHEN** `up` starts
- **THEN** LiveKit and the server use the environment's value and the file is unchanged

#### Scenario: Other commands
- **GIVEN** a data directory that `up --data-dir /srv/studio` created
- **WHEN** `streamgo user add --data-dir /srv/studio` runs from another directory with no secret in the environment
- **THEN** it succeeds on that directory's database

#### Scenario: A file that is not assignments
- **GIVEN** a `streamgo.env` whose third line is `rm -rf /tmp/x`
- **WHEN** a command reads it
- **THEN** the command exits 1 naming the file and line 3, and nothing was executed

#### Scenario: A file with some values
- **GIVEN** a `streamgo.env` that holds only the secret key
- **WHEN** `up` starts
- **THEN** the four missing variables are added and the secret key is unchanged

#### Scenario: Commands that need no configuration
- **WHEN** `streamgo version`, `streamgo licenses` or `streamgo up --help` runs in a directory with a broken
  `config.toml` and no secret
- **THEN** each succeeds

### Requirement: Settings and flags
The address and the ports of the media servers SHALL be settings with flags, and a flag the user gave SHALL win
over the environment, which wins over `config.toml`, which wins over the default.

#### Scenario: The flags
- **WHEN** `streamgo up --help` runs
- **THEN** it lists `--data-dir`, `--addr`, `--public-url`, `--node-ip`, `--livekit-tcp-port`,
  `--livekit-udp-port`, `--webrtc-port`, `--self-signed`, `--cert-file`, `--key-file` and `--no-migrate`, each
  with its default

#### Scenario: A flag wins
- **GIVEN** `STREAMGO_SERVER_ADDR=:9000` and `STREAMGO_MEDIA_WEBRTC_PORT=18189` in the environment
- **WHEN** `streamgo up --addr :9100` runs
- **THEN** the server listens on 9100 and MediaMTX's media port is 18189

#### Scenario: An explicit false
- **GIVEN** `self_signed = true` in `config.toml`
- **WHEN** `streamgo up --self-signed=false` runs
- **THEN** the server answers plain HTTP

#### Scenario: Node address
- **GIVEN** a host whose default route leaves through an interface with the address 192.0.2.10
- **WHEN** `streamgo up` runs without `--node-ip` and with `media.node_ip` empty
- **THEN** both media servers advertise 192.0.2.10 and the start line says so and names the flag

#### Scenario: No network
- **GIVEN** a host without a default route
- **WHEN** `streamgo up` runs without `--node-ip`
- **THEN** both media servers advertise 127.0.0.1

#### Scenario: A wrong value
- **WHEN** `streamgo up --webrtc-port 70000` runs, or `--node-ip not-an-address`
- **THEN** it exits 1 naming the flag and starts nothing

#### Scenario: The example lists the settings
- **WHEN** the example-configuration test runs
- **THEN** `config.toml.example` has the four `media` settings with their defaults

### Requirement: HTTPS
The HTTPS settings of the server SHALL work with `up` as they do with `streamgo server`.

#### Scenario: Self-signed
- **WHEN** `streamgo up --self-signed --public-url https://studio.example:8443 --addr :8443` runs twice
- **THEN** the server answers HTTPS with the same certificate both times, and the studio's signalling goes through
  `/livekit/` on that origin

#### Scenario: A certificate of the operator
- **WHEN** `streamgo up --cert-file c.pem --key-file k.pem` runs
- **THEN** the server answers HTTPS with that certificate

#### Scenario: Half a pair
- **WHEN** `streamgo up --cert-file c.pem` runs without a key, or with a key file it cannot read
- **THEN** it exits 1 naming the flag and starts no child

### Requirement: ffmpeg
`up` SHALL use the ffmpeg of the system and SHALL refuse to start without one the server accepts, as
`streamgo server` does.

#### Scenario: No ffmpeg
- **GIVEN** a host without ffmpeg
- **WHEN** `streamgo up` runs
- **THEN** it exits 1 with the server's message about ffmpeg, and no child was started

### Requirement: The files of a release
A release SHALL publish `streamgo_X.Y.Z_linux_amd64`, one executable with the media servers of that release,
smaller than the release's `.tar.gz` of the three programs, and `streamgo-server_X.Y.Z_linux_amd64.tar.gz`,
StreamGo alone, both listed in `SHA256SUMS`.

#### Scenario: Published
- **WHEN** the tag `vX.Y.Z` is released
- **THEN** the release has the two files beside the packages, their lines in `SHA256SUMS` verify,
  `./streamgo_X.Y.Z_linux_amd64 version` prints `vX.Y.Z`, and the archive holds the executable, the example
  configuration and the notices

#### Scenario: A release is complete by its files
- **GIVEN** a release that has the four files of an earlier version of the workflow
- **WHEN** the Packages workflow runs again for that tag
- **THEN** it adds the two files that are missing and `SHA256SUMS` lists the six

#### Scenario: Smoke test before publishing
- **WHEN** the Packages workflow runs
- **THEN** the single file runs `up` as an unprivileged user in a clean Debian container with the distribution's
  ffmpeg, passes "A working studio", keeps its secret key across a second start, and passes "The parent is
  killed"; a failure stops the release

#### Scenario: Upgrade
- **GIVEN** a data directory made by the single file of the previous release (when that release published one),
  with an account and a destination
- **WHEN** the new release's single file runs `up` on it
- **THEN** the account signs in, the destination's stream key is readable, and `streamgo.env` is unchanged

#### Scenario: The same media servers as the packages
- **WHEN** the single file unpacks its media servers
- **THEN** the two files have the SHA-256 the `Dockerfile` pins for the packages

#### Scenario: Smaller than the archive
- **WHEN** the release files are built
- **THEN** the single file is smaller than `streamgo_X.Y.Z_linux_amd64.tar.gz`

#### Scenario: Built before a release
- **WHEN** a commit is pushed to `main`
- **THEN** the Packages workflow builds the single file and runs its smoke test

### Requirement: Notices
`streamgo licenses` SHALL print the licences and notices of StreamGo and of the programs and fonts it carries,
without reading any configuration.

#### Scenario: Printed
- **WHEN** `streamgo licenses` runs, from the plain executable and from the single file, in an empty directory
- **THEN** the output holds StreamGo's licence and notice, `THIRD_PARTY`, LiveKit's notice and the six licence
  texts of the bundled fonts

### Requirement: Guide
The documentation SHALL have a guide for the single file.

#### Scenario: Install guide
- **WHEN** a reader follows `docs/install-single.md` on a clean Debian or Ubuntu host
- **THEN** they download and verify the file, install ffmpeg, create an unprivileged user, run `streamgo up`,
  create the first account with the same `--data-dir`, open the firewall ports the guide names, install the
  systemd unit the guide gives (its user, absolute paths, `KillMode=mixed`, a stop timeout of 50 s), and find how
  to upgrade and who can read the credentials of the running processes

#### Scenario: Sizes are stated
- **WHEN** a reader looks at the downloads in `README.md` or the guide
- **THEN** each file has its size and what it contains, and the small archive is named as the one for a host that
  already has LiveKit and MediaMTX

#### Scenario: The README is current
- **WHEN** a reader opens `README.md`
- **THEN** it shows the single file as a way to start without Docker, its "Good to know" section describes the
  current version (no mention of a first version), and every number in it (themes, resolutions, guests) is the
  current one
