# Spike: media path (task group 1 of `add-live-studio`)

Date: 2026-11-17. Machine: 24 cores, Linux, Docker 29, Chrome 157 (`/usr/bin/google-chrome`), ffmpeg 8.0.1.
Code: `spike/` (throwaway). Captures: `spike/out/` (git-ignored). Every number below comes from a file in
`spike/out/`; the file is named next to it.

## Verdict

| Task | Result | Key number |
|---|---|---|
| 1.1 compose + WHIP, VP8 and H.264 | **pass** (with one required change) | VP8 and H.264 1280x720 + Opus 48 kHz stereo on the RTSP path; the guest's audio was **silent** until a muted `<audio>` element was attached |
| 1.2 transcoder + relay, RTMPS, drift, CPU | **pass** | H.264 High 1280x720 / 1920x1080, 30.00 fps, keyframes every 2.000 s, AAC LC 48 kHz stereo, each tone once; A/V offset changed by −9.3 ms (owner) and −9.4 ms (guest) between minute 1 and minute 30, absolute offset ≤ 30 ms; transcoder 28 % of one core at 720p, 52 % at 1080p, relay < 1 % |
| 1.3 fresh pictures, hidden 5 min + minimized 5 min | hidden: **pass**; minimized: **pass on an emulated iconify, not verified on a real desktop** | recommended compositor: every second of both phases ≥ 28 advances for both participants, longest freeze 0.167 s (at start-up, before hiding). Compositor exactly as written in D2: 12 s below 25 (min 23), longest freeze 0.166 s |
| 1.4 WHIP proxy | **pass** | refused directly (401), publish through proxy, media unaffected by a 10 s proxy outage, `DELETE` after restart closed the session (200) and the transcoder exited 1.0 s later |
| 1.5 failure behaviour | **measured** (see table); the 15 s stall promise holds only with a short watchdog | publisher gone: transcoder and relay exit **code 0** in < 0.1 s; input stalled: exit after 6.2 s; sink stops reading / blackholed: relay **never exits, ignores SIGINT**, needs SIGKILL; with `-rw_timeout 5000000` it exits after 15–17 s (code 146) |
| 1.6 MediaMTX native `forward` | **meets** isolation, per-destination status/last error, retry (fixed 5 s); does not report "flowing" by itself | see section |
| 1.7 reload in a fresh profile | **measured** | `AudioContext` starts `suspended`; video and LiveKit come back alone; audio needs one click, and **a WHIP offer made while suspended loses its audio track for good** |

