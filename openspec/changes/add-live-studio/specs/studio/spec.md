# Spec Delta

## ADDED Requirements

### Requirement: Owner joins the studio
`POST /api/v1/broadcasts/:id/studio-token` SHALL return the LiveKit URL and a token for the room of that broadcast
with the identity `host-<user id>` and the owner's name, valid for 10 minutes, allowing the owner to publish and
subscribe and not to administer the room or change its metadata. The room SHALL be created first when it does
not exist, carrying the stored stage state as its metadata.

#### Scenario: Token grants
- **WHEN** the owner requests a studio token
- **THEN** the decoded token names the broadcast's room, allows publishing and subscribing, and grants neither
  room administration nor metadata updates

#### Scenario: Room recreated with its state
- **GIVEN** a broadcast whose stored stage has the layout `spotlight` and whose room no longer exists
- **WHEN** the owner requests a studio token
- **THEN** the room exists and its metadata has the layout `spotlight`

### Requirement: Rooms belong to the server
Only the server SHALL create studio rooms: a LiveKit token SHALL not be able to create one. Deleting a broadcast
SHALL delete its room.

#### Scenario: Reconnection after the room closed
- **GIVEN** a room that closed after staying empty
- **WHEN** a participant connects with a token issued before it closed
- **THEN** either the connection fails and the studio obtains a new token from the server, or a room appears, and
  in both cases the room carries the stored stage state within 15 seconds

#### Scenario: Broadcast deleted with people in the room
- **WHEN** the owner deletes an offline broadcast whose room still has participants
- **THEN** the room is closed and they are disconnected

### Requirement: One hosting tab
A broadcast SHALL have one active owner connection: opening the studio in another tab or browser takes over, and
the earlier one SHALL stop, say the studio was opened elsewhere, and neither reconnect nor publish the program.

#### Scenario: Second tab
- **GIVEN** the owner hosts a live broadcast in tab A
- **WHEN** they open the same studio in tab B
- **THEN** tab B is the studio, tab A shows that the studio was opened elsewhere, and only tab B publishes the
  program

### Requirement: Guests join by invite link
`GET /api/v1/join/:token` SHALL return the broadcast's title for a valid token. `POST /api/v1/join/:token` with a
name of 1 to 40 characters SHALL create a guest with the identity `guest-<id>` and return the LiveKit URL and a
token with the same grants as the owner's. No account SHALL be needed. Joining SHALL be limited to 20 requests
per minute per client address, and a broadcast SHALL hold at most 200 guests that have not expired.

#### Scenario: Join backstage
- **WHEN** Rafael opens a valid invite link and joins as "Rafael Lima"
- **THEN** he receives a token for the broadcast's room and is not on stage

#### Scenario: Invalid link
- **WHEN** a link with an unknown or rotated token is opened
- **THEN** both the `GET` and the `POST` answer 404 without saying whether the broadcast exists

#### Scenario: Too many guests
- **WHEN** a join would create the 201st unexpired guest of a broadcast
- **THEN** the response is 429 and no guest is created

### Requirement: Guest sessions
A join SHALL set a guest cookie for that broadcast that is `HttpOnly`, `SameSite=Lax`, limited to the join path,
`Secure` on HTTPS and valid for 12 hours, stored only as a hash. Joining again with it SHALL return the same
identity. Guests SHALL expire 24 hours after their last join and then be deleted.

#### Scenario: Reload keeps the identity
- **GIVEN** Rafael joined and is on stage
- **WHEN** his browser reloads and joins again with the guest cookie
- **THEN** he receives the same identity and is still on stage

#### Scenario: Two broadcasts
- **WHEN** the same browser joins two broadcasts
- **THEN** it holds one guest session for each, and joining either again returns that broadcast's identity

#### Scenario: Expired guest
- **WHEN** a guest's last join was more than 24 hours ago
- **THEN** the guest no longer exists and their cookie creates a new guest

### Requirement: Studio capacity
The room SHALL hold at most `studio.max_participants` participants, the owner included, enforced by the media
server. When the room is full without the owner, issuing the owner's token SHALL first remove the guest who
joined last, and the studio SHALL retry the owner's connection up to three times with a new token each time.

#### Scenario: Studio full
- **GIVEN** the room already holds `studio.max_participants` participants
- **WHEN** another guest connects
- **THEN** the connection is refused and the join page says the studio is full

#### Scenario: Owner arrives at a full studio
- **GIVEN** guests fill the room and the owner is not in it
- **WHEN** the owner opens the studio
- **THEN** the guest who joined last is removed with a message that the studio is full, and the owner connects

#### Scenario: Slot taken before the owner connects
- **GIVEN** the slot freed for the owner is taken by another guest before the owner connects
- **WHEN** the studio retries
- **THEN** a new token is issued, the guest who joined last is removed again, and the owner connects within three
  attempts

### Requirement: Removing a guest
The owner SHALL remove a guest with `DELETE /api/v1/broadcasts/:id/participants/:identity`: at once the guest is
disconnected, their sources leave the stage and their guest cookie stops working. A removed guest who reconnects
with a LiveKit token they still hold SHALL be removed again within 15 seconds, without ever being on stage. The
owner's own identity SHALL not be removable.

#### Scenario: Removed guest is taken off stage
- **GIVEN** Carla is on stage
- **WHEN** the owner removes her
- **THEN** the stage state no longer lists her sources and she is disconnected from the room

