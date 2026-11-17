#!/bin/sh
# Smoke test of the Linux packages (make test-package; the packages workflow runs it before a release). In a clean
# container of each distribution:
#   Debian 13, Ubuntu 26.04 (.deb):
#     1. the package installs with apt, which brings the distribution's ffmpeg
#     2. the files, their owners and modes, the secrets in streamgo.env, the streamgo user and the three units
#        (systemd-analyze verify) are right
#     3. the three services start as their units start them (ExecStart, user, working directory, umask, environment
#        file) until /readyz answers 200; MediaMTX's HTTP, RTSP and API and LiveKit's signalling listen on the
#        loopback address only; a user created with the CLI signs in; the server stops cleanly
#     4. an upgrade (the same package as a newer version) keeps an edited config.toml and the secret key
#     5. removal and purge keep /opt/streamgo/data and the secret key; installed again, the key is the same
#   AlmaLinux 9 and 10 (.rpm): install with dnf, the files, the user and the units, removal keeps the data
#   Rocky Linux 9 (.rpm) with EPEL's ffmpeg-free the server refuses to start naming libx264; with RPM Fusion's ffmpeg
#     (EPEL, CRB, rpmfusion-free-release) the three services start as on Debian and a user signs in
# systemd does not run in these containers: the units are verified, not started by systemd (make
# test-package-systemd does that), and the scripts' systemctl calls are skipped. Needs only Docker on the host;
# nothing is published, every probe runs inside the container.
# Portions adapted from MeetHub (MIT): packaging/smoke.sh
set -eu

dir=$(cd "${1:?usage: smoke.sh <packages directory>}" && pwd)
# exactly one package of each kind: two versions in the directory would install the wrong one
for kind in 'streamgo_*_amd64.deb' 'streamgo-*.x86_64.rpm'; do
  # shellcheck disable=SC2086 # the pattern must expand
  set -- "$dir"/$kind
  [ "$#" -eq 1 ] && [ -f "$1" ] || { echo "smoke: want exactly one $kind in $dir" >&2; exit 1; }
done

DEB_IMAGES="
debian:13-slim@sha256:a72b4433de1cf7d6bae0c4cee74d0547f58601a643b3375f070c51600d63b6b2
ubuntu:26.04@sha256:065aa8626841f16057b78b027b464cd59c4fac36c204c72dcbf48d66f5fa8a96
"
RPM_IMAGES="
almalinux:9@sha256:245f8aab832fe800aa42bdab5fad75ebbd8e0b4c9f24820a83c638b177857ae2
almalinux:10@sha256:5a4b41a427c9717be8f4ac2a32aa22efbae5f9124fbcb79a6765394f312acfe8
"
RPMFUSION_IMAGE=rockylinux/rockylinux:9@sha256:bb5755f96bec05ff26165e779ec843d7167544be9e50770508f307019a6d05c4

