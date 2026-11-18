#!/bin/sh
# single-payload.sh <streamgo> <livekit-server> <livekit-version> <mediamtx> <mediamtx-version> <output>
#
# Writes the single file: the streamgo executable (packed with UPX or not), then each media server compressed with
# zstd -19, then a trailer that `streamgo up` reads (internal/mediaservers/payload.go):
#
#   one line per program   "<name> <version> <sha256 of the program> <offset> <size>\n"
#                          (offset and size of its zstd stream, in bytes from the start of the file)
#   a footer               the trailer's length in 16 decimal digits, then "STREAMGO-PAYLOAD-1\n"
#
# Needs zstd and coreutils (sha256sum, stat, cut, cat, mktemp). A version is one word (no blank).
set -eu
[ $# -eq 6 ] || { echo "usage: $0 <streamgo> <livekit-server> <livekit-version> <mediamtx> <mediamtx-version> <output>" >&2; exit 2; }
exe=$1 lk=$2 lkv=$3 mtx=$4 mtxv=$5 out=$6
for v in "$lkv" "$mtxv"; do
  case $v in '' | *[[:space:]]*) echo "$0: a version is one word, got '$v'" >&2; exit 2 ;; esac
done

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
zstd -q -19 -c -- "$lk" >"$tmp/livekit-server.zst"
zstd -q -19 -c -- "$mtx" >"$tmp/mediamtx.zst"

off=$(stat -c %s -- "$exe")
: >"$tmp/trailer"
for p in livekit-server mediamtx; do
  if [ "$p" = livekit-server ]; then bin=$lk ver=$lkv; else bin=$mtx ver=$mtxv; fi
  sum=$(sha256sum -- "$bin" | cut -d' ' -f1)
  size=$(stat -c %s -- "$tmp/$p.zst")
  printf '%s %s %s %s %s\n' "$p" "$ver" "$sum" "$off" "$size" >>"$tmp/trailer"
  off=$((off + size))
done
printf '%016d%s\n' "$(stat -c %s -- "$tmp/trailer")" "STREAMGO-PAYLOAD-1" >"$tmp/footer"

cat -- "$exe" "$tmp/livekit-server.zst" "$tmp/mediamtx.zst" "$tmp/trailer" "$tmp/footer" >"$out.tmp"
chmod 755 "$out.tmp"
mv -f -- "$out.tmp" "$out"
