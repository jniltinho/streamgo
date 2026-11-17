#!/bin/sh
# longrun.sh <name> <extra query> <phases> : owner-direct run + transcoder + RTMP relay into the sink (recorded)
N=$1; D=out/$N; mkdir -p $D
env -u WAYLAND_DISPLAY xvfb-run -a -s "-screen 0 1920x1080x24" ./owner-direct.sh $N $N 720 "$2" $3 > $D/xvfb.log 2>&1 &
sleep 16
./transcode.sh $N 720 > $D/transcode.progress 2> $D/transcode.err & echo $! > $D/transcode.pid
sleep 4
./relay.sh $N rtmp://127.0.0.1:11935/live/$N > $D/relay.progress 2> $D/relay.err & echo $! > $D/relay.pid
wait
