# Lower-third styles for the name tags

## Why

The program has one name tag: a dark plate with a bar in the brand colour, on every tile, a shared screen
included. The owner can only take all of them off at once. The owner asked for more lower-third models and for
ways to remove them, built with UI SDK.

## What Changes

- **Five styles** for the name tags, chosen in the studio's Brand tab from a row of small previews:
  - **Bar** (today's): a dark plate with a bar in the brand colour at its left;
  - **Solid**: the name on a plate in the brand colour;
  - **Strip**: a translucent strip across the bottom of the tile, the name at its left;
  - **Text**: the name with no plate, a shadow to read over the picture and a line in the brand colour under it;
  - **Two lines**: the name and, under it, a second line the owner writes for each person (a role, a company).
- **Names off the shared screens**: a switch that takes the name tag off the tiles of shared screens and keeps it on
  the cameras. The switch that takes every name off stays.
- **A second line per person**, written by the owner from that person's backstage card, shown by the Two lines
  style.
- The controls are UI SDK components behind StreamGo's wrappers: the styles are a radio group, the two switches
  are switches, the second line is edited in a popover.

The choices are part of the stage state, as `hide_names` is: the program and every participant's view follow
them, and they survive a reload.

## Capabilities

### Modified Capabilities

- `name-tags`: the styles, the screens' names and the second line are added; hiding every name is unchanged.

## Impact

- `internal/core/stage` (three fields and their rules), `internal/core/studio` (the patch), the stage handler,
  `broadcast.DefaultStage`; no migration (the stage is stored as JSON text) and no new setting.
- `frontend/src/core/compositor.ts` (one function draws a name tag in a style, used by the program and by the
  previews), `stageState.ts`, `stores/studio.ts`, `BrandTab.vue`, `BackstageRow.vue`; new wrappers
  `UiSwitch.vue`, `UiRadioGroup.vue`, `UiPopover.vue` under `frontend/src/components/ui/` with recipes on role
  tokens; both translation catalogs.
- `CHANGELOG.md`, `docs/README.md` if the list of what UI SDK is used for changes.
