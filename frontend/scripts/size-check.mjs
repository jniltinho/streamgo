// The size check of design U8 (add-studio-interactions): run after a production build made with SIZE_CHECK=1 (which
// writes Vite's manifest and the list of the chunks that hold UI SDK), it measures in bytes after gzip the entry, and
// the sign-in, studio and guest routes with the shared chunks they import and their CSS, and fails when one passes
// its ceiling (10 % above the size measured when the ceiling was set) or when a chunk with UI SDK's code is in the
// static closure of the entry or of the sign-in route (the library is loaded right after them, not before the first
// paint). `node scripts/size-check.mjs` prints the sizes; a ceiling is raised only on purpose.
import { readFileSync, rmSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'

// measured on 2026-11-18 with UI SDK 0.1.0-beta.10, the toast stack and the dialog host loaded after the entry and
// the preferences modal when it is opened (entry 98027, sign-in 102298, studio 327592, guest 317091; with the hosts
// in the entry 149795, 156152, 336022 and 325190; before UI SDK entry 95845, studio 255463, guest 252181), plus 10 %
const CEILINGS = { entry: 107830, login: 112528, studio: 360352, guest: 348801 }

const dist = join(dirname(fileURLToPath(import.meta.url)), '../../web/dist')
const manifest = JSON.parse(readFileSync(join(dist, '.vite/manifest.json'), 'utf8'))
const uiSdk = new Set(JSON.parse(readFileSync(join(dist, '.vite/ui-sdk-chunks.json'), 'utf8')))
// fail closed: a build where no chunk holds the library means the list is wrong, not that the library is gone
if (uiSdk.size === 0) throw new Error('size check: no chunk holds UI SDK (is the ui-sdk-chunks plugin running?)')
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
const login = new Set([...app, ...closure('src/views/LoginView.vue')])
const sizes = {
  entry: total(app),
  login: total(login),
  studio: route('src/views/StudioView.vue'),
  guest: route('src/views/JoinView.vue'),
}
const hosts = new Set([...closure('src/components/ui/ToastStack.vue'), ...closure('src/components/ui/DialogHost.vue')])
const hostsOnly = [...hosts].filter((f) => !app.has(f))
rmSync(join(dist, '.vite'), { recursive: true, force: true }) // the manifest is not part of the embedded app

let failed = false
for (const [name, size] of Object.entries(sizes)) {
  const over = CEILINGS[name] > 0 && size > CEILINGS[name]
  failed ||= over
  console.log(`${over ? 'FAIL' : 'ok  '} ${name.padEnd(6)} ${size} B gzip (ceiling ${CEILINGS[name]} B)`)
}
console.log(`     hosts  ${total(hostsOnly)} B gzip (loaded right after the entry, not measured against a ceiling)`)
for (const [name, files] of [
  ['entry', app],
  ['sign-in route', login],
]) {
  const leaked = [...files].filter((f) => uiSdk.has(f))
  if (leaked.length === 0) continue
  failed = true
  console.log(`FAIL UI SDK is in the static closure of the ${name}: ${leaked.join(', ')}`)
}
if (failed) {
  console.error('size check: a bundle passed its ceiling or holds UI SDK (design U8 of add-studio-interactions)')
  process.exit(1)
}