Task 1.3 does not stop the approach, but only because the compositor was changed (D2 recommendations 1–3).
Both runs, the "as written" one and the recommended one, kept both counters moving in a hidden tab; the strict
"≥ 25 in every second" fails for the as-written compositor in visible and hidden state alike, for a reason that
has nothing to do with hiding (sampling of the guest's frames, see 1.3).

**Incident to know about.** The first long run (`out/long/`, discarded) ran Chrome on the person's **real
Wayland desktop**, not on Xvfb: `WAYLAND_DISPLAY` is inherited and Chrome 157 prefers Wayland even when
`DISPLAY` points at Xvfb. Several short test windows (tasks 1.1, early 1.2, 1.4 checks) and that run's windows
(about 12 minutes, muted with `--mute-audio`) appeared there before I noticed (Xvfb had zero windows). It was
stopped; every later run unsets `WAYLAND_DISPLAY` and passes `--ozone-platform=x11`, and `run.mjs`,
`whiptest.mjs`, `kicktest.mjs` and `owner-direct.sh` refuse to start on `:0`. Results from before the fix are
only used where the window's visibility does not matter (codecs, tones, WHIP protocol).

## Pinned versions

| Component | Version | Digest / source |
|---|---|---|
| MediaMTX | `bluenviron/mediamtx:1.21.4` (released 2026-11-15) | `sha256:c730bccdbc8739f8f0c13cefe9bea8b61926f599b88fa4d0d229baa91846c7bc` |
| LiveKit server | `livekit/livekit-server:v1.13.10` (already local, image id `818b3f378abb`) | single node, no Redis, `room.auto_create: false` |
| ffmpeg | 8.0.1-3ubuntu2 (host) | libx264, aac, libopus present |
| Chrome | 157, `/usr/bin/google-chrome` | headed on Xvfb, X11 |
| livekit-client (npm) | 2.24.1 | |
| playwright-core (npm) | 1.65.1 | driving the system Chrome only |

## Setup (what runs)

`spike/compose.yml`, project `streamgo-spike`:

- `livekit` (host network): 17880 HTTP/WS, 17881 TCP, 17882/udp. Config `spike/livekit.yaml`.
- `mediamtx` (host network, ingest, production-like): RTSP 127.0.0.1:18554 (TCP only), WHIP 127.0.0.1:18889,
  WebRTC ICE 127.0.0.1:18189 UDP+TCP, API 127.0.0.1:19997 (on for 1.6), `authMethod: internal`, one user
  `streamgo` with publish/read/api. RTMP, HLS, SRT, MoQ off. Config `spike/mediamtx.yml`.
- `sink` (bridge): test RTMP destination, RTMP 11935, RTMPS 11936 (self-signed), API 19998, records every path
  as fMP4 into `out/rec-sink/`. Config `spike/sink.yml`.
- `sink2` (own bridge network, no published ports): the same, used only for the failure tests (paused,
  disconnected), reached on its container IP.

Spike code:

- `server/main.go`: static pages, room creation + join token (stdlib JWT), the WHIP proxy (`POST`, `PATCH`,
  `DELETE`; stored session URL in `out/whip-sessions.json`), and `POST /log` for page logs.
- `web/common.js`, `web/guest.html`, `web/owner.html`, `web/tick.js`: test sources, guest, owner compositor and
  WHIP client. Each source is a 1280x720 canvas with a row of 18 blocks (white, black, 16 counter bits), a white
  "flash" box every 60 frames and a WebAudio tone (owner 440 Hz, guest 660 Hz) with a 30 ms click at each flash
  (owner 3 kHz, guest 5 kHz). The owner tile is drawn left, the guest right, each at half size.
- `analyze.py`: decodes the recording, reads both counters in every frame, counts per-second advances and the
  longest freeze, measures each tone's level per second and the click-to-flash offset (A/V) per minute.
- `run.mjs` (Playwright, guest and short runs), `owner-direct.sh` (owner in plain Chrome, see 1.3),
  `longrun.sh`, `chain.sh`, `transcode.sh`, `relay.sh`, `failtest.py`, `fwtest.sh`, `fwtest2.sh`,
  `whiptest.mjs`, `kicktest.mjs`, `lk-autocreate.mjs`, `cpu.sh`, `cpurun.sh`, `out/reload-test.sh`,
  `check-minimize.sh`.

Owner page options used in the runs: `gen=1` program frames through `MediaStreamTrackGenerator` with
timestamps on an exact 30 fps grid (default: `canvas.captureStream(0)` + `requestFrame()`); `proc=2` read each
source with `MediaStreamTrackProcessor` and show the next frame of a 2-frame queue on each tick (default: draw
the `<video>` element); `ael=0` turns off the muted `<audio>` element workaround (on by default after 1.1).
Sources always use the generator (camera-like steady timing) unless `srcgen=0`.

## Exact ffmpeg command lines (worked)

Transcoder (`spike/transcode.sh`; 720p: `W=1280 H=720 BR=4500k BUF=9000k`; 1080p: `W=1920 H=1080 BR=6000k BUF=12000k`):

```
ffmpeg -hide_banner -nostats -loglevel warning -progress pipe:1 \
  -rtsp_transport tcp -timeout 5000000 -i rtsp://streamgo:<secret>@127.0.0.1:18554/program/<id> \
  -map 0:v:0 -map 0:a:0 \
  -vf "scale=$W:$H:force_original_aspect_ratio=decrease,pad=$W:$H:(ow-iw)/2:(oh-ih)/2,format=yuv420p" -fps_mode cfr -r 30 \
  -c:v libx264 -preset veryfast -tune zerolatency -profile:v high -g 60 -keyint_min 60 -sc_threshold 0 \
  -b:v $BR -maxrate $BR -bufsize $BUF \
  -c:a aac -ar 48000 -ac 2 -b:a 160k \
  -f rtsp -rtsp_transport tcp rtsp://streamgo:<secret>@127.0.0.1:18554/air/<id>
```

Relay (`spike/relay.sh`):

```
ffmpeg -hide_banner -nostats -loglevel warning -progress pipe:1 \
  -rtsp_transport tcp -timeout 5000000 -i rtsp://streamgo:<secret>@127.0.0.1:18554/air/<id> \
  -map 0 -c copy -f flv -rw_timeout 5000000 rtmp(s)://<host>/<app>/<key>
```

(`-rw_timeout` is what ends a relay whose destination stops reading; without it the relay hangs forever, 1.5.)

What the two command lines depend on, for the startup check (names as `ffmpeg -demuxers/-muxers/-protocols/
-encoders/-decoders/-filters` print them):

| Kind | Transcoder | Relay |
|---|---|---|
| demuxers | `rtsp` (it is a format, not a protocol) | `rtsp` |
| muxers | `rtsp`, and `rtp` (the RTSP muxer opens one RTP muxer per stream) | `flv` |
| protocols | `tcp` (RTSP interleaved; `udp` and `rtp` only if UDP transport were used) | `tcp`, `rtmp`; `rtmps` and `tls` for RTMPS |
| decoders | `vp8`, `h264`, `opus` | none |
| encoders | `libx264`, `aac` | none |
| filters | `scale`, `pad`, `format` (`-r`/`-fps_mode cfr` use the built-in frame-rate conversion) | none |

## 1.1 Compose and publish by WHIP — pass, with a required change

- VP8 offer: `ffprobe` on `rtsp://…/program/t11vp8` → `vp8 1280x720`, `opus 48000 Hz 2 ch`
  (`out/t11-vp8b/`, frame grab `frame.png` shows both tiles).
- H.264 offer: `h264 Constrained Baseline 1280x720`, `opus 48000 Hz 2 ch` (`out/t11-h264/ffprobe.txt`). Chrome
  157's H.264 is the OpenH264 software encoder.
- Canvas driven by a Web Worker timer (`web/tick.js`, drift-corrected `setTimeout` in a dedicated worker),
  `adaptiveStream: false`, program bus with a `DynamicsCompressorNode` limiter into one
  `MediaStreamDestination` for the whole session.
- **Required change: the guest's audio was silent in the program.** Feeding a remote LiveKit (WebRTC) audio
  track into WebAudio through `createMediaStreamSource` without any media element gave 660 Hz at −86 dBFS
  (silence) while the owner's local tone was at −18.3 dBFS (`out/t11-vp8b/audio.wav`). With the remote stream
  also set as `srcObject` of a **muted** `<audio>` element that is played (never audible), the guest's tone
  arrives at −18.5 dBFS (`out/t11-ael/`). This is Chrome's long-standing behaviour for remote WebRTC audio in
  WebAudio and still holds in 157. D2's "LiveKit audio elements are never attached" must become "attached,
  muted, only to keep the remote track flowing; the sound is heard only through the monitor bus".
- Note: the `DynamicsCompressorNode` adds a fixed make-up gain (+1.7 dB with threshold −3 dB, ratio 20): a
  −20 dBFS tone comes out at −18.3 dBFS. A limiter that must not change level needs `threshold` at 0 or a
  compensating gain node.
- `PATCH` (trickle ICE) through the proxy answered 204; MediaMTX's `ETag` is always `*`.

## 1.2 Transcoder and relay — pass

Recording of the sink (`out/rec-sink/live/drift/*.mp4`, the 32-minute run; 1080p in `out/rec-sink/live/p1080/`):

- 720p: `h264 High 1280x720 yuv420p`, average 30.0007 fps (459940/15331), keyframes: 384, interval min 1.987 /
  median 2.000 / max 2.000 s; `aac LC 48000 Hz 2 ch`.
- 1080p: `h264 High 1920x1080`, 30.0 fps, keyframes 71, interval 1.968 / 2.000 / 2.000 s; `aac LC 48000 Hz 2 ch`.
- Each tone present once: 440 Hz at −18.3 dBFS and 660 Hz at −18.2 dBFS (median, min and max per second within
  ±0.1 dB over the whole run); 880/1100/1320 Hz below −79 dBFS. A doubled source would read about −12 dBFS.
- RTMPS: a second relay to `rtmps://127.0.0.1:11936/live/drift-rtmps`, started 2 minutes into the run without
  touching the first relay, ran to the end (`out/drift/relay-rtmps.*`; the sink shows it as `rtmpsConn`).
  ffmpeg does not verify the certificate by default (`-tls_verify 0`); production should keep the default only
  if destinations are trusted, or set `-tls_verify 1 -ca_file …`.

A/V drift (32-minute run `drift`, recommended compositor, tab visible; `out/rec-sink/live/drift/*.mp4.json`
and `…/drift-rtmps/*.mp4.json`). Offset = click onset in the audio minus flash onset in the video, both from the
same source frame; positive means audio later; 30 flashes per minute, median:

| Recording | Participant | Minute 1 | Minute 30 | Change | Range over the run |
|---|---|---|---|---|---|
| RTMP relay | owner (local source) | −9.0 ms | −18.3 ms | −9.3 ms | −8.0 … −23.8 ms |
| RTMP relay | guest (via LiveKit) | +26.0 ms | +16.6 ms | −9.4 ms | +28.4 … +16.5 ms |
| RTMPS relay | owner | −14.8 ms | −23.9 ms | −9.1 ms | −13.7 … −29.5 ms |
| RTMPS relay | guest | +19.3 ms | +9.9 ms | −9.4 ms | +21.8 … +9.8 ms |

Drift under 100 ms after 30 minutes: **pass** (about −0.3 ms per minute, in 2.5 ms steps; extrapolated about
−18 ms per hour). The same run kept fresh pictures at ≥ 26/s (owner) and ≥ 25/s (guest, after the 7 s start-up)
in every second. Earlier short runs showed that the transcoder itself shifts the offset by about +85 ms in one
configuration (`out/t15/`: −38.9 ms on `program/`, +46.0 ms on `air/`), so the absolute offset depends on the
chain; only its change over time is the drift.

CPU (`spike/cpurun.sh`, each run alone on the machine after the other runs ended, 45 s average, % of one core;
`out/cpu720/`, `out/cpu1080/`; test content is flat colour and text, real camera content will cost more):

| Program | Transcoder (VP8 decode + scale + x264 veryfast + AAC) | Relay (`-c copy`) |
|---|---|---|
| 720p30, 4.5 Mbps cap | 28 % | < 1 % |
| 1080p30, 6 Mbps cap | 52 % | < 1 % |

Measured while other runs shared the machine: 21 % at 720p (`out/long/`), 77 % at 1080p (`out/p1080/cpu.txt`).

Transcoder frame-rate conversion: in the first Playwright-driven runs, whose program frames came from
`canvas.captureStream(0)` + `requestFrame()`, the program's frame intervals were bimodal (5th percentile 26 ms,
95th 57 ms: frames land on the page's 60 Hz paint clock) and the CFR conversion dropped about 4 and duplicated
about 4 frames per second (`out/t12/transcode.progress`: 330 dropped, 375 duplicated in 97 s), which alone cut
the owner's fresh pictures from 29–30 to 26–27 per second (`out/t13/`). With program frames from a
`MediaStreamTrackGenerator` stamped on an exact 30 fps grid the transcoder dropped 0 (`out/t14/`). In the later
plain-Chrome runs the captureStream program also dropped 0 (`out/h13a/transcode.progress`), so the effect depends
on how the page is driven; the generator removes the dependency.

## 1.3 Fresh pictures with the owner's tab hidden, then minimized

How it was run (`spike/owner-direct.sh`, `spike/longrun.sh`):

- **Playwright cannot produce a hidden page.** Every page it drives reports `visible`, even as a background tab
  in the same window and with `Emulation.setFocusEmulationEnabled` turned off (`spike/vis-test.mjs`). So the
  owner runs in a plain Chrome with no DevTools attached, on Xvfb (X11), with a fresh profile, started by a real
  X click (xdotool). The guest stays on Playwright (it is never hidden).
- Hidden: Ctrl+T in the owner's window (a new tab in front). The page logged
  `visibilitychange → hidden` at the moment of the key press, and `visible` 300 s later
  (`out/pagelogs/h13a.log`, `out/pagelogs/h13b.log`).
- Minimized: Xvfb has no window manager, and both CDP `Browser.setWindowBounds {windowState: minimized}` and
  `xdotool windowminimize` do nothing without one (CDP keeps reporting `normal`, the page stays `visible`). I did
  what an X11 window manager does on iconify: set `_NET_WM_STATE_HIDDEN` and unmap the window. Chrome then
  reported the page `hidden` (logged) and the window minimized. This is **an emulation**: it is not verified on
  a real desktop, and the person's desktop is GNOME on **Wayland**, where Chrome learns about minimization
  differently (or not at all). `spike/check-minimize.sh` opens a test page in a throwaway profile on the real
  desktop; minimize the window for a minute, restore it, and the page lists `visibilityState` and Web Worker
  ticks per second while hidden.
- Each run: 60 s visible, 300 s hidden, 300 s minimized, 60 s visible; transcoder and relay into the sink; the
  recording decoded frame by frame (`analyze.py`). `--mute-audio` makes the page inaudible, which is the strict
  case (Chrome exempts audible pages from some throttling).

Results (`out/h13a/analysis.json`, `out/h13b/analysis.json`, per-second CSV next to each recording):

| Run | Phase | Owner advances/s (min) | Guest advances/s (min) | Seconds < 25 (guest) | Longest freeze |
|---|---|---|---|---|---|
| h13b: recommended (`gen=1&proc=2`) | visible (first 60 s) | 28 | 17 | 7 (seconds 1–7 after start-up) | 0.167 s (start-up) |
| | hidden 300 s | 29 | 28 | 0 | |
| | minimized 300 s (emulated) | 30 | 29 | 0 | |
| | visible again | 30 | 30 | 0 | |
| h13a: as written in D2 (captureStream + `<video>`) | visible (first 60 s) | 29 | 16 | 35 | 0.166 s |
| | hidden 300 s | 27 | 23 | 1 (guest histogram: 23×2, 25×4, 26×167, 27×86, 28×34) | |
| | minimized 300 s (emulated) | 29 | 24 | 11 (24×11, 25×48, 26×189, 27×42, 28×9) | |
| | visible again | 30 | 30 | 0 | |

What this says:

1. The browser keeps composing and publishing in a hidden tab and an (emulated) minimized window. Worker
   ticks, `MediaStreamTrackProcessor` frames, `captureStream` frames and the WebRTC encoder all kept running;
   the encoder reported 30 fps and no quality limitation throughout (`"stats"` lines in the page logs). No
   counter ever stopped for more than 0.167 s.
2. The as-written compositor fails the strict "≥ 25 in every second" for the **guest** in every state, visible
   included: drawing a `<video>` element samples the guest's 30 fps at the 30 fps tick with arrival jitter, so
   3–5 frames per second are skipped (the page received 30 fps: `guestIn.fps` 29.9–30.1). It is a compositor
   flaw, not a background-tab one.
3. A 2-frame queue per source fed by `MediaStreamTrackProcessor` (show the next queued frame on each tick) gives
   30 of 30 in steady state, visible, hidden and minimized (`out/t15/`, `out/h13b/`). Cost: 66 ms more latency
   for remote sources.
4. Every run shows 6–7 s at about 20 fps for the guest right after joining: LiveKit forwards the 360p simulcast
   layer (20 fps) first and switches to the 720p layer after its bandwidth probe (`guestIn.w` 640 → 1280). It
   happens at subscription, not while hiding; the studio should go live only after it settles, or the check
   should exclude the first 10 s after a source joins.
5. LiveKit's built-in video presets cap 360p at 20 fps and 540p at 25 fps; the spike publishes with an explicit
   `videoEncoding {maxBitrate: 2.5 Mbps, maxFramerate: 30}`. The guest camera must be published with 30 fps
   explicitly, or the "≥ 25 fresh pictures" cannot hold.

## 1.4 WHIP through the Go proxy — pass

`out/t14proxy/`, `out/server.log`, `out/whiptest.txt`:

- Direct `POST` to MediaMTX without credentials or with a wrong password: **401**. Through the proxy: 201,
  `Location: /b/<id>/program/whip/session`, `ETag: *`.
- Proxy killed for 10 s while publishing (`kill`, `curl` to it got no connection): the transcoder's `out_time`
  kept advancing 2 s per 2 s the whole time and the page kept encoding 30 fps (media is browser ↔ MediaMTX).
- Proxy restarted; it read the stored session URL from `out/whip-sessions.json`; `DELETE
  /b/w14/program/whip/session` → MediaMTX 200; the path disappeared (API 404) and the transcoder exited 1.0 s
  later (code 0, "Failed reading RTSP data: End of file").
- `PATCH` with `application/trickle-ice-sdpfrag` through the proxy works: 204 when the fragment carries the
  session's ufrag. MediaMTX **ignores `If-Match`** (a wrong value also gets 204; its ETag is always `*`).
  A fragment with a **different ufrag is taken as an ICE restart**: 200 with a new SDP answer. So D3's "ICE
  restart is not supported" is a choice of StreamGo, not a limit of MediaMTX; the proxy should reject a PATCH
  whose ufrag differs from the stored one if it wants to keep that rule.
- Second WHIP offer on a path that already has a publisher (direct to MediaMTX, `overridePublisher: true`, the
  default): the new offer gets 201, MediaMTX logs "closing existing publisher", the old peer connection goes
  `disconnected`, and only the new session is listed. **Replaced, not refused, and never both.** With
  `overridePublisher: false` it would be refused instead (not run).
- The WHIP resource id in `Location` (for example `…/whip/5b9b947b…`) is not the session id the API uses
  (`4d8e4af0…`); see 1.6 for the cleanup by session id.

## 1.5 Failure behaviour — measured

`spike/failtest.py`, separate chain: an ffmpeg publisher (VP8 + Opus, 2 Mbps, to `program/f5`) → transcoder
(`air/f5`, about 4.5 Mbps) → relay → `sink2` on its container IP. Each scenario: 15 s flowing, fault at t=22 s,
watch up to 75 s, then SIGINT and SIGKILL after 5 s. Files: `out/fail/<scenario>/` (each progress block
timestamped, `events.json`).

| Scenario | Transcoder | Relay | `-progress` while stalled |
|---|---|---|---|
| no publisher yet | exits at once (0.02 s), **code 8**, "404 Not Found" | — | — |
| publisher killed (SIGKILL) | exits in 0.05 s, **code 0** | exits in 0.04 s, **code 0** | — |
| publisher stalled (SIGSTOP) | exits after **6.2 s**, **code 0** ("Connection timed out", from `-timeout 5000000`) | exits with it, code 0 | blocks keep coming every 0.5 s with `out_time` frozen (transcoder) and `total_size` frozen (relay) |
| sink stops reading (`docker pause`), no `-rw_timeout` | unaffected | media time advanced **1.8 s** after the fault (about 0.9 MB absorbed by socket buffers), then frozen; **never exits** in 75 s; **ignores SIGINT**, needed SIGKILL | blocks every 0.5 s, `out_time` and `total_size` frozen (146 blocks) |
| sink stops reading, `-rw_timeout 5000000` | unaffected | media time +1.85 s, then frozen; **exits after 16.6 s, code 146** ("Error writing trailer: Connection timed out") | 10 blocks after the freeze, then none until exit |
| sink blackholed (`docker network disconnect`), no `-rw_timeout` | unaffected | media time +1.3 s, frozen, never exits, ignores SIGINT, SIGKILL | as above (147 blocks) |
| sink blackholed, `-rw_timeout 5000000` | unaffected | media time +0.3 s, **exits after 15.5 s, code 146** | 10 blocks, then none |

Conclusions:

- **Exit code 0 does not mean "ended normally".** Losing the input ends both processes with code 0. The
  supervisor must treat any exit while the run is live as a loss.
- `-progress` distinguishes flowing from stalled: while stalled, blocks either keep coming with the same
  `out_time_us` / `total_size`, or stop coming (blocked in a write). The watchdog has to treat both as stalled:
  "no `out_time` advance within N s", not "no progress line within N s".
- Timeouts that end a blocked process: input side `-timeout 5000000` on RTSP (≈ 6 s). Output side: only
  `-rw_timeout` ends a relay whose destination stops reading or disappears, and it takes about 3× its value
  (write, trailer, close: 15–17 s with 5 s). **Without it, a stuck relay is not even stopped by SIGINT**; the
  SIGKILL after 5 s in D4 is necessary.
- The promised "stalled destination noticed within 15 s": achievable by the watchdog, not by ffmpeg's own
  timeout. Media time keeps advancing for buffered seconds = buffered bytes / bitrate after the destination stops
  reading: measured 1.8 s at 4.5 Mbps (≈ 0.9 MB of socket buffers); at the 0.9–1.1 Mbps the test program
  actually used, that same buffer is ≈ 7–8 s (estimate from bytes, not measured). With a **5 s** "no `out_time`
  advance" watchdog the worst case is about 8 + 5 = 13 s; with D4's 10 s it is about 18 s at low bitrates. Use
  `-rw_timeout 5000000` as the backstop and SIGKILL after 5 s.

## 1.6 MediaMTX native `forward` (1.21.4) — meets the three requirements, with caveats

`spike/fwtest.sh` → `out/fw/fwtest.txt`, `spike/fwtest2.sh` → `out/fw/fwtest2.txt`. Path config changed
through `PATCH /v3/config/paths/patch/air/fw` with a publisher and an RTSP reader on the path.

- **Add/remove one destination without touching the others: yes.** Adding `fw2` left `fw1`'s connection on the
  sink unchanged (same connection id, same `readyTime`); removing `fw1` left `fw2` unchanged even though it moved
  from position 2 to 1 (same forward id `81d10aa5`). The publisher and the existing reader were not interrupted
  (reader `out_time` kept advancing, same session ids).
- **Per-destination status and last error: yes.** `GET /v3/paths/forward/list?path=air/fw` returns per forward:
  `id`, `pos`, `created`, `state` (`forwarding` / `error`), `lastError` (for example `dial tcp
  127.0.0.1:11999: connect: connection refused`, `write tcp …: i/o timeout`), `outboundBytes`, `remoteAddr`,
  `conf.dest`. (`/v3/paths/forward/list` without `?path=` answers "invalid path".)
- **Retry after failure: yes, fixed 5 s, forever.** A refused destination was retried every 5 s; a destination
  that stopped reading was dropped with `i/o timeout` 12 s after it stopped (the instance's `writeTimeout` 10 s)
  while MediaMTX logged "reader is too slow, discarding … frames" (it drops frames for that destination only,
  the other forward kept going), then retried; after the sink resumed, data flowed again within 5 s.
- Caveats: `state` goes back to `forwarding` while a retry connects to a destination that accepts TCP but does
  not read (`outboundBytes` stays 0), so "live" must come from `outboundBytes` advancing, not from `state`. The
  retry interval is not configurable per destination and there is no backoff. The stream key
  (`rtmp://host/app#key`) is **not** written to MediaMTX's log (the fragment is stripped), but it **is**
  returned in `conf.dest` by the forwards and config APIs, so the API must stay on the internal network with the
  internal user (as D3 already says).
- Verdict: native `forward` can replace the relay processes for switching, isolation, status and retry, and
  keeps keys out of process arguments. The supervisor would poll the forwards API instead of reading
  `-progress`. Not measured: forward over RTMPS to a real platform, and behaviour across a MediaMTX restart.

Orphan cleanup (coordinator's addition), `spike/kicktest.mjs` → `out/kicktest.txt`:

- `GET /v3/paths/get/program/<id>` → `source: {"type": "webRTCSession", "id": "3c7cc4a1-…"}` (the publishing
  session; readers are listed under `readers`).
- `GET /v3/webrtcsessions/get/<session id>` and `GET /v3/webrtcsessions/list` give the details (state `publish`,
  path, remote address).
- `POST /v3/webrtcsessions/kick/<session id>` → 200 `{"status":"ok"}`; the path was gone 2 s later (404) and the
  browser's peer connection went `disconnected`. No WHIP resource URL needed.

## 1.7 Reload in a fresh profile — measured

`out/reload-test.sh` → `out/t17reload/result.txt`, page log `out/pagelogs/r17.log`. Plain Chrome, new
`--user-data-dir`, owner page that starts itself on load (no gesture).

- The mixing and source `AudioContext`s start **`suspended`** ("The AudioContext was not allowed to start").
  The same happens to a second load of the page in the same profile (a new document has no user activation).
- What the page restores by itself: LiveKit connection, subscriptions, the composed canvas and the WHIP video
  (171 video frames in 6 s on the RTSP path, encoder at 30 fps).
- What it cannot: audio. While suspended, the program's audio track sends **no packets at all** (sender
  `packetsSent` 0). One click resumes the contexts and audio packets flow (50/s).
- **But the WHIP session made while suspended has lost its audio track for good**: MediaMTX waits
  `webrtcTrackGatherTimeout` (2 s) for each track's first packet and publishes the path with the tracks that
  arrived: "1 track (VP8)" for every suspended start, "2 tracks (VP8, Opus)" otherwise. After the click the
  browser sends audio but the path never gets it, and the transcoder's `-map 0:a:0` fails on such a path. So the
  studio must not publish WHIP until the `AudioContext` is `running`, or must re-publish (new offer) after
  "Resume audio".

## LiveKit with `auto_create: false` (coordinator's addition)

`spike/lk-autocreate.mjs` → `out/lk-autocreate.txt`:

- Valid token for a room that does not exist: `/rtc/validate` → **404 "requested room does not exist"**; the
  WebSocket join fails; no room is created.
- Join racing `DeleteRoom` (5 tries, both sent at once): in 3 of 5 the room **was recreated** by the join
  (LiveKit log: room `RM_Ycw…` closed by `API_DELETE`, then `starting RTC session` creates `RM_N8oo…` 1 ms
  later), with the default `empty_timeout` 300 s. So `auto_create: false` does not close the race; D1's
  reconciler sweep (delete rooms whose broadcast no longer exists) is what does.

## Recommended changes to the design (for the plan's owner to apply)

D2 (browser composition):

1. Draw each source from a `MediaStreamTrackProcessor` with a 2-frame queue (advance one frame per tick), not
   from `<video>` elements. Without it the guest's fresh pictures are 22–27/s even with the tab visible.
2. Emit program frames through a `MediaStreamTrackGenerator` with timestamps on an exact 30 fps grid instead of
   `canvas.captureStream(0)` + `requestFrame()`; it removes the 60 Hz paint-clock jitter that made the
   transcoder drop and duplicate frames in some runs.
3. Publish cameras (and screen shares that should count as fresh) with an explicit `maxFramerate: 30`; LiveKit's
   presets give 20 fps at 360p and 25 fps at 540p. Expect 6–7 s at 20 fps after each subscription.
4. Replace "LiveKit audio elements are never attached" by: each remote audio track is attached to a muted,
   playing `<audio>` element (Chrome needs it for WebAudio to receive data); audio is heard only through the
   monitor bus.
5. Reload: publish WHIP only once the `AudioContext` is `running`; until then show "Resume audio" and keep the
   broadcast `starting`/`interrupted`. A WHIP offer made while suspended loses the audio track at MediaMTX.
6. The limiter: `DynamicsCompressorNode` adds fixed make-up gain (+1.7 dB at threshold −3 dB, ratio 20);
   compensate or set the threshold to 0.
7. Minimized window: not proven. Keep the hosting requirement "Chromium desktop", and state that the hidden-tab
   case is proven on X11 and the minimized case only by emulation, pending `check-minimize.sh` on GNOME/Wayland
   and on Windows/macOS.

D3 (WHIP):

8. MediaMTX's default `overridePublisher: true` replaces the previous publisher on a second offer; keep it (the
   proxy's per-offer resources then only need to clean up), or set it to `false` to refuse.
9. MediaMTX ignores `If-Match` and treats a PATCH with a new ufrag as an ICE restart; the proxy should enforce
   both itself if "no ICE restart" stays the rule.
10. Orphan cleanup after a crash: `GET /v3/paths/get/program/<id>` → `source.id`, then
    `POST /v3/webrtcsessions/kick/<id>`.

D4 (ffmpeg):

11. Exit code 0 happens on input loss; treat every exit during a live run as a loss.
12. Watchdog on "no `out_time` advance for 5 s" (not 10 s, not "no progress line"), to meet the 15 s promise at
    low bitrates; `-rw_timeout 5000000` on the relay output; `-timeout 5000000` on RTSP input; SIGKILL after 5 s
    is required (a relay blocked on its destination ignores SIGINT).
13. The startup check list: use the table in "Exact ffmpeg command lines" (demuxer `rtsp`, muxers `rtsp`, `rtp`,
    `flv`, protocols `tcp`, `rtmp`, `rtmps`, `tls`, decoders `vp8`, `h264`, `opus`, encoders `libx264`, `aac`).
14. Native `forward` passed the switching, isolation, status and retry checks on 1.21.4; if adopted, relays become
    forwards and "live" for a destination is "`outboundBytes` advancing", polled from
    `/v3/paths/forward/list?path=air/<id>`; retry is a fixed 5 s (D5's 1/2/5/10 s backoff would not apply).

D5 (state machine):

15. Destination status `live` must come from advancing bytes/media time, never from "process running" or
    forward `state: forwarding`.
16. Going live should wait until every on-stage source has been received at full rate for a few seconds
    (LiveKit's first 6–7 s are at 20 fps).

## Not verified / not done

- Minimized window on a real desktop (see 1.3); `spike/check-minimize.sh` is for the person to run.
- Content complexity: the test sources are flat colours and text; real camera content needs more bitrate and CPU.
- Forward over RTMPS to a real platform; MediaMTX restart with forwards configured.
- `overridePublisher: false` behaviour.
- Socket buffer at low bitrate in 1.5 is computed from bytes, not measured.

## Cleanup

`docker compose -p streamgo-spike down -v` removed the four spike containers and both networks; no spike
process (ffmpeg, Chrome, Xvfb, spike server) is left. The `bluenviron/mediamtx:1.21.4` image stays pulled.
`meethub-meethub-1`, `meethub-redis-1` and `meethub-livekit-1` are up. Note: `meethub-meethub-1` showed "Up 9
minutes" at the start of the spike and "Up 22 minutes" at the end, so it was restarted around 13:18 local time
by something else; the spike never ran a command against any meethub container (all Docker commands named
`streamgo-spike` containers or networks).
