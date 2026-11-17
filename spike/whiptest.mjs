// 1.4 extras: a second WHIP offer on a path that already has a publisher (direct to MediaMTX), PATCH/If-Match via the proxy.
import { chromium } from 'playwright-core';
const API = 'http://streamgo:spike-internal-pass@127.0.0.1:19997';
const get = async (p) => { const r = await fetch(API.replace('streamgo:spike-internal-pass@', '') + p, { headers: { Authorization: 'Basic ' + btoa('streamgo:spike-internal-pass') } }); return r.status === 200 ? r.json() : r.status; };
const sleep = (s) => new Promise(r => setTimeout(r, s * 1000));
if (!process.env.DISPLAY || process.env.DISPLAY === ':0') throw new Error('run under xvfb-run');
const { WAYLAND_DISPLAY, ...xenv } = process.env;
const b = await chromium.launch({ env: xenv, executablePath: '/usr/bin/google-chrome', headless: false, args: ['--ozone-platform=x11', '--mute-audio'] });
const p = await (await b.newContext()).newPage();
await p.goto('http://127.0.0.1:18080/whiptest.html');
const direct = 'http://127.0.0.1:18889/program/dbl/whip';
console.log('direct, no credentials:', await p.evaluate(u => publish(u), direct));
console.log('direct, wrong credentials:', await p.evaluate(u => publish(u, 'streamgo:wrong'), direct));
const A = await p.evaluate(u => publish(u, 'streamgo:spike-internal-pass'), direct); await sleep(4);
const pathA = await get('/v3/paths/get/program/dbl');
console.log('offer A:', A, 'states', await p.evaluate(() => states()), 'path source', JSON.stringify(pathA.source));
const B = await p.evaluate(u => publish(u, 'streamgo:spike-internal-pass'), direct); await sleep(6);
const pathB = await get('/v3/paths/get/program/dbl');
console.log('offer B:', B, 'states after 6 s [A,B]', await p.evaluate(() => states()), 'path source', JSON.stringify(pathB.source));
const ws = await get('/v3/webrtcsessions/list');
console.log('webrtcsessions:', JSON.stringify(ws.items.filter(i => i.path === 'program/dbl').map(i => ({ id: i.id, state: i.state, path: i.path }))));
// PATCH through the proxy, with the right ETag, a wrong ETag and none, for a session published through the proxy
const P = await p.evaluate(u => publish(u), 'http://127.0.0.1:18080/b/patchtest/program/whip');
console.log('via proxy:', P);
const frag = 'a=ice-ufrag:abcd\r\na=ice-pwd:abcdefghijklmnopqrstuvwx\r\nm=video 9 UDP/TLS/RTP/SAVPF 0\r\na=mid:0\r\na=candidate:1 1 udp 2122260223 127.0.0.1 50000 typ host\r\n';
for (const im of [P.etag, '"wrong"', null]) {
  const h = { 'Content-Type': 'application/trickle-ice-sdpfrag' }; if (im) h['If-Match'] = im;
  const r = await fetch('http://127.0.0.1:18080' + P.location, { method: 'PATCH', headers: h, body: frag });
  console.log(`PATCH via proxy If-Match=${im}:`, r.status, (await r.text()).trim().slice(0, 100));
}
const r = await fetch('http://127.0.0.1:18080' + P.location, { method: 'DELETE' }); console.log('DELETE via proxy:', r.status);
await b.close();
