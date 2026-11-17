#!/bin/sh
# Run on your own desktop (not part of the automated spike). Opens a throwaway Chrome profile on a test page
# that logs document.visibilityState and Web Worker timer ticks. Minimize the window for ~1 minute, restore it
# and read the page; the same lines are in spike/out/check-minimize.log. Ctrl+C here to finish.
cd "$(dirname "$0")"; mkdir -p out
python3 -m http.server 18090 --bind 127.0.0.1 --directory web 2> out/check-minimize.log &
trap 'kill $!' EXIT; sleep 1
google-chrome --user-data-dir="$(mktemp -d)" --no-first-run --new-window http://127.0.0.1:18090/vis.html
wait
