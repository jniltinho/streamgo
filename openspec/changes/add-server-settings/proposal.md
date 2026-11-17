# Server settings in the web app

## Why

The output's default resolution, its ceiling and the video bitrate are set in `config.toml` or in the environment.
The owner of the install had to append lines to `.env` and restart the stack to allow 1440p, and a `config.toml`
made from an older example pins the bitrate without anyone noticing. The owner asked to change these in the
administrator's settings.

## What Changes

- **Server tab in Settings** (administrators only): the default output resolution of new broadcasts, the highest
  resolution a broadcast may use, and the video bitrate (by resolution, or one fixed value).
- **Stored in the database**, applied at once, without a restart. A value given by an environment variable still
  wins and shows as locked in the form; `config.toml` and the built-in defaults are what the form starts from.
- **API**: `GET /settings` and `PUT /settings` for administrators (403 for anyone else).

Only these three output settings move to the app in this change. The studio's limits, the destinations' limits
and everything about addresses, secrets and ports stay in the configuration.

## Capabilities

### New Capabilities

- `server-settings`: the stored settings, their precedence, the routes and the Server tab.

### Modified Capabilities

None formally; the output quality of `add-output-quality` now reads its `Quality` from this capability.

## Impact

- A migration `00004_settings` in the three dialects (a key-value table).
- A new `internal/core/settings`, its store adapter and its handlers; `internal/core/broadcast` and
  `internal/core/output` take the quality from a source instead of a fixed value; `cmd/server.go`.
- Web app: a Server tab in `PreferencesModal.vue` for administrators, both catalogs.
- Docs: `deployment.md`, `CHANGELOG.md`.
