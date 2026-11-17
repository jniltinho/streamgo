#!/bin/sh
# transcode.sh <bid> <height 720|1080> : program/<bid> (VP8|H264 + Opus) -> air/<bid> (H.264 + AAC, GOP 2 s, CFR 30)
H=${2:-720}; W=$((H * 16 / 9)); BR=$([ "$H" = 1080 ] && echo 6000k || echo 4500k); BUF=$([ "$H" = 1080 ] && echo 12000k || echo 9000k)
exec ffmpeg -hide_banner -nostats -loglevel warning -progress pipe:1 \
  -rtsp_transport tcp -timeout 5000000 -i "rtsp://streamgo:spike-internal-pass@127.0.0.1:18554/program/$1" \
  -map 0:v:0 -map 0:a:0 \
  -vf "scale=$W:$H:force_original_aspect_ratio=decrease,pad=$W:$H:(ow-iw)/2:(oh-ih)/2,format=yuv420p" -fps_mode cfr -r 30 \
  -c:v libx264 -preset veryfast -tune zerolatency -profile:v high -g 60 -keyint_min 60 -sc_threshold 0 \
  -b:v $BR -maxrate $BR -bufsize $BUF \
  -c:a aac -ar 48000 -ac 2 -b:a 160k \
  -f rtsp -rtsp_transport tcp "rtsp://streamgo:spike-internal-pass@127.0.0.1:18554/air/$1"