n=0
run() { # run <deb|rpm|rpmfusion> <image>
  n=$((n + 1))
  echo "smoke: $2 ($1)"
  docker run --rm -i --name "streamgo-probe-$$-$n" -v "$dir:/pkg:ro" "$2" sh -s "$1" <<'INSIDE'
set -eu
kind=$1
fail() {
  echo "smoke: $*" >&2
  for f in /tmp/*.service.log; do [ ! -f "$f" ] || { echo "── $f" >&2; tail -n 20 "$f" >&2; }; done
  exit 1
}
is() { [ "$(stat -c '%U:%G %a' "$1")" = "$2" ] || fail "$1 is $(stat -c '%U:%G %a' "$1"), want $2"; }
units="streamgo.service streamgo-livekit.service streamgo-mediamtx.service"

# start <unit>: runs the unit's ExecStart as systemd would (its user, working directory, umask and environment file)
start() {
  cmd=$(sed -n 's/^ExecStart=//p' "/usr/lib/systemd/system/$1" | sed 's/\$\$/$/g')
  (cd /opt/streamgo && umask 0027 && set -a && . ./streamgo.env && set +a &&
    exec setpriv --reuid streamgo --regid streamgo --init-groups sh -c "exec $cmd") >"/tmp/$1.log" 2>&1 &
}
# serve: the three services up, /readyz 200, a user created with the CLI signs in, the server stops cleanly
serve() {
  start streamgo-livekit.service
  lk=$!
  start streamgo-mediamtx.service
  mtx=$!
  start streamgo.service
  server=$!
  i=0
  until code=$(curl -s -o /tmp/ready.json -w '%{http_code}' http://127.0.0.1:8080/readyz) && [ "$code" = 200 ]; do
    i=$((i + 1))
    [ "$i" -le 60 ] || fail "/readyz is not 200 after 60 seconds: $code $(cat /tmp/ready.json 2>/dev/null)"
    kill -0 "$server" 2>/dev/null || fail "the server exited"
    sleep 1
  done
  is /opt/streamgo/data/streamgo.db streamgo:streamgo\ 640
  if command -v ss >/dev/null; then
    for port in 7880 8889 8554 9997; do
      addrs=$(ss -Hltn "sport = :$port" | awk '{print $4}' | sort -u | tr '\n' ' ')
      [ "$addrs" = "127.0.0.1:$port " ] || fail "port $port listens on '$addrs', want the loopback address only"
    done
  fi
  # throwaway credentials of a throwaway container
  pw="Smoke-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')-A1!"
  (cd /opt/streamgo && set -a && . ./streamgo.env && set +a && printf '%s\n' "$pw" |
    setpriv --reuid streamgo --regid streamgo --init-groups ./bin/streamgo user add --email smoke@example.com \
      --name Smoke --password-stdin) >/tmp/user.log 2>&1 || { cat /tmp/user.log >&2; fail "could not create a user"; }
  code=$(curl -s -o /tmp/login.json -w '%{http_code}' -H 'Content-Type: application/json' \
    -H 'Origin: http://localhost:8080' --data "{\"email\":\"smoke@example.com\",\"password\":\"$pw\"}" \
    http://127.0.0.1:8080/api/v1/auth/login)
  [ "$code" = 200 ] || fail "sign-in answered $code: $(cat /tmp/login.json)"
  kill "$server"
  wait "$server" || fail "the server did not stop cleanly"
  kill "$lk" "$mtx"
  wait "$lk" "$mtx" || true
  echo "smoke: ready $(cat /tmp/ready.json), signed in"
}
key() { grep '^STREAMGO_SECRET_KEY=' /opt/streamgo/streamgo.env; }

case "$kind" in
deb)
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq >/dev/null
  # systemd: systemd-analyze; iproute2: ss
  apt-get install -y -qq /pkg/streamgo_*_amd64.deb systemd curl iproute2 >/tmp/install.log 2>&1 ||
    { tail -n 20 /tmp/install.log >&2; fail "apt could not install the package"; }
  command -v ffmpeg >/dev/null || fail "the package did not bring the distribution's ffmpeg"
  ;;
*)
  # util-linux: setpriv; systemd: systemd-analyze (the smallest base images leave them out)
  dnf install -y -q /pkg/streamgo-*.x86_64.rpm util-linux systemd >/tmp/install.log 2>&1 ||
    { tail -n 20 /tmp/install.log >&2; fail "dnf could not install the package"; }
  ;;
esac

for b in streamgo livekit-server mediamtx init-env; do is /opt/streamgo/bin/$b root:root\ 755; done
# no ffmpeg of its own: the program is encoded with the distribution's
[ ! -e /opt/streamgo/bin/ffmpeg ] || fail "the package carries an ffmpeg"
for f in config.toml livekit.yaml mediamtx.yml LICENSE NOTICE THIRD_PARTY; do is /opt/streamgo/$f root:root\ 644; done
is /opt/streamgo/streamgo.env root:streamgo\ 640
is /opt/streamgo/data streamgo:streamgo\ 750
grep -qx 'data_dir = "/opt/streamgo/data"' /opt/streamgo/config.toml || fail "config.toml has another data_dir"
grep -qx 'url = ""' /opt/streamgo/config.toml || fail "config.toml does not leave livekit.url empty"
grep -q 'LiveKit server 1.13.10' /opt/streamgo/THIRD_PARTY && grep -q 'MediaMTX 1.21.4' /opt/streamgo/THIRD_PARTY ||
  fail "THIRD_PARTY does not name LiveKit and MediaMTX"
for v in STREAMGO_SECRET_KEY STREAMGO_LIVEKIT_API_KEY STREAMGO_LIVEKIT_API_SECRET STREAMGO_MEDIAMTX_USER STREAMGO_MEDIAMTX_SECRET; do
  grep -q "^$v=." /opt/streamgo/streamgo.env || fail "streamgo.env has no $v"
done
/opt/streamgo/bin/streamgo version | grep -q '^streamgo ' || fail "streamgo version does not answer"
/opt/streamgo/bin/livekit-server --version | grep -q '1.13.10' || fail "livekit-server is not 1.13.10"
/opt/streamgo/bin/mediamtx --version | grep -q 'v1.21.4' || fail "mediamtx is not 1.21.4"
[ "$(id -gn streamgo)" = streamgo ] || fail "no streamgo user in the streamgo group"
for unit in $units; do
  is /usr/lib/systemd/system/$unit root:root\ 644
  systemd-analyze verify /usr/lib/systemd/system/$unit >/tmp/unit.log 2>&1 ||
    { cat /tmp/unit.log >&2; fail "systemd-analyze refuses $unit"; }
  ! grep -q "$unit" /tmp/unit.log || { cat /tmp/unit.log >&2; fail "systemd-analyze warns about $unit"; }
done
key0=$(key)

case "$kind" in
deb)
  serve
  # an upgrade: the same package as a newer version keeps an edited config.toml and the secret key
  echo '# edited by the smoke test' >>/opt/streamgo/config.toml
  mkdir /tmp/up && dpkg-deb -R /pkg/streamgo_*_amd64.deb /tmp/up &&
    sed -i 's/^Version: .*/Version: 99.0.0/' /tmp/up/DEBIAN/control &&
    dpkg-deb --root-owner-group -b /tmp/up /tmp/streamgo_99.0.0_amd64.deb >/dev/null || fail "could not make a newer package"
  apt-get install -y -qq /tmp/streamgo_99.0.0_amd64.deb >/tmp/install.log 2>&1 ||
    { tail -n 20 /tmp/install.log >&2; fail "apt could not upgrade the package"; }
  [ "$(dpkg-query -W -f '${Version}' streamgo)" = 99.0.0 ] || fail "the upgrade did not happen"
  grep -qx '# edited by the smoke test' /opt/streamgo/config.toml || fail "the upgrade replaced the edited config.toml"
  [ "$(key)" = "$key0" ] || fail "the upgrade changed the secret key"

  apt-get remove -y -qq streamgo >/dev/null 2>&1 || fail "apt could not remove the package"
  [ -f /opt/streamgo/config.toml ] || fail "the removal deleted config.toml (only a purge may)"
  apt-get purge -y -qq streamgo >/dev/null 2>&1 || fail "apt could not purge the package"
  [ ! -e /opt/streamgo/config.toml ] || fail "the purge left config.toml"
  ;;
rpmfusion)
  # EPEL's ffmpeg-free has no libx264: the server refuses to start and names it
  dnf install -y -q epel-release >/tmp/install.log 2>&1 &&
    dnf install -y -q --enablerepo=crb ffmpeg-free >>/tmp/install.log 2>&1 ||
    { tail -n 20 /tmp/install.log >&2; fail "dnf could not install ffmpeg-free from EPEL"; }
  if (cd /opt/streamgo && set -a && . ./streamgo.env && set +a &&
    setpriv --reuid streamgo --regid streamgo --init-groups ./bin/streamgo server) >/tmp/noffmpeg.log 2>&1; then
    fail "the server started with EPEL's ffmpeg-free"
  fi
  grep -q 'libx264' /tmp/noffmpeg.log || { cat /tmp/noffmpeg.log >&2; fail "the refusal does not name libx264"; }
  echo "smoke: refused without libx264: $(grep -o 'output.ffmpeg_path.*' /tmp/noffmpeg.log | head -n 1)"
  # the guide's steps: RPM Fusion's ffmpeg (needs EPEL and CRB) in place of ffmpeg-free
  dnf install -y -q "https://mirrors.rpmfusion.org/free/el/rpmfusion-free-release-$(rpm -E %rhel).noarch.rpm" \
    >/tmp/install.log 2>&1 && dnf swap -y -q --enablerepo=crb --allowerasing ffmpeg-free ffmpeg >>/tmp/install.log 2>&1 ||
    { tail -n 20 /tmp/install.log >&2; fail "dnf could not install RPM Fusion's ffmpeg"; }
  serve
  rpm -e streamgo >/dev/null || fail "rpm could not remove the package"
  ;;
rpm)
  rpm -q --recommends streamgo | grep -qx '(ffmpeg or ffmpeg-free)' || fail "the package does not recommend ffmpeg"
  rpm -e streamgo >/dev/null || fail "rpm could not remove the package"
  ;;
