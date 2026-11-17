# Spec Delta

## ADDED Requirements

### Requirement: Build
`make build` SHALL produce `dist/streamgo`, a static linux/amd64 binary with the web app embedded, entirely inside
Docker, and `make check` and `make test` SHALL run the Go checks (gofmt, go vet, golangci-lint, govulncheck) and
tests with `-race`, and the frontend checks through Vite+ (`vp check` with the type check, `vp test`, and
`vp build` for the bundle), also inside Docker; `make test-db` SHALL run the Go tests against SQLite, MariaDB and
PostgreSQL.

#### Scenario: Clean checkout
- **WHEN** `make check test build` runs on a machine with only Docker and Make
- **THEN** all three succeed and `dist/streamgo version` prints the version

### Requirement: Layering rule
Packages under `internal/core/` SHALL import only the Go standard library, `golang.org/x/crypto` and each other,
and never `net/http`, `os/exec`, Echo, GORM, a database driver or the LiveKit SDK. `frontend/src/core/` SHALL not
import `vue`, `pinia` or `vue-router`. `make check` SHALL fail on a violation.

#### Scenario: Forbidden import in the core
- **WHEN** a file in `internal/core/` imports `gorm.io/gorm`
- **THEN** `make check` fails naming the file and the import

#### Scenario: Forbidden import in the frontend core
- **WHEN** a file in `frontend/src/core/` imports `vue`
- **THEN** `make check` fails naming the file and the import

### Requirement: Runtime image
The runtime image SHALL contain the binary and an ffmpeg with `libx264` and `aac`, run as a non-root user, and
declare a health check that uses `streamgo healthcheck`.

#### Scenario: Image starts
- **WHEN** the image runs with a valid environment
- **THEN** it becomes healthy and its process is not root

### Requirement: Compose stack
`deployments/compose` SHALL start one StreamGo instance, a single-node LiveKit server and MediaMTX, without Redis
or a job queue, with the database chosen by `DB=sqlite|mariadb|postgres` (SQLite by default) and pinned image
versions. Secrets SHALL come from an `.env` file that `make init` creates only when it is missing.

#### Scenario: Media servers configured
- **WHEN** the stack's configuration files are read
- **THEN** LiveKit has automatic room creation off, MediaMTX has only RTSP, WebRTC and its control API enabled
  with internal credentials, and the only published ports are the app's HTTP port, LiveKit's signalling and media ports, and
  MediaMTX's WebRTC media port

#### Scenario: Up and ready
- **WHEN** `make up` runs on a clean checkout
- **THEN** `GET /readyz` answers ready and a user created with the CLI can sign in

#### Scenario: Init keeps existing secrets
- **GIVEN** an `.env` with a secret key
- **WHEN** `make init` runs again
- **THEN** the file and the key are unchanged

#### Scenario: Reachable from outside
- **WHEN** the stack runs behind NAT with the public address configured as the deployment guide says
- **THEN** a browser on another network connects to the studio and publishes a program

#### Scenario: Media server not exposed
- **WHEN** the published ports of the stack are listed
- **THEN** MediaMTX's HTTP, RTSP and API ports are not among them, and its WebRTC media port is, on UDP and TCP

#### Scenario: End-to-end smoke
- **WHEN** the smoke test publishes a synthetic program by WHIP for a broadcast whose destination is a test RTMP
  server in the stack
- **THEN** the test server receives H.264 and AAC for that broadcast

### Requirement: Readiness
`GET /healthz` SHALL answer while the process runs, and `GET /readyz` SHALL answer ready only when the database
is reachable and LiveKit and MediaMTX answer.

#### Scenario: Media server down
- **WHEN** MediaMTX is stopped
- **THEN** `GET /readyz` answers 503 naming it and `GET /healthz` still answers 200

### Requirement: Deployment guide
`docs/deployment.md` SHALL state what the stack needs and does not provide, and how its data is backed up and
restored: the database, the assets directory and the secret key together.

#### Scenario: Guide covers the list
- **WHEN** the documentation test reads `docs/deployment.md`
- **THEN** it finds a section for each of: HTTPS in front of the app and secure WebSocket termination for
  LiveKit's signalling; the addresses LiveKit and MediaMTX advertise behind NAT; no TURN, so the owner needs UDP
  or TCP access to MediaMTX's WebRTC port; the reverse proxy not logging request paths of the join route; one
  StreamGo instance per deployment; backup and restore

#### Scenario: Restore from a backup
- **GIVEN** a backup of the database, the assets directory and the secret key of a stack with a user, a broadcast
  with a logo and a destination with a key
- **WHEN** they are restored into a new stack
- **THEN** the user signs in, the logo is served, the invite link is the same, and going live relays to the
  destination with its key
