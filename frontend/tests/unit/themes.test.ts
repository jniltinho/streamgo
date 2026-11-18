// Portions adapted from MeetHub (MIT): frontend/tests/unit/themes.test.ts
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { themeCatalog } from '@/themes/catalog'

const root = join(__dirname, '../..')
const themeFiles = () => readdirSync(join(root, 'src/themes')).filter((f) => f.endsWith('.css'))
const themeCss = (f: string) => readFileSync(join(root, 'src/themes', f), 'utf8')

// registry entries from the Go source of truth
function registry(): { id: string; label: string; scheme: string }[] {
  const go = readFileSync(join(root, '../internal/theme/theme.go'), 'utf8')
  return [...go.matchAll(/\{ID: "([^"]+)", Label: "([^"]*)", Scheme: "(light|dark)"/g)].map((m) => ({
    id: m[1]!,
    label: m[2]!,
    scheme: m[3]!,
  }))
}

// ids that have a [data-theme='id'] rule in the theme CSS files
function styled(): Set<string> {
  const ids = new Set<string>()
  for (const f of themeFiles()) {
    for (const m of themeCss(f).matchAll(/\[data-theme=['"]([a-z0-9-]+)['"]\]/g)) ids.add(m[1]!)
  }
  return ids
}

describe('theme lockstep', () => {
  const reg = registry()
  it('reads the 28 themes of the catalog scenario from the Go registry, in order', () => {
    expect(reg.map((t) => t.id)).toEqual([
      'streamgo-dark',
      'streamgo-light',
      'default',
      'admanager',
      'breeze',
      'breeze-dark',
      'violet',
      'violet-dark',
      'aurora',
      'aurora-dark',
      'crystal',
      'crystal-dark',
      'cone',
      'classic-blue',
      'classic-gray',
      'graphite',
      'grafana',
      'rabbitmq',
      'matrix',
      'winamp',
      'pink-panther',
      'high-contrast',
      'macos',
      'macos-dark',
      'macos-retro',
      'ubuntu',
      'obs-dark',
      'obs-light',
    ])
  })
  it('the picker catalog equals the registry, in order, label and scheme', () => {
    expect(themeCatalog.map((t) => ({ id: t.id, label: t.label, scheme: t.scheme }))).toEqual(reg)
  })
  it('every registry id has a style rule and every style rule a registry id', () => {
    expect([...styled()].sort()).toEqual(reg.map((t) => t.id).sort())
  })
  it('MeetHub’s own pair is not carried over under its old ids', () => {
    expect(themeFiles().filter((f) => /meethub/.test(themeCss(f).replace(/^\/\*.*\*\//, '')))).toEqual([])
  })
  it('no theme file declares a corner radius: the three shared radii are the contract', () => {
    expect(themeFiles().filter((f) => /border-radius|--radius-/.test(themeCss(f)))).toEqual([])
  })
  it('every theme file is imported by style.css', () => {
    const css = readFileSync(join(root, 'src/style.css'), 'utf8')
    expect(themeFiles().filter((f) => !css.includes(`@import './themes/${f}'`))).toEqual([])
  })
})
