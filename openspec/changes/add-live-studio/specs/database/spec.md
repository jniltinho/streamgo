# Spec Delta

## ADDED Requirements

### Requirement: Supported databases
The system SHALL store its data in SQLite (the default, a file under `data_dir`, through a pure-Go driver),
MariaDB or PostgreSQL, chosen by `database.driver` (`sqlite`, `mariadb` or `postgres`). For MariaDB and
PostgreSQL the connection string SHALL come only from the environment (`STREAMGO_DATABASE_DSN`).

#### Scenario: Default
- **WHEN** the server starts with no database setting
- **THEN** it creates and uses a SQLite file under `data_dir`

#### Scenario: MariaDB and PostgreSQL
- **WHEN** the server starts with `database.driver` set to `mariadb` or `postgres` and a valid
  `STREAMGO_DATABASE_DSN`
- **THEN** it connects, applies the migrations and serves requests

#### Scenario: Unknown driver
- **WHEN** `database.driver` is any other value
- **THEN** startup fails naming the allowed values

#### Scenario: Missing connection string
- **WHEN** the driver is `mariadb` or `postgres` and `STREAMGO_DATABASE_DSN` is empty
- **THEN** startup fails saying so, without printing any connection string

### Requirement: Same behaviour on every database
Every behaviour in these specifications SHALL hold on the three databases. E-mail uniqueness SHALL be
case-insensitive on all of them, timestamps SHALL be stored in UTC, and no query SHALL depend on a feature only
one of them has.

#### Scenario: Test matrix
- **WHEN** `make test-db` runs the Go test suite against SQLite, MariaDB and PostgreSQL
- **THEN** every test passes on the three

#### Scenario: E-mail case
- **WHEN** a user exists as `ana@example.com` and another is created as `Ana@Example.com`, on each database
- **THEN** the second is refused

### Requirement: Migrations
The schema SHALL be created and changed only by versioned migrations embedded in the binary, with the same
versions on the three databases, applied in order and recorded so each runs once.

#### Scenario: Same version everywhere
- **WHEN** `streamgo migrate up` runs on a new SQLite, MariaDB and PostgreSQL database
- **THEN** `streamgo migrate status` lists the same migrations as applied on the three

#### Scenario: Foreign keys enforced
- **WHEN** a row is inserted that references a broadcast that does not exist, on each database
- **THEN** the insert fails

### Requirement: SQLite durability
With SQLite the system SHALL use write-ahead logging, enforce foreign keys and wait for a busy database instead
of failing at once.

#### Scenario: Concurrent requests
- **WHEN** fifty requests write to the SQLite database at the same time
- **THEN** none fails with a "database is locked" error
