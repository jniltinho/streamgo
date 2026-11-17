// Portions adapted from MeetHub (MIT): frontend/tests/e2e/02-themes.spec.ts
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test, type Page } from '@playwright/test'
import { themeCatalog } from '../../src/themes/catalog'
import { ANA, recordFirstTheme, stubApi } from './helpers'

const here = dirname(fileURLToPath(import.meta.url))

interface Recipe {
  id: string
  fg: string
  bg: string[]
  size: 'normal' | 'large' | 'ui'
  aaaOnly?: boolean
  segmentedOnly?: boolean
}
const matrix = JSON.parse(readFileSync(join(here, '../../src/themes/matrix.json'), 'utf8')) as {
  recipes: Recipe[]
  pending: Record<string, string[]>
  aaa: string[]
}
const recipes = matrix.recipes.filter((r) => !r.segmentedOnly)
const meethub = JSON.parse(readFileSync(join(here, 'fixtures/meethub-roles.json'), 'utf8')) as {
  themes: Record<string, Record<string, number[]>>
}

// every role of the contract: the --color-* custom properties declared in style.css
const contractRoles = [
  ...new Set(
    [...readFileSync(join(here, '../../src/style.css'), 'utf8').matchAll(/^\s+--color-([a-z0-9-]+):/gm)].map(
      (m) => m[1]!,
    ),
  ),
]

// resolve evaluates roles in the page: each role's computed color as sRGB [r, g, b, a] (0-255) through a canvas.
async function resolve(page: Page, theme: string, roles: string[]) {
  return page.evaluate(
    ({ theme, roles }) => {
      document.documentElement.setAttribute('data-theme', theme)
      const probe = document.createElement('div')
      document.body.append(probe)
      const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true })!
      const out: Record<string, number[] | null> = {}
      for (const role of roles) {
        probe.style.color = 'rgb(1, 2, 3)'
        probe.style.color = `var(--color-${role})`
        const c = getComputedStyle(probe).color
        if (!c || c === 'rgb(1, 2, 3)' || !CSS.supports('color', c)) {
          out[role] = null
          continue
        }
        ctx.clearRect(0, 0, 1, 1)
        ctx.fillStyle = '#000'
        ctx.fillStyle = c
        ctx.fillRect(0, 0, 1, 1)
        const [r, g, b, a] = ctx.getImageData(0, 0, 1, 1).data
        out[role] = [r!, g!, b!, a!]
      }
      probe.remove()
      return out
    },
    { theme, roles },
  )
}

