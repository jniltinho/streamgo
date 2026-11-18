# Tasks: one file to run StreamGo

## 1. Configuration and secrets

- [ ] 1.1 `internal/config`: the bootstrap step of design B8 (unvalidated load, the data directory, the env file
  as data, validation), `--data-dir` on the root command, the `[media]` settings of design B10 with their
  defaults, range checks and lines in `config.toml.example`; table-driven tests for "A file that is not
  assignments", "Other commands", "Commands that need no configuration", "The example lists the settings"
- [ ] 1.2 First-start secrets: create and complete `streamgo.env` atomically, keeping a value the environment
  gave; tests for every other scenario of "First-start secrets"; rule 5 of `AGENTS.md`
- [ ] 1.3 The instance lock out of `runServer` (design B9); test for "A second start", with two builds of
  different versions

## 2. The media servers as child processes

- [ ] 2.1 `internal/mediaservers`: the payload of the own executable (design B2: trailer, offsets, the pinned
  digests compiled in), choose each program, unpack, verify, repair and clean up (design B4); tests with fake
  servers and a payload appended by the test for every scenario of "Where the media servers come from" and
  "Embedded media servers"
- [ ] 2.2 Supervision (design B5): the launcher thread, process groups and the parent-death signal, bounded log
  lines and tail, the restart delay, the ordered stop, cleanup on a failed start; tests with real processes for
  every scenario of "Supervision"
- [ ] 2.3 Ports, generated configuration and the children's environment (design B6 and B7), redaction of the
  secrets in logs and tails; tests for every scenario of "Ports" and "The children's environment"

## 3. The commands

- [ ] 3.1 `cmd/up.go`: the flags and their precedence, managed media settings, the refusal as root, the ffmpeg
  check before any child, the start and stop order; tests for every scenario of "The up command", "Settings and
  flags", "HTTPS" and "ffmpeg"
- [ ] 3.2 `cmd/licenses.go` with the embedded texts (design B13); the inventory test in both builds; `THIRD_PARTY`

## 4. Build and release

- [ ] 4.1 `Dockerfile`: the pinned digests into every build; a stage that packs the executable with
  `upx --lzma`, tests it, and appends the two verified binaries (zstd -19) and the trailer; `make single` writes
  `dist/streamgo-single`; `packaging/build.sh` writes the small archive; record both sizes in bytes in the
  proposal
- [ ] 4.2 `packaging/single-smoke.sh` (design B12: a clean Debian, an unprivileged user, an observer that stays
  alive): "A working studio", the key kept, "Upgrade", "The parent is killed"; `make test-single`
- [ ] 4.3 `.github/workflows/packages.yml` builds the single file and runs its smoke test on every push to
  `main`, gathers the six files in one directory, writes `SHA256SUMS` last and publishes by expected file names
- [ ] 4.4 `.claude/skills/create-release/SKILL.md`: the checks, the expected files, the downloads and the notes

## 5. Docs and checks

- [ ] 5.1 `docs/install-single.md` (with the systemd unit), `docs/README.md`, `docs/install-binary.md`,
  `CHANGELOG.md`
- [ ] 5.2 `README.md` brought up to date: the single file in the quick start, the downloads table with sizes,
  the "Good to know" section rewritten for the current version, every number checked against the code
- [ ] 5.3 `make check && make test && make test-db && make build && make test-package && make test-allinone &&
  make test-single`, `openspec validate add-single-binary --strict`
