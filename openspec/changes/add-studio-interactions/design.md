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

- **Default.** LiveKit checks a switch by comparing the id asked for with the capture's own, so the app's empty
  Default never matches. Default stays `''` in the store, and the switch asks for the browser's default device by
  its id: the `default` entry Chrome lists, otherwise the first device listed (the default, by the Media Capture
  specification).
- **One at a time.** Switches of one kind run one after the other, each with its restore, so an earlier choice
  that fails late cannot undo a later one. Each is bound to the room or preview it was made in: one that finishes
  after that room or preview is gone changes nothing.

### U4. The microphone meter

The store already measures a level for the microphone of the preview before joining (`micLevel`), with an
analyser that stops at the join. That analyser is generalised to follow the current local microphone track, in
the preview and in the room: same sampling (10 times a second, one reused buffer), bound again after a device
switch or a reconnection, stopped when the microphone is off, the tab is hidden, or the person leaves. Stopping
the meter never stops the capture and never touches the publishing mix; the analyser is connected to no
destination.

The level is the store's existing one: the root mean square of the samples, from 0 to 1. The meter shows it
without easing; its text value is "silent" below 0.05 and "heard" from 0.05 up. The analyser is also stopped when
the room disconnects or another tab takes the studio over, and started again when the tab is shown again with the
microphone on.

The meter is `role="meter"` with a label and that text value.
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
oscillator and a gain envelope; no audio file): joined, left, on air, off air. Each lasts at most 250 ms at a peak
gain of at most 0.15.

- **Not wired to the program.** The player has one `AudioContext` of its own, connected to the device's output
  only; it shares no node with `AudioMix`. That keeps a cue out of the mix by wiring. It does not keep it out by
  air: any participant's speakers can carry a cue into their microphone, and a shared screen with system audio
  can capture it. So the promise is "never connected to the program mix", not "never heard in the program".
- **Silent while the program is on air, for everyone, by default.** A cue can reach the program only while the
  program flows, and `stage.on_air` says exactly that, to owners and guests alike (it turns true when the program
  starts flowing and false when it stops, also at an interruption). It is the one source for every participant;
  no server change is needed. While it is true nobody hears cues, the "on air" cue included, unless they turn on
  "Sounds while on air", whose hint warns that a cue may be heard in the program and recommends headphones. A cue
  suppressed is never played later.
