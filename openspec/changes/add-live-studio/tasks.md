# Tasks

Go tasks run their tests with `-race` on SQLite, MariaDB and PostgreSQL (`make test-db`). Groups run in order;
inside group 3 the lettered packages own disjoint files (design.md, "Parallel work packages") and are built in
parallel by separate agents against the REST contract. Deadlines and timers are tested with a controlled clock;
media behaviour with real LiveKit, MediaMTX and ffmpeg. A task is done when the check it names passes.

## 1. Spike: prove the media path before building on it

The spike uses the real architecture: an owner and a guest connected through LiveKit, a throwaway page that
composes them, MediaMTX with the production settings (RTSP and WebRTC only, internal credentials), and a
separate test-only MediaMTX with RTMP enabled as the destination. Each test source burns a changing frame
counter into its video and plays a distinct tone.

- [x] 1.1 Compose the owner's and the guest's LiveKit tracks on a canvas driven by a Web Worker timer, with
  adaptive stream off, mix audio with WebAudio (program bus with a limiter, silent when empty) and publish by
  WHIP; verify with `ffprobe` on the RTSP path that video and audio arrive, once offering VP8 and once H.264
- [x] 1.2 Run the transcoder and one copy relay by hand into the test RTMP sink; verify H.264 at the configured
  size and 30 fps, keyframes 2 s apart, AAC 48 kHz, each tone present once, and A/V drift under 100 ms after 30
  minutes; repeat the relay over RTMPS; record CPU use at 720p and 1080p
- [x] 1.3 Hide the owner's tab for five minutes, then minimize its window for five minutes; verify in the sink's
  recording that both frame counters keep advancing (at least 25 times in every second, never stopped for more
  than 1 s). This is the result that decides whether browser composition stands (done: the hidden tab passed;
  the minimized window passed only as an emulation and is checked on a real desktop in 6.2)
- [x] 1.4 Proxy WHIP `POST`, `PATCH` and `DELETE` through a minimal Go handler with MediaMTX on internal
  credentials; verify publishing works through the proxy and is refused directly, that restarting the handler
  does not stop the media, and that a `DELETE` sent after the restart with the stored session URL closes it
- [x] 1.5 Failure behaviour: stop and restart the publisher during a relay; blackhole the sink's network; make
  the sink accept and stop reading. Verify what ffmpeg does in each case (exit or hang), that the media time in
  `-progress` output distinguishes flowing from stalled, how long a sink that stopped reading goes unnoticed,
  and which timeouts end a blocked process; record the timings. The 15 s bound of the `live-output` "Destination
  stalls" scenario stands or is corrected by this result
- [x] 1.6 Evaluate MediaMTX's native `forward` on the pinned version against the relay requirements: add and
  remove a destination without touching the others, per-destination status and last error through its API,
  retry after failure; record whether it can replace the relay processes
- [x] 1.7 Reload the owner's page in a fresh browser profile; record whether the `AudioContext` starts
  suspended and what the page can restore by itself
- [x] 1.8 Write the results (pinned MediaMTX, LiveKit and ffmpeg versions, exact ffmpeg arguments, measurements,
  the answers to 1.3, 1.5, 1.6 and 1.7) to `docs/spike-media-path.md`, and update design decisions D2 to D5 where
  the results differ. If 1.3 or the drift measurement fails, stop and take the result to the owner before group 2

## 2. Foundation (one agent; everything later builds on it)

- [x] 2.1 Create the module, `main.go`, the Cobra tree with `version` and the `--config` / `--json` flags,
  `internal/buildinfo`, the Makefile (`build`, `check`, `test`, `test-db`, `tools`, `init`, `up`, `down`, `dev`)
  and the tools image; verify the `server-cli` "Command tree" scenarios and `make check test build`
- [x] 2.2 Implement `internal/config` (file search, environment overrides, unknown keys, ranges, secrets only from
  the environment, startup checks including HTTPS and the ffmpeg capabilities) and `config.toml.example`; verify
  every `configuration` scenario with table-driven tests
