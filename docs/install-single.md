# Install the single file

This guide installs StreamGo from one downloaded file, on a linux/amd64 host with systemd, without Docker and
without a package. The file `streamgo_0.4.0_linux_amd64` is the StreamGo executable with the official LiveKit and
MediaMTX executables of the release appended to it. Its `streamgo up` command runs the whole studio:

- on the first start it creates the secret key and the credentials the three programs share in
  `<data_dir>/streamgo.env` (mode 0600), and never replaces them afterwards;
- it unpacks LiveKit and MediaMTX into `<data_dir>/media/`, checks each against the SHA-256 compiled into StreamGo,
  and runs only files that match; at the next starts it unpacks again only a file that is missing or changed;
- it starts the two media servers, with a configuration it writes for them at every start, waits until both answer
  (30 s at most), then runs the StreamGo server itself;
- it starts a media server again when it exits, after a delay that doubles from 1 s to 30 s;
- on SIGINT or SIGTERM it stops the StreamGo server first, then the two media servers.

ffmpeg is not in the file: it comes from your distribution, and `up` refuses to start without it. `up` also
refuses to run as root, so this guide runs it as a user of its own.

The single file is published from release 0.4.0 on. The examples use Debian or Ubuntu; the commands run as root
(`sudo -i`) unless they say otherwise.

## 1. ffmpeg from the distribution

StreamGo needs an ffmpeg with the libx264 and aac encoders, the h264, vp8 and opus decoders, RTSP, RTMP and RTMPS.
On Debian and Ubuntu:

```bash
apt install ffmpeg
```