- **Starting the audio.** The context is created and resumed from the same user gestures that already resume
  `AudioMix` (joining, the dock's buttons). A context that is not running drops the cue: nothing is queued,
  nothing throws. The context is closed and its listeners removed when the studio is left, also when the page
  stays (the "You left" page, a tab that lost the studio, a lost connection). A tone still sounding when cues stop
  being wanted (the program goes on air, or Sounds is turned off) is stopped at once.
- **Events.** Joined and left come from LiveKit's `ParticipantConnected` and `ParticipantDisconnected` for remote
  participants, after the room's first population is done; not from the length of the people list, a track, a
  mute, a screen share, a reconnection or one's own leaving. A join or leave cue plays only when at least one second has passed
  since the last join or leave cue played. "On air" and "off air" come from a change of `stage.on_air` after the
  first stage state was received; the state found on arrival plays nothing. A guest cannot tell an interruption
  from an end, so the cue says what is true in both: the program stopped.
- **Preferences where the studio can reach them.** Owners and guests both reach the dock's Device settings
  dialog, so "Sounds" (on by default) and "Sounds while on air" (off by default) are a section there,
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

UI SDK's Dialog and Alert Dialog put a content element (a `div`) in the browser's top layer; they do not call a
native `<dialog>`'s `showModal()`. So the dialog recipes move from the `dialog.pnl` selectors to the `.pnl` class
on that element with the library's open state, `::backdrop` stays (top-layer elements have one), and the title
bar, the title, the close control, the body and the footer keep their classes and their order, which is what the
themes' parameters act on. This is one migration of the shared recipes checked in 28 themes, not 28 rewrites.

What must stay as it is:

- **The API**: the props, events and slots of `AppModal`, and `toast.*`, `askConfirm`, `showAlert` in
  `frontend/src/feedback/`. No view or modal file changes.
- **A modal** (`AppModal`): traps focus, makes the rest of the page inert, closes on Escape and on its close
  control and not on a click outside it, puts its first focus on the first control of its body (not on the close
  control of the title bar), and returns focus to what opened it. Its title is associated through the library's
  title part, so two modals open at once no longer share one id.
- **A confirmation** (`DialogHost`, UI SDK's Alert Dialog, `role="alertdialog"`): focus starts on Cancel, although
  OK comes first in the markup; a typed confirmation starts on its input and keeps its guard (the text trimmed
  and compared without case, OK disabled until it matches). **An alert** (`showAlert`) stays `role="dialog"` and is
  built on Dialog. The queue of requests and how each promise settles are StreamGo's and do not change.
- **Toasts** (`ToastStack`): `frontend/src/feedback/toasts.ts` stays the one authority for which toasts exist: at
  most five, the oldest evicted, 4 s or 8 s by kind, the time paused while the pointer or focus is on a toast.
  UI SDK's Toast is driven by that list (its own limit, queue and timers are not used: two clocks would disagree)
  and brings the top-layer region, the close and its announcements. Announcements come from the library's live
  regions only, the error one assertive; the visible cards lose their `aria-live`, or each toast would be read
  twice.
- **Form validation** keeps StreamGo's themed messages (`feedback/native.ts` replaces the browser's validation
  bubbles); `alert`, `confirm` and `prompt` stay banned by the lint rule.
- **The look in every theme**: the title bar recipes (the XP and 98 captions, the Mac window lights at the left
  with the centred title, the Platinum stripes, the Ubuntu close button), the place 5 % below the top
  (`--dialog-top`), the sheet's entry motion, the backdrop, the toast's head and bar, flat mode; the role tokens
  and the recipe parameters are untouched, so no theme file changes. (The recipes also carry a `closing` class
  that no component sets today: there is no exit motion to keep.)
- **Layers**: a modal opened over a modal (Settings over a studio dialog, a confirmation over Invite): Escape
  closes the upper one only and focus returns inside the lower one, which is still modal. A toast shown while a
  modal is open, in either order, is above it and its close control can be clicked: the toast region stays in the
  top layer, as it is today, also when one of the three components kept its old implementation.
- **No page scrollbar** while a dialog is open.
- **Loaded after the entry** (U8): the root mounts `ToastStack` and `DialogHost` as asynchronous components, at once
  and unconditionally, so their chunk is requested as soon as the entry runs and never delays the first paint; a
  modal that a view of the entry's or the sign-in route's closure mounts with `v-if` (the preferences modal) is
  fetched when it is first opened (`components/lazy.ts`). Three rules make this safe. A toast pushed before the
  stack is mounted waits: `toasts.ts` starts its time only when the stack calls `toastsReady()`, so it is shown
  for its full time. A confirmation or an alert asked before the host is mounted is shown when it mounts, with
  the same first focus, and focus returns to what had it when it was asked (each request records it). A chunk
  that does not load is tried again three times, 0.5, 1 and 1.5 s apart; after that every pending and later
  confirmation settles as cancelled and an alert as seen (`dialogsUnavailable()`), the failure is logged once,
  and a modal that does not load shows an error toast and closes, so the control that opened it works again.

If one of the three cannot meet a point above with the beta as it is, that component keeps its current
implementation and the report says which point failed; the other two still move.

The dialog and toast suites (`03-feedback`, `10-app-pages`, `20-studio`, `02-themes`, the feedback unit tests) are
the acceptance test. What they check about behaviour does not change. What they pin about structure may: a
selector on the native element (`dialog[open]`) becomes a role, and the check of `aria-live` on a toast card
becomes a check of the live region. Each such change is listed in the report. New checks: the two layered cases
above, a toast over a modal in both orders with a real click on its close control, the title's centre within
2 px of the bar's centre in the themes that centre it, and a screenshot of a modal, a confirmation and the toasts
in each theme compared by eye before the merge.

### U8. Size

Measured on a production build (`vp build`, the one `make check` and `make build` run), from Vite's build
manifest: the entry, the studio and guest route chunks with the shared chunks they import, and their CSS, in
bytes after gzip. The proposal records the numbers before and after. A check that runs right after the build in
`make check` fails when the entry, or a route's total, passes a fixed ceiling set 10 % above the measured size.
Views are loaded lazily, so the library will land in route and shared chunks: an entry-only budget would miss it.
The sign-in route is measured the same way. The library is kept out of the static closure of the entry and of the
sign-in route (the dialog and toast hosts and the preferences modal are loaded asynchronously, U10): the build
lists the chunks that hold UI SDK's code beside the manifest, and the check fails when one of them is in either
closure. The library is still downloaded on every page, right after the entry; what changes is that the first
paint does not wait for it.

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
