# User management in the web app

## Why

Accounts are created only from the command line (`streamgo user add`). The person who runs a StreamGo for a team
has to open a shell on the server for every new colleague, every forgotten password and every departure. The
owner asked for an option to register users in the app.

## What Changes

- **Administrators**: an account is an administrator or not. The oldest account of an existing install becomes
  one in the migration, the first account created on a new install is one, and `streamgo user add --admin` and
  `streamgo user admin <email> [--revoke]` set it from the CLI.
- **Users page**: an administrator sees a "Users" page in the web app: the accounts with their name, e-mail,
  role and state; a "New user" modal; and, for each account, edit (name, role, a new password), disable and
  enable. An administrator cannot disable or demote themself, and the last administrator cannot be demoted or
  disabled.
- **Own password**: every user changes their own password in Settings → Profile, giving the current one.
- **API**: `GET /users`, `POST /users`, `PATCH /users/:id` for administrators (403 for anyone else);
  `PUT /me/password`; `GET /me` says whether the user is an administrator.

There is still no public sign-up and no password reset by e-mail.

## Capabilities

### New Capabilities

- `user-management`: administrators, the Users page, the own password.

### Modified Capabilities

None (the `authentication` capability of `add-live-studio` is unchanged: sessions, sign-in and the CLI behave
as before; this change adds to them).

## Impact

- A migration `00002_user_admin` in the three dialects (a column on `users`).
- `internal/core/account`, `internal/store/account.go`, `internal/httpserver/handlers` (a new `users.go`, `me.go`),
  `cmd/user.go`, `cmd/server.go` (one registration call).
- Web app: a Users view, two modals in their own files, a tab field in `PreferencesModal.vue`, the navigation of
  `AppLayout.vue`, both catalogs.
- Docs: the install guides say that the first account is the administrator and that the others are created in the
  app.
