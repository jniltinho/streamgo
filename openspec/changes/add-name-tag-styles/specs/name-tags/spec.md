# Name tags

## ADDED Requirements

### Requirement: Name tag styles
The owner SHALL choose the style of the name tags among bar, solid, strip, text and two lines. The choice SHALL
be part of the stage state, so the program and every participant's view follow it, and it SHALL survive a reload.

#### Scenario: Bar by default
- **WHEN** a broadcast is created, or was created before this change
- **THEN** its stage state says `name_style: "bar"`, `hide_screen_names: false` and no taglines, and the program
  looks as it did

#### Scenario: Choose a style
- **GIVEN** the owner on stage
- **WHEN** the owner chooses Solid in the Brand tab
- **THEN** the stage says `name_style: "solid"`, the owner's tile in the program has its name on a plate in the
  brand colour, a guest's view shows the same, and a reload keeps it

#### Scenario: Each style draws what it says
- **WHEN** a name tag is drawn in each style on a tile
- **THEN** bar has a dark plate with a strip in the brand colour at its left; solid has a plate in the brand
  colour; strip has a translucent dark strip as wide as the tile at its bottom; text has no plate and a line in
  the brand colour under the name; two lines has the name above the tagline on one plate

#### Scenario: Readable on any brand colour
- **WHEN** the solid style is drawn with a very light brand colour, and with a very dark one
- **THEN** the name is black on the light one and white on the dark one, at 4.5:1 or more against the plate
- **WHEN** it is drawn with any other colour, including the greys where white and black contrast about the same
- **THEN** the name is still at 4.5:1 or more against the plate

#### Scenario: A long name
- **WHEN** a name wider than its tile is drawn in any style
- **THEN** it ends with an ellipsis and nothing is drawn outside the tile

#### Scenario: The preview is the program
- **WHEN** the Brand tab shows the five previews
- **THEN** each is drawn by the function the program uses, with the brand colour of the broadcast

#### Scenario: Keyboard
- **WHEN** the style group has focus and the owner presses the right arrow
- **THEN** the next style is chosen and applied, and each preview has an accessible name

#### Scenario: Only a known style
- **WHEN** the stage is patched with `name_style` as `"neon"`, a number or null
- **THEN** the answer is 422 with the field `name_style` and the stage is unchanged

#### Scenario: Hidden names win
- **WHEN** `hide_names` is on
- **THEN** no tile has a name in any style, and the style chosen is kept for when names are shown again

### Requirement: Names off shared screens
The owner SHALL take the name tags off the tiles of shared screens and keep them on the cameras.

#### Scenario: Off the screens
- **GIVEN** a camera and a shared screen of the same person on stage
- **WHEN** the owner turns off "Show names on shared screens"
- **THEN** the stage says `hide_screen_names: true`, the camera's tile has the name and the screen's tile does not

#### Scenario: The placeholder stays
- **WHEN** the names are off the shared screens and a screen on stage is not being shared
- **THEN** its tile still shows the initials placeholder

#### Scenario: Depends on the first switch
- **WHEN** "Show each person's name on the program" is off
- **THEN** "Show names on shared screens" is disabled and says why

#### Scenario: Only a boolean
- **WHEN** the stage is patched with `hide_screen_names` as a string or as null
- **THEN** the answer is 422 with the field `hide_screen_names` and the stage is unchanged

### Requirement: A second line per person
The owner SHALL write a second line for each person, which the two-lines style shows under the name.

#### Scenario: Write one
- **GIVEN** the two-lines style and a guest on stage
- **WHEN** the owner opens the second line of that guest's backstage card, types "Head of Product" and saves
- **THEN** the stage's taglines hold that text for the guest's identity, and the guest's tile shows the name with
  "Head of Product" under it

#### Scenario: No second line
- **WHEN** the two-lines style draws a person without a tagline
- **THEN** the tag is the name alone on one line

#### Scenario: Another style ignores it
- **WHEN** a person has a tagline and the style is bar
- **THEN** the tag shows the name only, and the tagline is kept

#### Scenario: Remove it
- **WHEN** the owner removes a person's second line, or saves it empty
- **THEN** the entry is gone from the stage

#### Scenario: Rules
- **WHEN** a tagline of 61 characters, or one with a control character, is sent
- **THEN** the answer is 422 with the field `tagline` and the stage is unchanged; a text with spaces around it is
  stored trimmed
- **WHEN** a tagline patch has a null or missing `text`, a missing `identity`, or a field other than the two
- **THEN** the answer is 422 with the field `tagline` and the stage is unchanged: only an explicit empty text
  removes a line

#### Scenario: One at a time
- **WHEN** two taglines are set one after the other for two people
- **THEN** both are in the stage

#### Scenario: The limit
- **GIVEN** 50 taglines, some of people who have no source on the stage
- **WHEN** another is added
- **THEN** it is stored and the entries of people not on the stage are dropped; with 50 taglines of people who
  are all on the stage, the answer is 422 and the stage is unchanged

#### Scenario: Only the owner
- **WHEN** a guest patches the stage, or another user patches a broadcast they do not own
- **THEN** the answer is the one every stage patch gives them, and nothing changes

### Requirement: The controls are themed
The style group, the two switches and the second-line popover SHALL be UI SDK components behind StreamGo's
wrappers, styled by role tokens, and SHALL meet the contrast rules in every theme.

#### Scenario: Contrast
- **WHEN** the Brand tab and an open second-line popover are rendered in each of the 28 themes
- **THEN** the accessibility check finds no violation and the contrast matrix holds for the new pairs

#### Scenario: Both languages
- **WHEN** the studio is used in English and in Brazilian Portuguese
- **THEN** the styles' names, the switches, the popover and its messages are in that language

#### Scenario: Contained
- **WHEN** the three new wrappers are read
- **THEN** they are the only new files that import UI SDK, by component entry, and `make check` passes its lint
  rule
