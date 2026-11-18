# Design: livelier studio controls

## Context

The studio is Vue 3.5 with Tailwind 4 and StreamGo's own themed recipes (`style.css`, 28 themes over role tokens).
Icons are `@lucide/vue`. Dialogs, toasts and selects are StreamGo's own components with their own tests. The
program's audio is mixed in the owner's browser by `frontend/src/core/audioMix.ts`.

The plan was reviewed with Codex; its corrections are in the decisions below.

## Decisions

### U1. UI SDK is contained

`@ui-sdk/vue` and `@ui-sdk/core` are pinned to one exact version (no range). They are imported only by wrapper
components: `UiMenu`, `UiTooltip`, `UiMeter`, `DialogHost` and `ToastStack` under `frontend/src/components/ui/`,
and `frontend/src/components/AppModal.vue`. The studio and the pages import the wrappers, never the library; a lint rule in `frontend/vite.config.ts` (beside the one that keeps Vue out of
`frontend/src/core`) fails `make check` otherwise.

Why: the library is three weeks old and says its APIs may change. With wrappers, an upgrade that breaks is a fix
in six files, and removing the library is replacing six files.

A wrapper exposes what the studio needs and nothing else: its props, events and styling hooks are StreamGo's own
(no type of the library in a wrapper's interface, no `data-slot` of the library known outside the wrapper and its
recipe). The lint rule covers the package roots and their subpaths (`@ui-sdk/vue/menu`). `package-lock.json` is
committed with the change. The library's default stylesheet, its preferences provider and any app-wide plugin are
not installed.

Checked on 2026-11-18: npm has `@ui-sdk/vue` and `@ui-sdk/core` at `0.1.0-beta.10` (Apache-2.0, peer `vue >= 3.5`;
StreamGo has 3.5.45); the repository was created on 2026-10-26. The version to pin is the newest at the time of
implementation, verified the same way, with the docs it ships.

### U1a. Installed the way UI SDK says, for Vue

- Packages: `npm install @ui-sdk/vue @ui-sdk/core` in `frontend/` (the Vue adapter and the core; not the React
  one), then both versions made exact in `package.json`.
- Imports come from a component's own entry (`@ui-sdk/vue/menu`, `@ui-sdk/vue/tooltip`, `@ui-sdk/vue/meter`),
  never from the package root and never from `@ui-sdk/core/internal/*`.
- `style.css` declares `@layer ui-sdk;` above `@import "tailwindcss"`, as the library asks. Its optional default
  look (`@ui-sdk/core/web.css`) is not imported: the wrappers' recipes are StreamGo's own (U2), styled from the
  `data-slot` and state attributes the components set.
- The agent skill is installed with `npx skills@latest add vercel-labs/ui --skill ui-sdk` (telemetry off). It is
  guidance for agents, read before a wrapper is written or changed; the docs of the installed version ship in the
  package (`frontend/node_modules/@ui-sdk/vue/docs/llms.txt`) and win over the website. `skills-lock.json` is
  committed; the skill's files stay out of the repository, as the project's other skill does.
- Two things the skill asks for are not followed in this change, on purpose: it says not to hand-write a
  `role="toolbar"`, `"radiogroup"` or `"dialog"` once the library is installed (StreamGo's existing ones stay
  until the library is stable, U1b), and it ends every task with making the interface usable by agents
  (`createAgent`, WebMCP), which nobody asked for here.

### U1b. Upgrading and growing

The owner's direction is to follow UI SDK as it improves (https://github.com/vercel-labs/ui; its changelog is at
https://ui-sdk.dev/docs/core/changelog) and to use more of its components over time.

- **An upgrade** is one commit: the two packages at the new exact version (always the same version for both),
  the wrappers adjusted to what changed, `make check`, the unit tests and the browser suites, the size numbers
  updated. `docs/README.md` says so, so the next person does not bump the version alone.
- **A new component** enters the same way the first three do: a `Ui*.vue` wrapper under
  `frontend/src/components/ui/`, a recipe on role tokens, a gallery entry, its contrast recipes, and an OpenSpec
  change when it changes what a user can do.
- StreamGo's own select stays while the library is a beta. Dialogs and toasts move now (U10).

### U2. Styled by role tokens only

UI SDK components are unstyled. Each wrapper has a recipe in `style.css` built on existing role tokens (the menu on
the popover and selection roles, the tooltip on the tooltip roles, the meter on the state roles). No theme file
changes unless a theme needs a parameter, and no component declares a colour or a radius.

