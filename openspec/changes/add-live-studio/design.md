# Design

## Context

Empty repository. The author's MeetHub (Go, Cobra, Echo v5, GORM, Vue 3, Tailwind 4, LiveKit server SDK v2,
ffmpeg supervised for recordings) sets the conventions and is read-only reference. Three media architectures
were weighed with the owner; the chosen one is LiveKit for the studio and MediaMTX + ffmpeg for the output, with
the composition done in the owner's browser.

## Goals / Non-Goals

**Goals**

- A working studio: guests by link, stage with layouts, logo and banner, several RTMP destinations.
- Light to operate: no Redis, no headless Chrome, about one core per live broadcast.
- The smallest amount of our own media code: LiveKit owns the room, MediaMTX owns WebRTC-to-RTSP, ffmpeg owns
  encoding and RTMP.
- A domain core that can be tested without a database, a media server or a process.

**Non-Goals**

Everything under "Not in this change" in the proposal. In particular: no server-side composition, so a broadcast
does not survive the owner's browser going away for longer than the grace period.

## Architecture

```
 guests' browsers ──┐                         ┌──────────────── StreamGo (Go) ─────────────────┐
                    ├─ WebRTC ─ LiveKit ──────┤ tokens, rooms, room metadata (stage state),    │
 owner's browser ───┘   (studio room)         │ room reconciler                                │
       │                                      │ REST API ── core ── SQLite/MariaDB/PostgreSQL  │
       │  canvas + WebAudio = program         │                                                │
       └─ WHIP (HTTP via StreamGo, media direct) ─► MediaMTX  program/<id>                     │
                                              │              │ RTSP                             │
                                              │   ffmpeg transcoder (x264 + AAC, GOP 2 s)      │
                                              │              │ RTSP ► MediaMTX  air/<id>        │
                                              │   ffmpeg relay ×N (-c copy) ► rtmp(s)://dest   │
                                              └────────────────────────────────────────────────┘
```

## Decisions

### D1. LiveKit is the studio; the database owns the stage state and the room converges to it

The room name is `bc_<broadcast id>`. The stage state is one small JSON document. **The database is
authoritative**; the room's metadata is a copy that every client (owner, guests) renders from, reading it when
they join and on `RoomMetadataChanged`. Tokens never grant room administration or metadata updates.

Every writer of the stage state goes through one function in `core/stage`, under the broadcast's lock (D5): the
owner's stage patch, deleting the shown banner, uploading or deleting the logo, removing a guest, and the output
turning `on_air` on or off. It validates, increments `v`, commits to the database, then pushes the metadata. The
two cannot be atomic, so the guarantee is convergence, not atomicity: a failed push is logged and the **room
reconciler** repairs it.

Every route that changes the stage answers with the new stage state, and the owner's studio applies it at once
without waiting for the room: the compositor runs in the owner's browser, so what goes out follows the API's
answer even when the push to the room failed. Guests' previews follow the room and may lag until it is repaired.

The room reconciler runs every 15 s and at startup, over the LiveKit rooms named `bc_*`:

- a room whose broadcast no longer exists is deleted;
- a room whose metadata version differs from the database's gets the stored state pushed;
- a participant who is a removed guest, or whose identity the database does not know, is removed;
- an ingest session still publishing on MediaMTX for a broadcast that is `offline` is closed (D3).

LiveKit runs with automatic room creation off, and StreamGo calls `CreateRoom` (with the stored state,
`EmptyTimeout` 300 s, `MaxParticipants` `studio.max_participants`) before every token is issued. A client whose
room expired asks StreamGo for a new token. Deleting a broadcast deletes its room. Upstream reports races in
which a join recreates a room around a delete, so nothing relies on that setting alone: whatever room exists is
brought in line by the reconciler within 15 s, and the integration test reproduces the case on the pinned
release.

Capacity: the room limit includes the owner. When the room is full and the owner is not in it, issuing the
owner's token first removes the guest who joined last. That frees a slot but reserves nothing, so the studio
retries the owner's connection up to three times, each with a new token; the owner gets in unless guests refill
the room three times in a row.

One hosting tab: the owner's identity is `host-<user id>`, and LiveKit disconnects an older connection with the
same identity. The newer tab wins; the older one shows "studio opened elsewhere" and neither reconnects nor
publishes. The same rule applies to a guest cookie used in two tabs.

- *Alternative: our own WebSocket for stage state.* More code, a second reconnection story. Rejected.
- *Alternative: MediaMTX as the studio too (WHIP in, WHEP out per participant).* One media server fewer, but
  presence, stage sync and reconnection become ours and there is no simulcast. Rejected by the owner.
- *Alternative: the LiveKit webhook to catch a removed guest reconnecting.* Delivery is not guaranteed and it
  fires after admission, so a sweep is needed anyway; with the sweep the webhook adds only a public endpoint.

Stage state (the only shape clients parse; unknown fields are ignored, a lower `v` than the last seen is dropped):

```json
{ "v": 12, "on_air": true, "layout": "spotlight",
  "sources": [ { "p": "host-01J...", "k": "camera" }, { "p": "guest-01J...", "k": "screen" } ],
  "banner": { "id": "01J...", "text": "Q3 roadmap: what ships next" },
  "logo": "/assets/9f3c....png", "accent": "#3d41cf" }
```

