# Spec Delta

## ADDED Requirements

### Requirement: Package build
`make package` SHALL build inside Docker, for linux/amd64 only and from the same static binary as `make build`,
the files `streamgo_<version>_amd64.deb`, `streamgo-<version>-1.x86_64.rpm`,
`streamgo_<version>_linux_amd64.tar.gz` and `SHA256SUMS` into an emptied `dist/packages/`. A version `vX.Y.Z`
SHALL become the package version `X.Y.Z`; any other version SHALL become `0.0.0+<version>`.

#### Scenario: Build on a clean host
- **GIVEN** a host with Docker and make but no Go, Node.js, dpkg or rpm
- **WHEN** the developer runs `make package`
- **THEN** `dist/packages/` holds the three packages and a `SHA256SUMS` that `sha256sum -c` accepts

#### Scenario: Version of a release
- **WHEN** the packages are built with `VERSION=v1.2.3`
- **THEN** the `.deb` and the `.rpm` report version `1.2.3`

### Requirement: Install layout
Each package SHALL install everything under `/opt/streamgo`, including the pinned LiveKit and MediaMTX binaries
the studio needs, SHALL create the system user and group `streamgo` and `/opt/streamgo/data` (mode 0750, owned
by `streamgo`), and SHALL install three units, the only files outside `/opt/streamgo`.

#### Scenario: Files
- **WHEN** a package is installed
- **THEN** `/opt/streamgo` holds `bin/streamgo`, `bin/livekit-server`, `bin/mediamtx`, `config.toml` (the example
  configuration with `data_dir = "/opt/streamgo/data"` and the two media servers on this host), `streamgo.env`
  (mode 0640, group `streamgo`), the licences and `THIRD_PARTY`

#### Scenario: Install on Debian 13 and Ubuntu 26.04
- **WHEN** `apt install ./streamgo_1.2.3_amd64.deb` runs on a clean system
- **THEN** the files exist with those owners and modes, the `streamgo` user exists, and the distribution's ffmpeg
  is installed as a dependency

#### Scenario: Install on the RHEL family
- **WHEN** `dnf install ./streamgo-1.2.3-1.x86_64.rpm` runs on a clean AlmaLinux 9 or 10
- **THEN** the same files, user and data directory exist, without an ffmpeg, which those distributions do not
  carry with libx264

#### Scenario: Manual install
- **WHEN** `streamgo_1.2.3_linux_amd64.tar.gz` is extracted in `/opt`
- **THEN** `/opt/streamgo` holds the binaries, `config.toml`, `streamgo.env` and the three unit files

#### Scenario: Third-party notices
- **WHEN** the installed `/opt/streamgo/THIRD_PARTY` is read
- **THEN** it names LiveKit and MediaMTX with their versions, sources, checksums and licence texts

### Requirement: First-install secrets
A first install SHALL generate the secret key and the credentials the three services share (the LiveKit API key
and secret, the MediaMTX user and secret) into `/opt/streamgo/streamgo.env` and the media servers' own
configuration files, readable only by `streamgo`. An upgrade or a reinstall SHALL never replace them.

#### Scenario: Ready after install
- **WHEN** the package is installed on a clean system
- **THEN** `streamgo.env` holds a secret key and the shared credentials, and no other user can read it

#### Scenario: Upgrade keeps the key
- **GIVEN** an install with a destination saved
- **WHEN** a newer package is installed
- **THEN** the secret key is unchanged and the destination's key still decrypts

### Requirement: systemd services
`streamgo.service` SHALL run `/opt/streamgo/bin/streamgo server` as `streamgo` with `streamgo.env` as its
environment file, restart on failure and write only `/opt/streamgo/data`; `streamgo-livekit.service` and
`streamgo-mediamtx.service` SHALL run the two media servers as the same user and start before it. A first
install SHALL enable and start the three; an upgrade SHALL restart those that were running; a removal SHALL stop
and disable them.

#### Scenario: Running after the install
- **GIVEN** a Debian 13 or Ubuntu 26.04 host with systemd
- **WHEN** the package is installed
- **THEN** the three units are active and `GET /readyz` answers 200

#### Scenario: Units accepted by systemd
- **WHEN** `systemd-analyze verify` checks the three installed units
- **THEN** it reports nothing about them

#### Scenario: Media servers not exposed
- **WHEN** the listening sockets of an installed host are listed
- **THEN** MediaMTX's HTTP, RTSP and API ports and LiveKit's signalling port listen only on the loopback
  address, and only the app's port and the WebRTC media ports listen on other addresses

#### Scenario: No ffmpeg on the RHEL family
- **GIVEN** an AlmaLinux host without an ffmpeg that has libx264
- **WHEN** the package is installed
- **THEN** `streamgo.service` fails with the message naming the missing encoder, and the install guide says how
  to add one

### Requirement: Data survives upgrades and removal
An upgrade SHALL keep `config.toml`, `streamgo.env`, the media servers' configuration and `/opt/streamgo/data`.
Removing or purging the package SHALL never delete `/opt/streamgo/data` or the `streamgo` user.

#### Scenario: Removal keeps the data
- **GIVEN** an install whose database holds a broadcast
- **WHEN** the package is removed, and purged on Debian or Ubuntu
- **THEN** the binaries and the units are gone and `/opt/streamgo/data/streamgo.db` is still there

#### Scenario: Upgrade keeps the configuration
- **GIVEN** an edited `/opt/streamgo/config.toml`
- **WHEN** a newer package is installed
- **THEN** the edited file is still in place and the service runs the new version

### Requirement: Package smoke test
`make test-package` SHALL install the `.deb` in clean containers of Debian 13 and Ubuntu 26.04, check the files,
the user and the units, start the three services as the units do until `/readyz` answers 200, remove the package
and check that the data stayed; and SHALL install the `.rpm` in clean AlmaLinux 9 and 10 containers and check the
files, the user and the units. It SHALL fail with a message on the first problem.

#### Scenario: Every distribution passes
- **WHEN** `make test-package` runs on a host with Docker
- **THEN** it reports Debian 13, Ubuntu 26.04, AlmaLinux 9 and AlmaLinux 10 as ok and exits 0

### Requirement: Start under a real systemd
`make test-package-systemd` SHALL install the `.deb` in Debian 13 and Ubuntu 26.04 containers running systemd
and verify that the three units become active, that `/readyz` answers 200, that a user created with the CLI can
sign in, and that the units restart and stop cleanly.

#### Scenario: Debian 13 and Ubuntu 26.04 under systemd
- **WHEN** `make test-package-systemd` runs
- **THEN** both distributions report the three units active, a sign-in succeeded, and a clean stop

### Requirement: Installation documents
The repository SHALL document, in English, installing from the package on Ubuntu 26.04 and Debian 13
(`docs/install-linux.md`: layout, first start, creating the first user, HTTPS in front of the app and of
LiveKit's signalling, the ports to open, configuration, upgrade, backup, removal, the `.rpm` and its ffmpeg) and
installing the binary by hand (`docs/install-binary.md`), both linked from the README.

#### Scenario: Following the package document
- **WHEN** an administrator follows `docs/install-linux.md` on a clean Ubuntu 26.04
- **THEN** StreamGo runs as `streamgo.service`, the first user signs in, and a studio opens over HTTPS
