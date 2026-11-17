# Security

This page describes, in plain terms, how StreamGo protects accounts, guests, stream keys and the server it runs
on, and where its protection stops. The limits are stated as limits; nothing here is a promise beyond what the
server enforces.

## Accounts and sessions

- Accounts are created by an administrator on the command line (`streamgo user add`); there is no sign-up page.
- Passwords are hashed with argon2id. Changing a password (`streamgo user passwd`) ends that user's sessions.
- A session is a row in the database, keyed by the hash of a random value kept in a cookie (`HttpOnly`,
  `SameSite=Lax`, and `Secure` when the public URL is HTTPS). A session expires after 14 days without use, and
  signing out deletes it.
- Sign-in is rate-limited per client address (10 attempts a minute), with an in-memory table that has a size
  limit and forgets idle addresses. Behind a reverse proxy, the client address comes from `X-Forwarded-For` only
  when the proxy is listed in `server.trusted_proxies`.
- Every broadcast, destination and stage route checks ownership in the server, and answers "not found" for what
  the caller does not own, so one user cannot learn that another's broadcast exists.

## Requests

- **Origin check.** Every state-changing request (anything but `GET`, `HEAD` and `OPTIONS`) must carry an
  `Origin` header equal to the public URL's origin. A missing or `null` origin is refused. This is what stops
  another site from acting with a signed-in user's cookie.
- **Body limits** apply before a body is read: 64 kB for JSON and the program's SDP offers, 2 MB for the logo.
- API responses carry `Cache-Control: no-store`.
- `server.public_url` must be HTTPS, except on `localhost` and loopback addresses for development: browsers refuse
  the camera and the microphone elsewhere.

## Guests and invite links

