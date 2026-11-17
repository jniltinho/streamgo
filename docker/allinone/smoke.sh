#!/bin/sh
# Smoke test of the all-in-one image (make test-allinone; the publish workflow runs it before pushing):
#   1. a root-owned /data is refused with a clear message
#   2. `streamgo version` as the command prints the version and exits without starting the services
#   3. an empty named volume → healthy, /readyz 200, LiveKit and MediaMTX running, nothing runs as root, the secrets
#      file private to uid 65532
#   4. a user created with the CLI signs in
#   5. the container stops cleanly, and a new container on the same volume keeps the user and the secret key
# Needs only Docker on the host; nothing is published (every probe runs inside the container), and the container
# and the volume are deleted afterwards, also on failure.
# Portions adapted from MeetHub (MIT): docker/allinone/smoke.sh
set -eu

image="${1:?usage: smoke.sh <image>}"
name="streamgo-probe-allinone-$$"

cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker volume rm "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 1' INT TERM

fail() {
  echo "smoke: $*" >&2
  docker logs "$name" 2>&1 | tail -n 40 >&2 || true
  exit 1
}

start() {
  docker run -d --name "$name" -v "$name:/data" "$image" >/dev/null
  i=0
  while [ "$(docker inspect -f '{{.State.Health.Status}}' "$name")" != healthy ]; do
    i=$((i + 1))
    [ "$i" -le 90 ] || fail "the container is not healthy after 90 seconds"
    sleep 1
  done
  # streamgo healthcheck exits 0 only when /readyz answers 200 (database, LiveKit, MediaMTX)
  docker exec "$name" streamgo healthcheck >/dev/null || fail "/readyz is not 200"
}

docker run -d --name "$name" --tmpfs /data:mode=0755 "$image" >/dev/null
i=0
while [ "$(docker inspect -f '{{.State.Running}}' "$name")" = true ]; do
  i=$((i + 1))
  [ "$i" -le 30 ] || fail "a /data owned by root was accepted: the container is still running"
  sleep 1
done
[ "$(docker inspect -f '{{.State.ExitCode}}' "$name")" != 0 ] || fail "exit status 0 with an unwritable /data"
docker logs "$name" 2>&1 | grep -q 'not writable' || fail "no message about the unwritable /data"
docker rm "$name" >/dev/null
echo "smoke: unwritable /data refused"

out=$(docker run --rm "$image" streamgo version) || fail "streamgo version failed"
case "$out" in "streamgo "*) ;; *) fail "streamgo version printed: $out" ;; esac
echo "smoke: one command instead: $out"

# every process of the container, the media servers' among them, runs as uid 65532 (real and effective)
all_unprivileged() {
  uids=$(docker exec "$name" sh -c 'for f in /proc/[0-9]*/status; do awk "/^Uid:/ {print \$2; print \$3}" "$f"; done' |
    sort -u | tr '\n' ' ')
  [ "$uids" = "65532 " ] || fail "processes run as uids $uids"
}

start
for p in livekit mediamtx streamgo; do
  docker exec "$name" supervisorctl status "$p" | grep -q RUNNING || fail "$p is not running"
done
all_unprivileged
[ "$(docker exec "$name" stat -c '%a %u' /data/streamgo.env)" = "600 65532" ] || fail "/data/streamgo.env is not private"
echo "smoke: healthy, /readyz 200, nothing runs as root"

# throwaway credentials of a throwaway container
password="Smoke-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')-A1!"
printf '%s\n' "$password" | docker exec -i "$name" streamgo user add --email smoke@example.com --name Smoke \
  --password-stdin >/dev/null || fail "could not create a user"
signin() {
  docker exec -e SMOKE_PASSWORD="$password" "$name" python3 -c '
import json, os, urllib.request
req = urllib.request.Request("http://127.0.0.1:8080/api/v1/auth/login", method="POST",
    data=json.dumps({"email": "smoke@example.com", "password": os.environ["SMOKE_PASSWORD"]}).encode(),
    headers={"Content-Type": "application/json", "Origin": "http://localhost:8080"})
with urllib.request.urlopen(req, timeout=10) as r:
    assert r.status == 200 and "streamgo_session=" in r.headers.get("Set-Cookie", ""), r.status
' || fail "could not sign in"
}
signin
echo "smoke: a user created with the CLI signed in"

key=$(docker exec "$name" grep '^STREAMGO_SECRET_KEY=' /data/streamgo.env)
docker stop -t 50 "$name" >/dev/null
[ "$(docker inspect -f '{{.State.ExitCode}}' "$name")" = 0 ] || fail "the container did not stop cleanly"
docker rm "$name" >/dev/null
start
docker exec "$name" streamgo user list | grep -q smoke@example.com || fail "the user did not survive"
[ "$(docker exec "$name" grep '^STREAMGO_SECRET_KEY=' /data/streamgo.env)" = "$key" ] || fail "the secret key changed"
signin
all_unprivileged
echo "smoke: the user and the secret key kept across a new container — ok"
