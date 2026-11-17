import * as LK from '/lk/livekit-client.esm.mjs';
export { LK };
export const q = new URLSearchParams(location.search);
export const log = (ev, o = {}) => { const l = JSON.stringify({ t: Date.now(), ev, vis: document.visibilityState, ...o });
  console.log(l); if (q.get('beacon')) fetch(`/log?name=${q.get('beacon')}`, { method: 'POST', body: l, keepalive: true }).catch(() => {}); };
document.addEventListener('visibilitychange', () => log('visibilitychange'));
addEventListener('pagehide', () => log('pagehide')); addEventListener('freeze', () => log('freeze')); addEventListener('resume', () => log('resume'));

export function startTicker(fps, fn) {
  const w = new Worker('/tick.js'); w.onmessage = (e) => fn(e.data); w.postMessage({ fps });
}

// Test source: a 1280x720 canvas burning [white, black, 16 counter bits] blocks + a flash box every 2 s,
// and a WebAudio tone (freq) with a click (clickFreq) at each flash. Simulates a camera + microphone.
export const SRC_W = 1280, SRC_H = 720, BITS = 16;
export function makeSource(label, color, freq, clickFreq) {
  const c = document.createElement('canvas'); c.width = SRC_W; c.height = SRC_H;
  const g = c.getContext('2d');
  // camera-like source: frames with timestamps on an exact 30 fps grid (?srcgen=0: canvas.captureStream instead)
  const SG = new URLSearchParams(location.search).get('srcgen') !== '0';
  const gen = SG ? new MediaStreamTrackGenerator({ kind: 'video' }) : null, gw = gen?.writable.getWriter();
  const video = SG ? gen : c.captureStream(0).getVideoTracks()[0]; let t0 = 0;
  const actx = new AudioContext(), dest = actx.createMediaStreamDestination();
  const osc = actx.createOscillator(), og = actx.createGain();
  osc.frequency.value = freq; og.gain.value = 0.1; osc.connect(og).connect(dest); osc.start();
  const click = () => {
    const o = actx.createOscillator(), cg = actx.createGain();
    o.frequency.value = clickFreq; cg.gain.value = 0.5; o.connect(cg).connect(dest);
    o.start(actx.currentTime); o.stop(actx.currentTime + 0.03);
  };
  let n = 0;
  const draw = () => {
    const bw = SRC_W / (BITS + 2), bh = SRC_H * 0.12;
    g.fillStyle = color; g.fillRect(0, 0, SRC_W, SRC_H);
    const bits = [1, 0]; for (let i = BITS - 1; i >= 0; i--) bits.push((n >> i) & 1);
    bits.forEach((b, i) => { g.fillStyle = b ? '#fff' : '#000'; g.fillRect(i * bw, 0, bw, bh); });
    const flash = n % 60 === 0;
    g.fillStyle = flash ? '#fff' : '#000'; g.fillRect(SRC_W * 0.05, SRC_H * 0.55, SRC_W * 0.25, SRC_H * 0.4);
    if (flash) click();
    g.fillStyle = '#fff'; g.font = 'bold 120px sans-serif'; g.fillText(`${label} ${n}`, SRC_W * 0.35, SRC_H * 0.6);
    if (SG) { t0 ||= performance.now() * 1000; const f = new VideoFrame(c, { timestamp: Math.round(t0 + n * 1e6 / 30) }); gw.write(f); f.close(); }
    else video.requestFrame();
    n++;
  };
  return { video, audio: dest.stream.getAudioTracks()[0], actx, draw, count: () => n };
}

export async function join(room, id, setup = () => {}) {
  const r = await (await fetch(`/token?room=${room}&id=${id}`)).json();
  if (!r.token) throw new Error('token failed');
  const lk = new LK.Room({ adaptiveStream: false, dynacast: false });
  setup(lk); // handlers before connect, so no subscription event is missed
  await lk.connect(r.url, r.token);
  log('lk-connected', { id });
  return lk;
}

export async function publishSource(lk, src) {
  await lk.localParticipant.publishTrack(src.video, { source: LK.Track.Source.Camera, simulcast: true,
    videoEncoding: { maxBitrate: 2_500_000, maxFramerate: 30 } });
  await lk.localParticipant.publishTrack(src.audio, { source: LK.Track.Source.Microphone });
}
