#!/bin/sh
# fwtest.sh : 1.6 MediaMTX native forward on air/fw (publisher + an RTSP reader already running)
API="http://127.0.0.1:19997/v3"; C="curl -s -u streamgo:spike-internal-pass"
fw() { $C "$API/paths/forward/list" | python3 -c 'import json,sys; [print("   fwd", i["pos"], i["conf"]["dest"], i["state"], repr(i["lastError"]), i["id"][:8], "out", i["outboundBytes"]) for i in json.load(sys.stdin)["items"]]'; }
sink() { curl -s http://127.0.0.1:19998/v3/paths/list | python3 -c 'import json,sys; [print("   sink", i["name"], i["source"]["id"][:8], i["readyTime"]) for i in json.load(sys.stdin)["items"] if "fw" in i["name"]]'; }
state() { echo "-- $1 ($(date +%T))"; fw; sink; echo "   publisher alive: $(kill -0 $(cat out/fw/pub.pid) 2>/dev/null && echo yes || echo NO), source $($C $API/paths/get/air/fw | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["source"]["id"][:8], "readers", [r["id"][:8] for r in d["readers"]])')"; echo "   reader out_time $(grep out_time= out/fw/reader.progress | tail -1)"; }
patch() { $C -X PATCH "$API/config/paths/patch/air/fw" -H 'Content-Type: application/json' -d "$1" -w " patch:%{http_code}\n"; }
state "one destination"
patch '{"forward":[{"dest":"rtmp://127.0.0.1:11935/live/fw1"},{"dest":"rtmp://127.0.0.1:11935/live/fw2"}]}'; sleep 4; state "added fw2"
patch '{"forward":[{"dest":"rtmp://127.0.0.1:11935/live/fw2"}]}'; sleep 4; state "removed fw1 (fw2 moved to position 1)"
patch '{"forward":[{"dest":"rtmp://127.0.0.1:11935/live/fw2"},{"dest":"rtmp://127.0.0.1:11999/live/dead"}]}'; sleep 3; state "added a dead destination"
sleep 12; state "dead destination after 15 s (retry?)"
docker logs --since 40s streamgo-spike-mediamtx-1 2>&1 | grep -E 'air/fw' | tail -n 12
