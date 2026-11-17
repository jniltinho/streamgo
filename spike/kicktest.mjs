// Orphan cleanup: find the WebRTC session publishing on a path through the API and kick it, without its WHIP URL.
import { chromium } from 'playwright-core';
if (!process.env.DISPLAY || process.env.DISPLAY === ':0') throw new Error('run under xvfb-run');
const { WAYLAND_DISPLAY, ...xenv } = process.env;
const API = 'http://127.0.0.1:19997/v3', H = { Authorization: 'Basic ' + btoa('streamgo:spike-internal-pass') };
const j = async (u, m = 'GET') => { const r = await fetch(API + u, { method: m, headers: H }); return [r.status, await r.text()]; };
const b = await chromium.launch({ env: xenv, executablePath: '/usr/bin/google-chrome', headless: false, args: ['--ozone-platform=x11', '--mute-audio'] });
const p = await (await b.newContext()).newPage(); await p.goto('http://127.0.0.1:18080/whiptest.html');
console.log('publish:', await p.evaluate(() => publish('http://127.0.0.1:18889/program/kick/whip', 'streamgo:spike-internal-pass')));
await new Promise(r => setTimeout(r, 3000));
const [, path] = await j('/paths/get/program/kick'); const src = JSON.parse(path).source;
console.log('GET /v3/paths/get/program/kick -> source', JSON.stringify(src));
console.log('GET /v3/webrtcsessions/get/' + src.id, (await j('/webrtcsessions/get/' + src.id))[1].slice(0, 200));
console.log('POST /v3/webrtcsessions/kick/' + src.id, await j('/webrtcsessions/kick/' + src.id, 'POST'));
await new Promise(r => setTimeout(r, 2000));
console.log('after kick: path', await j('/paths/get/program/kick'), 'pc', await p.evaluate(() => states()));
await new Promise(r => setTimeout(r, 6000)); console.log('pc +6 s', await p.evaluate(() => states()));
await b.close();
