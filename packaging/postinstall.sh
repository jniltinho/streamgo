#!/bin/sh
# After the files are in place: the streamgo user, the data directory, the secrets (first install only: a value
# already in streamgo.env is never replaced), and the three services enabled and started (restarted on an upgrade).
# Arguments: deb `configure [old version]`; rpm `1` (install) or `2` (upgrade).
# Portions adapted from MeetHub (MIT): packaging/postinstall.sh
set -e

fresh=no
case "${1:-}" in
configure) [ -n "${2:-}" ] || fresh=yes ;;
1 | abort-remove) fresh=yes ;; # abort-remove: dpkg gave up a removal after preremove.sh stopped the services
2) ;;
*) exit 0 ;; # deb: abort-upgrade, abort-deconfigure
esac
# installed again after a removal: dpkg still passes the old version, as on an upgrade, but preremove.sh had
# stopped and disabled the services
if [ -e /opt/streamgo/.removed ]; then
  fresh=yes
  rm -f /opt/streamgo/.removed
fi

getent group streamgo >/dev/null || groupadd --system streamgo
getent passwd streamgo >/dev/null ||
  useradd --system --gid streamgo --home-dir /opt/streamgo --no-create-home --shell /usr/sbin/nologin \
    --comment StreamGo streamgo

# the database and the uploaded assets: the service's own, never shipped and never deleted
install -d -o streamgo -g streamgo -m 0750 /opt/streamgo/data
# the secrets: not a file of the package, so no upgrade, removal or purge touches it; read by systemd as root and
# by the streamgo CLI through the group, hidden from everyone else
env=/opt/streamgo/streamgo.env
[ -e "$env" ] || install -m 0640 -g streamgo /opt/streamgo/streamgo.env.example "$env"
/opt/streamgo/bin/init-env "$env"
chown root:streamgo "$env"
chmod 0640 "$env"

units="streamgo-livekit.service streamgo-mediamtx.service streamgo.service"
[ -d /run/systemd/system ] || exit 0 # no systemd here (a container, a chroot): nothing to enable
systemctl daemon-reload
if [ "$fresh" = yes ]; then
  # shellcheck disable=SC2086 # one word per unit
  systemctl enable --now $units ||
    echo "streamgo: the services did not start; see 'journalctl -u streamgo' (it needs an ffmpeg with libx264)" >&2
else
  # shellcheck disable=SC2086
  systemctl try-restart $units || true
fi
