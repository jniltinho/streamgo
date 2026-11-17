#!/bin/sh
# init-env <file>: appends to an environment file the StreamGo secrets it lacks: the secret key, LiveKit's API key and
# secret and MediaMTX's user and password (the three services share them). A value already there is never replaced:
# a new secret key makes every stored destination and invite link unreadable. Creates the file (mode 0600, or what
# the caller's umask allows) when missing. Used by the packages' postinstall (as /opt/streamgo/bin/init-env) and by
# the all-in-one image's entrypoint; the same values as `make init`.
set -eu
f="${1:?usage: init-env <file>}"
[ -e "$f" ] || (umask 077 && : >"$f")
add() { grep -q "^$1=" "$f" || printf '%s=%s\n' "$1" "$2" >>"$f"; }
add STREAMGO_SECRET_KEY "$(head -c32 /dev/urandom | base64)"
add STREAMGO_LIVEKIT_API_KEY streamgo
add STREAMGO_LIVEKIT_API_SECRET "$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')"
add STREAMGO_MEDIAMTX_USER streamgo
add STREAMGO_MEDIAMTX_SECRET "$(od -An -tx1 -N24 /dev/urandom | tr -d ' \n')"