The contrast matrix gets a recipe for each new text-on-background pair, and the studio's "every theme" check
covers the open menu and a shown tooltip.

### U3. Device menus

The Mic and Camera controls become two buttons each inside one group: the existing toggle, and an arrow that
opens a named menu of the devices of that kind as `menuitemradio` items, the current one checked, with the
existing **Default** choice first (the store's device id starts empty, so Default is what is checked at first).

- The list is read when the menu opens, on `devicechange`, and after a toggle that made the browser grant
  permission; listeners are removed on teardown. Three cases are told apart: no device of that kind, devices
  without labels (permission not given yet: the items say "Microphone 1", "Microphone 2"), and permission denied.
- When there is nothing to choose, the arrow stays focusable and `aria-disabled`, so its tooltip can say why.
- Choosing an item closes the menu and returns focus to the arrow (UI SDK keeps a radio menu open by default: the
  wrapper controls the open state). Choosing while the microphone or the camera is off selects the device for
  the next time it is turned on, and does not turn it on.
- The menu opens in the browser's top layer with collision handling and a bounded height that scrolls inside
  itself: the page never scrolls and the dock does not grow.

**Switching, and failing.** `studio.chooseDevice()` is the one operation both the menus and the selects of
`DevicePickers.vue` call. Today it ignores the boolean LiveKit's `switchActiveDevice()` returns, and LiveKit stops
the old capture before it has the new one. The operation is corrected for both callers: a rejection and a `false`
are both failures; the chosen id is committed only after success; on failure it tries the previous device again;
when that fails too, the control shows off and a toast says the device is unavailable. The menu's checked item
and the selects always show what is really in use.

### U4. The microphone meter

The store already measures a level for the microphone of the preview before joining (`micLevel`), with an
analyser that stops at the join. That analyser is generalised to follow the current local microphone track, in
the preview and in the room: same sampling (10 times a second, one reused buffer), bound again after a device
switch or a reconnection, stopped when the microphone is off, the tab is hidden, or the person leaves. Stopping
the meter never stops the capture and never touches the publishing mix; the analyser is connected to no
destination.

The meter is `role="meter"` with a label and a text value ("silent" below a stated threshold, "heard" above).
It tells a person their microphone picks up sound. It does not prove viewers receive it; the label does not say
so.

### U5. Tooltips

The name of an icon-only control moves from the browser's `title` to a `UiTooltip`; the control keeps its element,
its `aria-label`, its role and state attributes and its `data-*` hooks. The controls: Device settings and the two
device arrows in the dock, the icon buttons of the studio header, the layout thumbnails, and the icon marks of a
backstage card. Shown 500 ms after hover and at once on keyboard focus; hidden on blur, on pointer leave and on
Escape (and it stays hidden until the control is entered again); it never takes focus.

A `title` that carries a reason or an error rather than a name (why Add to stage is disabled, a destination's
error) is not a name tooltip and stays as it is in this change.

### U6. Motion is CSS, and optional

Three new decorative effects, in StreamGo's own CSS on the existing icons (`@lucide/vue`): a transform of at most
300 ms when a dock control's pressed state changes, bars that move while someone speaks, a pulse on the live
mark. Only `transform` and `opacity` are animated.

Under `prefers-reduced-motion: reduce`, and in flat mode, these three are off (also when the setting changes while
one runs) and the static marks stay. The meter keeps showing real readings without easing. The app's existing
motion (button presses, the sheet's entry, toasts) is not in this change's scope.

Why not UI SDK's transitions: these are properties of an icon, not entering and leaving elements; CSS does it
with no code. No animated-icon library is added.

### U7. Sound cues

A cue player in `frontend/src/core/cues.ts` (no Vue import) synthesises four tones with the Web Audio API (an
oscillator and a gain envelope; no audio file): joined, left, live, ended. Each lasts at most 250 ms at a peak
gain of at most 0.15.

- **Not wired to the program.** The player has one `AudioContext` of its own, connected to the device's output
  only; it shares no node with `AudioMix`. That keeps a cue out of the mix by wiring. It does not keep it out by
  air: any participant's speakers can carry a cue into their microphone, and a shared screen with system audio
  can capture it. So the promise is "never connected to the program mix", not "never heard in the program".
- **Silent during a broadcast, for everyone, by default.** While the broadcast is starting, live or interrupted,
  no participant hears cues, unless they turn on "Sounds during a broadcast", whose hint warns that a cue may be
  heard in the program and recommends headphones. A cue suppressed is never played later.
- **Starting the audio.** The context is created and resumed from the same user gestures that already resume
  `AudioMix` (joining, the dock's buttons). A context that is not running drops the cue: nothing is queued,
  nothing throws. The context is closed and its listeners removed when the studio is left.
- **Events.** Joined and left come from LiveKit's `ParticipantConnected` and `ParticipantDisconnected` for remote
  participants, after the room's first population is done; not from the length of the people list, a track, a
  mute, a screen share, a reconnection or one's own leaving. One cue of either kind per second: the first event
  plays, the others in that second do not. Live and ended come from an accepted change of the broadcast's state
  (the owner's output state, a guest's `stage.on_air`); the state found on arrival plays nothing, and
  `interrupted` plays nothing.
- **Preferences where the studio can reach them.** Owners and guests both reach the dock's Device settings
  dialog, so "Sounds" (on by default) and "Sounds during a broadcast" (off by default) are a section there,
  labelled "On this browser". They are stored in `localStorage`: they belong to the device, not to the account,
  which is why they are not account preferences like the theme. Storage that is blocked or holds something else
  gives the defaults.

### U10. Dialogs and toasts on UI SDK, behind the components the app already uses

The owner decided that dialogs and toasts use UI SDK. The app reaches them through three components only, and
that does not change:

| Component | Today | On UI SDK |
|---|---|---|
| `AppModal.vue` (every modal, by rule of `AGENTS.md`) | a native `<dialog>` opened with `showModal()` | Dialog |
| `ui/DialogHost.vue` (`askConfirm`, `showAlert`, the typed confirmation) | a native `<dialog>` | Alert Dialog |
| `ui/ToastStack.vue` (`toast.success`, `.info`, `.warning`, `.error`) | a list with `aria-live` regions | Toast |

What must stay exactly as it is, and is what the existing tests check:

- **The API**: the props, events and slots of `AppModal`, and `toast.*`, `askConfirm`, `showAlert` in
  `frontend/src/feedback/`. No view or modal file changes.
- **Behaviour**: a modal traps focus, closes on Escape and on its close control, returns focus to what opened it,
  and makes the rest of the page inert; a confirmation puts focus on the safe button; an error toast is announced
  assertively and the others politely; at most `MAX_TOASTS` toasts show; a toast closes by its button and by its
  time; the native `alert`, `confirm` and `prompt` stay replaced.
- **The look in every theme**: the title bar recipes (the XP and 98 captions, the Mac window lights at the left
  with the centred title, the Platinum stripes, the Ubuntu close button), the place 5 % below the top
  (`--dialog-top`), the sheet's entry motion, the backdrop, the toast's head and bar, flat mode. The recipes are
  written today against `dialog.pnl`, `::backdrop` and `.toast`; they are moved to the elements and state
  attributes UI SDK renders (`data-slot`, `data-open`), with the role tokens and the recipe parameters untouched,
  so no theme file changes.
- **No page scrollbar** while a dialog is open.

If UI SDK's Dialog can render inside a native `<dialog>` (its `render` part), that is preferred, because the
recipes then barely move. If one of the three cannot meet a point above with the beta as it is, that component
keeps its current implementation and the report says which point failed; the other two still move.

The dialog and toast suites (`03-feedback`, `10-app-pages`, `20-studio`, the contrast matrix in every theme) are
the acceptance test: they pass with no change to what they assert.

### U8. Size

Measured on a production build (`vp build`, the one `make check` and `make build` run), from Vite's build
manifest: the entry, the studio and guest route chunks with the shared chunks they import, and their CSS, in
bytes after gzip. The proposal records the numbers before and after. A check that runs right after the build in
`make check` fails when the entry, or a route's total, passes a fixed ceiling set 10 % above the measured size.
Views are loaded lazily, so the library will land in route and shared chunks: an entry-only budget would miss it.

### U9. The owner validates before it merges

The slice is built on a branch and run on the owner's local stack, with a short walkthrough: switch the camera
and see it change for another participant; speak and mute and watch the meter; a guest joins and one cue plays;
the sound settings from inside the studio; reduced motion, a narrow guest window, both languages. It merges after
the owner has tried it; what the owner changes or drops is applied first.

## Risks

- **A beta dependency**: contained by U1; if it proves unstable the three wrappers are rewritten on StreamGo's own
  popover recipe.
- **Accessibility regressions**: the wrappers are covered by the existing axe checks and by keyboard scenarios.
- **Twenty-eight themes**: every new recipe is one more thing each theme must carry; U2 keeps them on existing
  roles.