- [x] 2.3 Implement `internal/store` with the database opening for the three drivers (SQLite with WAL, foreign
  keys and busy timeout), every table of the data model as goose migrations per dialect with equal versions,
  `migrate up` / `status`, `make test-db` with throwaway MariaDB and PostgreSQL containers, and the instance lock
  file; verify every `database` scenario, the `server-cli` "Idempotent" and "Single instance" scenarios, and a
  test that the three migration directories list the same versions
- [x] 2.4 Declare only what more than one package depends on: the shared domain types and typed errors, the
  broadcast repository interface, the per-broadcast lock, the `internal/theme` registry with its 22 entries, and
  the `depguard` rule (each package defines its own ports with its first slice); verify the "Forbidden import in the core" scenario and the `ui-themes`
  "Catalog" scenario
- [x] 2.5 Build `internal/httpserver`: Echo v5 with the error envelope, the route-pattern request log, the
  `Origin` check, body limits, `Cache-Control: no-store`, trusted proxies, the session middleware hook, a
  registration function per package (no placeholder handlers), `healthz` / `readyz`, the embedded web app with
  the client-route fallback, and `cmd/server.go` with graceful shutdown; verify the `web-shell` "Unknown API
  route" scenario and the `authentication` "Cross-origin post", "Missing or null origin", "Oversized body",
  "Spoofed forwarding header" and "Nothing sensitive in logs or caches" scenarios
- [x] 2.6 Scaffold `frontend/` with Vite+ (`vp dev`, `vp build` into `web/dist`, `vp check`, `vp test`; lint
  rules in `vite.config.ts` banning native dialogs and framework imports in `frontend/src/core`), Vue 3,
  TypeScript, Pinia, Vue Router, Tailwind 4 and Playwright, in MeetHub's structure: the router with every client
  route as an empty view, the typed API client with the error envelope, the i18n module with `en` and `pt-BR`;
  copy MeetHub's theme base (role contract, shared recipes, the three radius tokens in `rem`, the root scale
  rule of D13, first-paint script) and its
  themed toast, alert and dialog components with the feedback gallery page; verify the `web-shell` "Catalog
  parity" and "No native dialogs" scenarios, the "Forbidden import in the frontend core" scenario and
  `make check`
- [x] 2.7 Write `AGENTS.md` (rules of this project, the layering rule, how to validate) and the `README.md`
  quick start; verify the commands in them run

## 3. Parallel packages

- [x] 3.A Accounts: `core/account` (argon2id, sessions with inactivity expiry, disable), its repositories, the
  auth and `me` handlers (`PATCH /me` validating the theme against the registry), the session middleware, the
  bounded sign-in rate limiter and `cmd/user.go`; verify the `authentication` "Accounts", "Password sign-in and
  sessions", "Sign-in rate limit" and "Authorization in the server" scenarios, the `server-cli` "User command"
  scenarios and the `ui-themes` "Follows the user" and "Unknown theme" scenarios
- [x] 3.B Broadcasts and destinations: `internal/sealed`, `core/broadcast` (management, deletion with its room,
  invite link and rotation), `core/destination` (management, sealed address and key, the private-address check,
  selection per broadcast, the in-use rule), the broadcast and destination repositories and handlers; verify
  every `broadcasts` and `destinations` scenario except "Redacted from errors" and "Name that later resolves to
  a private address", which are verified in 3.D
