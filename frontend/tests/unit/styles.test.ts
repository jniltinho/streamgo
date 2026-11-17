// The style check of the ui-themes spec: components use role tokens for colour and the shared radius tokens for
// corners. "Components" are the .vue files and the TypeScript outside the theme files and the compositor.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'

const src = join(__dirname, '../../src')

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f)
    return statSync(p).isDirectory() ? files(p) : [p]
  })
}

const components = files(src)
  .map((p) => relative(src, p))
  .filter((p) => /\.(vue|ts)$/.test(p) && !p.startsWith('themes/') && !p.startsWith('core/compositor'))

const literalColour = /#[0-9a-f]{3,8}\b|\brgba?\(|\bhsla?\(/gi

describe('style check', () => {
  it('finds no literal colour outside the theme files and the compositor', () => {
    const found = components.flatMap((p) =>
      [...readFileSync(join(src, p), 'utf8').matchAll(literalColour)].map((m) => `${p}: ${m[0]}`),
    )
    expect(found).toEqual([])
  })

  it('every border radius in a component or the base stylesheet is a shared token or the round token', () => {
    const allowedValue = /^(0|var\(--radius-(sm|md|lg|round)\)|calc\(var\(--radius-lg\) - 1px\)|\s)+$/
    const found: string[] = []
    for (const p of [...components, 'style.css']) {
      const text = readFileSync(join(src, p), 'utf8').replace(/\/\*[\s\S]*?\*\/|<!--[\s\S]*?-->/g, '')
      for (const m of text.matchAll(/border(?:-[a-z]+)*-radius\s*:\s*([^;"'}]+)/g)) {
        if (!allowedValue.test(m[1]!.trim())) found.push(`${p}: ${m[0]}`)
      }
      // Tailwind utilities: rounded-sm / -md / -lg read the shared tokens; anything else is a new radius
      for (const m of text.matchAll(/(?<=["'\s])rounded(?:-[a-z0-9[\]().%-]+)?(?=["'\s;])/g)) {
        if (!/^rounded(-[trblse]{1,2})?-(sm|md|lg|none)$/.test(m[0])) found.push(`${p}: ${m[0]}`)
      }
    }
    expect(found).toEqual([])
  })

  it('declares the three radii once, in rem, in the base stylesheet', () => {
    const css = readFileSync(join(src, 'style.css'), 'utf8')
    expect([...css.matchAll(/--radius-(sm|md|lg):\s*([^;]+);/g)].map((m) => `${m[1]} ${m[2]}`)).toEqual([
      'sm 0.125rem',
      'md 0.1875rem',
      'lg 0.25rem',
    ])
  })
})
