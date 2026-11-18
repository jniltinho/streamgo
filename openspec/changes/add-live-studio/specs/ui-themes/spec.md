# Spec Delta

## ADDED Requirements

### Requirement: Theme catalog
The system SHALL offer 27 themes, each with an id, a label and a `light` or `dark` scheme: StreamGo's own pair,
`streamgo-dark` (default) and `streamgo-light`, the 20 themes carried over from MeetHub's catalog with their ids,
labels and looks, and StreamGo's own `macos`, `macos-retro`, `ubuntu`, `obs-dark` and `obs-light`.

#### Scenario: Catalog
- **WHEN** the registry is listed
- **THEN** it holds, in order, `streamgo-dark`, `streamgo-light`, `default`, `admanager`, `breeze`, `breeze-dark`,
  `violet`, `violet-dark`, `aurora`, `aurora-dark`, `crystal`, `crystal-dark`, `cone`, `classic-blue`,
  `classic-gray`, `graphite`, `grafana`, `rabbitmq`, `matrix`, `winamp`, `pink-panther`, `high-contrast`, `macos`,
  `macos-retro`, `ubuntu`, `obs-dark` and `obs-light`

#### Scenario: Theme list from the API
- **WHEN** a client requests `GET /api/v1/themes`
- **THEN** it receives the 27 ids, labels and schemes in catalog order, with `streamgo-dark` marked as the default

#### Scenario: Registry, styles and picker agree
- **WHEN** the theme lockstep test runs
- **THEN** every registry id has a `[data-theme='<id>']` style rule and a picker entry, no style rule or picker
  entry exists without a registry id, and every theme file is imported by the base stylesheet

#### Scenario: Carried-over themes look the same
- **WHEN** the feedback gallery (buttons, fields, cards, tabs, toasts, alerts, dialogs) is rendered in each
  carried-over theme
- **THEN** every role token resolves to the same value as in MeetHub's theme of the same id

#### Scenario: The refined XP theme
- **WHEN** the Classic blue (XP) theme is applied
- **THEN** the app's and the studio's header are its blue title bar, its page is the XP desktop (a pale sky over
  a green hill), a dialog has a low title with the red close button and a face in the XP colour, tables stand on
  a surface of their own with the theme's shadow, and its role tokens equal MeetHub's except the page, the text
  roles that stand on it, the links and the field focus

#### Scenario: The refined 98 theme
- **WHEN** the Classic gray (98) theme is applied
- **THEN** the app is a maximised window: its pages are the gray face under the navy-to-blue title bar that the
  app's and the studio's header are, and the teal desktop shows behind the sign-in box; only the primary button
  has the black frame of a default button, fields are white and sunken, tables are a white sunken list under
  raised column heads, a dialog has a low caption with a raised gray close button, every toast has the navy
  caption, and its role tokens equal MeetHub's except the roles that refinement changes

#### Scenario: The macOS theme
- **WHEN** the macOS theme is applied
- **THEN** the header is a light unified toolbar with dark text, actions and selection are the system blue darkened
  for contrast, a dialog is a sheet that slides down with its title centred and a red close light at its left, and
  the text is set in Inter

#### Scenario: The Mac OS Retro theme
- **WHEN** the Mac OS Retro theme is applied
- **THEN** the header is the Platinum menu bar, the page is the pale blue-purple desktop with its fine pattern, a
  dialog has a striped title bar with the title on a plain patch and a square close box at its left, the primary
  button carries the default button's ring, card tabs are folder tabs, and the text is set in Barlow

#### Scenario: The Ubuntu theme
- **WHEN** the Ubuntu theme is applied
- **THEN** the header is the dark aubergine top bar with light text and an orange line under it, actions and
  selection are Ubuntu orange darkened for contrast, a dialog's close button is an orange circle at the right of a
  centred title, and the text is set in Ubuntu

#### Scenario: The OBS Studio themes
- **WHEN** the OBS Studio (dark) or OBS Studio (light) theme is applied
- **THEN** surfaces are flat and nearly shadowless on a blue-grey (or light grey) window, buttons are flat grey,
  actions, selection and the current tab are the OBS blue, and the text is set in Open Sans

