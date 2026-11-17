#!/bin/sh
# Builds the Linux packages from the static binary (Dockerfile stage package-build; make package):
#   streamgo_<version>_amd64.deb   streamgo-<version>-1.x86_64.rpm   streamgo_<version>_linux_amd64.tar.gz   SHA256SUMS
# The names carry the version (the deb, rpm and tar conventions), so files of several releases can sit side by side.
# The three hold the same files: everything under /opt/streamgo (StreamGo, the pinned LiveKit and MediaMTX, their
# configuration) and three systemd units. None carries an ffmpeg: the .deb depends on the distribution's, the .rpm
# recommends it (output.ffmpeg_path stays "ffmpeg", looked up in PATH).
# Run from a directory holding the binaries (./streamgo, ./livekit-server, ./mediamtx), config.toml.example,
# LICENSE, NOTICE and THIRD_PARTY.
# Portions adapted from MeetHub (MIT): packaging/build.sh
set -eu

version="${1:?usage: build.sh <version> <output directory>}"
out="${2:?usage: build.sh <version> <output directory>}"
here=$(cd "$(dirname "$0")" && pwd)

# v1.2.3 (and what git describe adds after it) → 1.2.3…; anything else (a commit, even one that starts with a
# digit, or "dev") → 0.0.0+<it>, older than every release; no "-" (RPM refuses it)
case "$version" in
v[0-9]*.[0-9]*.[0-9]*) v="${version#v}" ;;
*) v="0.0.0+$version" ;;
esac
v=$(printf '%s' "$v" | tr -- '-' '.')

work=$(mktemp -d)
root="$work/root"
app="$root/opt/streamgo"
mkdir -p "$out"

for b in streamgo livekit-server mediamtx; do install -D -m 0755 "$b" "$app/bin/$b"; done
install -m 0755 "$here/init-env.sh" "$app/bin/init-env"
install -m 0644 "$here/streamgo.env" "$app/streamgo.env.example"
install -m 0644 "$here/livekit.yaml" "$here/mediamtx.yml" LICENSE NOTICE THIRD_PARTY "$app/"
# the example configuration, with the data directory inside /opt/streamgo whatever the working directory is, the
# media servers on this host's loopback address, and livekit.url empty: browsers reach LiveKit's signalling through
# StreamGo at /livekit/ on its own origin
{
  echo '# Installed with StreamGo. Secrets go in /opt/streamgo/streamgo.env, never here.'
  sed -e 's|^data_dir = "\./data"|data_dir = "/opt/streamgo/data"|' \
    -e 's|^url = "ws://localhost:7880"|url = ""|' \
    -e 's,"http://localhost:\(7880\|8889\|9997\)","http://127.0.0.1:\1",' \
    -e 's|"rtsp://localhost:8554"|"rtsp://127.0.0.1:8554"|' config.toml.example
} >"$app/config.toml"
for want in 'data_dir = "/opt/streamgo/data"' 'url = ""' 'api_url = "http://127.0.0.1:7880"' \
  'whip_url = "http://127.0.0.1:8889"' 'rtsp_url = "rtsp://127.0.0.1:8554"' 'api_url = "http://127.0.0.1:9997"'; do
  grep -qxF "$want" "$app/config.toml" || {
    echo "build.sh: config.toml.example no longer has the line this script rewrites into: $want" >&2
    exit 1
  }
done
chmod 0644 "$app/config.toml"

# ── tar.gz: the folder alone (streamgo/…), with the units and an empty streamgo.env, for an install by hand ──
units="streamgo.service streamgo-livekit.service streamgo-mediamtx.service"
for u in $units; do install -m 0644 "$here/$u" "$app/$u"; done
install -m 0640 "$here/streamgo.env" "$app/streamgo.env"
tar -C "$root/opt" --owner=0 --group=0 --numeric-owner -czf "$out/streamgo_${v}_linux_amd64.tar.gz" streamgo
rm "$app/streamgo.env"
for u in $units; do
  rm "$app/$u"
  install -D -m 0644 "$here/$u" "$root/usr/lib/systemd/system/$u"
done

# ── rpm ──
sed "s/@VERSION@/$v/" "$here/streamgo.spec" >"$work/streamgo.spec"
rpmbuild -bb --quiet --target x86_64 \
  --define "_topdir $work/rpm" --define "_stagedir $root" --define "_scripts $here" \
  --define "_rpmdir $out" --define "_rpmfilename streamgo-$v-1.x86_64.rpm" \
  "$work/streamgo.spec"

# ── deb ──
size=$(du -sk "$root" | cut -f1)
mkdir "$root/DEBIAN"
sed -e "s/@VERSION@/$v/" -e "s/@SIZE@/$size/" "$here/control" >"$root/DEBIAN/control"
printf '%s\n' /opt/streamgo/config.toml /opt/streamgo/livekit.yaml /opt/streamgo/mediamtx.yml >"$root/DEBIAN/conffiles"
install -m 0755 "$here/postinstall.sh" "$root/DEBIAN/postinst"
install -m 0755 "$here/preremove.sh" "$root/DEBIAN/prerm"
install -m 0755 "$here/postremove.sh" "$root/DEBIAN/postrm"
dpkg-deb --root-owner-group -Zxz --build "$root" "$out/streamgo_${v}_amd64.deb" >/dev/null

(cd "$out" && sha256sum "streamgo_${v}_amd64.deb" "streamgo-$v-1.x86_64.rpm" "streamgo_${v}_linux_amd64.tar.gz" >SHA256SUMS)
rm -rf "$work"
echo "packages of StreamGo $v:"
ls -l "$out"