- Each broadcast has one invite link, `<public URL>/join/<token>`, with 32 random bytes. It is stored sealed (see
  [Secrets at rest](#secrets-at-rest)) so the owner can copy it again, and the owner can **rotate** it at any time,
  which makes the old link useless.
- A guest who joins gives a name and gets a cookie for that broadcast only (`HttpOnly`, `SameSite=Lax`, `Secure`
  on HTTPS, valid 12 hours, limited to the join API's path). Its random value is stored hashed. The same cookie
  brings the guest back with the same identity after a reload, so they keep their place on stage. Cookies of two
  broadcasts do not interfere.
- Guest joins are rate-limited per client address (20 a minute). A broadcast holds at most 200 guests; guest rows
  expire 24 hours after their last join and are deleted.
- LiveKit tokens last 10 minutes and never allow changing the stage or administering the room. The stage is
  changed only by the owner, through the API.

### What removing a guest guarantees

When the owner removes a guest, the server at once marks the guest as removed, disconnects them from the room,
drops their sources from the stage and invalidates their cookie.

What it does **not** do is revoke a LiveKit token that the guest's browser already holds: LiveKit has no way to
revoke one, and it refreshes the tokens of connected clients. So a removed guest with a modified client may manage
to reconnect for a while. Each time they do, the room reconciler removes them again within 15 seconds; while
connected they are only backstage and never in the program.

A removed guest can properly come back only through a valid invite link. **Rotating the link** closes that door.

### Backstage is not private

Backstage tracks are kept out of the program and out of normal playback, but they are not a privacy boundary
between participants of the same studio: a participant with a modified client could subscribe to another
participant's camera or microphone while that person is backstage. Do not invite to a studio anyone who should not
hear the others.

## Secrets at rest

- Secrets are sealed with XChaCha20-Poly1305 under `STREAMGO_SECRET_KEY`: the destinations' addresses and keys,
  and the invite links. The server refuses to start without a valid key.
- Secrets come only from the environment (`STREAMGO_SECRET_KEY`, `STREAMGO_DATABASE_DSN` and the LiveKit and
  MediaMTX credentials); the configuration loader refuses them in `config.toml`. `make init` writes them to
  `.env`, readable only by its owner, and never replaces an existing key.
- **The key and the data are one backup set.** A new key makes every stored destination and invite link
  unreadable; a database restored without its key keeps the accounts and loses the rest. Back up the database,
  the assets directory and the key together ([Backup and restore](deployment.md#backup-and-restore)).

## Destinations

People paste complete addresses with the stream key inside, so StreamGo treats **everything after the host as
secret**:

- the whole address is sealed together with the key;
- the API returns only the scheme, the host and whether a key is set, never the address or the key;
- the redaction of ffmpeg's output covers the whole address, not only the key.

The relay's target is the address alone when there is no key; otherwise the address without a trailing slash, a
slash, and the key. An address with a query string together with a separate key is refused, because the two
cannot be joined unambiguously. Addresses with whitespace or control characters are refused.

While a broadcast is not offline, the address and key of its destinations cannot be edited, the destinations
cannot be deleted, and no other broadcast can use them.

### The private-address rule

Destinations make the server open connections, which an attacker with an account could aim at the server's own
network. So by default an address whose host resolves to a loopback, private, link-local or unspecified address
(IPv4 or IPv6) is refused when it is saved, and again each time a relay starts.
`output.allow_private_destinations = true` turns that off, for operators who run their own RTMP server on their
network.

The check and ffmpeg's own name resolution are two separate lookups, which a hostile DNS server could exploit to
answer differently each time. If you need a hard guarantee, restrict the server's outgoing traffic at the network.

## The program's path

- The owner's browser publishes the program to StreamGo, never to MediaMTX directly. StreamGo checks that the
  caller is the owner and that the broadcast is going live, and forwards the offer with its own internal
  credentials. It never forwards the client's cookies or `Authorization` header, never follows redirects, and the
  client cannot choose where the offer goes.
- MediaMTX accepts only one internal user, with a name and secret from the environment. Its WHIP, RTSP and control
  API listeners are not published outside the internal network; only its WebRTC media port is.
- LiveKit's signalling, when it goes through StreamGo's `/livekit/` path, is forwarded only to the configured
  LiveKit, and LiveKit's own API still requires its key and secret.

## Uploads

Logos are PNG, JPEG or WebP, up to 2 MB and 2000 pixels a side; SVG is refused. The size limit applies before the
body is buffered, and the dimensions are read from the header before the image is decoded. The new file is written
and recorded before the old one is deleted, so a failed upload never loses the previous logo. Files have random
names under `<data_dir>/assets` and are served with a fixed content type and `nosniff`. A logo's address is hard
to guess, but it is not private: the logo is broadcast.

## Process arguments

ffmpeg runs without a shell and with a minimal environment that holds none of StreamGo's secrets. Its output is
kept with the destination's address redacted, and its argument list is never logged.

**Known limit:** a destination's address with its key is an argument of the relay process, so other processes of
the same user, or in the same container, can read it (for example with `ps`). StreamGo and ffmpeg run as one
dedicated user, alone in their container in the Compose stack. Do not run other software as that user.

## Logs

- Each request is logged with its method, route pattern, status, duration and client address. The route pattern
  is `/api/v1/join/:token`, never the raw path or the query string, and no body, cookie or header is logged, so
  invite tokens and session values do not reach the log.
- StreamGo answers every request with `Referrer-Policy: no-referrer`, so browsers do not send the join
  page's address (which carries the invite token) along with the requests that page makes.
- The reverse proxy must follow the same rule for the join routes and the `Referer` header; see
  [Behind a reverse proxy](reverse-proxy.md#the-join-route-is-never-logged).
- With HTTPS from the binary, the log names the certificate and its fingerprint, never the key.

## One instance

Run exactly one StreamGo process per deployment. A lock file in the data directory refuses a second one on the same
data. Two servers sharing a database with different data directories are not detected, and would each supervise
their own ffmpeg for the same broadcasts: that arrangement is not supported.
