#!/bin/sh
# chain.sh <dir> <bid> <res> <codec> <secs> <extra query> : browser run + transcoder + 60 s captures of program and air
D=out/$1; mkdir -p $D
EXTRA="$6" env -u WAYLAND_DISPLAY xvfb-run -a -s "-screen 0 1920x1080x24" node run.mjs $D $3 $4 $2 visible:$5 > $D/run.log 2>&1 &
sleep 8; ./transcode.sh $2 $3 > $D/transcode.progress 2> $D/transcode.err &
sleep 4
for p in program air; do ffmpeg -v error -rtsp_transport tcp -i rtsp://streamgo:spike-internal-pass@127.0.0.1:18554/$p/$2 -map 0 -c copy -t 60 -y $D/${p}_2026-01-01_00-00-00-0.mkv & done
wait
for f in $D/*.mkv; do echo "$f"; python3 -I analyze.py $f $3 > $f.json; python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); [print(" ",k, "min",v["min_advances_per_s"], "below25",len(v["seconds_below_25"]), "stall",v["longest_stall_s"], {m:x["median"] for m,x in v["av_offset_ms_by_minute"].items()}) for k,v in d["participants"].items()]' $f.json; awk -F, 'NR>1{printf "%s/%s ", $2,$3} END{print ""}' $f.seconds.csv; done
