# Design: livelier studio controls

## Context

The studio is Vue 3.5 with Tailwind 4 and StreamGo's own themed recipes (`style.css`, 28 themes over role tokens).
Icons are `@lucide/vue`. Dialogs, toasts and selects are StreamGo's own components with their own tests. The
program's audio is mixed in the owner's browser by `frontend/src/core/stageAudio.ts`.

## Decisions

### U1. UI SDK is contained

`@ui-sdk/vue` and `@ui-sdk/core` are pinned to one exact version (no range). They are imported only by wrapper
components under `frontend/src/components/ui/`: `UiMenu`, `UiTooltip`, `UiMeter`. The studio imports the wrappers,
never the library; a lint rule in `frontend/vite.config.ts` (beside the one that keeps Vue out of
`frontend/src/core`) fails `make check` otherwise.

Why: the library is three weeks old and says its APIs may change. With wrappers, an upgrade that breaks is a fix
in three files, and removing the library is replacing three files.

A wrapper exposes what the studio needs and nothing else (no pass-through of the library's whole API).

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
- While the library is a beta, StreamGo's own dialogs, toasts and selects stay. When UI SDK reaches a stable
  release, replacing them with wrappers over it becomes worth a proposal of its own.

### U2. Styled by role tokens only

UI SDK components are unstyled. Each wrapper has a recipe in `style.css` built on existing role tokens (the menu on
the popover and selection roles, the tooltip on the tooltip roles, the meter on the state roles). No theme file
changes unless a theme needs a parameter, and no component declares a colour or a radius.

The contrast matrix gets a recipe for each new text-on-background pair, and the studio's "every theme" check
covers the open menu and a shown tooltip.

### U3. Device menus

The Mic and Camera controls become two buttons each inside one group: the existing toggle, and an arrow that
opens a menu (`role="menu"` with `menuitemradio` items) of the devices of that kind, the current one checked.
Choosing calls the store's existing device switch; a failure shows the existing toast and leaves the previous
device.

Before the browser has given permission it gives no device labels: the arrow is then disabled with a tooltip that
says so. The list is read again on `devicechange`. The settings dialog keeps its selects.

### U4. The microphone meter

A meter beside the Mic button shows the level of one's own microphone while it is on, read from an analyser on
the local track (the level the speaking mark already uses, if one exists; else a small analyser in
`frontend/src/core/`). It is `role="meter"` with a label and a text value for screen readers ("silent", "heard").
It is redrawn at most 15 times a second and stops while the microphone is off or the tab is hidden.

It reads the local track only and is connected to no destination: it cannot change the program.

### U5. Tooltips

Icon-only controls get a `UiTooltip` with the control's name; the `title` attribute goes, the `aria-label` stays.
Shown on hover after a short delay and on keyboard focus at once; Escape hides it; it never takes focus. Buttons
that already show their name as text get none.

### U6. Motion is CSS, and optional

State motion is StreamGo's own CSS on the existing icons (`@lucide/vue`): a short transform when a control's
pressed state changes, bars that move while someone speaks, a pulse on the live mark. It uses only `transform` and
`opacity`, lasts at most 300 ms for a state change, and is absent under `prefers-reduced-motion: reduce` and in
flat mode.

Why not UI SDK's transitions: these are properties of an icon, not entering and leaving elements; CSS does it
with no code. No animated-icon library is added.

### U7. Sound cues

A cue player in `frontend/src/core/cues.ts` (no Vue import) synthesises four short tones with the Web Audio API
(an oscillator and a gain envelope; no audio file, so nothing to license or load): joined, left, live, ended.

- It has its own `AudioContext`, connected to the device's output only. It shares no node with the stage mix, so a
  cue cannot enter the program by construction.
- A cue can still reach the program through the air (speakers into the host's microphone). So for the owner cues
  are silent while the broadcast is live unless the owner turns on "Sounds while live"; the guide recommends
  headphones.
- A preference "Sounds" (on by default) and, for owners, "Sounds while live" (off by default) live in
  `PreferencesModal.vue` and are stored in the browser (`localStorage`), like the theme of a signed-out visitor.
  They are per device, not per account: a server setting is not needed for this.
- A suspended `AudioContext` (no user gesture yet) drops the cue silently; nothing is queued and nothing throws.
- Joined and left are for people entering or leaving the backstage, not for one's own join, and are limited to
  one cue per second.

### U8. Size

The built app's JavaScript and CSS are measured before and after. The change states the growth in the proposal
and adds a test that fails when the app's entry chunk passes a budget set 10 % above the measured size, so a
later upgrade of the library that bloats the app is noticed.

### U9. The owner validates before it merges

The slice is built on a branch and run on the owner's local stack. It merges after the owner has tried it; what
the owner changes or drops is applied first.

## Risks

- **A beta dependency**: contained by U1; if it proves unstable the three wrappers are rewritten on StreamGo's own
  popover recipe.
- **Accessibility regressions**: the wrappers are covered by the existing axe checks and by keyboard scenarios.
- **Twenty-eight themes**: every new recipe is one more thing each theme must carry; U2 keeps them on existing
  roles.
