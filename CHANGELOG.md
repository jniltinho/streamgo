# Changelog

What changes for the people who run and use StreamGo, newest first. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions are the git tags. The release notes of a `v*` tag
are the section of that version.

## [Unreleased]

### Added

- **User management** — the first account of an install is its administrator (on an existing install, the oldest
  enabled account becomes one when the upgraded server applies its migrations). Administrators create accounts,
  change their name, role and password, and disable or enable them on the new **Users** page; the command line gains
  `streamgo user add --admin` and `streamgo user admin <email> [--revoke]`. Every user changes their own password in
  Settings → Profile.

## [0.1.0] — 2026-11-17

**The first version: a self-hosted live-streaming studio. Invite guests, arrange the stage in the browser and go
live to RTMP and RTMPS destinations.**

### Added

- **Accounts** — users are created from the CLI (`streamgo user add`) and sign in with e-mail and password. There is
  no public sign-up. Each user changes their display name, the one on the name tag in the studio, in Settings.
- **Broadcasts** — each with a title, an invite link for guests and a set of destinations; a broadcast can go live
  many times.
- **Studio** — the owner opens the studio and guests join backstage from the invite link with a name, no account.
  The owner puts cameras and shared screens on stage (up to 6), picks one of six layouts, shows a logo and one
  lower-third banner at a time, removes a guest and rotates the invite link.
- **Backstage** — the host sees the live picture of every camera and shared screen before it goes on stage (a
  guest sees only their own), and a microphone mark changes while that person is heard. A studio holds 10 people by default
  (`studio.max_participants`, up to 100); a guest who arrives at a full studio is told so.
- **Wide windows** — in a window of 19:10 or wider the backstage and the side panel share the right-hand column,
  and the program fills the stage with no empty band beside it.
- **Program** — the owner's browser draws the stage, mixes the on-stage audio and publishes the result by WHIP,
  through StreamGo, to MediaMTX: what the owner sees on stage is what goes out.
- **Live output** — one ffmpeg per broadcast transcodes the program to H.264 and AAC, and one copy per destination
  relays it by RTMP or RTMPS. Each destination has its own status, can be switched on or off while live and is
  retried on failure; a program that disappears interrupts the broadcast, which resumes when it comes back in time,
  also across a server restart.
- **Destinations** — named RTMP targets per user; everything after the host is encrypted at rest and never returned
  by the API. Addresses in private networks are refused unless the operator allows them. YouTube, Twitch, Facebook
  and Instagram are one click away: the form fills their address, you paste the stream key.
- **Web app** — sign-in, broadcasts, destinations, the studio (owner and guest views) and the guest join page, in
  English and Brazilian Portuguese, designed at Full HD and scaled on QHD and 4K monitors.
- **Themes** — 22 themes: StreamGo's own dark and light pair and the 20 themes of MeetHub's catalog.
- **Databases** — SQLite by default, MariaDB or PostgreSQL by configuration, with the same behaviour on the three.
- **HTTPS from the binary** — a certificate you bring or a self-signed one StreamGo generates, with LiveKit's
  signalling proxied at `/livekit/` on the same origin: one HTTPS port is a complete install.
- **Docker** — a Compose stack of StreamGo, LiveKit and MediaMTX (`make up`, which advertises the host's network
  address for media unless `NODE_IP` is set), and an all-in-one image with the three
  and ffmpeg under one supervisor, published as `ghcr.io/jniltinho/streamgo:<version>-allinone`.
- **Linux packages** (linux/amd64) — `streamgo_<version>_amd64.deb`, `streamgo-<version>-1.x86_64.rpm` and
  `streamgo_<version>_linux_amd64.tar.gz`: StreamGo with LiveKit 1.13.10 and MediaMTX 1.21.4 under `/opt/streamgo`
  as three systemd services, the secrets generated on the first install, validated on Debian 13 and Ubuntu 26.04
  (and AlmaLinux 9 and 10, where an ffmpeg with libx264 comes from RPM Fusion).
- **Documentation** — architecture, installing from the packages, from the binary and with Docker, standalone
  HTTPS, reverse proxy, deployment and security.
