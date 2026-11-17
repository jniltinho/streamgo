// Drives guest + owner pages in real Chrome (headed, run under xvfb-run).
// node run.mjs <outdir> <res 720|1080> <codec vp8|h264> <bid> <phases e.g. visible:60,hidden:300,minimized:300,visible:60>
import { chromium } from 'playwright-core';
import fs from 'node:fs';
import { execFileSync } from 'node:child_process';
const [outdir, res = '720', codec = 'vp8', bid = 'test', phases = 'visible:60'] = process.argv.slice(2);
const extra = process.env.EXTRA || "";
fs.mkdirSync(outdir, { recursive: true });
const room = `bc_${bid}`, base = 'http://127.0.0.1:18080';
const dlog = (ev, o = {}) => fs.appendFileSync(`${outdir}/driver.log`, JSON.stringify({ t: Date.now(), ev, ...o }) + '\n');
const sleep = (s) => new Promise(r => setTimeout(r, s * 1000));
// never on the person's real desktop: X11 only, on the xvfb-run display
if (!process.env.DISPLAY || process.env.DISPLAY === ':0') throw new Error('run under xvfb-run');
const { WAYLAND_DISPLAY, ...xenv } = process.env;
const browser = await chromium.launch({ env: xenv,
  executablePath: '/usr/bin/google-chrome', headless: false,
  // keep Chrome's real background behaviour: drop Playwright's anti-throttling switches
  ignoreDefaultArgs: ['--disable-background-timer-throttling', '--disable-backgrounding-occluded-windows',
    '--disable-renderer-backgrounding', '--disable-ipc-flooding-protection'],
  args: ['--ozone-platform=x11', '--mute-audio', '--window-size=1000,700'],
});
const open = async (name, url) => {
  const ctx = await browser.newContext({ viewport: null });
  const p = await ctx.newPage();
  p.on('console', m => fs.appendFileSync(`${outdir}/${name}.log`, m.text() + '\n'));
  p.on('pageerror', e => dlog('pageerror', { name, e: String(e) }));
  await p.goto(url);
  await p.click('body'); // a real user gesture, so AudioContexts may start
  await p.evaluate(() => window.start());
  return { ctx, p };
};
const guest = await open('guest', `${base}/guest.html?room=${room}`);
await sleep(2);
if (process.env.GUEST_ONLY) { dlog('guest-only'); await new Promise(() => {}); }
const owner = await open('owner', `${base}/owner.html?room=${room}&res=${res}&codec=${codec}&bid=${bid}${extra}`);
dlog('started', { res, codec, bid });
const vis = async () => owner.p.evaluate(() => document.visibilityState);
const cdp = await owner.ctx.newCDPSession(owner.p);
// Playwright turns on focus emulation, which keeps every page 'visible'; turn it off for the owner page
await cdp.send('Emulation.setFocusEmulationEnabled', { enabled: false });
const { windowId } = await cdp.send('Browser.getWindowForTarget');
const xwin = () => { try { return execFileSync('xdotool', ['search', '--name', '^owner']).toString().trim().split('\n'); } catch { return []; } };
let other;
for (const ph of phases.split(',')) {
  const [name, secs] = ph.split(':');
  if (name === 'hidden') {
    other = other || await owner.ctx.newPage();
    await other.goto('about:blank'); await other.bringToFront();
  } else if (name === 'minimized') {
    await owner.p.bringToFront();
    await cdp.send('Browser.setWindowBounds', { windowId, bounds: { windowState: 'minimized' } });
    await sleep(2);
    const b = await cdp.send('Browser.getWindowBounds', { windowId });
    dlog('cdp-minimize', { windowState: b.bounds.windowState, vis: await vis() });
    if (b.bounds.windowState !== 'minimized' || (await vis()) !== 'hidden') {
      // no window manager: do what one does on iconify (unmap + _NET_WM_STATE_HIDDEN) by hand
      for (const w of xwin()) {
        execFileSync('xprop', ['-id', w, '-f', '_NET_WM_STATE', '32a', '-set', '_NET_WM_STATE', '_NET_WM_STATE_HIDDEN']);
        execFileSync('xdotool', ['windowunmap', w]);
      }
      await sleep(2);
      const b2 = await cdp.send('Browser.getWindowBounds', { windowId });
      dlog('emulated-minimize', { xwins: xwin(), windowState: b2.bounds.windowState, vis: await vis() });
    }
  } else {
    for (const w of xwin()) { try { execFileSync('xdotool', ['windowmap', w]); execFileSync('xprop', ['-id', w, '-remove', '_NET_WM_STATE']); } catch {} }
    await cdp.send('Browser.setWindowBounds', { windowId, bounds: { windowState: 'normal' } }).catch(() => {});
    await owner.p.bringToFront();
  }
  await sleep(1);
  dlog('phase', { name, secs: +secs, vis: await vis(), win: (await cdp.send('Browser.getWindowBounds', { windowId })).bounds.windowState });
  const end = Date.now() + secs * 1000;
  while (Date.now() < end) { await sleep(Math.min(30, (end - Date.now()) / 1000)); dlog('tick', { name, vis: await vis() }); }
}
dlog('done');
if (!process.env.KEEP) await browser.close();
else await new Promise(() => {}); // stay connected (publishing) until killed
