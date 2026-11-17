#!/bin/sh
# cpurun.sh <res> : owner-direct run alone + transcoder + relay; CPU (% of one core) over 45 s
N=cpu$1; D=out/$N; mkdir -p $D
env -u WAYLAND_DISPLAY xvfb-run -a -s "-screen 0 2560x1440x24" ./owner-direct.sh $N $N $1 '&gen=1&proc=2' visible:100 > $D/xvfb.log 2>&1 &
sleep 16; ./transcode.sh $N $1 > $D/transcode.progress 2> $D/transcode.err & T=$!
sleep 4; ./relay.sh $N rtmp://127.0.0.1:11935/live/$N > $D/relay.progress 2> $D/relay.err & R=$!
sleep 25; ./cpu.sh $T 45 > $D/cpu-transcoder.txt & ./cpu.sh $R 45 > $D/cpu-relay.txt
sleep 1; wait
echo "$1 transcoder $(cat $D/cpu-transcoder.txt) relay $(cat $D/cpu-relay.txt) $(grep speed= $D/transcode.progress | tail -1)"
