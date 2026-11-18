# name-tags Specification

## Purpose
The owner takes the name tags off the program and puts them back from the studio; the choice is part of the stage state, so the program and every participant's view follow it.

## Requirements

### Requirement: Name tags can be hidden
The owner SHALL hide and show the name tags of the program from the studio. The choice SHALL be part of the stage
state, so the program and every participant's view of the stage follow it, and it SHALL survive a reload.

#### Scenario: Shown by default
- **WHEN** a broadcast is created, or was created before this change
- **THEN** its stage state says `hide_names: false` and every tile carries its name

#### Scenario: Hide and show
- **GIVEN** the owner on stage
- **WHEN** the owner clears "Show each person's name on the program" in the Brand tab, and later sets it again
- **THEN** the name tag leaves the program and comes back, and the broadcast's stage says `hide_names: true` in
  between

#### Scenario: Only a boolean
- **WHEN** the stage is patched with `hide_names` as a string or as null
- **THEN** the answer is 422 with the field `hide_names` and the stage is unchanged

#### Scenario: Banners are not name tags
- **WHEN** the name tags are hidden and a banner is shown
- **THEN** the banner is drawn as before
