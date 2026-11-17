# analyze.py <recording.mp4> <height> [driver.log] : fresh-picture counters, tones, flash/click A/V offset.
# Run with python3 -I. Writes <recording>.seconds.csv and prints a JSON summary.
import sys, json, re, subprocess, threading, datetime, os, numpy as np
rec, H = sys.argv[1], int(sys.argv[2]); W = H * 16 // 9
drv = [json.loads(l) for l in open(sys.argv[3])] if len(sys.argv) > 3 else []
m = re.search(r'(\d{4}-\d\d-\d\d_\d\d-\d\d-\d\d-\d+)', os.path.basename(rec))
t_file = datetime.datetime.strptime(m.group(1), '%Y-%m-%d_%H-%M-%S-%f').replace(tzinfo=datetime.timezone.utc).timestamp() if m else 0

def tile(k):  # owner tile k=0 (left), guest k=1 (right); 1280x720 sources drawn at W/2 x H/2, y from H/4
    x0, y0, tw, th = k * W / 2, H / 4, W / 2, H / 2
    bits = [(int(y0 + 0.06 * th), int(x0 + (i + .5) * tw / 18)) for i in range(18)]
    return bits, (int(y0 + 0.75 * th), int(x0 + 0.175 * tw))
T = [tile(0), tile(1)]
def px(f, y, x): return f[y - 2:y + 3, x - 2:x + 3].mean()