`sources` is ordered (the first is the featured one), at most `studio.max_on_stage`. `k` is `camera` (the
participant's camera and microphone) or `screen` (their screen share and its audio). A stage patch carries any of
`layout`, `sources`, `banner_id`, `accent`; the logo changes only through its upload and delete routes, so it is
always a file of this broadcast on StreamGo's origin. The owner is the only writer, so the last write wins.

What is drawn (the layout function in `frontend/src/core/layout.ts`, with `n` the sources whose participant is
present; an absent participant's source stays in the list and is not drawn, since its name is unknown; a present
participant with the camera off is drawn as a placeholder with initials and name):

| Layout | Drawn |
|---|---|
| `solo` | the first source, full frame |
| `grid` | all sources in an even grid: 1, 2x1, 2x2 (3 to 4), 3x2 (5 to 6) |
| `spotlight` | the first source at 70 % width; the others share a column on the right |
| `sidebar` | the first source at 82 % width; the others as 16:9 thumbnails stacked in the right strip |
| `screen` | as `sidebar`, featuring the first `screen` source (else the first source) |
| `pip` | the first source full frame, the second inset bottom-right at 22 % width; the others not drawn |
| any, `n` = 0 | the backdrop with the logo only |

### D2. The owner's browser composes the program

`frontend/src/core/` holds, free of Vue: the layout function (pure), the compositor (draws videos, name tags,
logo and banner on a canvas), the audio mix, and the WHIP client. The owner's stage element is that canvas, so
preview and output cannot differ. Guests run the same compositor at a lower resolution without capturing it.

Video:

- Frames are driven by a timer in a dedicated Web Worker, not by `requestAnimationFrame`, which stops in a
  hidden tab. That alone proves nothing: the transcoder outputs a constant frame rate, so a frozen picture still
  arrives at 30 fps. The spike and the tests therefore measure **fresh pictures**: each test source burns a
  changing frame counter into its video, and the destination's recording must show it advancing.
- The owner's LiveKit connection turns adaptive stream off, so subscriptions do not pause when video elements are
  hidden or off-screen.
- The logo is served from StreamGo's own origin so the canvas is never tainted.
- Hosting requires a Chromium-based desktop browser (checked at studio entry); guests need any current browser.
- Program size and rate come from `output.resolution` (720p or 1080p) at 30 fps. The WebRTC sender asks for
  `maintain-resolution` and a bitrate ceiling above the transcoder's target, and offers only VP8 or H.264 with
  Opus, which is what the transcoder is tested with.

Audio (one `AudioContext`):

- Each on-stage audio track (a camera's microphone, a screen's audio) enters once, through its own gain node,
  even when its participant has two sources on stage.
- **Program bus**: every on-stage track, the owner's own microphone included, through a limiter, into one
  `MediaStreamDestination` that exists for the whole session, so the program always has an audio track, silent
  when the stage is empty.
- **Monitor bus**: every on-stage track except the listener's own, to the speakers. LiveKit audio elements are
  never attached, so nothing is heard twice and nothing bypasses the mix. Guests have only a monitor bus.
- Backstage tracks enter neither bus. This keeps backstage out of the program and out of normal playback; it is
  **not** a privacy boundary between participants of the same studio, who could subscribe to each other's tracks
  with a modified client.

Reload: a reloaded studio reconnects and publishes the video again by itself. Browsers may hold a new page's
`AudioContext` suspended until a click, and a screen share always needs a new user gesture, so the studio shows
one "Resume audio" action when the context is suspended and screen shares must be shared again.

- *Alternative: LiveKit Egress room composite (headless Chrome).* Survives the owner's browser, costs two to
  four cores, Redis and Chrome. Rejected by the owner for weight.
- *Alternative: publish the canvas as a LiveKit track and use track egress.* No Chrome, still Egress and Redis.

### D3. WHIP goes through StreamGo; MediaMTX trusts only StreamGo

MediaMTX runs with `authMethod: internal` and a single user (name and secret from the environment) allowed to
publish and read; RTSP and WebRTC are the only enabled protocols; its HTTP, RTSP and API listeners are not
published outside the internal network (the control API included). Media flows directly between the browser and MediaMTX's WebRTC port
(UDP, with its TCP listener as the fallback); there is no TURN in this version.

StreamGo is the WHIP endpoint the browser sees. A broadcast has one current ingest session, and each offer gets
its own opaque local resource, so the client never chooses a target and a late request for a replaced session
cannot touch the new one:

| Request | Body | StreamGo does |
|---|---|---|
| `POST …/program/whip` | `application/sdp` offer, at most 64 kB | checks owner and state under the broadcast's lock and notes the run number; without the lock, forwards to `<mediamtx.whip_url>/program/<id>/whip` with the internal credentials and a 10 s timeout; under the lock again, if the run is still the same, closes the previous upstream session, stores the new upstream URL with a new random session id, and answers 201 with the SDP answer, `Location: …/program/whip/sessions/<session id>` and the upstream `ETag` and `Link` headers; if the run changed meanwhile, it closes the new upstream session and answers 409 |
| `PATCH …/program/whip/sessions/<session id>` | `application/trickle-ice-sdpfrag` | forwards to the stored upstream URL with `If-Match`, returning the upstream status |
| `DELETE …/program/whip/sessions/<session id>` | none | forwards, clears the stored session |

A session id that is not the current one answers 404. Upstream calls never run while the broadcast's lock is
held, so a slow media server cannot delay End. The client's cookies and `Authorization` are never forwarded,
redirects are not followed, an upstream error is answered as 502, and if storing the session fails StreamGo closes
the upstream session. ICE restart is not supported: the client publishes again. The stored URL survives a
StreamGo restart, so ending a broadcast after a restart still closes its ingest.

A crash between the upstream answer and storing it would leave a session nobody remembers. MediaMTX's control
API is therefore enabled on the internal network (same credentials), and the reconciler (D1) closes any ingest
session publishing on the path of a broadcast that is `offline`; End does the same for its own path.

- *Alternative: browser talks to MediaMTX directly with MediaMTX's HTTP auth callback.* A public listener, a
  token format and a callback to maintain. More surface for the same result.

### D4. ffmpeg, supervised by StreamGo, does the encoding and the RTMP

MediaMTX does not transcode, and RTMP needs H.264 + AAC with regular keyframes while browsers send Opus and
keyframes on demand. So, per live broadcast:

- **Transcoder** (one): reads `rtsp://…/program/<id>` over TCP, scales to the configured size, encodes libx264
  (`veryfast`, `zerolatency`, `yuv420p`, GOP of 2 s, no scene-cut keyframes, constant frame rate, capped bitrate)
  and AAC 48 kHz stereo, and publishes `rtsp://…/air/<id>`. Input is VP8 or H.264 with Opus.
- **Relay** (one per enabled destination): reads `air/<id>` and writes `-c copy -f flv` to the destination.

Starting and stopping a relay does not touch the others, which gives switching a destination while live and a
status per destination for the price of a process that copies.

A running process is not a working one, so the supervisor (`internal/ffmpeg`) reports three things, not one:

- **started**: the process exists;
- **flowing**: ffmpeg's machine-readable progress (`-progress` on a pipe) shows the output's media time
  advancing between reports. The transcoder flowing is what makes a broadcast `live`; a relay flowing is what
  makes a destination `live`. Progress says ffmpeg wrote the data, not that the far end received it: a sink that
  stops reading is noticed only when socket buffers fill, so the 15 s bound in the specs is confirmed or
  corrected by spike task 1.5;
- **stalled or exited**: no progress for 10 s (the process is killed), or the process ended. Inputs and outputs
  carry connection and read/write timeouts so a blocked socket ends instead of hanging.

An ffmpeg whose input path does not exist yet exits at once, so retrying the transcoder every second until it
flows, bounded by `output.start_timeout`, is the wait for the program; the same loop bounded by the interruption
deadline is the resumption.

Processes are started without a shell, with a minimal environment (no StreamGo secrets), in their own process
group with SIGKILL as the parent-death signal, so they die with the server even when it is killed. Go delivers
that signal when the operating-system thread that started the child exits, not the process, so each child is
started from a goroutine locked to its thread for the child's whole life. They are stopped with
SIGINT then SIGKILL after 5 s, all at once under one deadline at shutdown. The supervisor keeps the last lines of
stderr with the destination's secret part redacted. At startup StreamGo checks that the configured ffmpeg has
what the two command lines need: the `libx264` and `aac` encoders, the `h264`, `vp8` and `opus` decoders, the
`rtsp` demuxer, the `rtsp` and `flv` muxers and the `rtmp` and `rtmps` protocols (RTSP is a format in ffmpeg,
not a protocol).

- *Alternative: one ffmpeg with the `tee` muxer.* One process, but changing destinations restarts everything
  and a failed output is silent.
- *Alternative: MediaMTX's native `forward`.* The transcoder is needed either way. Current MediaMTX reports
  per-destination state and errors through its API and keeps unchanged destinations across a reload, which
  would replace the relay processes and keep stream keys out of process arguments. The spike (task 1.6) checks
  it on the pinned version against switching, isolation, status and retry; if it meets them, relays become
  MediaMTX forwards behind the same `Relays` port and this section is updated.
- *Alternative: OvenMediaEngine instead of MediaMTX + ffmpeg.* It ingests WHIP, transcodes and pushes RTMP by
  REST on its own. AGPL-3.0, XML configuration, and WebRTC-in to RTMP-out is not its common path. Kept as the
  fallback if the spike shows the MediaMTX path is not stable.

Known limit: a destination's address with its key is an argument of the relay process, visible to other
processes of the same user or container. StreamGo and ffmpeg run in one container as a dedicated user; the
argument list is never logged.

### D5. Output state machine, runs and locks (in `internal/core/output`, pure)

```
offline ──go live──► starting ──transcoder flowing──► live ──program lost──► interrupted ──flowing──► live
   ▲                    │ start timeout                 │ end                    │ deadline passed / end
   └────────────────────┴───────────────────────────────┴────────────────────────┘   (with a reason unless "end")
```

Destination status: `off`, `connecting`, `live`, `retrying` (with the last error), shown only to the owner. A
relay that stalls or exits while the broadcast is live is restarted with a capped backoff (1, 2, 5, then 10 s).

- **One lock per broadcast** serializes everything that changes it: stage writes (D1), go live, end, the WHIP
  requests, destination changes, and the supervisor's events. Global capacity (`output.max_concurrent`) is
  reserved under one global lock inside go live, so two simultaneous requests cannot both pass.
- **Runs.** Each go live starts a run with a number stored in the broadcast. Every process, timer, ingest session
  and event carries its run number; an event from another run is ignored, and ending a run cancels its pending
  retries. A late "flowing" from the previous run therefore cannot revive an ended broadcast.
- **Deadlines are stored.** Entering `interrupted` stores the deadline (`now + output.reconnect_grace`); a
  restart keeps it instead of granting a new one, so repeated restarts cannot extend an outage.
- **Shutdown is not End.** Stopping the server stops the processes and leaves the stored state alone. At
  startup, a broadcast stored as `starting` or `live` becomes `interrupted` with a new deadline, one stored as
  `interrupted` keeps its deadline, and the transcoder loop resumes those whose program is still published.
- **Destinations in use.** A run uses the destination's address and key as they were when the relay started.
  While a broadcast is not `offline`, the address and key of its chosen destinations cannot be edited, the
  destinations cannot be deleted, and no other broadcast can use them. Claims are taken and released under the
  same global lock as capacity, by go live and by adding a destination to a live broadcast alike.

The core receives events (`GoLive`, `TranscoderFlowing`, `ProgramLost`, `RelayFlowing`, `RelayStopped`,
`DestinationsChanged`, `End`, `Tick`), each with its run number, and returns the new state and commands
(`StartTranscoder`, `StartRelay(dest)`, `StopRelay(dest)`, `StopAll`, `CloseIngest`, `SetOnAir(bool)`), so the
whole machine is table-tested with a fake clock and no process.

### D6. Guests: an invite link, a cookie, and a removal the server keeps enforcing

The invite token is 32 random bytes, stored sealed so the owner can copy the link again, and rotated on demand.
`POST /api/v1/join/:token` with a name creates a guest row (identity `guest-<ulid>`) and sets a cookie named for
the broadcast (`HttpOnly`, `SameSite=Lax`, `Secure` on HTTPS, path `/api/v1/join`, 12 hours) whose random value
is stored hashed; the same cookie later returns the same identity with a fresh LiveKit token, so a reload keeps
the guest on stage. Cookies of two broadcasts do not interfere. Guest rows expire 24 hours after their last join
and are deleted by the reconciler; a broadcast holds at most 200 of them.

LiveKit tokens are issued for 10 minutes, but removing a participant does not revoke a token and LiveKit
refreshes the tokens of connected clients, so a removed guest may be able to reconnect for a while. What the
server guarantees: removal marks the row, calls `RemoveParticipant`, drops the guest's sources from the stage and
invalidates the cookie at once; and the room reconciler (D1) removes them again within 15 s each time they
reconnect, during which they are backstage and not in the program. A removed guest can properly come back only
through a valid invite link; rotating the link closes that.

### D7. Accounts and requests

Passwords are hashed with argon2id. Sessions are server-side rows keyed by the hash of a random cookie value
(`HttpOnly`, `SameSite=Lax`, `Secure` on HTTPS), expiring after 14 days without use. State-changing requests must
carry an `Origin` equal to the public URL; a missing or `null` origin is refused. Sign-in and guest join are
rate-limited per client address with an in-memory token bucket whose table is bounded and evicts idle entries.
Request bodies are limited (64 kB for JSON and SDP, 2 MB for the logo) before they are read. Every broadcast,
destination and stage route checks ownership in the core. API responses carry `Cache-Control: no-store`, and the
request log records the route pattern (`/api/v1/join/:token`), never the raw path or query, so invite tokens do
not reach logs; `docs/deployment.md` says the reverse proxy must do the same.

`server.public_url` must be HTTPS, except on `localhost` and loopback addresses for development: browsers refuse
camera and microphone access elsewhere.

### D8. Secrets, destinations and uploads

Secrets are sealed with XChaCha20-Poly1305 under `STREAMGO_SECRET_KEY`; the server refuses to start without a
valid key. `make init` creates `.env` only when it does not exist and never replaces a key, because a new key
makes every stored destination and invite link unreadable; `docs/deployment.md` says the database, the assets
and the key are backed up and restored together.

A destination is a name, an address and an optional key. People paste complete URLs with the key in the path or
query, so **everything after the host is treated as secret**: the whole address is sealed with the key, the API
returns only the scheme, the host and whether a key is set, and redaction of process output covers the whole
address, not only the key field. The relay's target is the address alone when there is no key; otherwise the
address without a trailing slash, a slash, and the key, which may carry its own query string. An address with a
query string together with a separate key is refused, since the two cannot be joined unambiguously.

Destinations make the server open connections, so by default an address whose host resolves to a loopback,
private, link-local or unspecified address (IPv4 or IPv6) is refused when it is saved and again each time a relay
starts; `output.allow_private_destinations` turns that off for operators who run their own RTMP server. Addresses
with whitespace or control characters are refused. The check and ffmpeg's own name resolution are separate
lookups, which a hostile DNS server could exploit; operators who need a hard guarantee restrict egress at the
network.

Logos are PNG, JPEG or WebP up to 2 MB and 2000 px a side. The size limit applies before the body is buffered,
the dimensions are read from the header before the image is decoded, and the new file is written and recorded
before the old one is deleted, so a failed upload never loses the previous logo. Files have random names under
`<data_dir>/assets`, are served with a fixed content type and `nosniff`; SVG is refused. A logo URL is
unguessable rather than private, since the logo is broadcast.

### D9. Repository layout and the core rule

```
main.go                      cmd.Execute()
cmd/                         root.go server.go migrate.go user.go healthcheck.go version.go   (composition root)
internal/core/               domain; imports only the standard library, x/crypto and each other
  account/                   users, password hashing, sessions
  broadcast/                 broadcasts, invite link, ownership
  stage/                     stage state, validation, mutations
  studio/                    joining, guests, removal, room reconciler; port: Rooms
  destination/               destinations and selection; port: Sealer
  output/                    state machine and orchestration; ports: Transcoder, Relays, Ingest, Clock
internal/store/              GORM repositories for the core's ports; goose migrations per dialect (D11)
internal/livekit/            Rooms adapter (server SDK v2): tokens, rooms, metadata, participants
internal/mediamtx/           WHIP client for the proxy route, RTSP URLs
internal/ffmpeg/             process supervisor (progress, watchdog), arguments, redaction, capability check
internal/httpserver/         Echo v5: router, middleware, handlers/ (thin: decode, call core, encode)
internal/config/ sealed/ ratelimit/ theme/ buildinfo/
web/                         embeds web/dist
deployments/compose/  docker/  Makefile  config.toml.example  AGENTS.md  NOTICE
```

The frontend keeps MeetHub's structure, file for file where the role is the same:

```
frontend/
  index.html  vite.config.ts  tsconfig.json  env.d.ts  package.json  playwright.config.ts  go.mod
  public/                    icons, site.webmanifest, theme-boot.js (theme before first paint)
  src/
    main.ts  App.vue  style.css          style.css = theme base: role contract, shared recipes, theme imports
    api/                     client.ts (fetch, error envelope), types.ts (the REST contract's shapes)
    core/                    plain TypeScript, no Vue (the place MeetHub gives to src/chat):
                             layout.ts, compositor.ts, audioMix.ts, whip.ts, stageState.ts
    components/ui/           DialogHost.vue, ToastStack.vue, UiAlert.vue (copied from MeetHub)
    components/studio/       StageCanvas, LayoutPicker, Backstage, ParticipantCard, SidePanel, BannersTab,
                             BrandTab, DestinationsTab, ControlsDock, LiveHeader, PreJoin
    feedback/                dialogs.ts, native.ts, toasts.ts
    i18n/                    en.ts, pt-BR.ts, index.ts
    layouts/                 AppLayout.vue (signed-in shell), StudioLayout.vue (full-screen studio)
    router/                  index.ts
    stores/                  session.ts, broadcasts.ts, destinations.ts, studio.ts
    themes/                  one file per theme family, catalog.ts, apply.ts, matrix.json
    views/                   LoginView, BroadcastsView, DestinationsView, StudioView, JoinView, GalleryView
  tests/unit/                *.test.ts (vp test, jsdom)
  tests/e2e/                 numbered Playwright specs against a running server
```

`internal/core/...` must not import Echo, GORM, a database driver, the LiveKit SDK, `os/exec` or `net/http`; a
`depguard` rule in `golangci-lint` fails `make check` when it does. `frontend/src/core` must not import `vue`,
`pinia` or `vue-router`; a `no-restricted-imports` lint rule in `vite.config.ts`, run by `vp check`, enforces it.

### D10. Themes: MeetHub's theme system and catalog, carried over

The theme base is copied from MeetHub, not rewritten: the role contract and its defaults (the `@theme static`
blocks and shared recipes of `frontend/src/style.css` for buttons, fields, cards, tabs, toasts, alerts and
dialogs), the theme files under `frontend/src/themes/` (20 themes in 16 files), `matrix.json` with its contrast
recipes, `catalog.ts`, `apply.ts`, the first-paint script, and the lockstep, contract and contrast tests.
`internal/theme/theme.go` is the registry behind `GET /api/v1/themes`. MeetHub's chat-only styles are not
copied. The copy is credited in `NOTICE` and in the file headers.

What StreamGo adds on top:

- Its own pair, `streamgo-dark` (default) and `streamgo-light`, in `themes/streamgo.css`: MeetHub's own pair
  (`themes/meethub.css`) renamed, since the chosen design uses that palette, plus the studio roles.
- Studio roles in the contract (`on-air`, `stage-frame`, `on-stage`, `backstage`), each defaulting to an existing
  role (`on-air` to the danger fill, and so on), so the 20 carried-over files are not edited to pass the contract
  test; a theme may still refine them. Studio recipes are added to `matrix.json`.
- Corner radii: MeetHub's three shared tokens, unchanged (2, 3 and 4 px for small elements, controls and
  containers), which no theme declares. The owner asked for corners more square than round; a softer scale
  (6, 10, 14 px) was drawn as `design/variant-d-soft` and set aside; `design/variant-d-square` is the same screen
  with the settled radii.

