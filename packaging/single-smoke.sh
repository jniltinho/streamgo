#!/bin/sh
# Smoke test of the single file (make test-single; the Packages workflow runs it before a release; design B12).
# usage: single-smoke.sh <single file> [<packages directory>]
#
# In a clean debian:13-slim container with the distribution's ffmpeg, run with --init (tini is PID 1 and reaps
# orphans, so a killed process does not linger as a zombie: a zombie would count as NOT gone), as the unprivileged
# user "studio", with this script as the observer that stays alive (every probe is a docker exec beside `up`):
#   1. `streamgo version` answers in under 1 s and writes no file anywhere in the container
#   2. `streamgo up` in an empty directory becomes ready (/readyz 200, database, livekit and mediamtx ok); an account
#      made with `streamgo user add --password-stdin` in the same directory signs in
#   3. "A working studio" (packaging/singlecheck, built with go test -c in the tools image and run in the container):
#      a LiveKit client (server-sdk-go) joins the broadcast's room with a token of the server, through StreamGo's
#      /livekit/ proxy; a WHIP publish (pion, VP8 and Opus) sent through StreamGo is read back with ffprobe at
#      MediaMTX's RTSP address; the broadcast goes live to an RTMP sink (ffmpeg -listen) in the container. No
#      browser and no media from LiveKit: the client joins and publishes nothing there.
#   4. SIGTERM stops `up` with exit status 0; a second start keeps streamgo.env byte for byte (its digest is
#      compared, never printed), the account signs in again and the destination saved before is still listed
#   5. "The parent is killed": after SIGKILL of `up`, the two child processes and their process groups are gone
#      within 5 s
#   6. a single file with its payload altered, and one cut short, exits 1 saying it is damaged and starts no child
#   7. "Smaller than the archive": with a packages directory holding the .tar.gz of the same version, the single file
#      is smaller (the Dockerfile's single stage also checks it against the .tar.gz of the three programs it packs)
#   8. "Upgrade": with SMOKE_PREVIOUS_SINGLE (the previous release's single file), that file makes a data directory
#      with an account and a destination, and this one starts on it: the account signs in, the destination is
#      listed with its key and streamgo.env is unchanged. Without it the step is skipped, saying why. The
#      destination's stream key is checked only as "stored and listed" (the API never returns a key).
# Nothing is published on the host. The container, the image and the work directory are removed afterwards, also
# on failure. Needs Docker and the tools image (make tools).
set -eu

single=$(cd "$(dirname "${1:?usage: single-smoke.sh <single file> [<packages directory>]}")" && pwd)/$(basename "$1")
packages=${2:-}
repo=$(cd "$(dirname "$0")/.." && pwd)
name="streamgo-probe-single-$$"
image="streamgo-probe-single:$$"
work=$(mktemp -d)

cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker rmi -f "$image" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' INT TERM

fail() {
  echo "smoke: $*" >&2
  docker exec "$name" sh -c 'tail -n 40 /home/studio/*/up.log' >&2 2>/dev/null || true
  exit 1
}
in_box() { docker exec -u studio -w /home/studio "$name" "$@"; }

echo "smoke: building the studio check in the tools image"
docker run --rm -u "$(id -u):$(id -g)" -v "$repo:/src" -v streamgo-cache:/cache -e CGO_ENABLED=0 streamgo-tools \
  go test -c -o "/src/dist/single-check" ./packaging/singlecheck >/dev/null || fail "the studio check does not build"
cp "$repo/dist/single-check" "$work/single-check"

printf '%s\n' 'FROM debian:13-slim' \
  'RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends ffmpeg curl procps ca-certificates >/dev/null && rm -rf /var/lib/apt/lists/* && useradd -m -s /bin/sh studio' |
  docker build -q -t "$image" - >/dev/null || fail "the Debian image with ffmpeg does not build"
docker run -d --init --name "$name" "$image" sleep infinity >/dev/null
docker cp "$single" "$name:/usr/local/bin/streamgo"
docker cp "$work/single-check" "$name:/usr/local/bin/single-check"
docker exec "$name" chmod 755 /usr/local/bin/streamgo /usr/local/bin/single-check

# ── 1. version: fast, and nothing written ──
docker exec "$name" touch /tmp/.marker
t=$(in_box sh -c 's=$(date +%s%N); streamgo version >/tmp/version.out; e=$(date +%s%N); echo $(((e - s) / 1000000))') ||
  fail "streamgo version failed"
version=$(docker exec "$name" cat /tmp/version.out)
written=$(docker exec "$name" sh -c 'find / -xdev \( -path /proc -o -path /sys -o -path /dev \) -prune -o -type f -newer /tmp/.marker -print | grep -vx /tmp/version.out' || true)
[ -z "$written" ] || fail "streamgo version wrote: $written"
[ "$t" -lt 1000 ] || fail "streamgo version took $t ms"
echo "smoke: $version, in $t ms (packed), wrote nothing"

