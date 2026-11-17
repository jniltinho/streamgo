#!/bin/sh
# Before the files go away: stop and disable the three services, on a removal only. Arguments: deb `remove` (also
# `upgrade`, `deconfigure`…); rpm `0` (removal) or `1` (upgrade).
# Portions adapted from MeetHub (MIT): packaging/preremove.sh
set -e
case "${1:-}" in
remove | 0) ;;
*) exit 0 ;;
esac
# tells postinstall.sh, should the package come back, that the services have to be enabled and started again
touch /opt/streamgo/.removed 2>/dev/null || true
[ -d /run/systemd/system ] || exit 0
systemctl disable --now streamgo.service streamgo-livekit.service streamgo-mediamtx.service || true
