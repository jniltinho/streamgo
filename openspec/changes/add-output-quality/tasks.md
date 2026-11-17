# Tasks: output quality

## 1. Measure first

- [ ] 1.1 With a throwaway stack and Chrome, measure at 1080p, 1440p and 2160p: the compositor's tick time, and
  the frame rate and keyframe distance of what the test destination receives over 20 s (two 720p cameras and a
  shared screen on stage). Record the numbers in `docs/architecture.md`. A resolution below the bar of design Q6
  is left out of everything that follows.

## 2. Server

- [ ] 2.1 `internal/core/broadcast`: `Profile` and the ladder; `internal/config`: `output.max_resolution`, the new
  values and ranges, `config.toml.example`; tests for "Resolutions and ceiling"
- [ ] 2.2 Migration `00003_broadcast_resolution` in the three dialects; repository and store; `storetest.Each`
- [ ] 2.3 `broadcast.Service`: the resolution in `Rename`'s patch or its own method, with the rules of "Quality
  of a broadcast"; `handlers/broadcasts.go`: `PATCH` and the `studio` object of the detail; tests
- [ ] 2.4 `output.Processes.StartTranscoder` with the profile; `internal/ffmpeg`; `cmd/server.go`; the
  integration test in `internal/mediamtx` checks the format of a 1440p broadcast when 1440p ships

## 3. Web app

- [ ] 3.1 The studio store takes size and bitrate from the broadcast; the screen capture asks for the program's
  size; unit tests
- [ ] 3.2 The quality control in `DestinationsTab.vue`; both catalogs; e2e in `20-studio.spec.ts`

## 4. Docs and checks

- [ ] 4.1 `docs/deployment.md`, `architecture.md`, `README.md`, `CHANGELOG.md` (`## [Unreleased]`, with the
  breaking bitrate default)
- [ ] 4.2 `make check && make test && make test-db`, the studio suite against a throwaway stack,
  `openspec validate add-output-quality --strict`
