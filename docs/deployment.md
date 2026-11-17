# Deploying StreamGo with Docker Compose

`deployments/compose` runs one StreamGo, a single-node LiveKit server (the studio) and MediaMTX (it receives the
program from the owner's browser). There is no Redis and no job queue. The database is SQLite by default, or
MariaDB or PostgreSQL in a container of the stack.

What the stack does **not** provide, and you must:

- HTTPS with a certificate, for the app and LiveKit's signalling: a TLS reverse proxy
  ([Behind a reverse proxy](reverse-proxy.md)) or StreamGo's own HTTPS ([Standalone HTTPS](standalone-https.md));
- a host whose firewall lets browsers reach LiveKit's and MediaMTX's media ports;
- a TURN server (there is none in this version);
- backups.

## Quick start

On a machine with Docker and Make:

```bash
make init                # config.toml and .env (secret key and credentials), only when missing
make up                  # builds the image and starts the stack; DB=mariadb or DB=postgres for another database
docker compose -p streamgo exec streamgo streamgo user add --email you@example.com --name You
```

The app is on `http://localhost:8080`. `make logs` follows the server's log, `make down` stops the stack and keeps
its volumes (`make down V=1` deletes them), and `make dev` starts the stack with the Vite dev server in front of it
on `http://localhost:5173`. `make image` builds the runtime image alone: the binary and an ffmpeg with libx264 and
AAC, running as the non-root user 65532, with a health check that runs `streamgo healthcheck` (it asks `/readyz`).

`GET /healthz` answers while the process runs; `GET /readyz` answers 200 only when the database is reachable and
LiveKit and MediaMTX answer, and 503 naming the failing check otherwise.

## Ports

Only these ports are published. Each one is set by an environment variable (in the shell or in `.env`), and the
defaults are the standard ones:

| Variable | Default | What |
|---|---|---|
| `HTTP_PORT` | 8080/tcp | the app |
| `LIVEKIT_PORT` | 7880/tcp | LiveKit's signalling (WebSocket) |
| `LIVEKIT_TCP_PORT` | 7881/tcp | LiveKit's media over TCP |
| `LIVEKIT_UDP_PORT` | 7882/udp | LiveKit's media over UDP |
| `MEDIAMTX_WEBRTC_PORT` | 8189/udp and 8189/tcp | MediaMTX's WebRTC media |
| `DEV_PORT` | 5173/tcp | the Vite dev server (`make dev` only) |

The media ports are published on the same number inside and outside the containers, because LiveKit and MediaMTX
put them in the addresses they advertise. MediaMTX's WHIP (8889), RTSP (8554) and control API (9997) listeners are
never published: only StreamGo and its ffmpeg reach them, on the Compose network, with the internal credentials
from `.env`.

To try the stack on a machine where the standard ports are taken, choose others and keep the project apart:

```bash
COMPOSE_PROJECT_NAME=streamgo-try HTTP_PORT=28080 LIVEKIT_PORT=27880 LIVEKIT_TCP_PORT=27881 \
  LIVEKIT_UDP_PORT=27882 MEDIAMTX_WEBRTC_PORT=28189 make up
```

The same variables must be set for `make down` and `make logs` of that project.

## HTTPS and secure WebSocket

StreamGo carries LiveKit's signalling under `/livekit/`, so a proxy in front of it needs a single upstream and
LiveKit no name of its own: [Behind a reverse proxy](reverse-proxy.md) shows that arrangement with nginx and Caddy,
and [Standalone HTTPS](standalone-https.md) serves HTTPS with no proxy at all. The arrangement below, with LiveKit
behind a name of its own, still works.

Browsers give camera and microphone only to a secure origin, so anything but `localhost` needs HTTPS. Put a TLS
reverse proxy in front of the app's port and, behind the same proxy, terminate secure WebSocket (`wss://`) for
LiveKit's signalling port. Then tell StreamGo the public addresses, in `.env`:

```bash
STREAMGO_SERVER_PUBLIC_URL=https://studio.example.com
STREAMGO_LIVEKIT_URL=wss://livekit.example.com
STREAMGO_SERVER_TRUSTED_PROXIES=172.16.0.0/12   # the proxy's address as StreamGo sees it
```

With Caddy, for example:

```
studio.example.com {
	reverse_proxy 127.0.0.1:8080
}
livekit.example.com {
	reverse_proxy 127.0.0.1:7880
}
```

When the proxy runs on the same host, publish the app's and LiveKit's signalling ports on the loopback only (for
example `HTTP_PORT=127.0.0.1:8080` and `LIVEKIT_PORT=127.0.0.1:7880`). The media ports are not proxied: browsers
reach them directly.

## Advertised addresses behind NAT

LiveKit and MediaMTX tell each browser the address and port to send media to. Both advertise `NODE_IP`. `make up` sets
it to the host's network address (the source of its default route) unless the environment or `.env` sets it; a
plain `docker compose up` uses `127.0.0.1`, which only serves a browser on the same machine (and Firefox only after
the site was allowed to use the camera). On a server, set it to the address browsers reach, the public IP when the
host is behind NAT:

```bash
NODE_IP=203.0.113.10
```

and forward, on the NAT router or cloud firewall, the media ports to the host on the same numbers:
`LIVEKIT_TCP_PORT`, `LIVEKIT_UDP_PORT` and `MEDIAMTX_WEBRTC_PORT` (UDP and TCP), besides 443 for the proxy.

## No TURN: the owner needs MediaMTX's WebRTC port

There is no TURN server in this version, for LiveKit or MediaMTX. The owner's browser sends the program straight
to MediaMTX's WebRTC port, so the owner's network must allow outgoing UDP to `MEDIAMTX_WEBRTC_PORT`, or at least
TCP to it (MediaMTX listens on both; TCP is the fallback). Guests need the same towards LiveKit's media ports. A
network that allows only HTTPS through a web proxy cannot take part.

## The join route must not be logged

An invite link is `<public URL>/join/<token>`, and the token is the guest's credential. Configure the reverse proxy
not to log request paths of `/join/` and `/api/v1/join/` (or not to log paths at all), and do not send those URLs
to analytics. StreamGo itself never logs them. It also answers with `Referrer-Policy: no-referrer`, so browsers
do not send the join page's address in the `Referer` header; leaving that header out of the log covers clients
that ignore the policy. With Caddy, leave access logging off (its default) or filter the URI and the `Referer`
header out of the log; [Behind a reverse proxy](reverse-proxy.md#the-join-route-is-never-logged) has
examples for nginx and Caddy.

## Output quality

`output.resolution` (default `720p`) is the quality new broadcasts start at, and `output.max_resolution` (default
`1080p`) the highest one an owner may pick in the studio: `720p`, `1080p` or `1440p`, all at 30 fps. Raise the
ceiling to `1440p` only if the server and the owners can afford it: each live 1440p broadcast costs the server
about two cores (one transcoder), and the owner's browser sends up to 24 Mbit/s. Some platforms accept at most
1080p. `output.video_bitrate_kbps` is `0` by default, which sends each resolution at its own bitrate (4000, 6000 and
12000 kbit/s); any other value, from 500 to 40000, is used for every resolution. Lowering the ceiling later is safe:
a broadcast stored above it goes out at the ceiling. The measurements behind these numbers are in the
[architecture guide](architecture.md#output-quality).

## One StreamGo instance per deployment

Run exactly one StreamGo process per deployment, even with MariaDB or PostgreSQL: each process supervises its own
ffmpeg for the broadcasts it serves, and two would fight over them. StreamGo enforces this with a lock file in its
data directory, and the Compose stack has one `streamgo` service; do not scale it.

## Backup and restore

What you must not lose is three things together: the database, the assets directory (uploaded logos) and the
secret key (`STREAMGO_SECRET_KEY` in `.env`). The key seals every destination's stream key and every invite link:
a database restored without its key keeps the users but cannot open those. Keep `.env` with the backups, somewhere
as private as the key deserves.

With SQLite, the database and the assets are both in the `streamgo-data` volume (`/data`). Stop the stack so the
copy is consistent, then copy the volume and `.env`:

```bash
make down
docker run --rm -v streamgo_streamgo-data:/data -v "$PWD":/backup alpine:3.23 \
  tar czf /backup/streamgo-data.tgz -C /data .
cp .env config.toml /somewhere/safe/
```

With MariaDB or PostgreSQL, dump the database (`mariadb-dump` or `pg_dump` inside its container) as well as copying
the `streamgo-data` volume (assets) and `.env`.

To restore into a new stack: put the saved `.env` and `config.toml` in place **before** `make init` or `make up`
(`make init` never replaces an existing `.env` or key), create the volume by starting and stopping the stack once,
then unpack the archive into it and start again:

```bash
make up && make down
docker run --rm -v streamgo_streamgo-data:/data -v "$PWD":/backup alpine:3.23 \
  sh -c 'rm -rf /data/* && tar xzf /backup/streamgo-data.tgz -C /data && chown -R 65532:65532 /data'
make up
```

For MariaDB or PostgreSQL, load the dump into the new database before starting StreamGo. The volume names start
with the Compose project's name (`streamgo` unless `COMPOSE_PROJECT_NAME` says otherwise).
