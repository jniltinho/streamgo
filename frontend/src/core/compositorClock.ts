// The program's frame clock (design D2): a dedicated Web Worker posting the tick number every 1/fps s,
// drift-corrected. Not requestAnimationFrame, which stops in a hidden tab. Adapted from spike/web/tick.js.
let fps = 30
let t0 = 0
let n = 0

function loop() {
  postMessage(n++)
  setTimeout(loop, Math.max(0, t0 + (n * 1000) / fps - performance.now()))
}

onmessage = (e: MessageEvent<{ fps: number }>) => {
  fps = e.data.fps
  t0 = performance.now()
  n = 0
  loop()
}

export {}