const lum = ([r, g, b]: number[]) => {
  const ch = (v: number) => {
    const s = v / 255
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * ch(r!) + 0.7152 * ch(g!) + 0.0722 * ch(b!)
}
const over = (top: number[], under: number[]) => {
  const a = top[3]! / 255
  return [0, 1, 2].map((i) => top[i]! * a + under[i]! * (1 - a)).concat(255)
}
const ratio = (a: number[], b: number[]) => {
  const [l1, l2] = [lum(a), lum(b)].sort((x, y) => y - x)
  return (l1! + 0.05) / (l2! + 0.05)
}

// the computed corner radii of the gallery's controls and containers
async function radii(page: Page) {
  return page.evaluate(() => {
    const of = (sel: string) => [
      ...new Set([...document.querySelectorAll(sel)].map((e) => getComputedStyle(e).borderRadius)),
    ]
    return {
      controls: of('.btn, .btn-ghost, .btn-danger, .inp'),
      containers: of('.card, .alert, .toast, .stage, dialog.pnl'),
    }
  })
}

async function openGallery(page: Page) {
  await stubApi(page)
  await page.goto('/__gallery')
  await expect(page.getByRole('heading', { name: 'Feedback gallery' })).toBeVisible()
}

test.describe('every theme', () => {
  test.beforeEach(async ({ page }) => openGallery(page))

  for (const theme of themeCatalog) {
    test(`${theme.id}: contract roles resolve, the matrix holds, corners are 3 and 4 px`, async ({ page }) => {
      const roles = [...new Set([...contractRoles, ...recipes.flatMap((r) => [r.fg, ...r.bg]), 'page'])]
      const colors = await resolve(page, theme.id, roles)
      expect(
        roles.filter((r) => colors[r] === null),
        `unresolved roles in ${theme.id}`,
      ).toEqual([])

      const aaa = matrix.aaa.includes(theme.id)
      const failures: string[] = []
      for (const r of recipes) {
        if (r.aaaOnly && !aaa) continue
        let bg = [255, 255, 255, 255]
        for (const layer of [...r.bg, 'page'].reverse()) bg = over(colors[layer]!, bg)
        const fg = over(colors[r.fg]!, bg)
        const need = r.size === 'normal' ? (aaa ? 7 : 4.5) : aaa && r.size === 'large' ? 4.5 : 3
        const got = ratio(fg, bg)
        if (got < need - 0.01) failures.push(`${r.id} ${got.toFixed(2)} < ${need}`)
      }
      expect(failures, `contrast failures in ${theme.id}`).toEqual(matrix.pending[theme.id] ?? [])

      // the shared corner radii at 1920x1080: 3 px on controls, 4 px on containers, in every theme
      await page.locator('[data-open=toasts]').click()
      await expect(page.locator('.toast')).toHaveCount(4)
      expect(await radii(page)).toEqual({ controls: ['3px'], containers: ['4px'] })
    })
  }

  // ui-themes "Carried-over themes look the same": every role token as MeetHub's theme of the same id resolves it
  for (const theme of themeCatalog.filter((t) => !t.id.startsWith('streamgo-'))) {
    test(`${theme.id}: every role token equals MeetHub's`, async ({ page }) => {
      const want = meethub.themes[theme.id]!
      const got = await resolve(page, theme.id, Object.keys(want))
      const diff = Object.keys(want).filter((r) => JSON.stringify(got[r]) !== JSON.stringify(want[r]))
      expect(
        diff.map((r) => `${r}: ${JSON.stringify(got[r])} != ${JSON.stringify(want[r])}`),
        `roles that differ from MeetHub in ${theme.id}`,
      ).toEqual([])
    })
  }

  test('streamgo-dark is the chosen design palette', async ({ page }) => {
    const want: Record<string, string> = {
      page: '#0c1020',
      surface: '#151b31',
      'surface-2': '#1d2442',
      line: '#2c3458',
      fg: '#f2f4fb',
      'fg-muted': '#b9c0d8',
      action: '#3d41cf',
      'on-action': '#ffffff',
      accent: '#b3b8ff',
      ok: '#4ade80',
      warn: '#fbbf24',
      danger: '#9b1c1c',
      err: '#fca5a5',
      'on-air': '#f0464f',
      'stage-frame': '#080b17',
    }
    const c = await resolve(page, 'streamgo-dark', Object.keys(want))
    const hex = (v: number[]) =>
      '#' +
      v
        .slice(0, 3)
        .map((n) => n.toString(16).padStart(2, '0'))
        .join('')
    expect(Object.fromEntries(Object.keys(want).map((r) => [r, hex(c[r]!)]))).toEqual(want)
    expect(ratio(c['on-action']!, c.action!)).toBeGreaterThanOrEqual(7)
  })
})

test.describe('scale', () => {
  for (const [w, h, root, control, container] of [
    [1920, 1080, '16px', '3px', '4px'],
    [2560, 1440, '21.3333px', '4px', '5.33333px'],
    [3840, 2160, '32px', '6px', '8px'],
  ] as const) {
    test(`${w}x${h}: the root font size is ${root} and the corners keep their proportion`, async ({ page }) => {
      await page.setViewportSize({ width: w, height: h })
      await openGallery(page)
      await page.locator('[data-open=toasts]').click()
      await expect(page.locator('.toast')).toHaveCount(4)
      expect(await page.evaluate(() => getComputedStyle(document.documentElement).fontSize)).toBe(root)
      expect(await radii(page)).toEqual({ controls: [control], containers: [container] })
    })
  }
})

test.describe('theme precedence', () => {
  test('first visit in a dark browser: streamgo-dark from the first paint', async ({ browser }) => {
    const ctx = await browser.newContext({ colorScheme: 'dark' })
    const page = await ctx.newPage()
    await stubApi(page)
    const first = await recordFirstTheme(page)
    await page.goto('/login')
    expect(await first()).toBe('streamgo-dark')
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'streamgo-dark')
    await ctx.close()
  })

  test('first visit in a light browser: streamgo-light from the first paint', async ({ browser }) => {
    const ctx = await browser.newContext({ colorScheme: 'light' })
    const page = await ctx.newPage()
    await stubApi(page)
    const first = await recordFirstTheme(page)
    await page.goto('/login')
    expect(await first()).toBe('streamgo-light')
    await ctx.close()
  })

  test('a choice made before sign-in applies at once and survives a reload without a flash', async ({ page }) => {
    await openGallery(page)
    await page.locator("[data-gallery-picker] [data-theme-id='matrix']").click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'matrix')
    await expect(page.locator("[data-gallery-picker] [data-theme-id='matrix']")).toHaveAttribute('aria-checked', 'true')
    const first = await recordFirstTheme(page)
    await page.reload()
    expect(await first()).toBe('matrix')
  })

  test('signed in: the choice is saved in the user preferences', async ({ page }) => {
    const api = await stubApi(page, ANA)
    await page.goto('/broadcasts')
    await page.locator('summary[aria-label=Theme]').click()
    await page.locator("[data-theme-id='winamp']").click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'winamp')
    await expect.poll(() => api.patches).toEqual([{ theme: 'winamp' }])
  })

  test('the user preference wins over the theme remembered in the browser', async ({ browser }) => {
    const ctx = await browser.newContext({ colorScheme: 'dark' })
    const page = await ctx.newPage()
    await page.addInitScript(() => localStorage.setItem('streamgo.theme', 'matrix'))
    await stubApi(page, { ...ANA, theme: 'streamgo-light' })
    await page.goto('/broadcasts')
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'streamgo-light')
    await ctx.close()
  })

  test('signing out returns a shared browser to the default theme', async ({ page }) => {
    await stubApi(page, { ...ANA, theme: 'winamp' })
    await page.goto('/broadcasts')
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'winamp')
    await page.getByRole('button', { name: 'Sign out' }).click()
    await expect(page).toHaveURL('/login')
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'streamgo-dark')
  })
})
