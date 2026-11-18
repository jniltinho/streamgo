# Design: output quality

## Measured before the design (this machine, 24 threads, ffmpeg 8.0.1)

The transcoder's own command line (libx264 veryfast, zerolatency, GOP 60) on a synthetic 30 fps picture in real
time: 1080p at 6000 kbit/s used 0.5 core, 1440p at 12000 used 0.9, 2160p at 20000 used 2.3. Real pictures cost
more than a test pattern.

The browser side, measured in task 1.1 with headless Chrome (no GPU) against a throwaway stack, two fake 720p
cameras and a shared screen at the program's size on stage (numbers in `docs/architecture.md`, "Output quality"):
1080p and 1440p meet the bar of Q6 (compositor 30.0 ticks/s, 8.0 and 11.1 ms a tick on average, the WHIP sender at
30 fps, every frame the destination received distinct, a keyframe every 60). 2160p does not: the page stopped
answering, 30 to 40 distinct pictures reached the destination in 20 s, and the program was then lost. **This
change ships 720p, 1080p and 1440p; 2160p is left out** of the code and the documents.

## Decisions

### Q1. Three resolutions, one ladder
`720p` 1280x720 4000 kbit/s, `1080p` 1920x1080 6000, `1440p` 2560x1440 12000 (2160p was measured and left out); 30 fps and
a keyframe every 60 frames in all. The table lives in one place in `internal/core/broadcast` (a `Profile` with
width, height and video bitrate) and nothing else spells a size. `output.video_bitrate_kbps` is 0 by default
("by resolution") and, when not 0, replaces the ladder's value for every resolution (500 to 40000).

### Q2. Default and ceiling from the operator
`output.resolution` (default `720p`) is what a new broadcast uses. `output.max_resolution` (default `1080p`) is the
highest a broadcast may use; the loader refuses a default above the ceiling. A broadcast stored with a quality
above a ceiling that was lowered later goes out at the ceiling.

### Q3. The broadcast owns its quality
`broadcasts.resolution` (text, '' = the server's default). `PATCH /broadcasts/:id` takes two optional fields,
`title` and `resolution`, at least one of them. `broadcast.Service.Update` replaces `Rename`: it validates both,
takes `broadcast.Locks.Lock(ctx, id)` (the lock `output.Service.GoLive` holds), refuses a resolution above the
ceiling (`core.Invalid`) and a resolution change while the output is not offline (`core.ErrConflict`), and writes
both columns in one `UPDATE` (`Repository.Update`, which replaces `SetTitle`). A refused patch changes nothing,
and a patch racing a go-live is ordered by the lock: either it lands first and the run uses the new profile, or
the go-live lands first and the patch is refused. Patching the resolution with the value it already has is not a
change and is accepted in any state. The broadcast detail's `studio` object carries the broadcast's own
`program_width`, `program_height` and `video_bitrate_kbps`, plus `resolution` and `resolutions` (the choices up
to the ceiling), so the studio needs no other source.

### Q4. The transcoder gets the profile
`output.Processes.StartTranscoder(broadcastID string, p broadcast.Profile, notify)`: the output service reads the profile of
the broadcast when it starts the transcoder, so a run uses one profile from start to end. The relays copy and do
not care.

### Q5. The browser
- The compositor canvas and the WHIP sender already take their size and bitrate from the broadcast detail; the
  sender keeps `maintain-resolution` and twice the transcoder's bitrate as its ceiling.
- A shared screen is captured at up to the program's size (LiveKit's default stops at 1080p).
- Picking a quality in an open studio applies the PATCH's answer at once: the program canvas is rebuilt at the
  new size (one helper used by opening, polling and the quality control), a screen that is already shared gets
  the new size through `applyConstraints` on its track (no new prompt), and the WHIP sender reads the bitrate
  when it starts, which can only be after the change because the change is refused unless offline.
- The quality control is in the Destinations tab, disabled with a reason while the broadcast is not offline, and
  shows only the choices up to the ceiling. Above 1080p a hint says that some platforms accept at most 1080p.

### Q6. What ships depends on the measurement
The bar is the one of the first version: the destination receives 30 fps (29 to 31 measured over 20 s) with a
keyframe every 60 frames, from Chrome on this machine, with two 720p cameras and a shared screen on stage. Every
resolution that meets it ships; one that does not is left out of the code and of the documents, and the measured
numbers go into `docs/architecture.md`. The ceiling defaults to `1080p` whatever the result: the operator opts in.

### Q7. The migration stands alone
`00003_broadcast_resolution` adds one column with a default to `broadcasts` and touches nothing that
`00002_user_admin` (add-user-management) adds or reads; it depends on it only through its number. Either change
can ship first. So that an install already at 00003 still takes 00002 when it arrives later, the store's migrator
allows out-of-order migrations (`goose.WithAllowOutofOrder`): a missing lower number is applied, not refused.

## Risks

- A headless Chrome draws the canvas without a GPU, so the measurement may be worse than a desktop Chrome with
  one. If only headless fails, say so in the report rather than shipping an unmeasured claim.
- Upload: 1440p asks the owner's connection for up to 24 Mbit/s. The hint in the studio says what each choice
  needs.
