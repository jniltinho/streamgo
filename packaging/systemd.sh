#!/bin/sh
# The .deb under a real systemd (make test-package-systemd): for Debian 13 and Ubuntu 26.04, a throwaway privileged
# container whose PID 1 is systemd installs the package with apt; the postinstall script enables and starts the three
# units, which must become active with /readyz answering 200; a user created with the CLI signs in; the three units
# restart and become ready again; streamgo.service stops cleanly. The containers are removed on exit (--rm, then
# docker stop). Nothing is published: every probe runs inside the container. Needs only Docker on the host.
# Portions adapted from MeetHub (MIT): packaging/settings-systemd.sh
set -eu

dir=$(cd "${1:?usage: systemd.sh <packages directory>}" && pwd)
set -- "$dir"/streamgo_*_amd64.deb
[ "$#" -eq 1 ] && [ -f "$1" ] || { echo "systemd: want exactly one .deb in $dir" >&2; exit 1; }

IMAGES="
debian:13-slim@sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2
ubuntu:26.04@sha256:065aa8626841f16057b78b027b464cd59c4fac36c204c72dcbf48d66f5fa8a96
"
name=""
cleanup() { [ -z "$name" ] || docker stop -t 5 "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT INT TERM

n=0
for image in $IMAGES; do
  n=$((n + 1))
  name="streamgo-probe-systemd-$$-$n"
  echo "systemd: $image"
  docker run -d --rm --name "$name" --privileged --cgroupns=private --tmpfs /run --tmpfs /tmp \
    -v "$dir:/pkg:ro" "$image" sh -c 'export DEBIAN_FRONTEND=noninteractive;
      apt-get update -qq >/dev/null && apt-get install -y -qq systemd systemd-sysv curl >/dev/null 2>&1 &&
      exec /sbin/init' >/dev/null
  i=0
  until state=$(docker exec "$name" systemctl is-system-running 2>/dev/null) || [ "$state" = degraded ]; do
    i=$((i + 1))
    [ "$i" -le 180 ] || { echo "systemd: systemd did not start in $image" >&2; exit 1; }
    sleep 1
  done
  docker exec -i "$name" sh -s <<'INSIDE'
set -eu
units="streamgo-livekit.service streamgo-mediamtx.service streamgo.service"
fail() {
  echo "systemd: $*" >&2
  # shellcheck disable=SC2086
  journalctl --no-pager -n 40 -u streamgo -u streamgo-livekit -u streamgo-mediamtx >&2 || true
  exit 1
}
ready() { # ready <what>: the three units active and /readyz 200 within 60 seconds
  i=0
  # shellcheck disable=SC2086
  until systemctl is-active -q $units && [ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/readyz)" = 200 ]; do
    i=$((i + 1))
    [ "$i" -le 60 ] || fail "not ready after $1: $(systemctl is-active $units | tr '\n' ' ')"
    sleep 1
  done
}
export DEBIAN_FRONTEND=noninteractive
apt-get install -y -qq /pkg/streamgo_*_amd64.deb >/tmp/install.log 2>&1 ||
  { tail -n 20 /tmp/install.log >&2; fail "apt could not install the package"; }
ready "the install"
# shellcheck disable=SC2086
for u in $units; do systemctl is-enabled -q "$u" || fail "$u is not enabled"; done

pw="Smoke-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')-A1!"
(cd /opt/streamgo && set -a && . ./streamgo.env && set +a && printf '%s\n' "$pw" |
  setpriv --reuid streamgo --regid streamgo --init-groups ./bin/streamgo user add --email smoke@example.com \
    --name Smoke --password-stdin) >/dev/null || fail "could not create a user"
code=$(curl -s -o /tmp/login.json -w '%{http_code}' -H 'Content-Type: application/json' \
  -H 'Origin: http://localhost:8080' --data "{\"email\":\"smoke@example.com\",\"password\":\"$pw\"}" \
  http://127.0.0.1:8080/api/v1/auth/login)
[ "$code" = 200 ] || fail "sign-in answered $code: $(cat /tmp/login.json)"

# shellcheck disable=SC2086
systemctl restart $units || fail "the units did not restart"
ready "a restart"
systemctl stop streamgo.service || fail "streamgo.service did not stop"
[ "$(systemctl show -p Result --value streamgo.service)" = success ] || fail "streamgo.service did not stop cleanly"
echo "systemd: three units active, ready, signed in, restarted, clean stop — ok"
INSIDE
  docker stop -t 5 "$name" >/dev/null
  name=""
done
echo "systemd: the package runs under systemd on every distribution"