Precedence: the user's saved preference, then the theme remembered in the browser, then `streamgo-dark`
(`streamgo-light` when the browser prefers light). Guests have only the browser. The program drawn on the canvas
does not follow the UI theme: it uses the broadcast's own accent colour, so viewers see the same picture whatever
theme the owner uses. Because the studio must read well in 22 themes, including light and retro ones, its
components use only roles and recipes; the mockups in `design/` fix the arrangement and the StreamGo pair, not
literal colours in components.

### D11. Three databases behind GORM

SQLite (pure-Go `modernc.org/sqlite`, WAL, foreign keys, busy timeout) is the default; MariaDB and PostgreSQL are
chosen by `database.driver`, with the connection string only in the environment. `internal/store` is the only
package that knows GORM or a dialect. Migrations are goose files embedded per dialect under
`internal/store/migrations/{sqlite,mysql,postgres}` with identical version numbers; a test fails when the three
directories do not list the same versions. To stay portable: e-mails are stored lower-cased (no reliance on
collations), JSON documents (the stage state) are stored as text and parsed in Go, timestamps are UTC, ids are
ULID strings, and nothing uses `RETURNING`, upsert syntax or dialect functions outside the migrations. Stage
changes are serialized by an in-process lock per broadcast (D12 makes that sufficient) rather than by row locks,
whose behaviour differs between the three. `make test-db` runs the whole Go suite on the three, with MariaDB and
PostgreSQL as throwaway containers.

