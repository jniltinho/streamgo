# Design: user management

## Decisions

### U1. A boolean, not roles
`users.is_admin` (a boolean, NOT NULL, default false). Two kinds of account are all this version needs; a role
table would be a model with one row. Everything a non-administrator does today is unchanged.

### U2. Who is an administrator
- Migration `00002_user_admin`: adds the column and sets it for the enabled account with the smallest id (ids are
  ULIDs, so that is the oldest), or for the account with the smallest id when none is enabled, so the install
  has an enabled administrator whenever it can. An install with no account has nothing to set.
- `account.Repository.CreateUser`: when no account exists yet, the new one is an administrator, whatever was
  asked; the count and the insert share one transaction. Two creations racing on an empty install (two CLI
  processes, or a CLI and the server) may both become administrators, never neither. This covers the first
  `streamgo user add` of a new install.
- CLI: `streamgo user add --admin`; `streamgo user admin <email>` and `streamgo user admin <email> --revoke`;
  `streamgo user list` shows the role. The CLI may leave an install without an administrator (the operator has
  a shell and can undo it); the API may not.

### U3. Authorization lives in the core; the last-administrator rule in one write
`account.Service` gets `ListFor`, `CreateBy`, `UpdateBy`: each takes the acting user's id and answers
`core.ErrForbidden` (a new sentinel, HTTP 403 `forbidden`) when that user is not an enabled administrator. The
handlers only translate.
- An administrator cannot disable or demote themself (`account.ErrSelf`, 409).
- No change may leave the install without an enabled administrator (`account.ErrLastAdmin`, 409, with a message).
  A process mutex would not cover the CLI or a second server process, so the rule lives in the write:
  `Repository.UpdateUser(ctx, id, UserChanges)` runs one transaction that locks the enabled administrators and
  the target row (`SELECT … FOR UPDATE` through GORM's locking clause, which GORM leaves out on SQLite, whose
  transactions take the write lock up front), refuses with `core.ErrConflict` when the change would remove the
  last one, writes only the supplied columns, and deletes the account's sessions in the same transaction when
  the password changes or the account is disabled. A PATCH therefore applies all its fields or none.
- Two administrators demoting each other: in sequence the second actor is no longer an administrator (403); at
  the same time the locks serialize them and the second write is refused (409).
- The profile columns follow the same shape: `UpdateProfile(ctx, id, name, theme, language *string)` writes only
  the supplied ones (it replaces `SetName` and `SetPreferences`).

### U4. API shapes
```
GET    /users                                   200 { users: [AdminUser] } | 403
POST   /users      { email, name, password, admin? }   201 AdminUser | 403 | 409 conflict (e-mail) | 422
PATCH  /users/:id  { name?, admin?, disabled?, password? }  200 AdminUser | 403 | 404 | 409 | 422
PUT    /me/password { current, password }       204 | 422 invalid_input (field current: wrong; field password: too short)
GET    /me                                      User + { admin: bool }
AdminUser = { id, name, email, admin, disabled }
```
A password set by an administrator, like `streamgo user passwd`, ends that user's sessions. Changing one's own
password ends the user's other sessions and keeps the current one. Passwords follow the existing rule
(`checkPassword`). `PUT /me/password` is rate limited like sign-in (same limiter, per address).

### U5. Web app
- `UsersView.vue` at `/users`, linked in the header only for administrators; the route guard sends anyone else
  to `/broadcasts`.
- `UserModal.vue` (create and edit) on `AppModal.vue`; disabling asks with the themed confirm dialog.
- Settings → Profile gets "Change password" (current, new, repeat) below the name.
- No page scrollbar: the list scrolls inside its region, like the other pages.

## Risks

- An administrator who forgets the password of the only administrator account: `streamgo user passwd` remains.
- Demoting or disabling races between two administrators, in two browsers or two processes: the rule is checked
  under row locks in the write itself (U3), so it holds.
- The CLI (`user admin --revoke`, `user disable`) does not apply the rule: the operator has a shell and can undo it.
- Two first accounts created at the same moment may both be administrators (U2): harmless, and either can demote
  the other.
