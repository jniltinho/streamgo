# Design: output quality

## Measured before the design (this machine, 24 threads, ffmpeg 8.0.1)

The transcoder's own command line (libx264 veryfast, zerolatency, GOP 60) on a synthetic 30 fps picture in real
time: 1080p at 6000 kbit/s used 0.5 core, 1440p at 12000 used 0.9, 2160p at 20000 used 2.3. Real pictures cost
more than a test pattern. The browser side (drawing the program and encoding it for WHIP at 1440p and 2160p) is
**not measured yet**: it is task 1 and decides how far this change goes.

## Decisions

### Q1. Four resolutions, one ladder
`720p` 1280x720 4000 kbit/s, `1080p` 1920x1080 6000, `1440p` 2560x1440 12000, `2160p` 3840x2160 20000; 30 fps and
a keyframe every 60 frames in all. The table lives in one place in `internal/core/broadcast` (a `Profile` with
width, height and video bitrate) and nothing else spells a size. `output.video_bitrate_kbps` is 0 by default
("by resolution") and, when not 0, replaces the ladder's value for every resolution (500 to 40000).

### Q2. Default and ceiling from the operator
`output.resolution` (default `720p`) is what a new broadcast uses. `output.max_resolution` (default `1080p`) is the
highest a broadcast may use; the loader refuses a default above the ceiling. A broadcast stored with a quality
above a ceiling that was lowered later goes out at the ceiling.

### Q3. The broadcast owns its quality
`broadcasts.resolution` (text, '' = the server's default). `PATCH /broadcasts/:id` accepts `resolution`; the core
refuses a value above the ceiling (`core.Invalid`) and any change while the output is not offline
(`core.ErrConflict`). The broadcast detail's `studio` object carries the broadcast's own `program_width`,
`program_height` and `video_bitrate_kbps`, plus `resolution` and `resolutions` (the choices up to the ceiling),
so the studio needs no other source.

### Q4. The transcoder gets the profile
`output.Processes.StartTranscoder(broadcastID string, p Profile, notify)`: the output service reads the profile of
the broadcast when it starts the transcoder, so a run uses one profile from start to end. The relays copy and do
not care.

### Q5. The browser
- The compositor canvas and the WHIP sender already take their size and bitrate from the broadcast detail; the
  sender keeps `maintain-resolution` and twice the transcoder's bitrate as its ceiling.
- A shared screen is captured at up to the program's size (LiveKit's default stops at 1080p).
- The quality control is in the Destinations tab, disabled with a reason while the broadcast is not offline, and
  shows only the choices up to the ceiling. Above 1080p a hint says that some platforms accept at most 1080p.

### Q6. What ships depends on the measurement
The bar is the one of the first version: the destination receives 30 fps (29 to 31 measured over 20 s) with a
keyframe every 60 frames, from Chrome on this machine, with two 720p cameras and a shared screen on stage. Every
resolution that meets it ships; one that does not is left out of the code and of the documents, and the measured
numbers go into `docs/architecture.md`. The ceiling defaults to `1080p` whatever the result: the operator opts in.

## Risks

- A headless Chrome draws the canvas without a GPU, so the measurement may be worse than a desktop Chrome with
  one. If only headless fails, say so in the report rather than shipping an unmeasured claim.
- Upload: 2160p asks the owner's connection for up to 40 Mbit/s. The hint in the studio says what each choice
  needs.
