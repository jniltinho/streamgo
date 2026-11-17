#!/bin/sh
# owner-direct.sh <name> <bid> <res> <extra query> <phases>   (run under: env -u WAYLAND_DISPLAY xvfb-run -a ...)
# Guest via Playwright; owner in plain Chrome with no DevTools attached (Playwright keeps its pages 'visible'),
# started by a real X click. hidden = Ctrl+T (another tab in front); minimized = what a window manager does on
# iconify (_NET_WM_STATE_HIDDEN + unmap), since Xvfb has no window manager.
N=$1; B=$2; R=$3; X=$4; P=$5; D=out/$N; mkdir -p $D
[ -z "$DISPLAY" ] || [ "$DISPLAY" = ":0" ] && { echo "run under xvfb-run"; exit 1; }
GUEST_ONLY=1 node run.mjs $D $R vp8 $B > $D/guest-run.log 2>&1 & G=$!
sleep 6
google-chrome --ozone-platform=x11 --user-data-dir=$PWD/$D/owner-profile --no-first-run --no-default-browser-check --mute-audio \
  --window-size=1000,700 "http://127.0.0.1:18080/owner.html?room=bc_$B&res=$R&codec=vp8&bid=$B&autostart=1&beacon=$N$X" > $D/owner-chrome.log 2>&1 & C=$!
sleep 6; W=$(xdotool search --name '^owner' | head -1); echo "owner window $W" >> $D/driver.txt
xdotool mousemove --window $W 300 300 click 1
log() { echo "{\"t\":$(date +%s%3N),\"ev\":\"phase\",\"name\":\"$1\",\"secs\":$2,\"vis\":\"(page log)\",\"win\":\"$3\"}" >> $D/driver.log; }
prev=visible
for ph in $(echo $P | tr , ' '); do name=${ph%:*}; secs=${ph#*:}
  [ "$prev" = hidden ] && { xdotool windowfocus --sync $W; xdotool key --window $W ctrl+w; sleep 1; }
  [ "$prev" = minimized ] && { xdotool windowmap $W; xprop -id $W -remove _NET_WM_STATE; sleep 1; }
  case $name in
    hidden) xdotool windowfocus --sync $W; xdotool key --window $W ctrl+t; log hidden $secs normal;;
    minimized) xprop -id $W -f _NET_WM_STATE 32a -set _NET_WM_STATE _NET_WM_STATE_HIDDEN; xdotool windowunmap $W; log minimized $secs emulated-iconify;;
    *) log visible $secs normal;;
  esac
  prev=$name; sleep $secs
done
kill $C $G
