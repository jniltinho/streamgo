# Tasks: livelier studio controls

## 1. The library, contained

- [ ] 1.1 `npm install @ui-sdk/vue @ui-sdk/core` in `frontend/`, both made exact; `@layer ui-sdk;` in `style.css`;
  the lint rule that allows the library only under `frontend/src/components/ui/` and only by component entry;
  `THIRD_PARTY`; `skills-lock.json` and the line in `AGENTS.md` (design U1a)
- [ ] 1.2 `UiMenu.vue`, `UiTooltip.vue`, `UiMeter.vue` with their recipes on role tokens in `style.css`; the matrix
  recipes for the new pairs; gallery entries for the three
- [ ] 1.3 The size test: the entry chunk before and after, the budget, the numbers in the proposal

## 2. The studio

- [ ] 2.1 Device menus on Mic and Camera in `ControlsDock.vue` (design U3); tests for every scenario of "Device
  menus"
- [ ] 2.2 The microphone meter (design U4); tests for every scenario of "Microphone meter"
- [ ] 2.3 Tooltips on the icon-only controls of the studio (design U5); tests for "Tooltips"
- [ ] 2.4 State motion in `style.css` (design U6); tests for "State motion"
- [ ] 2.5 `frontend/src/core/cues.ts`, the two preferences in `PreferencesModal.vue`, both catalogs (design U7);
  unit tests for every scenario of "Sound cues", a real-studio check that a guest's join is heard once

## 3. Validation and checks

- [ ] 3.1 The real studio suite in every theme with a menu open and a tooltip shown ("In every theme")
- [ ] 3.2 The owner tries the slice on the local stack; what the owner changes is applied
- [ ] 3.3 `CHANGELOG.md`; `docs/README.md` (UI SDK: what it is used for, https://github.com/vercel-labs/ui, the
  upgrade steps of design U1b); `make check && make test`, the browser suites,
  `openspec validate add-studio-interactions --strict`
