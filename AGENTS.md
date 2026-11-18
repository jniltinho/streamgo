# AGENTS.md — instructions for agents

StreamGo: a self-hosted live-streaming studio. Go at the repository root (Cobra + Echo v5 + GORM; SQLite by default
through pure-Go `modernc.org/sqlite`, also MariaDB and PostgreSQL) with an embedded Vue 3 + TypeScript web app
(`frontend/` → `web/dist`). LiveKit hosts the studio, MediaMTX receives the program by WHIP, ffmpeg relays it to RTMP.
Layout and conventions follow MeetHub (github.com/jniltinho/meethub, MIT); the plan is `openspec/changes/add-live-studio`.

## Rules

1. **Everything in English**: code, identifiers, comments, CLI output, logs, API errors, docs, specs and commit
   messages. Portuguese exists only in the pt-BR translation catalog of the frontend.
2. **Idiomatic Go**: context first, wrapped errors (`%w`) and typed or sentinel domain errors, `log/slog`, no global
   mutable state, small consumer-side interfaces, table-driven tests with `-race`.
3. **Layering rule** (design D9): `internal/core/**` imports only the standard library, `golang.org/x/crypto` and
   `internal/core/**` — never `net/http`, `os/exec`, Echo, GORM, a database driver or the LiveKit SDK. Adapters
   (`internal/store`, `internal/livekit`, `internal/mediamtx`, `internal/ffmpeg`, `internal/httpserver`) implement the
   core's ports. The `depguard` rule in `.golangci.yml` fails `make check` on a violation. `frontend/src/core` does not
   import `vue`, `pinia` or `vue-router` (a lint rule in `frontend/vite.config.ts`).
4. **Docker-only builds**: `make build` writes `dist/streamgo` (static linux/amd64). Never commit `dist/`,
   `web/dist/*` (except `.gitkeep`), binaries, `config.toml`, `.env` or databases.
5. **Secrets only from the environment** (`STREAMGO_SECRET_KEY`, `STREAMGO_DATABASE_DSN`, the LiveKit and MediaMTX
   credentials). The loader refuses them in `config.toml`. Never write a secret to a versioned file, a log or test
   output. The one file the binary reads secrets from is `<data_dir>/streamgo.env` (`NAME=value` lines read as data,
   for variables the environment lacks), which `streamgo up` creates with mode 0600 on its first start and never
   rewrites a value of.
6. **Authorization lives in the server**: every broadcast, destination and stage route checks ownership in the core
   and answers 404 when the user does not own it.
7. **Same behaviour on the three databases**: e-mails stored lower-cased, timestamps in UTC, JSON as text, ids as
   ULID strings, no `RETURNING`, upsert syntax or dialect functions outside the migrations.
8. **UI**: themed toasts, alerts and dialogs only — never `alert()`, `confirm()` or `prompt()`; components use role
   tokens and `rem` sizes only.
9. Behaviour change → OpenSpec change (`openspec/`) with testable scenarios.

## Validate

```bash
make check && make test && make test-db && make build && dist/streamgo version
```

## Contracts between packages

- **Routes.** Each package owns `internal/httpserver/handlers/<name>*.go` and exports a registration function,
  e.g. `RegisterBroadcasts(g *echo.Group, d BroadcastsDeps)`, that `cmd/server.go` calls with `srv.API()` (anyone) or
  `srv.UserAPI()` (signed-in; `httpserver.UserID(c)` is the user). Nobody edits the router. Handlers return core
  errors (`core.ErrNotFound` → 404, `core.ErrConflict` → 409, `core.Invalid(field, msg)` → 400) or a
  `*httpserver.Error`; the server writes `{"error":{"code","message"}}`. Bodies are limited to 64 kB; a route that
  needs more adds `"METHOD /api/v1/pattern": bytes` to `httpserver.Options.BodyLimits`. The accounts package provides
  `httpserver.Options.Authenticate`; LiveKit and MediaMTX append their `httpserver.Check` to `ReadyChecks`.
- **Broadcasts.** `broadcast.Repository` (`internal/core/broadcast/repository.go`) is the one store of the broadcast
  row; package B implements it in `internal/store/broadcast*.go`, C and D call it. Stage and output writes happen
  while holding `broadcast.Locks.Lock(ctx, id)`; one `Locks` value is shared by the whole process.
- **Configuration.** `config.Load(path)` (defaults, then `config.toml`, then `STREAMGO_<SECTION>_<KEY>`); a new
  setting is a field with a `toml` tag, a default in `Default()`, a range check in `Validate()` and a line in
  `config.toml.example` (the example test fails otherwise). A secret gets the `secret:"true"` tag.
- **Migrations.** goose SQL files in `internal/store/migrations/{sqlite,mysql,postgres}` with the same number in all
  three (`00002_<name>.sql`); a test fails when the directories differ. `storetest.Each(t, fn)` runs a test on every
  database `make test-db` provides.

## Web app conventions

- Every modal is a component in its own file under `frontend/src/components/` built on `AppModal.vue`; views never
  inline a modal. Modals and themed dialogs open 5 % of the window height below the top (`--dialog-top`).
- No page scrollbar: the document never scrolls; a long list scrolls inside its own region.
- The theme and the language live in `PreferencesModal.vue` (a tab each); pages do not carry their own controls.
- UI SDK (`@ui-sdk/vue`, `@ui-sdk/core`, one exact version) is used only through the `Ui*` wrappers in
  `frontend/src/components/ui/` and `AppModal.vue` (a lint rule enforces it). Before writing or changing one, restore
  its agent skill (`npx skills@latest experimental_install`, from `skills-lock.json`) and read it
  (`.claude/skills/ui-sdk/SKILL.md`), then `frontend/node_modules/@ui-sdk/vue/docs/llms.txt` and only the pages you
  need: the installed docs and `.d.ts` types are the API. Upgrading it follows `docs/README.md`.

## ffmpeg

ffmpeg is never built from source: the `.deb` depends on the distribution's, the `.rpm` recommends it, and the
Docker images install their base distribution's package.
