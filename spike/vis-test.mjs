// How to get a really hidden tab with Playwright-driven headed Chrome on Xvfb (diagnostic)
import { chromium } from 'playwright-core';
if (!process.env.DISPLAY || process.env.DISPLAY === ':0') throw new Error('run under xvfb-run');
const { WAYLAND_DISPLAY, ...xenv } = process.env;
const b = await chromium.launch({ env: xenv, executablePath: '/usr/bin/google-chrome', headless: false,
  ignoreDefaultArgs: ['--disable-background-timer-throttling', '--disable-backgrounding-occluded-windows', '--disable-renderer-backgrounding'],
  args: ['--ozone-platform=x11'] });
const ctx = await b.newContext({ viewport: null }); const A = await ctx.newPage(); await A.goto('http://127.0.0.1:18080/whiptest.html');
const cdp = await ctx.newCDPSession(A);
const win = async (p) => (await (await ctx.newCDPSession(p)).send('Browser.getWindowForTarget')).windowId;
const v = () => A.evaluate(() => document.visibilityState);
console.log('A window', await win(A), 'vis', await v());
const B = await ctx.newPage(); await B.bringToFront(); await new Promise(r => setTimeout(r, 1000));
console.log('B window', await win(B), 'A vis after B front', await v());
await cdp.send('Emulation.setFocusEmulationEnabled', { enabled: false }); await new Promise(r => setTimeout(r, 500));
console.log('A vis, focus emulation off', await v());
// a tab in A's own window, opened by the page itself
const [C] = await Promise.all([ctx.waitForEvent('page'), A.evaluate(() => window.open('about:blank', '_blank'))]);
await new Promise(r => setTimeout(r, 1000));
console.log('C (window.open) window', await win(C), 'A vis', await v());
await C.bringToFront(); await new Promise(r => setTimeout(r, 1000)); console.log('after C.bringToFront A vis', await v());
await (await ctx.newCDPSession(C)).send('Emulation.setFocusEmulationEnabled', { enabled: false });
await (await ctx.newCDPSession(C)).send('Page.bringToFront'); await new Promise(r => setTimeout(r, 1000)); console.log('C emu off + Page.bringToFront: A vis', await v());
await b.close();
