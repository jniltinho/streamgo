# Tasks: livelier studio controls

## 1. The library, contained

- [ ] 1.1 `npm install @ui-sdk/vue @ui-sdk/core` in `frontend/`, both made exact; `@layer ui-sdk;` in `style.css`;
  the lint rule that allows the library only under `frontend/src/components/ui/` and only by component entry;
  `THIRD_PARTY`; `skills-lock.json` and the line in `AGENTS.md` (design U1a)
- [ ] 1.2 `UiMenu.vue`, `UiTooltip.vue`, `UiMeter.vue` with their recipes on role tokens in `style.css`; the matrix
  recipes for the new pairs; gallery entries for the three
- [ ] 1.3 The size check after `vp build` in `make check` (design U8): entry and route totals after gzip, before
  and after, the ceilings, the numbers in the proposal

## 2. The studio

- [ ] 2.1 `studio.chooseDevice()` handles refusal and failure, restores the previous device, reports unavailable
  (design U3); unit tests for "A device that cannot be used" and "Neither device can be used", for the menu and the
  selects
- [ ] 2.2 Device menus on Mic and Camera in `ControlsDock.vue` (design U3); tests for every other scenario of
  "Device menus", with a two-device fixture, for the owner and for a guest
- [ ] 2.3 The microphone meter on the generalised analyser (design U4); tests for every scenario of "Microphone meter"
- [ ] 2.4 Tooltips on the controls design U5 lists; tests for "Tooltips"
- [ ] 2.5 State motion in `style.css` (design U6); tests for "State motion"
- [ ] 2.6 `frontend/src/core/cues.ts`, its wiring to the room's events and the broadcast's state, the Sounds
  section of `DeviceSettingsModal.vue`, both catalogs (design U7); unit tests for every scenario of "Sound cues",
  a real-studio check that a guest's join is heard once and that the program's output is unchanged by a cue

## 3. Dialogs and toasts on UI SDK

- [ ] 3.1 `ToastStack.vue` on UI SDK's Toast behind `toast.*` (design U10); the toast recipes moved; the feedback
  unit tests and `03-feedback` pass with no change to what they assert
- [ ] 3.2 `DialogHost.vue` on Alert Dialog behind `askConfirm`, `showAlert` and the typed confirmation; the native
  replacements still installed
- [ ] 3.3 `AppModal.vue` on Dialog behind its props, events and slots; the dialog recipes moved for every theme
  (title bars, close controls, `--dialog-top`, backdrop, motion, flat mode); `10-app-pages`, the close-control
  test in every theme and `20-studio` pass
- [ ] 3.4 A component that cannot meet a scenario keeps its implementation; the report says which and why

## 4. Validation and checks

- [ ] 4.1 The real studio suite in every theme with a menu open, a tooltip shown and the meter half full; both
  languages; a guest in Firefox (run by hand with the session's driver, not in the CI)
- [ ] 4.2 The owner tries the slice on the local stack with the walkthrough of design U9; what the owner changes
  is applied
- [ ] 4.3 `CHANGELOG.md`; `docs/README.md` (UI SDK: what it is used for, https://github.com/vercel-labs/ui, the
  upgrade steps of design U1b); `make check && make test`, the browser suites,
  `openspec validate add-studio-interactions --strict`
