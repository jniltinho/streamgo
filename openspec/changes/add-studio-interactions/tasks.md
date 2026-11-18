# Tasks: livelier studio controls

## 1. The library, contained

- [x] 1.1 `npm install @ui-sdk/vue @ui-sdk/core` in `frontend/`, both made exact; `@layer ui-sdk;` in `style.css`;
  the lint rule that allows the library only under `frontend/src/components/ui/` and in `AppModal.vue`, and only
  by component entry;
  `THIRD_PARTY`; `skills-lock.json` and the line in `AGENTS.md` (design U1a)
- [x] 1.2 `UiMenu.vue`, `UiTooltip.vue`, `UiMeter.vue` with their recipes on role tokens in `style.css`; the matrix
  recipes for the new pairs; gallery entries for the three
- [x] 1.3 The size check after `vp build` in `make check` (design U8): entry and route totals after gzip, before
  and after, the ceilings, the numbers in the proposal

## 2. The studio

- [x] 2.1 `studio.chooseDevice()` handles refusal and failure, restores the previous device, reports unavailable
  (design U3); unit tests for "A device that cannot be used" and "Neither device can be used", for the menu and the
  selects
- [x] 2.2 Device menus on Mic and Camera in `ControlsDock.vue` (design U3); tests for every other scenario of
  "Device menus", with a two-device fixture, for the owner and for a guest
- [x] 2.3 The microphone meter on the generalised analyser (design U4); tests for every scenario of "Microphone meter"
- [x] 2.4 Tooltips on the controls design U5 lists; tests for "Tooltips"
- [x] 2.5 State motion in `style.css` (design U6); tests for "State motion"
- [x] 2.6 `frontend/src/core/cues.ts`, its wiring to the room's events and the broadcast's state, the Sounds
  section of `DeviceSettingsModal.vue`, both catalogs (design U7); unit tests for every scenario of "Sound cues",
  a real-studio check that a guest's join is heard once and that the program's output is unchanged by a cue

## 3. Dialogs and toasts on UI SDK

- [x] 3.1 `ToastStack.vue` on UI SDK's Toast, driven by the list of `feedback/toasts.ts` (design U10: one timer
  authority, one announcement path); tests for "Toasts behave as before", "Announced once" and "A toast over a
  modal"
- [x] 3.2 `DialogHost.vue`: confirmations on Alert Dialog (focus on Cancel, the typed guard), alerts on Dialog, the
  queue kept; tests for the four scenarios of confirmations and alerts
- [x] 3.3 `AppModal.vue` on Dialog behind its props, events and slots; the shared dialog recipes moved from
  `dialog.pnl` to the element the library renders; tests for "A modal behaves as before", "A modal over a modal"
  and "The look in every theme" (the close control and the title's centre in every theme); a screenshot of a
  modal, a confirmation and the toasts in each theme
- [x] 3.4 A component that cannot meet a scenario keeps its implementation; the report says which and why, and
  lists every existing assertion whose structure changed
- [x] 3.5 The toast stack and the dialog host loaded after the entry, the preferences modal when first opened
  (`components/lazy.ts`): a toast waits for the stack before its time runs, a request made before the host keeps
  its opener, a chunk that does not load is retried and then settles every confirmation as cancelled; the size
  check fails when UI SDK is in the static closure of the entry or of the sign-in route; unit tests
  (`lazyFeedback.test.ts`) and a browser suite (`09-lazy-feedback`) with the chunk held back and refused

## 4. Validation and checks

- [ ] 4.1 The real studio suite in every theme with a menu open, a tooltip shown and the meter half full; both
  languages; a guest on the backstage and on stage, and one who joins while the program is on air; the menus
  after the browser's permission is given; the open menu at 1280x720, on an ultrawide window and in a narrow
  touch window; a guest in Firefox (run by hand with the session's driver, not in the CI)
- [ ] 4.2 The owner tries the slice on the local stack with the walkthrough of design U9; what the owner changes
  is applied
- [ ] 4.3 `CHANGELOG.md`; `docs/README.md` (UI SDK: what it is used for, https://github.com/vercel-labs/ui, the
  upgrade steps of design U1b); `make check && make test`, the browser suites,
  `openspec validate add-studio-interactions --strict`
