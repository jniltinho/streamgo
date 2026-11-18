# Livelier studio controls, with UI SDK where it fits

## Why

The owner asked for a more modern, more dynamic web app, built with UI SDK (https://ui-sdk.dev): icons that react,
sound, the camera, and more. The studio's controls today are plain buttons: nothing moves when a state changes, a
device is chosen only inside a settings dialog, nothing tells you your microphone is being heard except a small mark
in the backstage, and people come and go in silence.

## What UI SDK is, and is not

UI SDK (`@ui-sdk/vue` and `@ui-sdk/core`, Apache-2.0, by Vercel Labs; source at
https://github.com/vercel-labs/ui, documentation at https://ui-sdk.dev/docs/vue) is a library of unstyled components and
behaviours: menus, tooltips, popovers, sliders, meters, drag and drop, dragging and resizing, transitions, a
pannable canvas for node graphs. You bring the markup and the styles; it brings keyboard, focus and screen-reader
behaviour. That suits StreamGo: its 28 themes style everything through role tokens.

It has no icons, no sound and no camera code. Those parts of the request are StreamGo's own work in this change.
It is also young: version 0.1.0-beta.10, first published three weeks before this proposal, and its own
documentation says its APIs may still change. The change is shaped around that.

## What Changes

A first slice, in the studio, that the owner validates on a local stack before anything else is built on it:

- **Device menus** (UI SDK Menu): an arrow beside the Mic and the Camera buttons opens the list of microphones or
  cameras; choosing one switches at once. The settings dialog stays for everything else.
- **A level meter for your microphone** (UI SDK Meter) beside the Mic button: it moves when you are heard.
- **Tooltips** (UI SDK Tooltip) on the icon-only controls, on hover and on keyboard focus, in the theme's colours,
  in place of the browser's `title`.
- **Icons that react** (StreamGo's own CSS): a short motion when a control changes state, an animated mark while
  someone speaks, a pulse on the live mark. None of it with "reduce motion" on.
- **Sound cues** (StreamGo's own, synthesised, no audio file): someone joins or leaves, the broadcast starts or
  ends. They play on the listener's own device and are never wired to the program. Because speakers and a shared
  screen can still carry a sound into the program, cues are silent for everyone while a broadcast runs, unless a
  person turns them on. The switches are in the studio's Device settings, for owners and guests.
- **A device switch that fails safely**: choosing a microphone or a camera that cannot be used goes back to the
  previous one, in the menus and in the settings dialog alike.

- **Dialogs and toasts on UI SDK** (the owner's decision): every modal, the confirmation and alert dialogs and the
  toasts keep their look in the 28 themes and their behaviour, and get their focus handling, dismissal and
  announcements from UI SDK's Dialog, Alert Dialog and Toast. The three components the whole app already goes
  through (`AppModal.vue`, `DialogHost.vue`, `ToastStack.vue`) are where the library is used, so no view changes.

UI SDK is used only inside small wrappers under `frontend/src/components/` (`ui/` and `AppModal.vue`), at an exact
pinned version, so that a breaking beta release is fixed in one place and can be dropped without touching the
studio.

**As UI SDK matures, StreamGo follows it.** The owner's direction is to upgrade as the library improves and to take
more of its components over time. Each upgrade is a deliberate step (a new exact version, the wrappers adjusted,
the suites run), and each new component arrives as a wrapper of its own with its recipe. The candidates already
named below are the first ones.

## Not in this change

Kept for later changes, once the first slice is validated:

- dragging a backstage card onto the stage and reordering the stage by dragging (UI SDK drag and drop);
- moving and resizing the logo and the banner on the stage (UI SDK dragging and resizing);
- a command palette;
- volume per person in the program.

Not planned:

- replacing StreamGo's select with UI SDK's: it is themed and tested, and the owner asked for dialogs and toasts
  only;
- UI SDK's canvas behaviour (pan, zoom and connecting ports): the stage is a fixed 16:9 composition, not a board.

## Capabilities

### New Capabilities

- `studio-interactions`: the device menus, the microphone meter, tooltips, state motion, sound cues and the rules
  that contain the library.

### Modified Capabilities

None formally (`add-live-studio` is not archived yet); `studio-ui` gains what this capability describes.

## Impact

- `frontend/package.json`: the Vue adapter and the core, as UI SDK's install guide gives them for Vue
  (`npm install @ui-sdk/vue @ui-sdk/core`), each at an exact version; a lint rule that allows importing
  them only under `frontend/src/components/ui/`; a check after the production build that fails when the entry or a route grows beyond a stated ceiling;
  `package-lock.json`.
- `AppModal.vue`, `ui/DialogHost.vue` and `ui/ToastStack.vue` rebuilt on UI SDK behind the same props, events and
  feedback API (`toast.*`, `askConfirm`, `showAlert`); the dialog and toast recipes of `style.css` moved from the
  native `<dialog>` to the elements UI SDK renders, for every theme.
- New `frontend/src/components/ui/UiMenu.vue`, `UiTooltip.vue`, `UiMeter.vue`; `ControlsDock.vue`,
  `BackstageRow.vue`, `StudioHeader.vue`, `LayoutPicker.vue`; `stores/studio.ts` (the device switch, the meter's
  analyser, the cue events); a small cue player in `frontend/src/core/` (no Vue import) and a Sounds section in
  `DeviceSettingsModal.vue`; both translation catalogs; `style.css` (motion, the three wrappers' recipes
  with role tokens).
- UI SDK's agent skill (`npx skills@latest add vercel-labs/ui --skill ui-sdk`): `skills-lock.json` at the
  repository root records its source and commit, and a line in `AGENTS.md` tells agents to restore and read it
  before touching a `Ui*` wrapper.
- `THIRD_PARTY` (Apache-2.0 notice of UI SDK, with its repository), `CHANGELOG.md`, `docs/README.md` (what UI SDK
  is used for, the link to https://github.com/vercel-labs/ui, and how to upgrade it).
- No server change, no migration, no new setting.
