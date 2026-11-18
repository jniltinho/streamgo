# Studio interactions

## ADDED Requirements

### Requirement: Device menus
A person in the studio SHALL choose their microphone and their camera from a menu beside each control, without
opening the settings.

#### Scenario: Choose another microphone
- **GIVEN** two microphones and the browser's permission
- **WHEN** the person opens the menu beside Mic and chooses the second
- **THEN** their audio comes from the second microphone, the menu closes, focus returns to the arrow, and the
  second is the checked item the next time the menu opens

#### Scenario: Keyboard
- **WHEN** the arrow beside Camera has focus and the person presses Enter, the down arrow, Enter
- **THEN** the menu opened, the second camera was chosen and focus is on the arrow again; Escape in an open menu
  closes it and changes nothing

#### Scenario: No permission yet
- **GIVEN** a browser that has not been allowed to use the microphone
- **WHEN** the person looks at the arrow beside Mic
- **THEN** it is disabled and its tooltip says the microphone must be allowed first

#### Scenario: A device that cannot be used
- **WHEN** switching to a chosen camera fails
- **THEN** an error toast says so and the camera in use is still the previous one

#### Scenario: A device unplugged
- **WHEN** a microphone is unplugged while the menu is closed
- **THEN** the menu no longer lists it the next time it opens

### Requirement: Microphone meter
The studio SHALL show a person the level of their own microphone while it is on.

#### Scenario: Heard
- **GIVEN** a microphone that is on and receives a steady tone
- **WHEN** the person looks at the meter beside Mic
- **THEN** it shows a level above empty and its text value for screen readers says the microphone is heard

#### Scenario: Off
- **WHEN** the microphone is off
- **THEN** the meter is empty and nothing reads the microphone

#### Scenario: It does not change the program
- **WHEN** the meter runs while the broadcast is live
- **THEN** the program's audio graph has no node of the meter

### Requirement: Tooltips
Every control of the studio that shows only an icon SHALL show its name in a themed tooltip, on hover and on
keyboard focus.

#### Scenario: Focus
- **WHEN** the Device settings button gets keyboard focus
- **THEN** a tooltip with "Device settings" shows at once in the theme's tooltip colours, the button has no `title`
  attribute, and Escape hides the tooltip

#### Scenario: Buttons with text
- **WHEN** a control already shows its name as text
- **THEN** it has no tooltip

### Requirement: State motion
Controls and marks SHALL move when their state changes, and SHALL NOT when the person asked for reduced motion.

#### Scenario: A control changes
- **WHEN** the microphone is turned off
- **THEN** its icon changes with a motion of at most 300 ms that animates only transform and opacity

#### Scenario: Speaking and live
- **WHEN** a person on the backstage is heard, and when the broadcast is live
- **THEN** that person's speaking mark is animated, and the live mark pulses

#### Scenario: Reduced motion
- **GIVEN** the browser asks for reduced motion, or the app is in flat mode
- **WHEN** the same things happen
- **THEN** no animation runs and every state is still told by its icon and its text

### Requirement: Sound cues
The studio SHALL play a short sound on the listener's own device when someone joins or leaves the backstage and
when the broadcast starts or ends, and a cue SHALL never be part of the program.

#### Scenario: Someone joins
- **GIVEN** sounds on and the audio already started by a click
- **WHEN** a guest joins the backstage
- **THEN** the others in the studio hear the join cue once, and the guest does not

#### Scenario: Never in the program
- **WHEN** a cue plays while the broadcast is live
- **THEN** the program's audio graph has no node of the cue player, which has an audio context of its own

#### Scenario: Silent for the host while live
- **GIVEN** an owner with "Sounds while live" off (the default)
- **WHEN** a guest joins during a live broadcast
- **THEN** the owner hears no cue, and hears it again once the broadcast is over

#### Scenario: Turned off
- **GIVEN** "Sounds" turned off in Settings
- **WHEN** anything that has a cue happens, also after a reload
- **THEN** no cue plays

#### Scenario: Audio not started
- **GIVEN** a tab where nothing was clicked yet
- **WHEN** a cue is due
- **THEN** nothing plays, nothing is queued for later and no error is logged

#### Scenario: Many at once
- **WHEN** five guests join within one second
- **THEN** one join cue plays

### Requirement: The library is contained
UI SDK SHALL be used at one exact version and only through StreamGo's wrapper components.

#### Scenario: Imported in one place
- **WHEN** a file outside `frontend/src/components/ui/` imports `@ui-sdk/vue` or `@ui-sdk/core`
- **THEN** `make check` fails naming the file

#### Scenario: Pinned
- **WHEN** `frontend/package.json` is read
- **THEN** it has `@ui-sdk/vue` and `@ui-sdk/core` at the same exact version, with no range, and no other
  `@ui-sdk` adapter

#### Scenario: Imported by entry
- **WHEN** a wrapper imports a component of the library
- **THEN** the import is from that component's entry (such as `@ui-sdk/vue/menu`), not from the package root

#### Scenario: The layer is declared
- **WHEN** `frontend/src/style.css` is read
- **THEN** `@layer ui-sdk;` comes before the Tailwind import, and the library's default stylesheet is not imported

#### Scenario: The agent skill is recorded
- **WHEN** the repository is read
- **THEN** `skills-lock.json` names the `ui-sdk` skill of `vercel-labs/ui` with its commit, and `AGENTS.md` says to
  restore and read it before changing a `Ui*` wrapper

#### Scenario: Size
- **WHEN** the app is built
- **THEN** its entry chunk is within the budget the size test states

#### Scenario: Notice
- **WHEN** `THIRD_PARTY` is read
- **THEN** it names UI SDK, its version, its Apache-2.0 licence and its repository,
  https://github.com/vercel-labs/ui

#### Scenario: How to upgrade is written down
- **WHEN** a developer reads `docs/README.md`
- **THEN** it says what UI SDK is used for, links to https://github.com/vercel-labs/ui, and gives the steps of an
  upgrade: both packages at the same exact version, the wrappers, the checks and suites, the size numbers

### Requirement: In every theme
The menus, the tooltips and the meter SHALL be styled by role tokens and SHALL meet the contrast rules in every
theme.

#### Scenario: Contrast
- **WHEN** the studio is rendered in each theme with a device menu open and a tooltip shown
- **THEN** the accessibility check finds no violation and the contrast matrix holds for the new pairs

#### Scenario: Tokens only
- **WHEN** the recipes of the three wrappers are read
- **THEN** they use role tokens and `rem` sizes, and declare no colour and no radius of their own
