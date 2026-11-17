# Spec Delta

## ADDED Requirements

### Requirement: Single-page app
The server SHALL serve the embedded web app at every path that is not an API, asset or health route, with client
routes `/login`, `/broadcasts`, `/destinations`, `/studio/:id` and `/join/:token`. Routes other than `/login` and
`/join/:token` SHALL require a session, sending a signed-out visitor to `/login` and back to where they were
after signing in.

#### Scenario: Deep link
- **WHEN** a signed-out visitor opens `/studio/<id>` and signs in
- **THEN** they land on `/studio/<id>`

#### Scenario: Unknown API route
- **WHEN** a request is made to an unknown path under `/api/`
- **THEN** the response is a JSON 404, not the web app

### Requirement: Broadcasts and destinations pages
The broadcasts page SHALL list the user's broadcasts with their state and let them create, rename, delete and
open one in the studio. The destinations page SHALL list, create, edit and delete destinations, showing whether a
key is set and never the key.

#### Scenario: Create and open
- **WHEN** the user creates a broadcast from the broadcasts page and opens it
- **THEN** the studio for that broadcast loads

#### Scenario: Key field
- **WHEN** the user edits a destination that has a key
- **THEN** the key field is empty with a note that a key is set, and saving without typing keeps it

### Requirement: Pages on large screens
The sign-in, broadcasts and destinations pages SHALL follow the same scale as the studio and keep their content
in a centred column of limited width, so lines do not stretch across a QHD or 4K window.

#### Scenario: Broadcasts page at 4K
- **WHEN** the broadcasts page is opened in a 3840x2160 window
- **THEN** its content is a centred column no wider than half the window, with text at twice its Full HD size

### Requirement: Language
The web app SHALL be in English by default with a pt-BR catalog, choose the language from the user's preference,
else the browser's, and let the user change it without a reload. Every catalog SHALL have the same keys.

#### Scenario: Catalog parity
- **WHEN** the catalog test compares `en` and `pt-BR`
- **THEN** neither has a key the other lacks

#### Scenario: Switch language
- **WHEN** the user picks Português (Brasil)
- **THEN** the visible text changes at once and the choice is kept after signing in elsewhere

### Requirement: Feedback
The web app SHALL report results and ask for confirmation with themed toasts, alerts and dialogs, and SHALL never
call the browser's `alert`, `confirm` or `prompt`. API errors SHALL be shown with a human-readable message.

#### Scenario: No native dialogs
- **WHEN** the lint check searches the frontend sources
- **THEN** it finds no call to `alert(`, `confirm(` or `prompt(`

#### Scenario: Delete confirmation
- **WHEN** the user deletes a broadcast
- **THEN** a themed dialog asks for confirmation and a toast confirms the result
