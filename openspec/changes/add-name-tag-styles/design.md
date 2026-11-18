# Design: lower-third styles

## Context

The owner's browser draws the program on a canvas (`frontend/src/core/compositor.ts`) from the stage state, which
the server stores as JSON text and sends to every participant. `hide_names` (archived change
`add-name-tag-switch`) is the pattern this change follows. UI SDK is in the project through wrappers
(`add-studio-interactions`).

## Decisions

### N1. Three fields in the stage state

| Field | Values | A state stored before this change |
|---|---|---|
| `name_style` | `bar`, `solid`, `strip`, `text`, `two-line` | `bar` |
| `hide_screen_names` | boolean | `false` |
| `taglines` | an object from a participant's identity to a text | none |

They are patched like `hide_names`, by the owner only, under the broadcast's lock. `hide_names` wins: with it on,
no tile has a name, whatever the other two say.

### N2. The second line

A tagline is the owner's text for one person: at most 60 characters after trimming, no control characters (the
rule of display names). An empty text removes the entry. The patch sets one entry at a time
(`tagline: {identity, text}`), so two edits do not overwrite each other.

The stage holds at most 50 taglines. Guests who leave would otherwise fill it over a long broadcast: when a new
entry would pass the limit, entries of identities that have no source on the stage are dropped first, and only if
it is still full is the patch refused.

Only the Two lines style draws it; the other styles ignore it, and Two lines with no tagline draws the name alone
on one line.

### N3. One function draws a name tag

`compositor.ts` gets `drawNameTag(g, rect, name, tagline, accent, style)`: a pure drawing function with no state.
The program calls it for each tile; the Brand tab's previews call it on small canvases, so a preview is what the
program draws. Sizes derive from the tile's height as today, the font is the bundled Inter, and the text is cut
with an ellipsis when it does not fit the tile.

Readability does not depend on the picture behind: Bar, Solid and Two lines have their own plate; Strip has a
translucent black strip at 60 %; Text has a dark shadow around white letters. A Solid plate takes the brand
colour, which the owner picks freely: the text on it is white or near black, whichever contrasts more with that
colour.

### N4. Screens

`hide_screen_names` takes the tag off tiles whose source is a shared screen. The placeholder of a screen that is
not being shared (the initials) is not a name tag and is not affected.

### N5. The controls, on UI SDK

- The styles: a radio group of five previews (`UiRadioGroup`, UI SDK's Radio), arrow keys move and choose, each
  preview named for screen readers.
- "Show each person's name on the program" and "Show names on shared screens": `UiSwitch` (UI SDK's Switch). The
  second is disabled, with its reason, while the first is off. The existing checkbox becomes the first switch and
  keeps its `data-name-tags` hook.
- The second line: a small button on a backstage card, for the owner, opens `UiPopover` (UI SDK's Popover) with one
  field, Save and Remove. It is shown for every person; a hint says it appears with the Two lines style.

The three wrappers follow the rules of `add-studio-interactions` (design U1, U1a, U2): the library imported only
there, by component entry, recipes on role tokens, app-owned props and events.

## Not in this change

A person writing their own second line, a tagline for a shared screen, styles for the banner, moving a name tag
on the tile.
