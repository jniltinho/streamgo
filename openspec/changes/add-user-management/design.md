# Design: user management

## Decisions

### U1. A boolean, not roles
`users.is_admin` (a boolean, NOT NULL, default false). Two kinds of account are all this version needs; a role
table would be a model with one row. Everything a non-administrator does today is unchanged.

### U2. Who is an administrator
- Migration `00002_user_admin`: adds the column and sets it for the account with the smallest id (ids are ULIDs,
  so that is the oldest account). An install with no account has nothing to set.
- `account.Service.Create`: when no account exists yet, the new one is an administrator, whatever was asked. This
  covers the first `streamgo user add` of a new install.
- CLI: `streamgo user add --admin`; `streamgo user admin <email>` and `streamgo user admin <email> --revoke`;
  `streamgo user list` shows the role. The CLI may leave an install without an administrator (the operator has
  a shell and can undo it); the API may not.

### U3. Authorization lives in the core
`account.Service` gets `ListFor`, `CreateBy`, `UpdateBy`: each takes the acting user's id and answers
`core.ErrForbidden` (a new sentinel, HTTP 403 `forbidden`) when that user is not an enabled administrator. The
handlers only translate. Rules checked in the core, in one transaction-free read-then-write that the single
process makes safe behind `account.Service`'s own mutex for these three operations:
- an administrator cannot disable or demote themself;
- the last enabled administrator cannot be disabled or demoted (`core.ErrConflict`, 409, with a message).

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
- Demoting or disabling races between two administrators in two browsers: both requests go through the same
  process and the same mutex, so the last-administrator rule holds.
