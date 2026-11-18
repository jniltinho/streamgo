# user-management Specification

## Purpose
Administrators and what they do in the app: the Users page and its routes for creating, editing, disabling and enabling accounts, the last-administrator rule, and every user's own password.

## Requirements

### Requirement: Administrators
An account SHALL be an administrator or not. The oldest enabled account of an install that existed before this
change SHALL become an administrator when the migration runs, or the oldest account when none is enabled, and the
first account created on an install without accounts SHALL be one, also when two creations race. The CLI SHALL
create administrators and grant or revoke the role.

#### Scenario: Existing install
- **GIVEN** a database with the accounts ana (older) and bruno, both enabled
- **WHEN** the migration `00002_user_admin` runs
- **THEN** ana is an administrator and bruno is not, on SQLite, MariaDB and PostgreSQL

#### Scenario: Existing install whose oldest account is disabled
- **GIVEN** a database with the accounts ana (older, disabled) and bruno (enabled)
- **WHEN** the migration runs
- **THEN** bruno is the administrator and ana is not; when both are disabled, ana is, on the three databases

#### Scenario: First account
- **GIVEN** an install with no account
- **WHEN** `streamgo user add` creates one without `--admin`
- **THEN** that account is an administrator, and the next one created the same way is not

#### Scenario: First accounts created at the same time
- **GIVEN** an install with no account
- **WHEN** several accounts are created at the same time (two CLI processes, or a CLI and the server)
- **THEN** at least one of them is an administrator

#### Scenario: Grant and revoke from the CLI
- **WHEN** the operator runs `streamgo user admin bruno@example.com` and later the same with `--revoke`
- **THEN** bruno is an administrator after the first and not after the second, and `streamgo user list` shows
  the role each time

### Requirement: Users routes
`GET /users`, `POST /users` and `PATCH /users/:id` SHALL serve only an enabled administrator and answer 403
`forbidden` to any other signed-in user. `GET /me` SHALL say whether the user is an administrator.

#### Scenario: Not an administrator
- **WHEN** bruno, who is not an administrator, calls each of the three routes
- **THEN** each answers 403 `forbidden` and nothing changes

#### Scenario: Create
- **WHEN** an administrator posts an e-mail, a name and a password
- **THEN** the account exists, can sign in with that password, and is not an administrator unless `admin` was
  true; a duplicate e-mail in any letter case answers 409, a short password 422 with the field `password`

#### Scenario: Edit
- **WHEN** an administrator patches another account's name, role, state or password
- **THEN** only the given fields change, all of them or none in one write, and a new password or a disabled
  state ends that account's sessions in that same write

#### Scenario: Not themself
- **WHEN** an administrator patches their own account with `disabled: true` or `admin: false`
- **THEN** the answer is 409 and nothing changes

#### Scenario: Last administrator
- **GIVEN** ana and bruno are the only enabled administrators
- **WHEN** ana demotes or disables bruno, and bruno demotes or disables ana, each request allowed on its own
- **THEN** the first succeeds and the second is refused, so an enabled administrator remains: 403 when bruno is
  no longer an administrator at its check, and 409 with a message that an install needs an administrator when
  both passed their checks together (two requests at once, or two processes); the refusal is made under row
  locks in the same write, so it holds on SQLite, MariaDB and PostgreSQL

### Requirement: Own password
A signed-in user SHALL change their own password by giving the current one.

#### Scenario: Change
- **WHEN** a user puts the current password and a valid new one to `PUT /me/password`
- **THEN** the answer is 204, the new password signs in, the old one does not, the user's other sessions end
  and the current one stays

#### Scenario: Wrong current password
- **WHEN** the current password is wrong
- **THEN** the answer is 422 with the field `current` and the password is unchanged

### Requirement: Users page
The web app SHALL show administrators a Users page with the accounts and the actions on them, each modal in its
own file, and SHALL not show the page or its link to other users.

#### Scenario: List and create
- **WHEN** an administrator opens Users and creates an account in the "New user" modal
- **THEN** the list shows every account with its name, e-mail, role and state, and the new one appears with a
  toast

#### Scenario: Edit, disable, enable
- **WHEN** an administrator edits an account, disables it after a themed confirmation, and enables it again
- **THEN** the list follows each change, and the controls that would disable or demote the administrator
  themself are disabled with a reason

#### Scenario: Hidden from others
- **WHEN** a user who is not an administrator signs in
- **THEN** the header has no Users link and `/users` leads to the broadcasts page

#### Scenario: Change my password
- **WHEN** a user fills the current password, a new one and its repetition in Settings → Profile
- **THEN** a success toast confirms it, and a wrong current password or a different repetition is shown at the
  field without a request for the latter
