# Spec Delta

## ADDED Requirements

### Requirement: Command tree
The `streamgo` binary SHALL expose the commands `server`, `migrate`, `user`, `healthcheck` and `version` through
Cobra, with the persistent flags `--config` (a configuration file path) and `--json` (machine-readable output),
exit status 0 on success and 1 on error, and errors printed as `error: <message>` on standard error.

#### Scenario: Version needs no configuration
- **WHEN** `streamgo version` runs in a directory without a configuration file
- **THEN** it prints the version, commit and build date and exits 0

#### Scenario: Unknown command
- **WHEN** `streamgo nope` runs
- **THEN** it prints an error on standard error and exits 1

#### Scenario: JSON output
- **WHEN** `streamgo version --json` runs
- **THEN** standard output is one JSON object with `version`, `commit` and `date`

### Requirement: Server command
`streamgo server` SHALL load and validate the configuration, apply pending migrations unless `--no-migrate` is
given, serve the API and the web app, and on SIGINT or SIGTERM stop accepting requests, end every ffmpeg process
it started, all at once, and exit within 15 seconds, leaving the stored state of live broadcasts as it was so
they resume at the next start.

#### Scenario: Graceful stop while live
- **GIVEN** a broadcast is live with two destinations
- **WHEN** the server receives SIGTERM
- **THEN** no ffmpeg process started by it remains, the process exits 0, and the broadcast is not stored as
  `offline`

#### Scenario: Invalid configuration
- **WHEN** `streamgo server` starts with a configuration that fails validation
- **THEN** it exits 1 naming the offending setting, without listening

### Requirement: Single instance
`streamgo server` SHALL hold an exclusive lock on `<data_dir>/streamgo.lock` for as long as it runs and SHALL
refuse to start while another process holds it, because the live outputs of a deployment are supervised by one
process. Two servers with different data directories on one database are not supported and not detected.

#### Scenario: Second server on the same data directory
- **GIVEN** a server is running
- **WHEN** a second `streamgo server` starts with the same `data_dir`
- **THEN** it exits 1 saying another instance is running, and the first keeps working

#### Scenario: Lock released
- **WHEN** the server stops, even by being killed
- **THEN** a new server starts without manual cleanup

### Requirement: Migrate command
`streamgo migrate up` SHALL apply pending migrations and `streamgo migrate status` SHALL list each migration with
whether it is applied.

#### Scenario: Idempotent
- **WHEN** `streamgo migrate up` runs twice on a new database
- **THEN** the second run applies nothing and exits 0

### Requirement: User command
`streamgo user add`, `list`, `passwd`, `disable` and `enable` SHALL manage accounts. A password SHALL be read
from an interactive prompt without echo, or from standard input with `--password-stdin`, and never from a flag.

#### Scenario: Create a user
- **WHEN** `streamgo user add --email ana@example.com --name "Ana Souza" --password-stdin` receives a password of
  at least 10 characters
- **THEN** the user exists and can sign in with it

#### Scenario: Duplicate e-mail
- **WHEN** `streamgo user add` is given an e-mail that already exists, in any letter case
- **THEN** it exits 1 and no user is created

#### Scenario: Short password
- **WHEN** a password shorter than 10 characters is given
- **THEN** the command exits 1 and nothing is stored

### Requirement: Healthcheck command
`streamgo healthcheck` SHALL request the running server's readiness endpoint and exit 0 only when it answers
ready, so container health checks need no other tool.

#### Scenario: Server not running
- **WHEN** `streamgo healthcheck` runs and nothing listens on the configured address
- **THEN** it exits 1