### D12. No Redis and no job queue; one instance

MeetHub needs Redis for its Asynq queue, for fan-out between instances and for short-lived shared state.
StreamGo has none of the three in this version:

- Nothing is deferred. The long-running work is supervising ffmpeg and a reconciliation loop every 15 s, both
  inside the server and both rebuilt from the database at startup. Supervising ffmpeg is not a job a queue can
  hold:
  the process dies with the server, and what survives a restart is the broadcast's state in the database, from
  which the output is resumed (D5). A queue would add a second place for that state to disagree with.
- Fan-out to browsers is LiveKit room metadata (D1), not our own channel.
- Sessions are database rows, rate limits are in memory, and single-node LiveKit runs without Redis.

The price is that a deployment runs exactly one StreamGo process, enforced by a lock file in `data_dir`. That
holds even with MariaDB or PostgreSQL: a shared database does not make two processes safe, because each would
supervise its own ffmpeg for the same broadcast.

Redis and Asynq (as in MeetHub) become the right tools when any of these arrives: more than one StreamGo
instance (ownership of a live output, shared rate limits), LiveKit in several nodes or with Egress or Ingress,
or deferred work such as processing and uploading recordings, scheduled broadcasts or e-mail. None is in this
change, and the core's ports (D9) are where a queue would plug in.