# ── 2. up: ready, an account signs in ──
start_up() { # <directory> [<binary>]: up in /home/studio/<directory>, its log and exit status beside it
  in_box mkdir -p "$1"
  docker exec -d -u studio -w "/home/studio/$1" -e STREAMGO_OUTPUT_ALLOW_PRIVATE_DESTINATIONS=true "$name" \
    sh -c "rm -f up.exit; ${2:-streamgo} up >up.log 2>&1; echo \$? >up.exit"
}
wait_ready() {
  i=0
  until ready=$(in_box curl -fsS http://127.0.0.1:8080/readyz 2>/dev/null); do
    i=$((i + 1))
    [ "$i" -le 90 ] || fail "/readyz is not 200 after 90 seconds"
    sleep 1
  done
  for c in database livekit mediamtx; do
    echo "$ready" | grep -q "\"$c\":\"ok\"" || fail "/readyz: $c is not ok: $ready"
  done
}
up_cmd='^[^ ]*streamgo[^ ]* up$' # the command line of `up`, not of the sh that waits for it
up_pid() { docker exec "$name" pgrep -u studio -f "$up_cmd"; }
stop_up() { # <directory>: SIGTERM, then exit status 0
  docker exec "$name" pkill -TERM -u studio -f "$up_cmd" || fail "no up to stop"
  i=0
  until docker exec "$name" test -f "/home/studio/$1/up.exit"; do
    i=$((i + 1))
    [ "$i" -le 60 ] || fail "up did not stop within 60 s of SIGTERM"
    sleep 1
  done
  [ "$(docker exec "$name" cat "/home/studio/$1/up.exit")" = 0 ] || fail "up exited $(docker exec "$name" cat "/home/studio/$1/up.exit") on SIGTERM"
}
password="Smoke-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')-A1!" # throwaway account of a throwaway container
add_user() { # <directory> [<binary>]
  printf '%s\n' "$password" | docker exec -i -u studio -w "/home/studio/$1" "$name" ${2:-streamgo} user add \
    --email smoke@example.com --name Smoke --password-stdin >/dev/null || fail "streamgo user add failed in $1"
}
signin() { # the session cookie lands in /home/studio/jar
  docker exec -u studio -e SMOKE_PASSWORD="$password" "$name" sh -c \
    'printf "{\"email\":\"smoke@example.com\",\"password\":\"%s\"}" "$SMOKE_PASSWORD" | curl -fsS -o /dev/null -c /home/studio/jar \
       -H "Origin: http://localhost:8080" -H "Content-Type: application/json" --data @- http://127.0.0.1:8080/api/v1/auth/login' ||
    fail "the account does not sign in"
}
api() { # <method> <path> [<json>]
  in_box curl -fsS -b /home/studio/jar -X "$1" -H "Origin: http://localhost:8080" -H "Content-Type: application/json" \
    ${3:+--data "$3"} "http://127.0.0.1:8080$2"
}
env_digest() { docker exec "$name" sha256sum "/home/studio/$1/data/streamgo.env" | cut -d' ' -f1; }

start_up studio
wait_ready
in_box test -f studio/data/streamgo.db || fail "no studio/data/streamgo.db"
[ "$(in_box stat -c %a studio/data/streamgo.env)" = 600 ] || fail "studio/data/streamgo.env is not mode 0600"
add_user studio
signin
echo "smoke: up is ready (database, livekit, mediamtx ok) and an account made with user add signs in"

# ── 3. a working studio ──
docker exec -u studio -w /tmp -e SMOKE_URL=http://127.0.0.1:8080 -e SMOKE_ORIGIN=http://localhost:8080 \
  -e SMOKE_EMAIL=smoke@example.com -e SMOKE_PASSWORD="$password" -e SMOKE_DATA_DIR=/home/studio/studio/data \
  -e SMOKE_SINK=rtmp://127.0.0.1:19350/live "$name" single-check -test.run TestStudio -test.v -test.count=1 \
  >"$work/studio.log" 2>&1 || { sed 's/^/  /' "$work/studio.log" >&2; fail "a working studio: the check failed"; }
sed -n 's/^ *studio_test.go:[0-9]*: /  /p; /^studio:/p' "$work/studio.log"
[ "$(api GET /api/v1/destinations | grep -o '"has_key":true' | wc -l)" = 1 ] || fail "the destination of the studio check is not listed"

# ── 4. a second start keeps the key ──
before=$(env_digest studio)
stop_up studio
start_up studio
wait_ready
[ "$(env_digest studio)" = "$before" ] || fail "streamgo.env changed on the second start"
signin
[ "$(api GET /api/v1/destinations | grep -o '"has_key":true' | wc -l)" = 1 ] || fail "the destination is gone after the second start"
echo "smoke: SIGTERM exits 0; a second start keeps streamgo.env, the account and the destination"

# ── 5. the parent is killed ──
pid=$(up_pid) || fail "no up process"
kids=$(docker exec "$name" pgrep -P "$pid" | tr '\n' ' ')
[ "$(echo $kids | wc -w)" = 2 ] || fail "up has children '$kids', want 2"
groups=$(docker exec "$name" ps -o pgid= -p "$(echo $kids | tr ' ' ',')" | tr -s ' \n' ' ')
docker exec "$name" kill -KILL "$pid"
i=0
while left=$(docker exec "$name" ps -e -o pid=,pgid=,stat=,comm= | awk -v g=" $groups " 'index(g, " " $2 " ")') && [ -n "$left" ]; do
  i=$((i + 1))
  [ "$i" -le 10 ] || fail "5 s after SIGKILL of up, these remain: $left"
  sleep 0.5
done
echo "smoke: SIGKILL of up: its two children and their process groups ($groups) are gone within $((i / 2)) s"

# ── 6. a damaged single file ──
docker exec "$name" sh -c 'cp /usr/local/bin/streamgo /tmp/altered && cp /usr/local/bin/streamgo /tmp/short && chmod 755 /tmp/altered /tmp/short
  n=$(tail -c 35 /tmp/altered | head -c 16 | sed "s/^0*//")
  off=$(tail -c $((35 + n)) /tmp/altered | head -n 1 | cut -d" " -f4)
  printf "\377\377\377\377\377\377\377\377" | dd of=/tmp/altered bs=1 seek=$((off + 4096)) conv=notrunc 2>/dev/null
  truncate -s -1000 /tmp/short'
for f in altered short; do
  in_box mkdir "$f"
  status=0
  docker exec -u studio -w "/home/studio/$f" "$name" "/tmp/$f" up >"$work/$f.log" 2>&1 || status=$?
  [ "$status" = 1 ] || fail "the $f file exited $status: $(tail -n 3 "$work/$f.log")"
  grep -q 'damaged' "$work/$f.log" || fail "the $f file did not say it is damaged: $(tail -n 3 "$work/$f.log")"
  [ -z "$(in_box find "$f" -name 'livekit-server-*' -o -name 'mediamtx-*')" ] || fail "the $f file unpacked a media server"
  ! grep -q '"media server"\|msg="media server"' "$work/$f.log" || fail "the $f file chose a media server to run"
  echo "smoke: $f single file: exit 1, $(grep -o 'the file is damaged[^"]*' "$work/$f.log" | head -n 1)"