### Requirement: Bundled fonts
Every font family a theme or the program names SHALL be bundled with the web app, so the interface and the
program's text look the same on any system; only a generic family follows as the fallback.

#### Scenario: No system font
- **WHEN** the theme files, the base stylesheet and the compositor are read
- **THEN** every named family is Inter, Noto Sans, JetBrains Mono, Barlow, Ubuntu or Open Sans, each has its font
  files in the app, and each stack ends in `sans-serif` or `monospace`

#### Scenario: Fonts load
- **WHEN** the app opens in the default, the Breeze and the Matrix theme
- **THEN** the page's text is drawn with the bundled Inter, Noto Sans and JetBrains Mono

### Requirement: Theme contract
The role-token contract SHALL be MeetHub's (text, page and surfaces, borders, primary, danger, success, warning,
focus, tabs, and the toast, alert and dialog roles) extended with the studio's roles (on-air, stage frame, on-stage
and backstage marks), each studio role defaulting to an existing role so that a carried-over theme needs no
change. Every theme SHALL resolve every role token to a valid colour, and components SHALL use only role tokens
for colour.

#### Scenario: Unresolved token
- **WHEN** the contract test applies each theme and reads the computed value of every role token
- **THEN** it fails naming any theme and token that does not resolve to a colour

#### Scenario: No literal colours in components
- **WHEN** the style check searches component styles and templates
- **THEN** it finds no hex, `rgb(` or `hsl(` colour outside the theme files and the compositor

### Requirement: Corner radii
Corner radii SHALL be three shared values, declared once in the base stylesheet and by no theme, and SHALL keep
the interface more square than round: 2 px for small elements, 3 px for controls and 4 px for containers at the
base size, as in MeetHub, growing with the interface scale. Only avatars and status dots may be round.

#### Scenario: Shared radii
- **WHEN** the style check reads the theme files and the component styles
- **THEN** no theme declares a border radius, and every border radius in a component is one of the three shared
  tokens or the round token

#### Scenario: Nearly square corners
- **WHEN** any theme is active in a 1920x1080 window
- **THEN** buttons and inputs render with a 3 px radius, cards, panels, toasts, dialogs, the stage and its tiles
  with 4 px, and nothing but avatars and status dots has a larger one

#### Scenario: Corners keep their proportion at 4K
- **WHEN** the same screen is opened in a 3840x2160 window
- **THEN** each radius is twice its Full HD value

### Requirement: Contrast
Every theme SHALL meet WCAG AA on the declared contrast matrix (MeetHub's recipes plus the studio's: 4.5:1 for
text, 3:1 for large text, icons, input borders and focus indicators), and `streamgo-dark`, `streamgo-light`,
`high-contrast`, `crystal`, `crystal-dark` and `classic-blue` SHALL meet AAA (7:1 for text, 4.5:1 for large text).

#### Scenario: Contrast test
- **WHEN** the contrast test renders each of the 27 themes and measures every pair of the matrix
- **THEN** every pair meets its threshold and the list of pending failures is empty

#### Scenario: Studio in every theme
- **WHEN** the studio is rendered in each of the 27 themes
- **THEN** the on-air mark, the destination statuses, the on-stage marks and every control label meet the matrix

### Requirement: Theme picker and persistence
Users SHALL choose a theme from a picker that previews each one; the choice SHALL apply at once without a reload,
be saved in the user's preferences on the server, and follow them to other browsers. Before sign-in and for
guests, the choice SHALL be kept in the browser, and the theme SHALL be applied before the first paint.

#### Scenario: Follows the user
- **WHEN** Ana picks `streamgo-light` and signs in from another browser
- **THEN** the app opens in `streamgo-light`

#### Scenario: Guest choice
- **WHEN** a guest picks a theme on the join page and reloads
- **THEN** the page opens in that theme without flashing the default

#### Scenario: First visit
- **WHEN** someone with no saved choice opens the app
- **THEN** it opens in `streamgo-dark`, or in `streamgo-light` when the browser prefers a light scheme

#### Scenario: Unknown theme
- **WHEN** a client sends `PATCH /api/v1/me` with a theme id that is not in the registry
- **THEN** the response is 422 and the preference is unchanged
