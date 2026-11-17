# Design: server settings

## Decisions

### S1. Precedence: environment, then database, then file
For each of the three settings the effective value is: the environment variable when it is set
(`STREAMGO_OUTPUT_RESOLUTION`, `STREAMGO_OUTPUT_MAX_RESOLUTION`, `STREAMGO_OUTPUT_VIDEO_BITRATE_KBPS`), else the
value stored in the database, else what the loader read from `config.toml` or its default. `cmd/server.go` tells
the settings service which of the three the environment sets (`os.LookupEnv`); such a setting is **locked**: the
API refuses to change it and the form shows it disabled with the reason.

### S2. One small table
`settings (name VARCHAR(64) PRIMARY KEY, value TEXT NOT NULL)`, migration `00004_settings`. The three settings are
three rows (`output.resolution`, `output.max_resolution`, `output.video_bitrate_kbps`). No upsert syntax: the
repository updates and, when no row changed, inserts, inside one transaction.

### S3. One source of the quality
`internal/core/settings.Service` holds the effective `broadcast.Quality` in memory (an atomic value), loaded at
start and replaced after a successful update. `broadcast.Service` and `output.Service` take
`Quality func() broadcast.Quality` instead of a value. A run of the output keeps the profile it started with; a
lower ceiling applies from the next go-live (as when the ceiling is lowered in the file today). The output fixes
the profile in the run's state at its first transcoder, so a transcoder retried later in the run keeps it.

### S4. Rules, in the core
An update is refused unless the actor is an enabled administrator (`core.ErrForbidden`, through a small port that
`account.Service` implements). The values follow the loader's rules: known resolutions, the default not above the
ceiling, the bitrate 0 or 500 to 40000. A patch gives any of the three; the stored and the locked values complete
it before the rules are checked, and the update writes all given values or none. A locked field given with its
current value is not a change (the form can send it back); with another value the update is a conflict. At start,
a stored combination that breaks the rules with the configuration (an environment variable set later can cause it)
is ignored with a warning and the configuration's values apply.

### S5. API
```
GET /settings   200 { output: { resolution, max_resolution, video_bitrate_kbps, resolutions: [...], locked: [...] } } | 403
PUT /settings   { output: { resolution?, max_resolution?, video_bitrate_kbps? } }  200 (the same shape) | 403 | 422 | 409 (locked)
```
`resolutions` is the whole ladder; `locked` names the fields the environment sets.

### S6. Web app
A "Server" tab in `PreferencesModal.vue`, shown to administrators: two selects and the bitrate as "By
resolution" or a number of kbit/s, a Save button, the hint that a value locked by the environment is changed
there, and that the ceiling applies to a live broadcast from its next go-live. The start-up warning about a fixed
bitrate below the ladder uses the effective values.
