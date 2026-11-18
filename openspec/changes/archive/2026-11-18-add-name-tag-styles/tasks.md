# Tasks: lower-third styles

## 1. Server

- [x] 1.1 `internal/core/stage`: `name_style`, `hide_screen_names`, `taglines` with their rules and defaults for a
  stored state that lacks them (design N1, N2); `broadcast.DefaultStage`; table-driven tests for "Bar by
  default", "Only a known style", "Only a boolean", "Rules", "One at a time", "The limit"
- [x] 1.2 `internal/core/studio` patch and the stage handler's decoding (`name_style`, `hide_screen_names`,
  `tagline`); handler tests, among them "Only the owner"; `make test-db` (the three databases)

## 2. Program

- [x] 2.1 `drawNameTag` in `compositor.ts` (design N3, N4) and its use by the program; pixel tests for "Each
  style draws what it says", "Readable on any brand colour", "A long name", "Off the screens", "The placeholder
  stays", "No second line", "Another style ignores it", "Hidden names win"

## 3. Controls

- [x] 3.1 `UiSwitch.vue`, `UiRadioGroup.vue`, `UiPopover.vue` with recipes on role tokens, gallery entries, matrix
  recipes
- [x] 3.2 The Brand tab: the style group with previews and the two switches (design N5); the store; both catalogs;
  tests for "Choose a style", "The preview is the program", "Keyboard", "Depends on the first switch"
- [x] 3.3 The second line on a backstage card (owner only); tests for "Write one" and "Remove it"

## 4. Checks

- [x] 4.1 The real studio suite: a style chosen by the owner reaches a guest's view and survives a reload; names
  off a shared screen; a second line; every theme with the Brand tab and the popover open; both languages
- [x] 4.2 `CHANGELOG.md`; `make check && make test && make test-db`, the browser suites,
  `openspec validate add-name-tag-styles --strict`
