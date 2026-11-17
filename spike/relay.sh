#!/bin/sh
# relay.sh <bid> <rtmp(s) url> : air/<bid> -> destination, stream copy
exec ffmpeg -hide_banner -nostats -loglevel warning -progress pipe:1 \
  -rtsp_transport tcp -timeout 5000000 -i "rtsp://streamgo:spike-internal-pass@127.0.0.1:18554/air/$1" \
  -map 0 -c copy -f flv ${RW_TIMEOUT:+-rw_timeout $RW_TIMEOUT} "$2"
