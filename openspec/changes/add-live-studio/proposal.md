# Live studio: invite guests, arrange the stage, broadcast to RTMP

## Why

Going live with guests today means either a hosted service (StreamYard, Restream) that holds the recordings, the
branding and the bill, or OBS plus a meeting tool glued together by hand. There is no small self-hosted studio that
a team can run next to its other tools. StreamGo is that studio: one binary, a browser for the host and the guests,
and the destinations the team already uses.

## What Changes

First version of the application; everything is new.

- **Accounts**: users are created from the CLI and sign in with e-mail and password. There is no public sign-up.
- **Broadcasts**: a signed-in user creates broadcasts, each with a title, an invite link for guests and a set of
  destinations. A broadcast can go live many times.
- **Studio**: the owner opens the studio and joins a LiveKit room; guests open the invite link, type a name and
  join backstage without an account. The owner puts cameras and shared screens on stage (up to 6), picks one of
  six layouts, shows a logo and one lower-third banner at a time, removes a guest, and rotates the invite link.
  The stage state lives in the database; a reconciler keeps each room in line with it and keeps removed guests
  out. Backstage is kept out of the program, not private between participants of the same studio.
- **Program**: the owner's browser draws the stage on a canvas, mixes the on-stage audio and publishes the result
  by WHIP, through StreamGo, to MediaMTX. What the owner sees in the stage is what goes out.
- **Live output**: StreamGo supervises ffmpeg: one process per broadcast transcodes the program to H.264 + AAC
  with a fixed keyframe interval, and one cheap copy process per destination relays it by RTMP or RTMPS. A
  destination is live only while media flows to it, has its own status, can be switched on or off while live,
  and is retried on failure. A program that disappears (the owner's tab or network) interrupts the broadcast,
  which resumes when it comes back before a stored deadline, also across a server restart.
- **Destinations**: named RTMP targets kept per user. Everything after the host is secret: encrypted at rest and
  never returned by the API. Addresses in private networks are refused unless the operator allows them.
- **Screen sizes**: the interface is designed at Full HD and scales as a whole on QHD and 4K monitors; the stage
  always shows the program as the largest 16:9 picture that fits.
- **Web app**: sign-in, broadcasts, destinations, the studio (owner and guest views) and the guest join page, in
  English with a pt-BR catalog, with themed toasts and dialogs.
- **Themes**: 22 themes over one role-token contract: StreamGo's own dark and light pair plus the 20 themes of
  MeetHub's catalog, carried over with the theme system itself (a registry in the server, one CSS file per theme
  family, the contrast matrix, lockstep, contract and contrast tests, three shared corner radii, more square than round).
- **CLI and layout**: the Cobra command tree and repository layout of MeetHub, with the domain in `internal/core/`
  kept free of framework, database, media-server and process imports.
- **Database**: SQLite by default, MariaDB or PostgreSQL by configuration, with the same behaviour and the same
  test suite on the three.
- **Deployment**: a Docker-only build and a Compose stack of StreamGo (with ffmpeg), LiveKit and MediaMTX, run as
  a single StreamGo instance without Redis or a job queue.

Not in this change: recording; studio chat and comments from the platforms; signing in to YouTube or Twitch to
fetch keys or set titles; RTMP or OBS input; scheduled broadcasts; playing video files; tickers, countdowns and
backgrounds; muting a guest from the owner's side; several owners, teams or roles; hosting from a phone; a
server-side composition fallback; more than one StreamGo instance; background jobs; single sign-on; Linux
packages.

## Capabilities

### New Capabilities

- `server-cli`: the command tree (`server`, `migrate`, `user`, `healthcheck`, `version`) and its conventions.
- `configuration`: `config.toml`, environment overrides, secrets, validation at startup.
- `database`: the three supported databases, their equal behaviour and the migrations.
- `authentication`: accounts, password sign-in, sessions, request protections.
- `broadcasts`: creating, listing, renaming and deleting broadcasts; ownership; the invite link.
- `studio`: joining as owner or guest, guest sessions, removing a guest, the stage state and how it reaches
  every participant.
- `destinations`: RTMP destinations, stream-key secrecy, choosing destinations for a broadcast.
- `live-output`: going live, the program ingest, transcoding and relaying, per-destination status, interruption
  and resumption, ending, limits.
- `studio-ui`: the studio screen, the compositor, the guest join page and guest view.
- `web-shell`: the single-page app shell: routes, sign-in, broadcasts and destinations pages, language, feedback.
- `ui-themes`: the theme catalog, contract, contrast, picker and persistence.
- `build-and-deploy`: the build, the runtime image, the Compose stack and the layering rule.

### Modified Capabilities

None.

## Impact

- New repository content: `main.go`, `cmd/`, `internal/`, `frontend/`, `web/`, `deployments/`, `docker/`,
  `Makefile`, `config.toml.example`, `AGENTS.md`, `README.md`, `NOTICE`.
- The theme system and its 20 themes are copied from MeetHub (MIT, same author; itself adapted from ad-manager,
  MIT), credited in `NOTICE`.
- Frontend toolchain: Vite+ (`vp dev`, `vp build`, `vp check`, `vp test`), as in MeetHub.
- Runtime dependencies: LiveKit server (single node, no Redis), MediaMTX, ffmpeg with libx264 and AAC.
- Network: HTTPS for the app (required outside localhost), LiveKit's WebRTC ports, and MediaMTX's WebRTC port on
  UDP and TCP. MediaMTX's HTTP and RTSP listeners stay on the internal network. No TURN in this version.
- Cost per live broadcast: about one CPU core for the transcode at 720p30 (to be measured in the spike), against
  two to four for a headless-Chrome compositor.
- The broadcast depends on the owner's browser: a Chromium-based desktop browser is required to host, and the
  owner's uplink and machine set the quality ceiling.