#### Scenario: Removal reaches the program even when the room is not updated
- **GIVEN** Carla is on stage and the push of the stage state to the room fails
- **WHEN** the owner removes her and she reconnects with an old token
- **THEN** the owner's studio already applied the state returned by the removal, so she is not drawn in the
  program or heard in it

#### Scenario: Reconnection with an old token
- **GIVEN** Carla was removed and still holds an unexpired LiveKit token
- **WHEN** she connects to the room with it
- **THEN** within 15 seconds she is disconnected again, and the stage state never lists her

#### Scenario: Old cookie after removal
- **WHEN** a removed guest posts to the join route with their old guest cookie
- **THEN** they get a new identity, backstage, only if the invite link is still valid

### Requirement: Room reconciliation
Every 15 seconds and at startup the server SHALL bring each studio room in line with the database: delete a room
whose broadcast does not exist, push the stored stage state to a room whose metadata is older, and remove
participants who are removed guests or unknown.

#### Scenario: Missed update
- **GIVEN** a stage change was stored but its push to the room failed
- **THEN** within 15 seconds the room's metadata carries the stored version

#### Scenario: Unknown participant
- **WHEN** a participant is in a studio room with an identity the database does not know
- **THEN** within 15 seconds they are removed

#### Scenario: After a server restart
- **GIVEN** a guest was removed and the server restarted before they reconnected
- **WHEN** they reconnect with an old token
- **THEN** they are removed within 15 seconds of the server being up

### Requirement: Stage state
Each broadcast SHALL have one stage state holding a version, whether it is on air, the layout (`solo`, `grid`,
`spotlight`, `sidebar`, `screen` or `pip`), an ordered list of at most `studio.max_on_stage` sources (a
participant identity with the kind `camera` or `screen`), the banner being shown or none, the logo or none, and
an accent colour. The stored state SHALL be the authority; the room's metadata is its copy.

#### Scenario: New broadcast
- **WHEN** a broadcast is created
- **THEN** its stage state has version 1, is off air, has the layout `grid`, no sources, no banner and no logo

#### Scenario: Joining reads the state
- **WHEN** a participant joins a room
- **THEN** they have the current stage state before any change event arrives

### Requirement: Changing the stage
Only the owner SHALL change the layout, sources, shown banner and accent, through
`PATCH /api/v1/broadcasts/:id/stage`. Every change to the stage state, from that route or from a banner deletion,
a logo change, a guest removal or the output going on or off air, SHALL be applied one at a time per broadcast,
increase the version by one, be stored, be returned in the route's answer, and then reach the room.

#### Scenario: Put a guest on stage
- **WHEN** the owner patches `sources` to include Rafael's camera
- **THEN** the stored state and the room metadata list it, with the version one higher

#### Scenario: Too many sources
- **WHEN** the owner patches `sources` with more entries than `studio.max_on_stage`
- **THEN** the response is 422 and the state is unchanged

#### Scenario: Invalid values
- **WHEN** a patch carries an unknown layout, a duplicate source, a source whose identity is not of this
  broadcast's room, a `banner_id` of another broadcast, an accent that is not a `#rrggbb` colour, or a `logo` field
- **THEN** the response is 422 naming the field and the state is unchanged

#### Scenario: A guest cannot change the stage
- **WHEN** a guest, or anyone without the owner's session, patches the stage or tries to update the room metadata
  with their LiveKit token
- **THEN** the request is refused and the state is unchanged

#### Scenario: Room not open
- **GIVEN** the broadcast's room does not exist
- **WHEN** the owner patches the stage
- **THEN** the change is stored and the response is successful

#### Scenario: Concurrent changes
- **WHEN** a stage patch and a guest removal for the same broadcast arrive at the same time
- **THEN** both are applied one after the other and the version increases by two

### Requirement: Banners
The owner SHALL keep up to 50 saved banner texts of 1 to 120 characters per broadcast and show at most one at a
time by setting `banner_id` in a stage patch (`null` hides it). Deleting the banner being shown SHALL hide it.

#### Scenario: Show a banner
- **WHEN** the owner sets `banner_id` to a saved banner
- **THEN** the stage state carries that banner's id and text

#### Scenario: Delete the shown banner
- **WHEN** the owner deletes the banner currently shown
- **THEN** the stage state has no banner

### Requirement: Logo
The owner SHALL upload one logo per broadcast as PNG, JPEG or WebP of at most 2 MB and 2000 pixels a side, only
through the logo routes. The size SHALL be enforced before the upload is buffered and the dimensions before the
image is decoded. The file SHALL be stored under a random name and served from StreamGo's own origin with its
real image content type and `X-Content-Type-Options: nosniff`. A failed upload SHALL leave the previous logo.

#### Scenario: Upload
- **WHEN** the owner uploads a 300 kB PNG
- **THEN** the stage state's `logo` is a URL under `/assets/` that returns the image

#### Scenario: Not an image
- **WHEN** the upload is an SVG, or a file whose content does not decode as PNG, JPEG or WebP whatever its name
- **THEN** the response is 422, nothing is stored and the previous logo is still shown

#### Scenario: Too large
- **WHEN** the upload exceeds 2 MB, or its header declares more than 2000 pixels on a side
- **THEN** the response is 413 or 422 without the image being decoded, and the previous logo is still shown

#### Scenario: Replace
- **WHEN** the owner uploads a second logo
- **THEN** the stage state points at the new file and the first file no longer exists
