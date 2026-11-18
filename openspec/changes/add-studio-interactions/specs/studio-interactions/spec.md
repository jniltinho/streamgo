# Studio interactions

## ADDED Requirements

### Requirement: Device menus
A person in the studio, owner or guest, SHALL choose their microphone and their camera from a menu beside each
control, without opening the settings.

#### Scenario: Choose another microphone
- **GIVEN** two microphones, the browser's permission, and Default checked
- **WHEN** the person opens the menu beside Mic and chooses the second microphone
- **THEN** their audio comes from the second microphone, the menu closes, focus returns to the arrow, and the
  second is the checked item in the menu and the selected one in the settings dialog's select

#### Scenario: Keyboard
- **GIVEN** the cameras "Front" and "Back" in that order after Default, Default checked, focus on the arrow beside
  Camera
- **WHEN** the person presses Enter, the down arrow twice, Enter
- **THEN** the menu opened with focus on Default, "Back" was chosen, the menu closed and focus is on the arrow;
  Escape in an open menu closes it and changes nothing

#### Scenario: Chosen while off
- **GIVEN** the camera off
- **WHEN** the person chooses another camera in the menu
- **THEN** the camera stays off, and turning it on uses the chosen one

#### Scenario: Labels not given yet
- **GIVEN** a browser that lists devices without labels
- **WHEN** the menu opens
- **THEN** its items are numbered ("Microphone 1"), and after a toggle that made the browser grant permission the
  next opening shows the real names

#### Scenario: Nothing to choose
- **GIVEN** no camera, or camera permission denied
- **WHEN** the arrow beside Camera gets keyboard focus
- **THEN** it is focusable and marked disabled, opens nothing, and its tooltip says why

#### Scenario: A device that cannot be used
- **WHEN** switching to a chosen camera is refused or reports failure
- **THEN** the previous camera is started again, the menu and the select show the previous camera, and an error
  toast says the chosen one could not be used

#### Scenario: Neither device can be used
- **WHEN** the switch fails and the previous camera cannot be started again either
- **THEN** the Camera control shows off, the toast says the camera is unavailable, and nothing claims a camera is
  in use

#### Scenario: A device unplugged
- **WHEN** a microphone is unplugged
- **THEN** the menu no longer lists it the next time it opens

#### Scenario: A long list in a small window
- **GIVEN** a window of 1280 by 720 and twelve microphones with long names
- **WHEN** the menu opens
- **THEN** it is wholly inside the window, scrolls inside itself, and the page has no scrollbar

### Requirement: Microphone meter
The studio SHALL show a person the level of their own microphone while it is on, before joining and in the room.

#### Scenario: Heard
- **GIVEN** a microphone that is on and receives a steady tone at half of full scale
- **WHEN** one second has passed
- **THEN** the meter shows more than a quarter of its range and its text value says the microphone is heard

#### Scenario: Silent
- **GIVEN** a microphone that is on and a level below 0.05
- **THEN** the meter's text value says silent, and from 0.05 up it says heard

#### Scenario: Off
- **WHEN** the microphone is turned off
- **THEN** the meter is empty and its analyser stops sampling

#### Scenario: Stopping the meter stops nothing else
- **WHEN** the tab is hidden while the microphone is on, then shown again
- **THEN** the analyser stopped and started again, and the microphone was captured and published the whole time

#### Scenario: Leaving
- **WHEN** the room disconnects, the person leaves, or another tab takes the studio over
- **THEN** the analyser is stopped and nothing of it remains

#### Scenario: After a switch
- **WHEN** the person chooses another microphone, or the connection is made again
- **THEN** the meter follows the microphone now in use

#### Scenario: It does not change the program
- **WHEN** the meter runs while the broadcast is live
- **THEN** its analyser is connected to no destination and the program's output measures the same with and
  without it

### Requirement: Tooltips
The dock's Device settings button and device arrows, the icon buttons of the studio header, the layout thumbnails
and the icon marks of a backstage card SHALL show their name in a themed tooltip, on hover and on keyboard focus.

