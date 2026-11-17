# Spec Delta

## ADDED Requirements

### Requirement: Configuration file and environment overrides
The system SHALL read `config.toml` from `--config`, else `./config.toml`, else `/etc/streamgo/config.toml`, and
SHALL let an environment variable `STREAMGO_<SECTION>_<KEY>` override any setting. With no file, defaults plus
the environment SHALL be enough to start.

#### Scenario: Environment wins
- **GIVEN** `config.toml` sets `server.addr = ":8080"` and the environment sets `STREAMGO_SERVER_ADDR=":9090"`
- **WHEN** the server starts
- **THEN** it listens on `:9090`

#### Scenario: Unknown key
- **WHEN** `config.toml` contains a key the system does not know
- **THEN** startup fails naming the key

### Requirement: Settings
The configuration SHALL provide the sections `server`, `database`, `livekit`, `mediamtx`, `studio` and `output`,
validate every value against its allowed range at startup, and document every setting with its default in
`config.toml.example`.

#### Scenario: Sections and defaults
- **WHEN** the server starts with no configuration file
- **THEN** `server` has `addr`, `public_url`, `data_dir`, `trusted_proxies`, `cert_file`, `key_file` (both empty) and
  `self_signed` false; `database` has `driver` `sqlite` and
  `path`; `livekit` has
  `url` (for browsers) and `api_url` (for the server); `mediamtx` has `whip_url` and `rtsp_url`; `studio` has
  `max_participants` 10 and `max_on_stage` 6; and `output` has `ffmpeg_path` `ffmpeg`, `resolution` `720p`,
  `video_bitrate_kbps`, `audio_bitrate_kbps`, `max_destinations` 5, `max_concurrent` 2, `start_timeout` 30 s,
  `reconnect_grace` 120 s and `allow_private_destinations` false

#### Scenario: Example file is complete
- **WHEN** the configuration test loads `config.toml.example`
- **THEN** it loads without error and names every setting the loader knows

#### Scenario: Out-of-range value
- **WHEN** `studio.max_on_stage` is 0 or greater than 6, or `output.resolution` is not `720p` or `1080p`
- **THEN** startup fails naming the setting and its allowed values

### Requirement: Secrets only from the environment
The secret key (`STREAMGO_SECRET_KEY`, 32 bytes in base64), the LiveKit API key and secret
(`STREAMGO_LIVEKIT_API_KEY`, `STREAMGO_LIVEKIT_API_SECRET`), the MediaMTX user and secret
(`STREAMGO_MEDIAMTX_USER`, `STREAMGO_MEDIAMTX_SECRET`) and the database connection string
(`STREAMGO_DATABASE_DSN`) SHALL be read only from the environment, SHALL be refused
when present in `config.toml`, and SHALL never be written to logs or command output.

#### Scenario: Missing secret key
- **WHEN** the server starts without `STREAMGO_SECRET_KEY`, or with one that is not 32 bytes
- **THEN** startup fails saying so, without printing the value

#### Scenario: Secret in the file
- **WHEN** `config.toml` contains `livekit.api_secret`
- **THEN** startup fails telling the operator to move it to the environment

### Requirement: Startup checks
At startup the server SHALL verify that `public_url` is an absolute HTTPS URL (plain HTTP only for `localhost`
and loopback addresses), that `data_dir` is writable, and that the configured ffmpeg runs and offers what the
output needs: the `libx264` and `aac` encoders, the `h264`, `vp8` and `opus` decoders, the `rtsp` demuxer, the
`rtsp`, `rtp` and `flv` muxers, the `tcp`, `tls`, `rtmp` and `rtmps` protocols and the `scale`, `pad` and
`format` filters; it SHALL refuse to start otherwise.

#### Scenario: ffmpeg missing a capability
- **WHEN** the configured ffmpeg lacks the `libx264` encoder, or the `rtmps` protocol
- **THEN** startup fails with a message naming what is missing

#### Scenario: A stock ffmpeg is accepted
- **WHEN** the configured ffmpeg is the one of the runtime image
- **THEN** the capability check passes

#### Scenario: Plain HTTP on a public host
- **WHEN** `public_url` is `http://studio.example.com`
- **THEN** startup fails saying browsers need HTTPS for camera and microphone

#### Scenario: Local development
- **WHEN** `public_url` is `http://localhost:8080`
- **THEN** the server starts
