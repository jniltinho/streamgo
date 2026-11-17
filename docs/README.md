# StreamGo documentation

**StreamGo** is a live-streaming studio that runs on your own server. The host opens a studio in the browser,
invites guests with a link, chooses who is on stage and in which layout, adds a logo and banners, and broadcasts
the result to several RTMP destinations at once (YouTube, Twitch, your own RTMP server). It is one Go binary with
the web app inside, a database (SQLite, MariaDB or PostgreSQL), [LiveKit](https://livekit.io) for the studio
room, [MediaMTX](https://github.com/bluenviron/mediamtx) to receive the program, and ffmpeg to encode it and send
it out.

## Where to start

| I want to… | Read |
|---|---|
| **Try it, or run it on a server with Docker** | [Install with Docker](install-docker.md) |
| Install it on Debian or Ubuntu, or on the RHEL family (a package, systemd services) | [Install from the Linux packages](install-linux.md) |
| Install the binary by hand | [Install the binary](install-binary.md) |
| Serve HTTPS from StreamGo itself, self-signed or with my certificate, and know which ports to open | [Standalone HTTPS](standalone-https.md) |
| Put it behind nginx or Caddy | [Behind a reverse proxy](reverse-proxy.md) |
| Configure the Compose stack: ports, addresses behind NAT, backups | [Deployment](deployment.md) |
| Understand how it works inside | [Architecture](architecture.md) |
| Know how it is secured, and what it does not protect | [Security](security.md) |

## Run it your way

| | For | What runs |
|---|---|---|
| **Compose stack** | A first look, or a server with Docker | StreamGo, LiveKit and MediaMTX in containers; SQLite, MariaDB or PostgreSQL |
| **Linux package** | A server without Docker | A `.deb` or `.rpm`: StreamGo, LiveKit and MediaMTX in `/opt/streamgo` as three systemd services, the distribution's ffmpeg |
| **Binary** | Any other distribution | The same binaries, laid out by hand |
| **All-in-one image** | One container on one host | StreamGo, LiveKit, MediaMTX and ffmpeg under one supervisor, state in `/data` |

The packages, the binary archive and the all-in-one image arrive with the packaging step; until then the
Compose stack is the way to run StreamGo.

![The pieces of StreamGo: browsers reach StreamGo over HTTPS and LiveKit over WebRTC; the owner's browser sends the program to MediaMTX; ffmpeg reads it and sends it to the RTMP destinations](images/diagram-overview.svg)

What every install needs, whichever way you run it:

- **HTTPS**, except for a browser on the server itself: browsers give a page the camera and the microphone only
  on a secure origin. Either StreamGo serves HTTPS itself ([Standalone HTTPS](standalone-https.md)) or a reverse
  proxy does ([Behind a reverse proxy](reverse-proxy.md)).
- **The media ports open to browsers**: LiveKit's and MediaMTX's WebRTC ports are not HTTP and never go through a
  proxy. There is no TURN server in this version, so a network that allows only web traffic cannot take part.
  The list is in [Standalone HTTPS: ports](standalone-https.md#ports).
- **A desktop Chromium-based browser for the host** (Chrome, Edge, Brave and the like). Guests can use any
  current browser.

## Where it stands

StreamGo is in its first version. This documentation describes the design the project builds to; where a
behaviour was measured rather than only designed, the [architecture guide](architecture.md) says so and gives the
numbers from the [media spike](spike-media-path.md). Two things are known and not yet checked on real hardware: a
host whose browser window is minimized (it passed only in an emulation), and the media ports reached from an
outside network behind NAT. Both are part of the manual run before the first release.

Not in this version, so you can plan around it: recordings, more than one StreamGo process, a TURN server, and
composing the program on the server (the broadcast depends on the host's browser staying open; see
[the limits](architecture.md#limits)).

## For developers

Everything builds and tests in Docker; the host needs Docker and `make`. The commands are in the
[root README](../README.md), the rules in [AGENTS.md](../AGENTS.md), and the plan with its specifications in
[`openspec/`](../openspec/).
