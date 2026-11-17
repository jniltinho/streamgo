#!/bin/sh
# StreamGo all-in-one: checks the /data volume, creates the secret key and the credentials the three services share
# in /data/streamgo.env on the first start (never replaced afterwards), then supervisord runs LiveKit, MediaMTX and
# the streamgo server. The container never runs as root (uid 65532), so /data must already belong to that user: a
# named volume takes its owner from the image, a bind-mounted host directory keeps the owner it has on the host.
#
#   docker run … <image>                    start LiveKit, MediaMTX and the server
#   docker run … <image> streamgo version   run one command instead (no services)
# Portions adapted from MeetHub (MIT): docker/allinone/entrypoint.sh
set -eu

if [ $# -gt 0 ]; then
  exec "$@"
fi

if ! [ -w /data ]; then
  echo "streamgo: /data is not writable by uid $(id -u): use a named volume (-v streamgo-data:/data)" \
    "or give the host directory to that user (chown -R $(id -u):$(id -g) <dir>)" >&2
  exit 1
fi
first=no
[ -e /data/streamgo.env ] || first=yes
streamgo-init-env /data/streamgo.env
[ "$first" = no ] || echo "streamgo: created the secret key and the shared credentials in /data/streamgo.env (back it up with /data)"

# the media servers take StreamGo's credentials under their own names (as the packages' units do); a variable
# given to the container wins over the file
given=$(export -p)
set -a
. /data/streamgo.env
set +a
eval "$given"
export LIVEKIT_KEYS="${STREAMGO_LIVEKIT_API_KEY}: ${STREAMGO_LIVEKIT_API_SECRET}" \
  MTX_AUTHINTERNALUSERS_0_USER="$STREAMGO_MEDIAMTX_USER" MTX_AUTHINTERNALUSERS_0_PASS="$STREAMGO_MEDIAMTX_SECRET" \
  MTX_WEBRTCADDITIONALHOSTS="$NODE_IP"

exec supervisord -c /etc/supervisor/supervisord.conf
