#!/bin/sh
# /usr/local/bin/streamgo of the all-in-one image: the server, the CLI (docker exec) and the health check get the
# secrets the entrypoint created in /data/streamgo.env. A variable given to the container (-e) wins over the file.
# Portions adapted from MeetHub (MIT): docker/allinone/meethub.sh
if [ -r /data/streamgo.env ]; then
  given=$(export -p)
  set -a
  . /data/streamgo.env
  set +a
  eval "$given"
fi
exec /usr/local/libexec/streamgo "$@"