### D13. Screen sizes: one arrangement, scaled

The studio is designed at Full HD and must also serve QHD and 4K monitors. All sizes in the interface (spacing,
type, icons, radii, the side panel's width) are in `rem`, and the root font size is the only thing that changes
with the window:

```css
html { font-size: clamp(16px, min(100vw / 120, 100vh / 67.5), 32px); }
```

That is 16 px up to 1920x1080, 21.3 px at 2560x1440 and 32 px at 3840x2160, so QHD and 4K windows show the Full
HD arrangement larger. Taking the smaller of the width and height ratios keeps ultrawide windows from growing past
their height. A 4K monitor at 200 % system scaling already reports a 1920x1080 window and is unaffected.

Inside the studio the regions are a CSS grid: header, side panel, controls row and backstage keep their natural
size, and the stage region takes the rest. The program canvas is the largest 16:9 box that fits the stage region
(CSS `aspect-ratio` with both dimensions capped), centred on the stage backdrop. Below 1920x1080 nothing scales
down: the stage region shrinks, and the backstage row scrolls sideways when its cards do not fit; under 1280x720
the studio asks for a larger window to host.

The canvas keeps the program's resolution (`output.resolution`) whatever the window: on a large monitor the
preview is the program scaled up by the browser, slightly soft, and that is deliberate, since the stage shows
exactly what goes out. This decision is about the interface only; program resolutions above 1080p are not in
this change.

### D14. Packages, images and releases, as in MeetHub, for linux/amd64 only

Packaging follows MeetHub's (`packaging/`, the `packages` and `allinone` stages of the `Dockerfile`, the three
workflows), adapted to one difference: MeetHub runs without LiveKit, StreamGo cannot run a studio without
LiveKit and MediaMTX. A package that installed only StreamGo would leave the administrator to find, pin,
configure and wire two media servers by hand, so both artifacts carry them:

