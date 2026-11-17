# failtest.py <scenario> : 1.5 failure behaviour on a separate chain (program/f5 -> air/f5 -> sink2).
# Logs every -progress block with a wall-clock timestamp; prints a JSON summary.
import subprocess, threading, time, json, sys, os, signal
SC = sys.argv[1]; OUT = f'out/fail/{SC}'; os.makedirs(OUT, exist_ok=True)
M = 'rtsp://streamgo:spike-internal-pass@127.0.0.1:18554'
SINK = subprocess.check_output(['docker', 'inspect', 'streamgo-spike-sink2-1', '--format', '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}']).decode().strip()
T0 = time.time(); ev = []
def note(e, **k): ev.append({'t': round(time.time() - T0, 2), 'ev': e, **k}); print(ev[-1], flush=True)
procs = {}
def spawn(name, cmd, env=None):
    p = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=open(f'{OUT}/{name}.err', 'w'), env={**os.environ, **(env or {})}, start_new_session=True)
    p.blocks = []
    def rd():
        blk = {}
        with open(f'{OUT}/{name}.progress', 'w') as f:
            for l in p.stdout:
                l = l.decode().strip(); f.write(f'{time.time() - T0:.2f} {l}\n'); k, _, v = l.partition('=')
                blk[k] = v
                if k == 'progress': p.blocks.append((time.time() - T0, int(blk.get('out_time_us', '0').replace('N/A', '0') or 0), blk.get('total_size'), blk.get('frame'))); blk = {}
        p.wait(); note('exit', name=name, code=p.returncode)
    threading.Thread(target=rd, daemon=True).start(); procs[name] = p; note('start', name=name); return p
def publisher(): return spawn('publisher', ['ffmpeg', '-hide_banner', '-nostats', '-loglevel', 'warning', '-re', '-f', 'lavfi', '-i', 'testsrc2=size=1280x720:rate=30',
    '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000', '-c:v', 'libvpx', '-deadline', 'realtime', '-cpu-used', '8', '-b:v', '2M', '-g', '60',
    '-c:a', 'libopus', '-f', 'rtsp', '-rtsp_transport', 'tcp', f'{M}/program/f5'])
def transcoder(): return spawn('transcoder', ['./transcode.sh', 'f5', '720'])
def relay(rw=None): return spawn('relay', ['./relay.sh', 'f5', f'rtmp://{SINK}:1935/live/f5'], {'RW_TIMEOUT': rw} if rw else {})
def last_advance(p, after):  # last wall time out_time grew, and blocks printed after it
    adv = None; prev = None
    for t, us, size, fr in p.blocks:
        if prev is not None and us > prev: adv = (t, us, size)
        prev = us
    n_after = sum(1 for b in p.blocks if adv and b[0] > adv[0])
    return {'last_advance_t': adv and round(adv[0], 2), 'since_fault_s': adv and round(adv[0] - after, 2), 'blocks_printed_after': n_after,
            'size_at_fault': next((b[2] for b in reversed(p.blocks) if b[0] <= after), None), 'size_last': adv and adv[2]}
def wait_exit(names, secs):
    end = time.time() + secs
    while time.time() < end and any(procs[n].poll() is None for n in names): time.sleep(0.2)
def stop(name):  # what the supervisor would do: SIGINT, then SIGKILL after 5 s
    p = procs[name]
    if p.poll() is not None: return
    t = time.time(); os.killpg(p.pid, signal.SIGINT)
    try: p.wait(5); note('stopped by SIGINT', name=name, after_s=round(time.time() - t, 2))
    except subprocess.TimeoutExpired: os.killpg(p.pid, signal.SIGKILL); p.wait(); note('SIGINT ignored for 5 s, SIGKILL', name=name)

if SC == 'nopublisher':
    transcoder(); wait_exit(['transcoder'], 30)
else:
    publisher(); time.sleep(3); transcoder(); time.sleep(4); relay(sys.argv[2] if len(sys.argv) > 2 else None); time.sleep(15)
    tf = time.time() - T0; note('fault', scenario=SC)
    if SC == 'killpublisher': os.killpg(procs['publisher'].pid, signal.SIGKILL)
    elif SC == 'stoppublisher': os.killpg(procs['publisher'].pid, signal.SIGSTOP)
    elif SC.startswith('pausesink'): subprocess.run(['docker', 'pause', 'streamgo-spike-sink2-1'])
    elif SC.startswith('blackhole'): subprocess.run(['docker', 'network', 'disconnect', 'streamgo-spike_sink2net', 'streamgo-spike-sink2-1'])
    wait_exit(['relay', 'transcoder'] if 'publisher' in SC else ['relay'], 75)
    note('observed', **{n: last_advance(procs[n], tf) for n in ('transcoder', 'relay')})
    for n in ('relay', 'transcoder'): stop(n)
    if SC == 'stoppublisher': os.killpg(procs['publisher'].pid, signal.SIGCONT)
    stop('publisher')
    if SC.startswith('pausesink'): subprocess.run(['docker', 'unpause', 'streamgo-spike-sink2-1'])
    if SC.startswith('blackhole'): subprocess.run(['docker', 'network', 'connect', 'streamgo-spike_sink2net', 'streamgo-spike-sink2-1'])
time.sleep(1)
json.dump(ev, open(f'{OUT}/events.json', 'w'), indent=1)
