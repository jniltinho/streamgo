# server-settings Specification

## Purpose
The output settings an administrator changes in the app: where they are stored, which source wins (environment, database, configuration file), the routes and the Server tab.

## Requirements

### Requirement: Stored output settings
An administrator SHALL change the default output resolution, the highest output resolution and the video bitrate
in the app. The server SHALL store them in the database and apply them without a restart.

#### Scenario: Raise the ceiling
- **GIVEN** a ceiling of `1080p` from the configuration file
- **WHEN** an administrator puts `max_resolution: "1440p"`
- **THEN** the answer carries `1440p`, a broadcast's detail lists `1440p` among its choices at once, and after a
  restart of the server the ceiling is still `1440p`

#### Scenario: Bitrate by resolution
- **GIVEN** a configuration file with `video_bitrate_kbps = 4000`
- **WHEN** an administrator puts `video_bitrate_kbps: 0`
- **THEN** a 1080p broadcast's detail says 6000 kbit/s

#### Scenario: Rules
- **WHEN** an administrator puts a default above the ceiling, an unknown resolution, or a bitrate of 300
- **THEN** the answer is 422 with the field at fault and nothing is stored

#### Scenario: All or nothing
- **WHEN** a put gives a valid ceiling and an invalid bitrate
- **THEN** neither is stored

### Requirement: Precedence
A setting given by its environment variable SHALL win over the stored value and SHALL be locked in the app; a
stored value SHALL win over the configuration file.

#### Scenario: Locked by the environment
- **GIVEN** the server started with `STREAMGO_OUTPUT_MAX_RESOLUTION=1080p`
- **WHEN** an administrator reads the settings and then puts another ceiling
- **THEN** the read lists `max_resolution` as locked, and the put is refused with 409 and changes nothing

#### Scenario: Stored wins over the file
- **GIVEN** `resolution = "720p"` in the configuration file and `1080p` stored
- **WHEN** the server restarts and a broadcast is created
- **THEN** the new broadcast uses 1080p

### Requirement: Administrators only
`GET /settings` and `PUT /settings` SHALL serve only an enabled administrator.

#### Scenario: Not an administrator
- **WHEN** a signed-in user who is not an administrator calls either route
- **THEN** the answer is 403 `forbidden`

### Requirement: Server tab
The Settings modal SHALL show administrators a Server tab with the three settings.

#### Scenario: Change and save
- **WHEN** an administrator picks another ceiling and saves
- **THEN** a toast confirms it and the studio of an offline broadcast offers the new choices when it is opened

#### Scenario: Locked field
- **WHEN** a setting is locked by the environment
- **THEN** its control is disabled and says the environment sets it

#### Scenario: Hidden from others
- **WHEN** a user who is not an administrator opens Settings
- **THEN** there is no Server tab
