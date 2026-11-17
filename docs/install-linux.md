# Install from the Linux packages

The `.deb` and `.rpm` packages arrive with the packaging step; this page will then describe installing them. Until
they exist, run StreamGo with Docker: [Install with Docker](install-docker.md).

What the packages will install, as the design fixes it:

- Everything under `/opt/streamgo`: StreamGo and the pinned `livekit-server` and `mediamtx` binaries, with three
  systemd services.
- ffmpeg from the distribution, never built from source: the `.deb` depends on the distribution's (which has
  libx264 on Debian and Ubuntu); the `.rpm` only recommends it, because the RHEL family has none with libx264 in
  its own repositories. On those systems the service refuses to start until an ffmpeg with libx264 is installed,
  from RPM Fusion, and this page will give the steps.
- On the first install, the secret key and the credentials the three services share, in `streamgo.env`, so the
  stack answers on `http://localhost:8080` right after the install.
- LiveKit's and MediaMTX's HTTP, RTSP and API listeners on the loopback address; only the app and the WebRTC media
  ports are reachable from outside. HTTPS comes from StreamGo itself ([Standalone HTTPS](standalone-https.md)) or
  from a reverse proxy ([Behind a reverse proxy](reverse-proxy.md)).

![The Linux packages: StreamGo, LiveKit and MediaMTX as three systemd services in /opt/streamgo, with the distribution's ffmpeg](images/diagram-deploy-packages.svg)

The packages will be validated on Debian 13 and Ubuntu 26.04.
