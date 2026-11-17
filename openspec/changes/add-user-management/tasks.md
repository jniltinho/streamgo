# Tasks: user management

## 1. Server

- [ ] 1.1 Migration `00002_user_admin` in `internal/store/migrations/{sqlite,mysql,postgres}`: the `is_admin`
  column and the oldest account as administrator; store test on the three databases ("Existing install")
- [ ] 1.2 `internal/core`: `ErrForbidden`; `internal/httpserver/errors.go`: 403 `forbidden`
- [ ] 1.3 `internal/core/account`: `User.Admin`, first account is an administrator, `ListFor`, `CreateBy`,
  `UpdateBy` with the rules of design U3, `ChangePassword`; repository methods; table-driven tests for every
  scenario of "Administrators", "Users routes" and "Own password"
- [ ] 1.4 `internal/store/account.go`: the new repository methods; `storetest.Each` test
- [ ] 1.5 `internal/httpserver/handlers/users.go` (`RegisterUsers`) and `me.go` (`admin` in the answer,
  `PUT /me/password`, rate limited); handler tests; register in `cmd/server.go`
- [ ] 1.6 `cmd/user.go`: `add --admin`, `admin <email> [--revoke]`, the role in `list`; `cmd/user_test.go`

## 2. Web app

- [ ] 2.1 `api/types.ts`, a `users` store, `UsersView.vue`, `UserModal.vue`, the route with its guard, the link
  in `AppLayout.vue` for administrators only
- [ ] 2.2 "Change password" in `PreferencesModal.vue` → Profile
- [ ] 2.3 Both catalogs (the parity test), e2e in `10-app-pages.spec.ts` with the fake API for every scenario of
  "Users page"

## 3. Docs and checks

- [ ] 3.1 `docs/install-linux.md`, `install-docker.md`, `install-binary.md`, `security.md`, `CHANGELOG.md`
  (`## [Unreleased]`): the first account is the administrator; the others are created in the app
- [ ] 3.2 `make check && make test && make test-db`, `openspec validate add-user-management --strict`
