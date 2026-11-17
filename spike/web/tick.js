// Web Worker frame clock: posts a tick every 1/fps s, drift-corrected (not requestAnimationFrame).
let fps = 30, t0 = 0, n = 0;
onmessage = (e) => { fps = e.data.fps; t0 = performance.now(); n = 0; loop(); };
function loop() { postMessage(n++); setTimeout(loop, Math.max(0, t0 + n * 1000 / fps - performance.now())); }
