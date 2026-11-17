# Output quality: HD, Full HD and QHD, chosen per broadcast

## Why

The program goes out at one resolution for the whole server (`output.resolution`: 720p or 1080p). The owner asked
to choose the output quality, up to QHD and 4K: a shared screen from a QHD or 4K monitor is reduced to 720p or
1080p today and its text is soft.

## What Changes

- **Resolutions**: 720p, 1080p and 1440p (QHD), all at 30 fps. 2160p (4K) was asked for, measured first
  (task 1.1) and left out: the owner's browser could not compose and send it at 30 fps.
- **Chosen per broadcast**: the owner picks the output quality of a broadcast in the studio while it is offline.
  New broadcasts start at the server's default.
- **Operator's ceiling**: `output.max_resolution` (default `1080p`) is the highest quality a broadcast may use,
  because 1440p costs CPU on the server and upload on the owner's side. `output.resolution` stays as
  the default of new broadcasts.
- **Bitrate follows the resolution**: 4000, 6000 and 12000 kbit/s for 720p, 1080p and 1440p;
  `output.video_bitrate_kbps` overrides it for every resolution when set. **BREAKING**: an install that did not
  set it and uses 1080p now sends 6000 instead of 4000 kbit/s.
- **Sharper shared screens**: a screen is captured at up to the program's size instead of at most 1080p.

Not in this change: other frame rates, a different quality per destination, capturing cameras above 720p.

## Capabilities

### New Capabilities

- `output-quality`: the resolutions, the ceiling, the per-broadcast choice and its effect on the program, the
  transcoder and the screen capture.

### Modified Capabilities

None formally (`add-live-studio` is not archived yet); this change extends its `configuration`, `broadcasts`,
`live-output` and `studio-ui` behaviour as described in the new capability.

## Impact

- Configuration: `output.max_resolution` (new), `output.resolution` (two more values), `output.video_bitrate_kbps`
  (0 = by resolution, the new default; up to 40000), `config.toml.example`.
- A migration `00003_broadcast_resolution` in the three dialects (a column on `broadcasts`); the store's migrator
  accepts out-of-order migrations so it and `00002_user_admin` can ship in either order.
- `internal/core/broadcast`, `internal/core/output` (the transcoder is started with the broadcast's profile),
  `internal/ffmpeg`, `internal/httpserver/handlers/broadcasts.go`, `cmd/server.go`.
- Web app: the studio store (program size and bitrate from the broadcast), the screen capture, a quality control
  in the Destinations tab, both catalogs.
- Docs: `deployment.md`, `architecture.md` (limits and measurements), `README.md` ("What you get", "Good to
  know"), `CHANGELOG.md`.
