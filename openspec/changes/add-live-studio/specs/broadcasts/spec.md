# Spec Delta

## ADDED Requirements

### Requirement: Broadcast management
A signed-in user SHALL create a broadcast with a title of 1 to 120 characters, list their own broadcasts newest
first, rename one and delete one. A new broadcast SHALL start `offline` with the layout `grid`, an empty stage, no
banner, no logo and the default accent colour.

#### Scenario: Create and list
- **WHEN** Ana creates "Weekly Product Live"
- **THEN** it appears first in her list with the state `offline`, and not in Bruno's list

#### Scenario: Empty title
- **WHEN** a broadcast is created or renamed with a blank title or one longer than 120 characters
- **THEN** the response is 422 naming the field

### Requirement: Deleting a broadcast
Deleting a broadcast SHALL remove its banners, guests, destination selections and logo file, and SHALL be refused
with 409 while its output state is not `offline`.

#### Scenario: Delete while live
- **GIVEN** a broadcast is live
- **WHEN** its owner deletes it
- **THEN** the response is 409 and the broadcast still exists

#### Scenario: Delete removes the logo file
- **GIVEN** an offline broadcast with an uploaded logo
- **WHEN** its owner deletes it
- **THEN** the logo file no longer exists in the data directory

### Requirement: Invite link
Every broadcast SHALL have an invite link `<public_url>/join/<token>` whose token is 32 random bytes, stored
encrypted so the owner can read the link again. The owner SHALL be able to rotate it, after which the previous
link stops working at once. The link SHALL be visible only to the owner.

#### Scenario: Owner reads the link again
- **WHEN** the owner requests the broadcast twice
- **THEN** both answers carry the same invite link

#### Scenario: Rotation
- **GIVEN** a guest holds the invite link
- **WHEN** the owner rotates it
- **THEN** opening the old link answers 404 and the new one works

#### Scenario: Token not stored in clear
- **WHEN** the database is inspected after a broadcast is created
- **THEN** the invite token does not appear in it in clear text