# video: gray frames on stdout, pts from showinfo on stderr
p = subprocess.Popen(['ffmpeg', '-v', 'info', '-copyts', '-i', rec, '-map', '0:v:0', '-vf', 'showinfo,format=gray',
                      '-fps_mode', 'passthrough', '-f', 'rawvideo', '-'], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
pts = []
def rd():
    for l in p.stderr:
        mm = re.search(rb'pts_time:([\d.]+)', l)
        if mm and b'Parsed_showinfo' in l: pts.append(float(mm.group(1)))
th_ = threading.Thread(target=rd); th_.start()
cnt, flash = [[], []], [[], []]
fs = W * H
while True:
    b = p.stdout.read(fs)
    if len(b) < fs: break
    f = np.frombuffer(b, np.uint8).reshape(H, W)
    for k in (0, 1):
        v = [px(f, y, x) for y, x in T[k][0]]
        ok = v[0] > 160 and v[1] < 90 and all(a > 160 or a < 90 for a in v[2:])
        cnt[k].append(int(''.join('1' if a > 128 else '0' for a in v[2:]), 2) if ok else None)
        flash[k].append(px(f, *T[k][1]) > 128)
p.wait(); th_.join()
n = min(len(pts), len(cnt[0])); pts = np.array(pts[:n])
t0 = pts[0]

# audio: mono s16 on the same (copyts) timeline, gaps filled
a = subprocess.run(['ffmpeg', '-v', 'error', '-copyts', '-i', rec, '-map', '0:a:0', '-af', 'aresample=async=1:first_pts=0',
                    '-ac', '1', '-ar', '48000', '-f', 's16le', '-'], capture_output=True).stdout
x = np.frombuffer(a, np.int16) / 32768.0; SR = 48000
def amp(seg, f):  # sine amplitude at f
    k = np.exp(-2j * np.pi * f * np.arange(len(seg)) / SR); return 2 * abs((seg * k).sum()) / len(seg)
def onset(tv, f):  # first 2 ms window where the click at f exceeds half its peak, within tv +-0.5 s
    i0 = max(0, int((tv - .5) * SR)); seg = x[i0:int((tv + .5) * SR)]
    if len(seg) < SR // 2: return None
    h = seg * np.exp(-2j * np.pi * f * np.arange(len(seg)) / SR); c = np.concatenate([[0], np.cumsum(h)])
    mag = abs(c[96:] - c[:-96]) * 2 / 96
    if mag.max() < 0.05: return None
    return (i0 + np.argmax(mag > mag.max() / 2)) / SR

out = {'file': rec, 'frames': int(n), 'duration_s': round(float(pts[-1] - t0), 1), 'participants': {}}
rows = {}
for k, name, cf in ((0, 'owner', 3000), (1, 'guest', 5000)):
    c = cnt[k][:n]; sec = ((pts - t0)).astype(int)
    adv = np.zeros(sec.max() + 1, int); bad = np.zeros_like(adv); prev = None; last_change = pts[0]; stall = 0; stall_at = 0
    for i in range(n):
        if c[i] is None: bad[sec[i]] += 1; continue
        if prev is not None and c[i] != prev:
            adv[sec[i]] += 1
            if pts[i] - last_change > stall: stall, stall_at = pts[i] - last_change, last_change - t0
            last_change = pts[i]
        prev = c[i]
    if pts[-1] - last_change > stall: stall, stall_at = pts[-1] - last_change, last_change - t0
    full = adv[1:-1]  # skip partial first/last second
    # flash onsets -> click onsets
    fl = flash[k][:n]; offs = []
    for i in range(1, n):
        if fl[i] and not fl[i - 1]:
            ta = onset(pts[i], cf)
            if ta is not None: offs.append((pts[i] - t0, ta - pts[i]))
    bymin = {}
    for tv, o in offs: bymin.setdefault(int(tv // 60), []).append(o)
    tone = cf and (440 if k == 0 else 660)
    lv = [20 * np.log10(amp(x[int(s * SR):int(s * SR) + SR], tone) + 1e-9) for s in range(int(t0) + 1, int(pts[-1]) - 1)]
    out['participants'][name] = {
        'seconds': int(len(full)), 'min_advances_per_s': int(full.min()) if len(full) else None,
        'seconds_below_25': [int(s) for s in np.nonzero(adv < 25)[0] if 0 < s < len(adv) - 1],
        'undecodable_frames': int(bad.sum()), 'longest_stall_s': round(float(stall), 3), 'longest_stall_at_s': round(float(stall_at), 1),
        f'tone_{tone}Hz_dBFS': {'median': round(float(np.median(lv)), 1), 'min': round(float(np.min(lv)), 1), 'max': round(float(np.max(lv)), 1)} if lv else None,
        'av_offset_ms_by_minute': {m_: {'n': len(v), 'median': round(1000 * float(np.median(v)), 1), 'min': round(1000 * min(v), 1), 'max': round(1000 * max(v), 1)} for m_, v in sorted(bymin.items())},
    }
    rows[name] = adv
# other tones (leaks/doubles): whole-file spectrum level at a few frequencies
seg = x[int((t0 + 1) * SR):][:SR * 60]
out['spectrum_dBFS_first_min'] = {f: round(20 * np.log10(amp(seg, f) + 1e-9), 1) for f in (440, 660, 880, 1100, 1320)}
# phases from the driver log, mapped to recording seconds by wall clock
if drv and t_file:
    ph = [d for d in drv if d['ev'] == 'phase']
    for i, d in enumerate(ph):
        a_ = d['t'] / 1000 - t_file; b_ = (ph[i + 1]['t'] / 1000 - t_file) if i + 1 < len(ph) else 1e9
        seg_s = lambda adv: [int(v) for s, v in enumerate(adv) if a_ + 2 <= s <= b_ - 2 and 0 < s < len(adv) - 1]
        out.setdefault('phases', []).append({'phase': d['name'], 'vis': d['vis'], 'win': d['win'], 'from_s': round(a_), 'to_s': round(min(b_, n / 30)),
            **{f'{nm}_min_adv': (min(seg_s(r)) if seg_s(r) else None) for nm, r in rows.items()},
            **{f'{nm}_secs_below_25': sum(1 for v in seg_s(r) if v < 25) for nm, r in rows.items()}})
with open(rec + '.seconds.csv', 'w') as f:
    f.write('second,owner_advances,guest_advances\n')
    for s in range(len(rows['owner'])): f.write(f"{s},{rows['owner'][s]},{rows['guest'][s] if s < len(rows['guest']) else ''}\n")
print(json.dumps(out, indent=1))