- [x] 3.C Studio: `core/stage` (state, validation, the single writer under the broadcast's lock), `core/studio`
  (owner and guest joining, guest sessions and expiry, capacity with the owner's priority, removal, the room
  reconciler), `internal/livekit` (tokens, rooms, metadata, participants), the banner and guest repositories,
  the studio, join, banners and logo handlers; verify every `studio` scenario, with a fake `Rooms` and a
  controlled clock in the core and a real LiveKit container (automatic room creation off) in an integration test
- [x] 3.D Live output: `core/output` (the state machine with runs, stored deadlines, capacity reservation and
  restart recovery, table-tested with a controlled clock), `internal/ffmpeg` (supervisor with progress parsing,
  the stall watchdog, timeouts, process group and parent-death signal from a thread-locked goroutine, clean
  environment, the arguments from the spike, redaction of the whole address, backoff), `internal/mediamtx` (the
  WHIP client behind the proxy routes, without `PATCH`, with one opaque resource per offer and no upstream call
  under the lock,
  and the control API client that closes orphan ingest sessions), destination claims under the global lock, the live and WHIP handlers; verify every `live-output` scenario, the two
  `destinations` scenarios left by 3.B and the `server-cli` "Graceful stop while live" scenario, using a fake
  ffmpeg script in unit tests and the real stack, with SIGKILL of the server, in an integration test. If the
  spike adopted MediaMTX forwards (1.6), the relays are implemented with them behind the same port
- [x] 3.E Frontend core: the layout function, the compositor (sources read with `MediaStreamTrackProcessor`,
  the program emitted with `MediaStreamTrackGenerator` on a 30 fps grid from a worker timer, the simple
  `<video>` drawing for guests' previews; placeholders, name tags, logo, banner), the audio mix (program and
  monitor buses, remote tracks through muted audio elements, limiter with its gain taken back, each track once)
  and the WHIP client (candidates gathered before the offer, no offer while audio is suspended), without Vue; verify the `studio-ui` "Layout rectangles" and "Absent participant" scenarios as a
  property test over every layout and source count, compositor snapshot tests, and audio-graph tests with tones
- [x] 3.F App pages: the sign-in, broadcasts and destinations views with their stores and API modules, the route
  guard and the language switch, in a centred column that follows the D13 scale; verify every `web-shell`
  scenario with Playwright against the running server
- [x] 3.G Themes: copy MeetHub's 20 carried-over theme files, `matrix.json`, `catalog.ts`, `apply.ts` and the
  lockstep, contract and contrast tests; add `themes/streamgo.css` from MeetHub's own pair renamed, the studio
  roles with their defaults and the studio recipes in the matrix; `GET /api/v1/themes` (`handlers/themes.go`), the
  picker with previews,
  and the `NOTICE` entry and file headers; verify every `ui-themes` scenario except "Follows the user" and
  "Unknown theme" (3.A) and "Studio in every theme" (4.2)
- [x] 3.H Deployment: the runtime Dockerfile (non-root, ffmpeg with libx264), `deployments/compose` with pinned
  LiveKit (automatic room creation off, advertised address) and MediaMTX (RTSP and WebRTC only, internal
  credentials, advertised address, WebRTC on UDP and TCP) and the database chosen by
  `DB=sqlite|mariadb|postgres`, a create-only `make init`, and `docs/deployment.md`; verify the
  `build-and-deploy` "Image starts", "Up and ready", "Media servers configured", "Media server not exposed",
  "Init keeps existing secrets", "Media server down" and "Guide covers the list" scenarios (LiveKit's signalling
  behind the same TLS reverse proxy as the app)
- [x] 3.I Integration: wire every package's adapters and routes in `cmd/server.go`, run the whole suite on the
  three databases against the Compose stack, and fix what does not meet between packages; verify
  `make check test test-db build` and a route test listing every route of the REST contract with its owner check

- [x] 3.T Standalone HTTPS: copy MeetHub's `internal/tlsutil`, add `server.cert_file`, `key_file` and
  `self_signed`, serve HTTPS, proxy LiveKit's signalling at `/livekit/` with WebSocket upgrades, and derive the
  browsers' LiveKit URL from the public URL when `livekit.url` is empty; verify every `standalone-https` scenario,
  the studio one with two real browsers against a server on a self-signed certificate

## 4. Studio (after 3.C, 3.D and 3.E; design: `design/variant-d-square`)

- [x] 4.1 Build the studio's region arrangement of `design/variant-d-square` as layout components
  (`StudioLayout.vue` and the empty region components) using only role tokens and `rem` sizes; verify every
  `studio-ui` "Screen sizes" scenario with Playwright at 1280x720, 1440x900, 1920x1080, 2560x1440, 3840x2160 and
  3440x1440, the `ui-themes` "Corners keep their proportion at 4K" scenario and the `ui-themes` contract test
- [x] 4.2 Build the owner's studio: LiveKit connection (adaptive stream off, the "opened elsewhere" state), the
  stage canvas bound to the stage state, the layout picker, the backstage with add and remove, the Banners, Brand
  and Destinations tabs, the device controls and the invite link; verify the `studio-ui` "Studio screen for the
  owner", "The stage is the program" and "Program audio" scenarios and the `studio` "One hosting tab" scenario
  with Playwright using fake media devices and an axe accessibility check, and the `ui-themes` "Studio in every
  theme" scenario over the 22 themes
- [x] 4.3 Build the live controls: go live, publishing the program through the WHIP client, state and
  per-destination status polling, switching destinations while live, automatic re-publishing when
  `interrupted`, the "Resume broadcast" action, the hidden-tab warning, the browser check and the end confirmation;
  verify the `studio-ui` "Live controls", "Recovery after a reload" and "Publishing while hidden" scenarios end to
  end
- [x] 4.4 Build the guest join page and guest view; verify every `studio-ui` "Guest join page and guest view"
  scenario and the `studio` "Reload keeps the identity" scenario with two browser contexts

## 5. Packages, images and release (after group 4; package P, files in design.md)

- [x] 5.P1 Build the packages as MeetHub does (`packaging/build.sh`, the units, the maintainer scripts, the
  `packages` stage, `make package`), with the pinned LiveKit and MediaMTX binaries verified by checksum, their
  configuration with loopback listeners, the first-install secrets, and `THIRD_PARTY`; verify the
  `linux-packages` "Package build", "Install layout", "First-install secrets" and "Data survives upgrades and
  removal" scenarios
- [x] 5.P2 Write `packaging/smoke.sh` and `make test-package` (Debian 13 and Ubuntu 26.04 with a real start;
  AlmaLinux 9 and 10 layout only) and `make test-package-systemd` (Debian 13 and Ubuntu 26.04 containers running
  systemd); verify the `linux-packages` "systemd services", "Package smoke test" and "Start under a real
  systemd" scenarios
- [x] 5.P3 Build the all-in-one image (`docker/allinone/`, the `allinone` stage, `make image-allinone`,
  `make test-allinone`); verify the `build-and-deploy` "All-in-one image" scenarios
- [ ] 5.P4 Add the three workflows (`ci.yml`, `packages.yml`, `docker-image-publish.yml`) with actions pinned by
  commit, and `CHANGELOG.md`; verify the `build-and-deploy` "Continuous integration" scenario on a pull request,
  and "Published images" for `main` after the push
- [x] 5.W1 Write `docs/README.md`, `architecture.md`, `install-docker.md`, `standalone-https.md`,
  `reverse-proxy.md` and `security.md`, and generate the diagrams with Codex as SVG with a PNG each under
  `docs/images/`; add the documentation test; verify the `documentation` "Index is complete", "No dead links",
  "Every diagram has both formats" and "Diagrams match the design" scenarios and the Docker half of "Commands in
  the guides run"
- [x] 5.W2 Write `docs/install-linux.md` and `docs/install-binary.md` once the packages exist, with the RPM
  Fusion steps for the RHEL family; verify the `linux-packages` "Installation documents" and "Start on the RHEL
  family with RPM Fusion" scenarios and the packages half of "Commands in the guides run" by following the guide
  on a clean Ubuntu 26.04 container

## 6. Validation and first release

- [ ] 6.1 Add the end-to-end smoke test to the Compose stack (a synthetic program by WHIP to the test RTMP sink);
  verify the `build-and-deploy` "End-to-end smoke" scenario in `make test-e2e`
- [ ] 6.2 Run a 30-minute live broadcast by hand, with the server behind NAT and the owner on another network,
  one guest, two destinations (one real platform), a layout change, a banner, a destination switched off and on,
  an owner reload, a server restart and the studio window minimized for five minutes on a real desktop; verify the `build-and-deploy` "Reachable from outside" scenario and
  record the result in `docs/spike-media-path.md`
- [ ] 6.3 Back up the database, the assets and the secret key of a stack with data and restore them into a new
  one; verify the `build-and-deploy` "Restore from a backup" scenario
- [ ] 6.4 `make check test test-db build`, `make test-package test-package-systemd test-allinone`, `openspec validate add-live-studio --strict`, and a Codex review of
  the whole change (`codex exec --sandbox read-only`); fix what they find
- [ ] 6.5 Tag `v0.1.0` when the owner approves; verify the `build-and-deploy` "Release assets" and "Published
  images" tag scenarios on the published release
