#!/bin/sh
# fwtest2.sh : forward to a destination that stops reading (sink2 paused), recovery, and how the key shows up
API="http://127.0.0.1:19997/v3"; C="curl -s -u streamgo:spike-internal-pass"
S2=$(docker inspect streamgo-spike-sink2-1 --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
fw() { echo "-- $1 ($(date +%T))"; $C "$API/paths/forward/list?path=air/fw" | python3 -c 'import json,sys; [print("   fwd", i["pos"], i["conf"]["dest"], i["state"], repr(i["lastError"]), i["id"][:8], "out", i["outboundBytes"]) for i in json.load(sys.stdin)["items"]]'; }
$C -X PATCH "$API/config/paths/patch/air/fw" -H 'Content-Type: application/json' -d "{\"forward\":[{\"dest\":\"rtmp://127.0.0.1:11935/live/fw2\"},{\"dest\":\"rtmp://$S2:1935/live#SECRETKEY123\"}]}" -w " patch:%{http_code}\n"
sleep 5; fw "sink2 forwarding"
docker pause streamgo-spike-sink2-1 >/dev/null; echo "paused sink2 $(date +%T)"
for i in 1 2 3 4 5 6; do sleep 5; fw "sink2 paused +$((i*5))s"; done
docker unpause streamgo-spike-sink2-1 >/dev/null; echo "unpaused $(date +%T)"
for i in 1 2 3; do sleep 5; fw "after unpause +$((i*5))s"; done
echo "-- key in MediaMTX log:"; docker logs --since 80s streamgo-spike-mediamtx-1 2>&1 | grep -c SECRETKEY123; docker logs --since 80s streamgo-spike-mediamtx-1 2>&1 | grep -E 'air/fw' | grep -vE 'dial tcp 127.0.0.1:11999|11999/live/dead' | tail -n 8
