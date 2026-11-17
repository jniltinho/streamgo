# Install the binary

This guide installs StreamGo by hand from the `.tar.gz` archive, on any linux/amd64 distribution with systemd. The
archive holds the same files as the packages: StreamGo, the pinned LiveKit and MediaMTX, their settings and the
three systemd units. You add the user, the secrets and an ffmpeg from your distribution. On Ubuntu, Debian or the
RHEL family the packages do all of this for you: see [Install from the Linux packages](install-linux.md).

All commands run as root (`sudo -i`).

## 1. ffmpeg from the distribution

StreamGo never ships or builds ffmpeg. It needs one with the libx264 and aac encoders, the h264, vp8 and opus
decoders, RTSP, RTMP and RTMPS. On Debian and Ubuntu:

```bash
apt install ffmpeg
```

On the RHEL family, the distribution's own repositories have no ffmpeg with libx264: use RPM Fusion's, as
[Install from the Linux packages](install-linux.md#the-rpm-almalinux-rocky-linux-and-the-rhel-family) shows.
StreamGo checks ffmpeg when it starts and refuses to start, naming what is missing, when it lacks something.

## 2. Download, verify and extract

```bash
curl -LO https://github.com/jniltinho/streamgo/releases/download/v0.1.0/streamgo_0.1.0_linux_amd64.tar.gz
```

```bash
curl -LO https://github.com/jniltinho/streamgo/releases/download/v0.1.0/SHA256SUMS
```

```bash
sha256sum --check --ignore-missing SHA256SUMS
```

```bash
tar -xzf streamgo_0.1.0_linux_amd64.tar.gz -C /opt
```

This creates `/opt/streamgo`:

| Path | What |
|---|---|
| `bin/streamgo`, `bin/livekit-server`, `bin/mediamtx` | StreamGo, LiveKit 1.13.10 and MediaMTX 1.21.4 |
| `bin/init-env` | Adds the missing secrets to an environment file |
| `config.toml`, `livekit.yaml`, `mediamtx.yml` | The settings, ready for this layout: data in `/opt/streamgo/data`, the media servers' internal listeners on 127.0.0.1, LiveKit's signalling reached through StreamGo at `/livekit/` |
| `streamgo.env` | The secrets file, still without secrets (mode 0640); `streamgo.env.example` is the same template |
| `streamgo.service`, `streamgo-livekit.service`, `streamgo-mediamtx.service` | The systemd units |
| `LICENSE`, `NOTICE`, `THIRD_PARTY` | Licences |

The units and `config.toml` expect exactly `/opt/streamgo`. To use another directory, change the paths in all of
them.

## 3. The user, the data directory and the secrets

```bash
groupadd --system streamgo
```

```bash
useradd --system --gid streamgo --home-dir /opt/streamgo --no-create-home --shell /usr/sbin/nologin streamgo
```

```bash
install -d -o streamgo -g streamgo -m 0750 /opt/streamgo/data
```

```bash
/opt/streamgo/bin/init-env /opt/streamgo/streamgo.env
```

```bash
chown root:streamgo /opt/streamgo/streamgo.env && chmod 0640 /opt/streamgo/streamgo.env
```

`init-env` appends the secret key, LiveKit's API key and secret, and MediaMTX's user and password. It never
replaces a value already in the file: a new secret key would make every stored destination and invite link
unreadable. Keep `streamgo.env` with your backups.

## 4. The services

```bash
cp /opt/streamgo/streamgo.service /opt/streamgo/streamgo-livekit.service /opt/streamgo/streamgo-mediamtx.service /etc/systemd/system/
```

```bash
systemctl daemon-reload
```

```bash
systemctl enable --now streamgo-livekit streamgo-mediamtx streamgo
```

```bash
curl http://localhost:8080/readyz
```

`/readyz` answers `{"checks":{"database":"ok","livekit":"ok","mediamtx":"ok"},"status":"ready"}` once the three
are up. The logs are in the journal (`journalctl -u streamgo`).

## 5. From here on

Everything else is as for the packages, in [Install from the Linux packages](install-linux.md): the
[first user](install-linux.md#4-create-the-first-user), the [ports](install-linux.md#5-open-the-ports),
[HTTPS](install-linux.md#6-https), the [configuration](install-linux.md#7-configuration) and
[backups](install-linux.md#9-backup).

To upgrade: stop the three services; copy `config.toml`, `livekit.yaml`, `mediamtx.yml` and `streamgo.env`
somewhere safe, because the archive carries its own versions of those four and extracting it overwrites them;
extract the new archive with the same `tar` command; put your four files back; copy the units again; run
`systemctl daemon-reload` and start the services. `data/` is not in the archive and is left alone. StreamGo
applies new database migrations when it starts.

To build the binary yourself from a checkout instead, `make build` writes `dist/streamgo`; the
[documentation index](README.md#build-and-run-from-source) shows how to run it. It still needs LiveKit, MediaMTX and ffmpeg.
