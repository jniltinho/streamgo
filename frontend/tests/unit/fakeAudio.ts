// A stand-in for the Web Audio API in jsdom (which has none): every node and parameter call is recorded, so a
// test can read what was scheduled and what was connected to what.
export interface ParamCall {
  fn: string
  value: number
  at: number
}

export class FakeParam {
  calls: ParamCall[] = []
  value = 0
  setValueAtTime(value: number, at: number) {
    this.calls.push({ fn: 'set', value, at })
  }
  linearRampToValueAtTime(value: number, at: number) {
    this.calls.push({ fn: 'linear', value, at })
  }
  exponentialRampToValueAtTime(value: number, at: number) {
    this.calls.push({ fn: 'exp', value, at })
  }
}

export class FakeNode {
  outputs: unknown[] = []
  constructor(readonly context: FakeAudioContext) {}
  connect<T>(n: T): T {
    this.outputs.push(n)
    return n
  }
  disconnect() {
    this.outputs = []
  }
}

export class FakeOscillator extends FakeNode {
  type = 'sine'
  frequency = new FakeParam()
  started = -1
  stopped = -1
  onended: (() => void) | null = null
  start(at: number) {
    this.started = at
  }
  stop(at: number) {
    this.stopped = at
  }
}

export class FakeGain extends FakeNode {
  gain = new FakeParam()
}

export class FakeAnalyser extends FakeNode {
  fftSize = 2048
  samples = 0
  reads = 0
  getFloatTimeDomainData(d: Float32Array) {
    this.reads++
    d.fill(this.samples)
  }
}

export class FakeAudioContext {
  static all: FakeAudioContext[] = []
  /** what a new context starts as ('suspended' in a tab nothing was clicked in) */
  static initial: AudioContextState = 'running'
  state: AudioContextState = FakeAudioContext.initial
  currentTime = 0
  destination = new FakeNode(this)
  oscillators: FakeOscillator[] = []
  gains: FakeGain[] = []
  analysers: FakeAnalyser[] = []
  sources: FakeNode[] = []
  onstatechange: (() => void) | null = null
  constructor() {
    FakeAudioContext.all.push(this)
  }
  resume() {
    if (this.state !== 'closed') this.state = 'running'
    return Promise.resolve()
  }
  close() {
    this.state = 'closed'
    return Promise.resolve()
  }
  createOscillator() {
    const o = new FakeOscillator(this)
    this.oscillators.push(o)
    return o
  }
  createGain() {
    const g = new FakeGain(this)
    this.gains.push(g)
    return g
  }
  createAnalyser() {
    const a = new FakeAnalyser(this)
    this.analysers.push(a)
    return a
  }
  createMediaStreamSource() {
    const s = new FakeNode(this)
    this.sources.push(s)
    return s
  }
}

export const asContext = (c: FakeAudioContext) => c as unknown as AudioContext