#### Scenario: Focus
- **WHEN** the Device settings button gets keyboard focus
- **THEN** a tooltip with "Device settings" shows at once in the theme's tooltip colours, the button has no `title`
  attribute and keeps its accessible name, and Escape hides the tooltip until the button is entered again

#### Scenario: Hover
- **WHEN** the pointer rests on a layout thumbnail for 500 ms, then leaves
- **THEN** the tooltip with the layout's name showed, then hid

#### Scenario: Buttons with text, and reasons
- **WHEN** a control already shows its name as text, or carries a `title` that gives a reason or an error
- **THEN** it has no name tooltip, and the reason or the error is still there

### Requirement: State motion
The dock's controls, the speaking mark and the live mark SHALL move when their state changes, and SHALL NOT when
the person asked for reduced motion.

#### Scenario: A control changes
- **WHEN** the microphone is turned off
- **THEN** its icon changes with a motion of at most 300 ms that animates only transform and opacity

#### Scenario: Speaking and live
- **WHEN** a person on the backstage is heard, and when the broadcast is live
- **THEN** that person's speaking mark is animated, and the live mark pulses

#### Scenario: Reduced motion
- **GIVEN** the browser asks for reduced motion, or the app is in flat mode, also when that starts while an effect
  runs
- **WHEN** the same things happen
- **THEN** none of the three effects runs, every state is still told by its icon and its text, and the meter still
  shows the level

### Requirement: Sound cues
The studio SHALL play a short sound on the listener's own device when someone joins or leaves the backstage and
when the program goes on air or off air; the cue player SHALL never be connected to the program mix, and cues
SHALL be silent while the program is on air unless the listener turned them on.

#### Scenario: Someone joins
- **GIVEN** sounds on, the program off air, and the audio started by a click
- **WHEN** a guest joins the backstage
- **THEN** the owner and the other guests hear the join cue once, and the guest who joined does not

#### Scenario: Not a join
- **WHEN** the listener enters a studio that already has people, a participant shares a screen, mutes, or the
  connection is made again
- **THEN** no join or leave cue plays

#### Scenario: Not wired to the program
- **WHEN** a cue plays while the program is on air with "Sounds while on air" on
- **THEN** the cue player's audio context is not the mix's, no node connects the two, and the program's output
  measured from the mix is the same with and without the cue

#### Scenario: Silent while on air
- **GIVEN** an owner or a guest with "Sounds while on air" off (the default)
- **WHEN** the program goes on air, and a guest joins while it is on air
- **THEN** that participant hears neither cue; nothing is played later for them; when the program goes off air
  they hear the off-air cue, and a join after that is heard

#### Scenario: On air and off air
- **GIVEN** an owner or a guest with "Sounds while on air" on
- **WHEN** `stage.on_air` turns true, then false
- **THEN** they hear the on-air cue, then the off-air cue; a participant who arrives while the program is on air
  hears no cue for the state they found

#### Scenario: Turned off, from inside the studio
- **GIVEN** an owner or a guest who turned "Sounds" off in the studio's Device settings dialog
- **WHEN** anything that has a cue happens, also after a reload
- **THEN** no cue plays

#### Scenario: Storage that cannot be used
- **GIVEN** a browser that blocks storage, or a stored value that is not a preference
- **THEN** sounds are on and sounds while on air are off

#### Scenario: Audio not started
- **GIVEN** a tab where nothing was clicked yet
- **WHEN** a cue is due
- **THEN** nothing plays, nothing is queued for later and no error is logged

#### Scenario: Many at once
- **WHEN** five guests join within one second, and a sixth 1.2 s after the first
- **THEN** the first and the sixth play a cue and the other four do not

#### Scenario: Short and quiet
- **WHEN** a cue plays
- **THEN** it lasts at most 250 ms and its gain never passes 0.15

### Requirement: Dialogs and toasts on UI SDK
Every modal, the confirmation and alert dialogs and the toasts SHALL be built on UI SDK inside `AppModal.vue`,
`DialogHost.vue` and `ToastStack.vue`, with the behaviour and the look they have today.

#### Scenario: Nothing else changes
- **WHEN** the three components are moved
- **THEN** that move changed no view and no other modal file, and `toast.*`, `askConfirm` and `showAlert` keep
  their signatures