On the RHEL family, the distribution's own repositories have no ffmpeg with libx264: use RPM Fusion's, as
[Install from the Linux packages](install-linux.md#the-rpm-almalinux-rocky-linux-and-the-rhel-family) shows.
`up` checks ffmpeg before it starts anything and, when something is missing, stops with a message that names it.

## 2. Download and verify

```bash
curl -LO https://github.com/jniltinho/streamgo/releases/download/v0.4.0/streamgo_0.4.0_linux_amd64
```

```bash
curl -LO https://github.com/jniltinho/streamgo/releases/download/v0.4.0/SHA256SUMS
```

```bash
sha256sum --check --ignore-missing SHA256SUMS
```

```bash
install -m 0755 streamgo_0.4.0_linux_amd64 /usr/local/bin/streamgo
```

```bash
streamgo version
```

It prints `streamgo v0.4.0 (commit …, built …)`. `version`, `licenses` and `--help` read no configuration and
unpack nothing.
`streamgo licenses` prints the licences and notices of StreamGo, LiveKit, MediaMTX and the bundled fonts, which a
single file cannot ship beside itself.

The file is 68 MB. A host that already runs LiveKit and MediaMTX does not need it: the
`streamgo-server_0.4.0_linux_amd64.tar.gz` archive (14 MB) holds StreamGo alone, the example configuration
and the notices.

## 3. A user and a data directory

```bash
useradd --system --create-home --home-dir /var/lib/streamgo --shell /usr/sbin/nologin streamgo
```

```bash
install -d -o streamgo -g streamgo -m 0750 /var/lib/streamgo/data
```

The data directory `/var/lib/streamgo/data` holds everything StreamGo keeps: the SQLite database, the uploaded
logos, `streamgo.env`, the self-signed certificate when you use one, and the unpacked media servers in `media/`.
It must be on a file system that allows running programs: `up` stops with a message that says `noexec` and names
`--data-dir` when it is mounted that way (as `/tmp` often is).

`up` runs programs from the data directory, so it is strict about who can change it: the directory must belong to
the user that runs `up` and be writable by that user only, no directory above it may be writable by everyone
(a sticky directory such as `/tmp` is accepted), and `streamgo.env` must be a regular file of that user. A wrong
mode on your own `streamgo.env` is set back to 0600; anything else stops the start with a message that names the
path.

## 4. The first start

Run it once by hand to see it come up. From the root shell:

```bash
cd /var/lib/streamgo
```

```bash
sudo -u streamgo /usr/local/bin/streamgo up --data-dir /var/lib/streamgo/data
```

The commands of this guide run in `/var/lib/streamgo` because StreamGo looks for an optional `config.toml` in the
working directory first; it needs none. The first lines of the log say where each media server came from, its
version, and the address both advertise to browsers. Once the three are up, in a second terminal:

```bash
curl http://localhost:8080/readyz
```

answers `{"checks":{"database":"ok","livekit":"ok","mediamtx":"ok"},"status":"ready"}`.

`up` holds the instance lock of the data directory from before its first change until its last child has stopped:
a second `streamgo up` or `streamgo server` on the same directory exits with an error and changes nothing.

## 5. Create the first account

In the second terminal, as root, with the same `--data-dir`; it asks for the password twice:

```bash
cd /var/lib/streamgo && sudo -u streamgo /usr/local/bin/streamgo user add --data-dir /var/lib/streamgo/data --email you@example.com --name "Your Name"
```

Every command that reads the configuration takes `--data-dir` and reads the secrets from
`<data_dir>/streamgo.env`; nothing needs to be loaded into the shell first.

The first account of an install is its administrator. Once signed in, the administrator creates the other
accounts, changes their role or password, and disables or enables them on the **Users** page of the web app. The
same way as above, `streamgo user list`, `user passwd`, `user disable`, `user enable` and
`user admin <email> [--revoke]` manage accounts from the server.

Open `http://<server>:8080` and sign in. From any machine other than the server itself, the browser allows the
camera and the microphone only over HTTPS: set it up first ([section 7](#7-https)). Stop the server with Ctrl+C in
the first terminal before you install the service.

## 6. Open the ports

Browsers must reach these ports on the server:

| Port | Protocol | What | Flag |
|---|---|---|---|
| 8080 | TCP | The app, the API and LiveKit's signalling (`/livekit/`) | `--addr` |
| 7881 | TCP | LiveKit's media over TCP | `--livekit-tcp-port` |
| 7882 | UDP | LiveKit's media over UDP | `--livekit-udp-port` |
| 8189 | UDP and TCP | MediaMTX's media: the owner's program | `--webrtc-port` |

With ufw:

```bash
ufw allow 8080/tcp
```

```bash
ufw allow 7881/tcp
```

```bash
ufw allow 7882/udp
```

```bash
ufw allow 8189
```

The other listeners of the media servers (LiveKit's signalling and API, MediaMTX's WHIP, RTSP and API) are on the
loopback address, on four free ports `up` chooses at every start: never open them. `up` checks the three media
ports before it starts anything, and stops naming the flag of a port already in use. On a host that already runs
a LiveKit or a MediaMTX on those ports, give others and open those instead:

```bash
sudo -u streamgo /usr/local/bin/streamgo up --data-dir /var/lib/streamgo/data --livekit-tcp-port 17881 --livekit-udp-port 17882 --webrtc-port 18189
```

There is no TURN server, so a network that blocks these ports cannot take part; see
[Standalone HTTPS: ports](standalone-https.md#ports) for the outgoing connections to the destinations.

**The advertised address.** Both media servers tell browsers one address. By default it is the IPv4 address of the
interface of the default route, or `127.0.0.1` on a host without one. **Behind NAT** (a cloud server with a
private address, or a router forwarding the ports), give the public address with `--node-ip`, and forward the
four media ports on the same numbers:

```bash
sudo -u streamgo /usr/local/bin/streamgo up --data-dir /var/lib/streamgo/data --node-ip 203.0.113.10
```

## 7. HTTPS

Browsers need HTTPS for the camera and the microphone. Choose one way:

- **StreamGo's own HTTPS, self-signed**, the quickest (use the name or address people type):

  ```bash
  sudo -u streamgo /usr/local/bin/streamgo up --data-dir /var/lib/streamgo/data --self-signed --public-url https://studio.example.com:8080
  ```

  StreamGo now speaks HTTPS on port 8080 with a certificate it generated in
  `/var/lib/streamgo/data/tls/selfsigned.crt` and reuses at the next starts, and LiveKit's signalling works over
  the same port at `/livekit/`. Browsers warn once per device, guests included.
- **A certificate you bring**, in PEM, readable by the `streamgo` user:

  ```bash
  sudo -u streamgo /usr/local/bin/streamgo up --data-dir /var/lib/streamgo/data --public-url https://studio.example.com:8080 --cert-file /etc/streamgo/tls/fullchain.pem --key-file /etc/streamgo/tls/privkey.pem
  ```

- **A reverse proxy** (nginx or Caddy) with a public certificate on port 443: see
  [Behind a reverse proxy](reverse-proxy.md). Then give `--addr 127.0.0.1:8080 --public-url https://studio.example.com`,
  so only the proxy reaches StreamGo, and close 8080 in the firewall.

The fingerprint, what guests see and when the self-signed certificate is renewed are in
[Standalone HTTPS](standalone-https.md). To serve HTTPS on port 443 without a proxy, give `--addr :443` and
`--public-url https://studio.example.com`, and add the capability shown in the next section.

## 8. Run it as a service

Write `/etc/systemd/system/streamgo.service` with the flags you chose above:

```ini
[Unit]
Description=StreamGo: self-hosted live-streaming studio (single file)
Documentation=https://github.com/jniltinho/streamgo/blob/main/docs/install-single.md
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=streamgo
Group=streamgo
WorkingDirectory=/var/lib/streamgo
ExecStart=/usr/local/bin/streamgo up --data-dir /var/lib/streamgo/data
Restart=on-failure
RestartSec=5
# SIGTERM to streamgo up only: it stops the server, then the two media servers, in order
KillMode=mixed
# the server drains its connections and stops every ffmpeg, then the two media servers get 10 s together
TimeoutStopSec=50
LimitNOFILE=65536
UMask=0027

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload
```

```bash
systemctl enable --now streamgo
```

```bash
curl http://localhost:8080/readyz
```

The logs of the three programs are in the journal (`journalctl -u streamgo`); the lines of the media servers carry
a `component` of `livekit` or `mediamtx`.

`KillMode=mixed` sends the stop signal to `streamgo up` alone, which stops the three programs in order; anything
still running when `TimeoutStopSec` ends is killed. If `up` itself is killed, the kernel stops the two media
servers with it.

**A port below 1024** (`--addr :443`) needs the permission to bind it. Add this line under `[Service]`, and only
then:

```ini
AmbientCapabilities=CAP_NET_BIND_SERVICE
```

**Other settings.** The flags cover the address, the public URL, HTTPS and the media ports. Every other setting
goes in `/var/lib/streamgo/config.toml` (each one is documented with its default in
[`config.toml.example`](../config.toml.example)), or as `STREAMGO_<SECTION>_<KEY>=value` in the environment or in
`/var/lib/streamgo/data/streamgo.env`. A flag wins over the environment and `streamgo.env`, which win over
`config.toml`, which wins over the default. `up` manages the media servers' addresses itself: it ignores
`livekit.url`, `livekit.api_url` and the `mediamtx.*_url` settings and logs one line when it replaces a value you
set. A setting of LiveKit or MediaMTX beyond these flags needs the packages or Compose.

## 9. Upgrade

Download and verify the new file as in [section 2](#2-download-and-verify), then:

```bash
install -m 0755 streamgo_0.4.0_linux_amd64 /usr/local/bin/streamgo
```

```bash
systemctl restart streamgo
```

The data directory and `streamgo.env` stay as they are. At its first start the new version unpacks its own media
servers, removes the ones of the earlier version (and nothing else in `media/`), and applies new database
migrations. A broadcast live during the upgrade comes back as `interrupted` and resumes only if the owner's browser
is still publishing.

## 10. Backup

Everything is in the data directory: the database, the logos and `streamgo.env` with the secret key. A database
restored without its key keeps the accounts but cannot open the stream keys and invite links. `media/` is unpacked
again from the executable and need not be kept. Stop StreamGo for a consistent copy:

```bash
systemctl stop streamgo
```

```bash
tar czf streamgo-backup.tgz -C /var/lib/streamgo --exclude=data/media data
```

```bash
systemctl start streamgo
```

Add `config.toml` if you wrote one. Keep the archive as private as the key deserves. To restore, unpack it into
`/var/lib/streamgo` as root, give it back to the user (`chown -R streamgo:streamgo /var/lib/streamgo/data`) and
start the service. With MariaDB or PostgreSQL, dump the database with its own tools as well.

## 11. Who can read the credentials

`streamgo.env` has mode 0600 and belongs to the `streamgo` user. `up` gives LiveKit its API key and secret and
MediaMTX its user and password in their environment, never in a command line, a generated file or a log; neither
receives StreamGo's secret key or database connection string. The environment of a running process
(`/proc/<pid>/environ`) is readable by the same user and by root, so anyone who can run commands as `streamgo` or
as root can read those credentials, as under the packages' systemd units. Run nothing else as the `streamgo` user.

A destination's address and stream key are an argument of its relay process; see
[Security: process arguments](security.md#process-arguments).

## When to use something else

| Use | When |
|---|---|
| [The Linux packages](install-linux.md) | A Debian, Ubuntu or RHEL-family server where apt or dnf should install and upgrade StreamGo, with three systemd services and editable LiveKit and MediaMTX settings |
| [The `.tar.gz` archive](install-binary.md) | The packages' layout on another distribution |
| [Docker Compose](install-docker.md) | A host with Docker, MariaDB or PostgreSQL in containers, or settings of the media servers beyond the flags of `up` |
| [The all-in-one image](install-docker.md#the-all-in-one-image) | One container on a host with Docker |
| `streamgo-server_0.4.0_linux_amd64.tar.gz` | A host that already runs LiveKit and MediaMTX: its `streamgo up` runs the `livekit-server` and `mediamtx` beside the executable, or else in `PATH`, and `streamgo server` uses media servers you run yourself |
