# Output quality

## ADDED Requirements

### Requirement: Resolutions and ceiling
The server SHALL know the output resolutions `720p`, `1080p`, `1440p` and `2160p` at 30 fps, each with a video
bitrate (4000, 6000, 12000, 20000 kbit/s) that `output.video_bitrate_kbps` replaces when it is not 0.
`output.max_resolution` SHALL be the highest resolution a broadcast uses, and the loader SHALL refuse an
`output.resolution` above it.

#### Scenario: Defaults
- **WHEN** the server starts without any output setting
- **THEN** new broadcasts use 720p at 4000 kbit/s and the ceiling is 1080p

#### Scenario: Default above the ceiling
- **WHEN** `output.resolution` is `1440p` and `output.max_resolution` is `1080p`
- **THEN** the server refuses to start and names both settings

#### Scenario: Bitrate override
- **WHEN** `output.video_bitrate_kbps` is 8000
- **THEN** every resolution is sent at 8000 kbit/s

### Requirement: Quality of a broadcast
A broadcast SHALL have an output resolution, the server's default when none was chosen, changed by its owner
only while its output is offline and never above the ceiling.

#### Scenario: Choose
- **GIVEN** a ceiling of `2160p` and an offline broadcast
- **WHEN** its owner patches it with `resolution: "1440p"`
- **THEN** the broadcast detail says `1440p`, 2560x1440 and 12000 kbit/s, and lists the four choices

#### Scenario: Above the ceiling
- **WHEN** the owner asks for `2160p` under a ceiling of `1080p`
- **THEN** the answer is 422 with the field `resolution` and nothing changes

#### Scenario: Not while live
- **WHEN** the owner changes the resolution while the output is starting, live or interrupted
- **THEN** the answer is 409 and nothing changes

#### Scenario: Ceiling lowered later
- **GIVEN** a broadcast stored at `1440p`
- **WHEN** the server restarts with a ceiling of `1080p`
- **THEN** the broadcast goes out, and shows, 1080p

#### Scenario: Someone else's broadcast
- **WHEN** a user patches the resolution of a broadcast they do not own
- **THEN** the answer is 404

### Requirement: The program follows the quality
The transcoder of a run SHALL encode at the broadcast's resolution and bitrate, and the studio SHALL draw and
send the program at that size.

#### Scenario: 1440p reaches the destination
- **GIVEN** a broadcast at `1440p`
- **WHEN** it goes live to the test destination
- **THEN** the destination receives H.264 at 2560x1440 and 30 fps with a keyframe every 60 frames, and AAC at
  48 kHz stereo

#### Scenario: Shared screen at the program's size
- **GIVEN** a broadcast at `1440p` or `2160p`
- **WHEN** the owner shares a screen
- **THEN** the capture is asked for at up to the program's size, not at most 1080p

### Requirement: Quality control in the studio
The owner's studio SHALL offer the output quality in the Destinations tab, with the choices up to the ceiling.

#### Scenario: Pick a quality
- **WHEN** the owner picks another quality while the broadcast is offline
- **THEN** the program canvas takes the new size, a toast confirms it, and a quality above 1080p shows a hint
  that some platforms accept at most 1080p

#### Scenario: Locked while live
- **WHEN** the broadcast is not offline
- **THEN** the control is disabled and says why
