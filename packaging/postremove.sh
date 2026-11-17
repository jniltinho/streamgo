#!/bin/sh
# After a removal: systemd forgets the units. The data, the secrets (streamgo.env) and the user stay — deleting the
# broadcasts, or the key that reads their destinations, is never a side effect of removing a package. Arguments:
# deb `remove` or `purge`; rpm `0` (removal).
# Portions adapted from MeetHub (MIT): packaging/postremove.sh
set -e
case "${1:-}" in
remove | purge | 0) ;;
*) exit 0 ;;
esac
if [ -d /run/systemd/system ]; then
  systemctl daemon-reload || true
fi
if [ -d /opt/streamgo/data ]; then
  echo "streamgo: /opt/streamgo/data (the database), /opt/streamgo/streamgo.env (the secret key) and the streamgo user were kept"
fi
