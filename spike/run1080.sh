#!/bin/sh
# 1080p: owner-direct run + transcoder at 1080 + relay; CPU of transcoder and relay over 60 s
N=p1080; D=out/$N; mkdir -p $D
env -u WAYLAND_DISPLAY xvfb-run -a -s "-screen 0 2560x1440x24" ./owner-direct.sh $N $N 1080 '&gen=1&proc=2' visible:150 > $D/xvfb.log 2>&1 &
sleep 16; ./transcode.sh $N 1080 > $D/transcode.progress 2> $D/transcode.err & T=$!
sleep 4; ./relay.sh $N rtmp://127.0.0.1:11935/live/$N > $D/relay.progress 2> $D/relay.err & R=$!
sleep 30; ./cpu.sh $T 60 > $D/cpu.txt & ./cpu.sh $R 60 >> $D/cpu.txt & wait $!
wait