- **`.deb`, `.rpm`, `.tar.gz`** (`make package`): everything under `/opt/streamgo`, with the pinned
  `livekit-server` and `mediamtx` binaries (the versions the spike proved, verified by checksum in the build and
  listed with their licences in `THIRD_PARTY`) and three units. The first install generates the secret key and
  the credentials the three share, so the stack is ready on `http://localhost:8080` after `apt install`. The
  media servers' HTTP, RTSP and API listeners bind to the loopback address; only the app and the WebRTC media
  ports are reachable. HTTPS stays the administrator's reverse proxy, in front of the app and of LiveKit's
  signalling, with a worked example in `docs/install-linux.md`.
- **ffmpeg is not shipped** in the packages: the `.deb` depends on the distribution's, which has libx264 on
  Debian and Ubuntu; the `.rpm` only recommends it, because the RHEL family has none with libx264 in its own
  repositories. On AlmaLinux the package installs and the service refuses to start until one is added, which
  the guide explains; the smoke test checks the layout there and a real start on Debian 13 and Ubuntu 26.04.
- **All-in-one image** (`make image-allinone`): StreamGo, LiveKit, MediaMTX and Debian's ffmpeg under
  supervisord, non-root, state in `/data`, published to `ghcr.io/<owner>/streamgo` with the `-allinone` tags.
  The plain runtime image (`make image`) is what the Compose stack uses and is built locally.
