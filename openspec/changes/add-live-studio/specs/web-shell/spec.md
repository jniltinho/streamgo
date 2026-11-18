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

### Requirement: Modals
Every modal SHALL be a component in its own file, open 5 % of the window height below the top, centred
horizontally, in every theme, and scroll inside itself when its content is taller than the window.

#### Scenario: Modal position
- **WHEN** any modal or themed dialog opens in a 1280x720 or a 1920x1080 window
- **THEN** its top edge is 5 % of the window height below the top

### Requirement: Theme and language modal
The theme and the language SHALL be chosen in one modal with a tab each, opened from the sign-in page and from the
signed-in header, which carry no theme or language control of their own.

#### Scenario: Sign-in page is only the form
- **WHEN** the sign-in page opens
- **THEN** it shows the e-mail, the password, the sign-in button and one "Theme and language" button, and the
  page does not scroll

#### Scenario: Theme and language modal
- **WHEN** "Theme and language" is opened
- **THEN** the Theme tab shows the 28 themes without scrolling at 1280x720, and the Language tab the language
  choice

### Requirement: Profile and header
The signed-in header SHALL show the user's name as a button that opens the settings on a Profile tab, a settings
button and a sign-out button, the last two as icons with accessible names. The Profile tab SHALL change the
user's display name, which is the name on their name tag in the studio.

#### Scenario: Change the display name
- **WHEN** a signed-in user opens their profile from the header, types a new name and saves
- **THEN** `PATCH /me` stores the trimmed name, the header shows it at once, and the studio shows it on the name
  tag the next time the user opens it; an empty name is refused with 422 and changes nothing

### Requirement: No page scrollbar
No page SHALL show a page scrollbar: the document never scrolls, and a list longer than its region scrolls inside
that region.

#### Scenario: No page scrollbar
- **WHEN** the sign-in, broadcasts or destinations page is open
- **THEN** the document's height does not exceed the window's

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
