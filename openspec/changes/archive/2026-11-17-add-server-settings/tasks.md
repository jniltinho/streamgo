# Tasks: server settings

## 1. Server

- [x] 1.1 Migration `00004_settings` in the three dialects; the store adapter with update-then-insert in one
  transaction; `storetest.Each`
- [x] 1.2 `internal/core/settings`: the effective quality (environment, database, file), `Get`, `Update` with the
  rules of design S4, the atomic source; table-driven tests for every scenario of "Stored output settings",
  "Precedence" and "Administrators only"
- [x] 1.3 `broadcast.Service` and `output.Service` read the quality from the source; their tests and fakes
- [x] 1.4 `internal/httpserver/handlers/settings.go` (`RegisterSettings`), handler tests, `cmd/server.go` (which
  settings the environment locks; the start-up warning on the effective values)

## 2. Web app

- [x] 2.1 The Server tab in `PreferencesModal.vue`, a small store, both catalogs, e2e with the fake API for every
  scenario of "Server tab"

## 3. Docs and checks

- [x] 3.1 `docs/deployment.md` and `CHANGELOG.md` (`## [Unreleased]`)
- [x] 3.2 `make check && make test && make test-db`, `openspec validate add-server-settings --strict`