done

# ── 7. smaller than the archive ──
v=$(echo "$version" | cut -d' ' -f2)
case "$v" in v[0-9]*.[0-9]*.[0-9]*) v="${v#v}" ;; *) v="0.0.0+$v" ;; esac
v=$(printf '%s' "$v" | tr -- '-' '.')
archive="$packages/streamgo_${v}_linux_amd64.tar.gz"
if [ -n "$packages" ] && [ -f "$archive" ]; then
  s=$(stat -c %s "$single") a=$(stat -c %s "$archive")
  [ "$s" -lt "$a" ] || fail "the single file ($s bytes) is not smaller than $(basename "$archive") ($a bytes)"
  echo "smoke: the single file ($s bytes) is smaller than $(basename "$archive") ($a bytes)"
else
  echo "smoke: size against the .tar.gz skipped: no streamgo_${v}_linux_amd64.tar.gz in '${packages:-no packages directory given}' (make package); the Dockerfile's single stage checked it against the three programs"
fi

# ── 8. upgrade ──
if [ -z "${SMOKE_PREVIOUS_SINGLE:-}" ]; then
  echo "smoke: upgrade skipped: no previous single file (SMOKE_PREVIOUS_SINGLE); v0.3.0, the last release, published none"
else
  docker cp "$SMOKE_PREVIOUS_SINGLE" "$name:/usr/local/bin/streamgo-previous"
  docker exec "$name" chmod 755 /usr/local/bin/streamgo-previous
  start_up upgrade streamgo-previous
  wait_ready
  add_user upgrade streamgo-previous
  signin
  api POST /api/v1/destinations '{"name":"Upgrade","address":"rtmp://127.0.0.1:19350/live","key":"upgrade-key"}' >/dev/null ||
    fail "the previous release did not save a destination"
  before=$(env_digest upgrade)
  stop_up upgrade
  start_up upgrade
  wait_ready
  signin
  [ "$(env_digest upgrade)" = "$before" ] || fail "upgrade: streamgo.env changed"
  api GET /api/v1/destinations | grep -q '"has_key":true' || fail "upgrade: the destination is gone"
  stop_up upgrade
  echo "smoke: upgrade from $(docker exec "$name" streamgo-previous version | cut -d' ' -f2): the account signs in, the destination is kept, streamgo.env unchanged"
fi

echo "smoke: the single file passed"
