# Tasks: output quality

## 1. Measure first

- [x] 1.1 With a throwaway stack and Chrome, measure at 1080p, 1440p and 2160p: the compositor's tick time, and
  the frame rate and keyframe distance of what the test destination receives over 20 s (two 720p cameras and a
  shared screen on stage). Record the numbers in `docs/architecture.md`. A resolution below the bar of design Q6
  is left out of everything that follows. (Headless Chrome: 1080p and 1440p pass, 2160p does not and is left out;
  the frame rate at the destination is counted in distinct frames, because the transcoder repeats pictures to keep
  30 fps constant.)

## 2. Server

- [x] 2.1 `internal/core/broadcast`: `Profile` and the ladder; `internal/config`: `output.max_resolution`, the new
  values and ranges, `config.toml.example`; tests for "Resolutions and ceiling"
- [x] 2.2 Migration `00003_broadcast_resolution` in the three dialects; repository and store; `storetest.Each`;
  the migrator accepts out-of-order migrations ("Migrations in either order")
- [x] 2.3 `broadcast.Service.Update` (replaces `Rename`) with the rules of "Quality of a broadcast" under the
  broadcast's lock, `Repository.Update` (replaces `SetTitle`); `handlers/broadcasts.go`: `PATCH` with optional
  fields and the `studio` object of the detail; tests, the race with a go-live included
- [x] 2.4 `output.Processes.StartTranscoder` with the profile; `internal/ffmpeg`; `cmd/server.go`; the
  integration test in `internal/mediamtx` checks the format of a 1440p broadcast

## 3. Web app

- [x] 3.1 The studio store takes size and bitrate from the broadcast; the screen capture asks for the program's
  size; picking a quality rebuilds the canvas and resizes a shared screen; unit tests (`tests/unit/quality.test.ts`)
- [x] 3.2 The quality control in `DestinationsTab.vue`; both catalogs; e2e in `20-studio.spec.ts`

## 4. Docs and checks

- [x] 4.1 `docs/deployment.md`, `architecture.md`, `README.md`, `CHANGELOG.md` (`## [Unreleased]`, with the
  breaking bitrate default)
- [x] 4.2 `make check && make test && make test-db`, the studio suite against a throwaway stack,
  `openspec validate add-output-quality --strict`