#### Scenario: A modal behaves as before
- **WHEN** a modal opens from a button, and Escape is pressed
- **THEN** focus started on the first control of the modal's body, the page behind it could not be reached by Tab
  or by a click, a click outside the modal did not close it, the modal closed on Escape and focus is on the button
  again

#### Scenario: A modal over a modal
- **GIVEN** the Invite modal open in the studio
- **WHEN** Settings, or a confirmation, opens over it, and Escape is pressed
- **THEN** only the upper one closed, focus is inside the lower one, the lower one is still modal, and the two had
  different title ids

#### Scenario: A confirmation behaves as before
- **WHEN** a destructive confirmation opens
- **THEN** it is an `alertdialog`, focus is on Cancel, Escape answers no, and a click outside it does nothing

#### Scenario: A typed confirmation
- **WHEN** a typed confirmation opens and the person types the word with other letter case and spaces around it
- **THEN** focus started on the input, OK was disabled until the text matched, and OK then answers yes

#### Scenario: An alert
- **WHEN** `showAlert` shows a message
- **THEN** it is a `dialog`, not an `alertdialog`, and closes on its button and on Escape

#### Scenario: Requests wait their turn
- **WHEN** two confirmations are asked at once
- **THEN** the second shows after the first was answered, and each promise settles with its own answer

#### Scenario: Toasts behave as before
- **WHEN** six toasts are shown one after another
- **THEN** five show and the oldest is gone; an error stays 8 s and the others 4 s; the time of a toast stops
  while the pointer or focus is on it; each closes by its button

#### Scenario: Announced once
- **WHEN** an error toast and an information toast are shown
- **THEN** the error is announced in an assertive live region and the information in a polite one, each once, and
  the visible cards are not live regions

#### Scenario: A toast over a modal
- **WHEN** a toast is shown while a modal is open, and when a modal opens while a toast is shown
- **THEN** in both cases the toast is above the modal and a click on its close control closes it

#### Scenario: The look in every theme
- **WHEN** a modal, a confirmation and the four toasts are shown in each of the 28 themes
- **THEN** each theme's title bar, close control, place below the top, backdrop and toast recipe are the ones it
  had, the close control of each theme closes the dialog, the centre of the title is within 2 px of the centre of
  the bar in the themes that centre it, the contrast matrix holds and no theme file changed

#### Scenario: A piece the library cannot do yet
- **WHEN** UI SDK's Dialog, Alert Dialog or Toast cannot meet one of the scenarios above
- **THEN** that component keeps its current implementation, the other two move, and the change's report names the
  scenario that failed

### Requirement: The library is contained
UI SDK SHALL be used at one exact version and only through StreamGo's wrapper components.

#### Scenario: Imported in one place
- **WHEN** a file other than `frontend/src/components/AppModal.vue` and the files under
  `frontend/src/components/ui/` imports `@ui-sdk/vue` or `@ui-sdk/core`
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
- **WHEN** the production app is built
- **THEN** the entry, and the studio and guest routes with the shared chunks they import and their CSS, measured
  after gzip from the build manifest, are each within the ceiling the size check states, and the check ran in
  `make check`

#### Scenario: The lock file
- **WHEN** the dependencies are installed with `npm ci`
- **THEN** they install from the committed `package-lock.json`

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
- **WHEN** the studio is rendered in each of the 28 themes with a device menu open (an item highlighted, one
  checked, one focused), a tooltip shown and the meter half full
- **THEN** the accessibility check finds no violation and the contrast matrix holds for the new pairs, the meter's
  fill against its track among them

#### Scenario: Both languages
- **WHEN** the studio is used in English and in Brazilian Portuguese, also after the language changes while it is
  open
- **THEN** the menus, the tooltips, the meter's text value and the sound settings are in that language

#### Scenario: A guest in Firefox
- **WHEN** a guest joins in Firefox
- **THEN** the device menus, the tooltips and the meter work for that guest

#### Scenario: Tokens only
- **WHEN** the recipes of the three wrappers are read
- **THEN** they use role tokens and `rem` sizes, and declare no colour and no radius of their own