esac

[ ! -e /opt/streamgo/bin/streamgo ] || fail "the removal left the binary"
for unit in $units; do
  [ ! -e /usr/lib/systemd/system/$unit ] || fail "the removal left $unit"
done
[ -d /opt/streamgo/data ] || fail "the removal deleted the data directory"
[ "$kind" = rpm ] || [ -f /opt/streamgo/data/streamgo.db ] || fail "the removal deleted the database"
[ "$(key)" = "$key0" ] || fail "the removal changed or deleted the secret key"
id streamgo >/dev/null 2>&1 || fail "the removal deleted the streamgo user"
if [ "$kind" = deb ]; then
  apt-get install -y -qq /pkg/streamgo_*_amd64.deb >/tmp/install.log 2>&1 ||
    { tail -n 20 /tmp/install.log >&2; fail "apt could not install the package again"; }
  [ "$(key)" = "$key0" ] || fail "installed again, the secret key changed"
fi
echo "smoke: ok ($(if command -v ffmpeg >/dev/null; then ffmpeg -version | head -n 1 | cut -d' ' -f1-3; else echo no ffmpeg; fi))"
INSIDE
}

for image in $DEB_IMAGES; do run deb "$image"; done
for image in $RPM_IMAGES; do run rpm "$image"; done
run rpmfusion "$RPMFUSION_IMAGE"
echo "smoke: the packages install, start and remove cleanly on every distribution"
