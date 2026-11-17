# Output quality

## ADDED Requirements

### Requirement: Resolutions and ceiling
The server SHALL know the output resolutions `720p`, `1080p` and `1440p` at 30 fps, each with a video bitrate
(4000, 6000 and 12000 kbit/s) that `output.video_bitrate_kbps` replaces when it is not 0. `output.max_resolution`
SHALL be the highest resolution a broadcast uses, and the loader SHALL refuse an `output.resolution` above it and
any other resolution (`2160p` was measured and left out, design Q6).

#### Scenario: Defaults
- **WHEN** the server starts without any output setting
- **THEN** new broadcasts use 720p at 4000 kbit/s and the ceiling is 1080p

#### Scenario: Default above the ceiling
- **WHEN** `output.resolution` is `1440p` and `output.max_resolution` is `1080p`
- **THEN** the server refuses to start and names both settings

#### Scenario: Unknown resolution
- **WHEN** `output.resolution` or `output.max_resolution` is not one of the three, `2160p` included
- **THEN** the server refuses to start, names the setting and lists the three

#### Scenario: Bitrate override
- **WHEN** `output.video_bitrate_kbps` is 8000
- **THEN** every resolution is sent at 8000 kbit/s

### Requirement: Quality of a broadcast
A broadcast SHALL have an output resolution, the server's default when none was chosen, changed by its owner
only while its output is offline and never above the ceiling. `PATCH /broadcasts/:id` SHALL take `title` and
`resolution`, both optional but not both absent, and apply them together or not at all, under the broadcast's lock.

#### Scenario: Choose
- **GIVEN** a ceiling of `1440p` and an offline broadcast
- **WHEN** its owner patches it with `resolution: "1440p"` alone
- **THEN** the broadcast detail says `1440p`, 2560x1440 and 12000 kbit/s, lists the three choices, and the title
  is unchanged

#### Scenario: Title and resolution together
- **WHEN** the owner patches an offline broadcast with a new title and `resolution: "1080p"`
- **THEN** both change in one update

#### Scenario: Above the ceiling
- **WHEN** the owner asks for `1440p` under a ceiling of `1080p`, alone or with a new title
- **THEN** the answer is 422 with the field `resolution` and nothing changes, the title included

#### Scenario: Not while live
- **WHEN** the owner changes the resolution while the output is starting, live or interrupted
- **THEN** the answer is 409 and nothing changes; patching the resolution it already goes out at is accepted

#### Scenario: Racing a go-live
- **WHEN** a resolution patch and a go-live of the same offline broadcast arrive at the same time
- **THEN** either the patch is applied and the run goes out at the new resolution, or the go-live is first and
  the patch answers 409 with nothing changed

#### Scenario: Ceiling lowered later
- **GIVEN** a broadcast stored at `1440p`
- **WHEN** the server restarts with a ceiling of `1080p`
- **THEN** the broadcast goes out, and shows, 1080p

#### Scenario: Someone else's broadcast
- **WHEN** a user patches the resolution of a broadcast they do not own
- **THEN** the answer is 404

#### Scenario: Migrations in either order
- **GIVEN** an install whose database has a migration numbered above one that arrives later
- **WHEN** the server migrates
- **THEN** the missing lower migration is applied, not refused

### Requirement: The program follows the quality
The transcoder of a run SHALL encode at the broadcast's resolution and bitrate, and the studio SHALL draw and
send the program at that size.

#### Scenario: 1440p reaches the destination
- **GIVEN** a broadcast at `1440p`
- **WHEN** it goes live to the test destination
- **THEN** the destination receives H.264 at 2560x1440 and 30 fps with a keyframe every 60 frames, and AAC at
  48 kHz stereo

#### Scenario: Shared screen at the program's size
- **GIVEN** a broadcast at `1440p`
- **WHEN** the owner shares a screen
- **THEN** the capture is asked for at up to the program's size, not at most 1080p

### Requirement: Quality control in the studio
The owner's studio SHALL offer the output quality in the Destinations tab, with the choices up to the ceiling.

#### Scenario: Pick a quality in the open studio
- **WHEN** the owner picks another quality while the broadcast is offline and a screen is shared
- **THEN** the program canvas is rebuilt at the new size, the shared screen is asked for the new size without a
  new prompt, a toast confirms it, the hint says the size and the upload it uses, and a quality above 1080p adds
  that some platforms accept at most 1080p; the WHIP sender takes the new bitrate when the broadcast goes live

#### Scenario: Locked while live
- **WHEN** the broadcast is not offline
- **THEN** the control is disabled and says why