- **Workflows**: `ci.yml` (checks, tests, database matrix, browser tests, build), `packages.yml` (smoke test in
  clean containers, install under the runner's systemd, release assets on a `v*` tag) and
  `docker-image-publish.yml` (smoke test, then GHCR), with actions pinned by commit.
- **Validation targets**: Debian 13 and Ubuntu 26.04, in clean containers and under a real systemd
  (`make test-package`, `make test-package-systemd`).

- *Alternative: package only StreamGo and document the media servers.* Smaller package, but the install is not
  usable or testable end to end, and every administrator repeats the same wiring.

## REST contract (all under `/api/v1`, JSON; errors are `{ "error": { "code", "message" } }`)

| Route | Who | Notes |
|---|---|---|
| `POST /auth/login`, `POST /auth/logout`, `GET /me`, `PATCH /me` | anyone / user | `PATCH /me` sets `theme`, `language` |
| `GET /themes` | anyone | ids, labels, schemes, default |
| `GET/POST /broadcasts`, `GET/PATCH/DELETE /broadcasts/:id` | owner | `GET :id` includes output state and destination statuses |
| `POST /broadcasts/:id/invite` | owner | rotates and returns the link; `GET :id` returns the current link |
| `POST /broadcasts/:id/studio-token` | owner | LiveKit URL and token |
| `PATCH /broadcasts/:id/stage` | owner | `layout`, `sources`, `banner_id`, `accent` (D1) |
| `GET/POST /broadcasts/:id/banners`, `DELETE /broadcasts/:id/banners/:bid` | owner | saved banner texts |
| `POST /broadcasts/:id/logo`, `DELETE /broadcasts/:id/logo` | owner | multipart upload; the only way the logo changes |
| `DELETE /broadcasts/:id/participants/:identity` | owner | remove a guest |
| `PUT /broadcasts/:id/destinations` | owner | `{ "ids": [...] }` chosen for this broadcast; allowed while live |
| `POST /broadcasts/:id/live`, `DELETE /broadcasts/:id/live` | owner | go live, end |
| `POST /broadcasts/:id/program/whip`, `PATCH/DELETE /broadcasts/:id/program/whip/sessions/:sid` | owner | D3 |
| `GET/POST /destinations`, `PATCH/DELETE /destinations/:id` | user | address and key are write-only; returns scheme, host, `has_key` |
| `GET /join/:token`, `POST /join/:token` | anyone | broadcast title; join with `{ "name" }` |
| `GET /healthz`, `GET /readyz` | anyone | outside `/api/v1` |

The owner's studio polls `GET /broadcasts/:id` every 3 s while the state is not `offline`; `on_air` reaches
everyone through the stage state.

## Data model

`users` (id, email unique, name, password_hash, theme, language, disabled_at) · `sessions` (id_hash, user_id,
last_used_at) · `broadcasts` (id, owner_id, title, invite_sealed, invite_hash unique, stage (text), stage_version,
logo, output_state, output_reason, output_run, output_deadline, ingest_id, ingest_url, started_at) · `banners` (id,
broadcast_id, text) · `guests` (id, broadcast_id, name, cookie_hash, joined_at, last_join_at, removed_at) ·
`destinations` (id, owner_id, name, scheme, host, address_sealed, has_key) · `broadcast_destinations`
(broadcast_id, destination_id). Ids are ULIDs. Deleting a broadcast deletes its banners, guests, selections,
logo file and LiveKit room, and is refused while it is not `offline`.

## Parallel work packages and file ownership

After the spike (group 1) and the foundation (group 2), these packages touch disjoint files and can be built by
separate agents against the contract above. Each shared piece has one owner:

| Package | Owns |
|---|---|
| A accounts | `internal/core/account`, `internal/store/account*.go`, `handlers/{auth,me}*.go` (including `PATCH /me`, validating the theme with the registry the foundation created), `cmd/user.go` |
| B broadcasts and destinations | `internal/core/broadcast`, `internal/core/destination`, `internal/sealed`, `internal/store/{broadcast,destination}*.go` (the whole broadcast row: C and D call its repository methods, declared by the foundation), `handlers/{broadcasts,destinations}*.go` |
| C studio | `internal/core/stage`, `internal/core/studio` (with the room reconciler), `internal/livekit`, `internal/store/{guest,banner}*.go`, `handlers/{studio,join,banners,logo}*.go` |
| D live output | `internal/core/output`, `internal/ffmpeg`, `internal/mediamtx` (WHIP client, control API client), `handlers/{live,whip}*.go` |
| E frontend core | `frontend/src/core/**` |
| F app pages | `frontend/src/views/{Login,Broadcasts,Destinations}*`, their stores and API modules |
| G themes | `frontend/src/themes/**`, the theme picker, theme tests, `handlers/themes.go`, `NOTICE` |
| H deployment | `docker/`, `deployments/`, `Makefile` targets beyond the foundation's, docs |
| P packaging (group 6) | `packaging/`, `docker/allinone/`, the `packages` and `allinone` stages of the `Dockerfile`, `.github/workflows/`, `THIRD_PARTY`, `CHANGELOG.md`, `docs/install-*.md` |

The foundation declares the tables, the contracts more than one package depends on (the broadcast repository,
the per-broadcast lock, the theme registry) and nothing else: no placeholder handlers, and each package defines
its own ports with its first slice. Each package registers its own routes through a function the foundation
calls (`handlers.RegisterAuth(g, deps)` and so on), so no package edits the router. A last integration task
(3.I) wires the real adapters in `cmd/server.go`, runs everything together and fixes what does not meet. The
studio views (group 4) come after C, D and E and follow the chosen design.

## Risks / Trade-offs

- **Hidden or throttled owner tab freezes the program** → worker-driven frames, adaptive stream off, a visible
  warning, and a spike that measures fresh pictures (a changing frame counter in each source) through real
  LiveKit with the tab hidden and the window minimized for more than five minutes. If it cannot be made
  reliable, fall back to D2's first alternative before building further.
- **Owner's uplink or machine limits quality; WebRTC lowers resolution under congestion** → the transcoder
  normalizes size and rate so destinations always get a valid stream; the studio shows the publish bitrate.
- **The broadcast depends on one browser** → interruption with a stored deadline instead of an abrupt end; stated
  in the product, not hidden. A reload may need one click to resume audio, and a screen share must be shared
  again.
- **WebRTC-in to RTMP-out timing (A/V drift, timestamp jumps on reconnect)** → measured in the spike over 30
  minutes; the transcoder restarts on resumption, which resets timestamps.
- **Transcode cost unknown until measured** → `output.max_concurrent` (default 2) refuses a go-live beyond it.
- **A removed guest can reconnect for a few seconds at a time** while a token they hold is valid → the
  reconciler removes them within 15 s, they are never in the program, and the limit is documented.
- **Backstage is not private between participants** → stated in the product and the docs.
- **No TURN** → the owner needs UDP (or TCP to MediaMTX's WebRTC port) to the server; documented as a supported
  network requirement. The advertised addresses of LiveKit and MediaMTX behind NAT are configured in the
  Compose stack and checked from an outside network in the manual run (task 5.2).
- **Stream key in a process argument** → D4's known limit; removed if the spike adopts MediaMTX forwards.
- **Destination addresses reach into the server's network** → refused by default for private ranges (D8); the
  DNS race is documented.
- **Two servers on one database with different data directories** are not detected by the lock file →
  documented as unsupported.
- **MediaMTX and LiveKit version drift** → versions pinned in the Compose file; the spike records the ones proven.

## Migration Plan

New application: no data to migrate. Once it holds data, the database, the assets directory and the secret key
are one backup set (D8); removing the stack without them loses every destination and invite link.

## Open Questions

- Whether TURN is needed for the owner's WHIP connection on restrictive networks; the spike notes it, and the
  first version documents the UDP port only.

## Chosen design

The owner picked `design/variant-d-square` (FigCraft source and PNG): a header bar; a main column with the stage,
then one row with the layout picker at the left and the controls dock at the right, then the backstage as a row
of compact cards; and a right side panel of about 328 px with the Banners, Brand and Destinations tabs. Its
palette is MeetHub's and becomes `streamgo-dark`: page `#0c1020`, panel `#151b31`, surface `#1d2442`, border
`#2c3458`, text `#f2f4fb`, muted text `#b9c0d8`, primary `#3d41cf` with white text, accent text `#b3b8ff`, success
`#4ade80`, warning `#fbbf24`, danger fill `#9b1c1c`, danger text `#fca5a5`, on-air `#f0464f`, stage backdrop
`#080b17`. `streamgo-light` uses the same primary hue on light neutrals, as MeetHub's light theme does. The other
three mockups in `design/` are kept as references only. `design/variant-d-square-fullhd.png`, `-qhd.png` and
`-4k.png` show the same screen at 1920x1080, 2560x1440 and 3840x2160 under D13.
