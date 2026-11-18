// The size check of design U8 (add-studio-interactions): run after a production build made with SIZE_CHECK=1 (which
// writes Vite's manifest), it measures in bytes after gzip the entry, and the studio and guest routes with the
// shared chunks they import and their CSS, and fails when one passes its ceiling (10 % above the size measured when
// the ceiling was set). `node scripts/size-check.mjs` prints the sizes; a ceiling is raised only on purpose.
import { readFileSync, rmSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

// measured on 2026-11-18 with UI SDK 0.1.0-beta.10 and the dialogs and toasts on it (entry 148915, studio 325082,
// guest 321742; with the menus, tooltips and meter only 98733, 307538 and 304177; before UI SDK 95845, 255463 and
// 252181), plus 10 %
const CEILINGS = { entry: 163807, studio: 357590, guest: 353916 }

const dist = join(dirname(fileURLToPath(import.meta.url)), '../../web/dist')
const manifest = JSON.parse(readFileSync(join(dist, '.vite/manifest.json'), 'utf8'))
const gz = (file) => gzipSync(readFileSync(join(dist, file)), { level: 9 }).length

// every file a chunk loads before it runs: itself, its CSS, and the same for each chunk it imports statically. A key
// missing from the manifest is an error: a moved route or a lost import would otherwise measure as nothing.
function closure(key, seen = new Set()) {
  const c = manifest[key]
  if (!c) throw new Error(`size check: ${key} is not in the build's manifest (moved or renamed? update this script)`)
  if (seen.has(c.file)) return seen
  seen.add(c.file)
  for (const css of c.css ?? []) seen.add(css)
  for (const i of c.imports ?? []) closure(i, seen)
  return seen
}
const total = (files) => [...files].reduce((n, f) => n + gz(f), 0)

const entries = Object.keys(manifest).filter((k) => manifest[k].isEntry)
if (entries.length !== 1) throw new Error(`size check: the build has ${entries.length} entries, not one`)
const app = closure(entries[0])
// a route opened cold loads the entry and everything the route imports
const route = (key) => total(new Set([...app, ...closure(key)]))
const sizes = {
  entry: total(app),
  studio: route('src/views/StudioView.vue'),
  guest: route('src/views/JoinView.vue'),
}
rmSync(join(dist, '.vite'), { recursive: true, force: true }) // the manifest is not part of the embedded app

let failed = false
for (const [name, size] of Object.entries(sizes)) {
  const over = CEILINGS[name] > 0 && size > CEILINGS[name]
  failed ||= over
  console.log(`${over ? 'FAIL' : 'ok  '} ${name.padEnd(6)} ${size} B gzip (ceiling ${CEILINGS[name]} B)`)
}
if (failed) {
  console.error('size check: a bundle passed its ceiling (design U8 of add-studio-interactions)')
  process.exit(1)
}
