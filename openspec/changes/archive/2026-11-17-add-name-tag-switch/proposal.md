# Name tags on or off

## Why

Every tile of the program carries its person's name. The owner asked to take the name tags off, and put them back,
from the studio: some broadcasts show names in their own banner, or want a clean picture.

## What Changes

- The stage state gets `hide_names` (false by default, so a broadcast made before this change keeps its tags).
- `PATCH /broadcasts/:id/stage` accepts `hide_names`.
- The Brand tab of the studio has a "Name tags" switch; the program and every guest's view follow it.

## Capabilities

### New Capabilities

- `name-tags`: the switch, the stage field and what the program draws.

### Modified Capabilities

None formally (`add-live-studio` is not archived).

## Impact

`internal/core/stage`, `internal/core/studio`, `internal/httpserver/handlers/studio.go`, `internal/core/broadcast`
(the stage document of a new broadcast); `frontend/src/core/stageState.ts`, `compositor.ts`, the studio store,
`BrandTab.vue`, both catalogs.
