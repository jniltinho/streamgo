# StreamGo

A self-hosted live-streaming studio in the browser: a host opens a studio, invites guests by link, arranges who is
on stage, and broadcasts the composed program to several RTMP destinations. One Go binary with the web app embedded;
LiveKit hosts the studio, MediaMTX receives the program, ffmpeg relays it.

## Quick start

Everything builds in Docker; the host needs Docker and Make.

```bash
make init                      # config.toml and .env (secret key and credentials), only when missing
make build                     # dist/streamgo
dist/streamgo version
```

Run the server directly (SQLite under `./data`, an ffmpeg with libx264 on the `PATH`):

```bash
set -a; . ./.env; set +a       # secrets come only from the environment
dist/streamgo migrate up
dist/streamgo server           # http://localhost:8080; GET /healthz and /readyz
```

`dist/streamgo --help` lists the commands. Every setting is documented with its default in `config.toml.example`
and can be overridden by `STREAMGO_<SECTION>_<KEY>`. The secret key, the database connection string and the
LiveKit and MediaMTX credentials are read only from the environment.

## Development

```bash
make check     # gofmt, go vet, golangci-lint (with the layering rule), govulncheck, frontend check and build
make test      # Go tests with -race on SQLite, frontend unit tests
make test-db   # Go tests on SQLite, MariaDB and PostgreSQL (throwaway containers)
make dev       # Vite dev server on http://localhost:5173, API proxied to a server on :8080
```

See `AGENTS.md` for the project's rules.
