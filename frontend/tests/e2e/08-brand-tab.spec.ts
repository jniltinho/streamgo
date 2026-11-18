// name-tags (add-name-tag-styles): the studio's Brand tab with the API stubbed and no LiveKit (the studio stays
// connecting, its side panel shown). The stage patch is answered by a fake that keeps the state; the real studio,
// the program and a guest's view are in 20-studio.
import { expect, test, type Page } from '@playwright/test'
import { ANA } from './helpers'
import { broadcast, fakeApi } from './pages-api'

interface Stage {
  v: number
  accent: string
  hide_names: boolean
  hide_screen_names: boolean
  name_style: string
  [k: string]: unknown
}

async function openBrand(page: Page) {
  const b = broadcast('Weekly Product Live')
  const api = await fakeApi(page, ANA, true, { broadcasts: [b] })
  const stage: Stage = {
    v: 1,
    on_air: false,
    layout: 'grid',
    sources: [],
    banner: null,
    logo: null,
    accent: '#3b46d8',
    hide_names: false,
    hide_screen_names: false,
    name_style: 'bar',
    taglines: {},
  }
  const patches: Record<string, unknown>[] = []
  await page.route('**/studio-token', () => {}) // no LiveKit here
  await page.route(`**/api/v1/broadcasts/${b.id}/stage`, async (route) => {
    const body = route.request().postDataJSON() as Record<string, unknown>
    patches.push(body)
    Object.assign(stage, body, { v: stage.v + 1 })
    await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ stage }) })
  })
  await page.goto(`/studio/${b.id}`)
  await page.getByRole('tab', { name: 'Brand' }).click()
  await expect(page.getByRole('radiogroup', { name: 'Style of the name tags' })).toBeVisible()
  return { api, stage, patches }
}

const group = (page: Page) => page.getByRole('radiogroup', { name: 'Style of the name tags' })

test('Choose a style: a click on Solid patches the stage and Solid is the chosen one', async ({ page }) => {
  const { patches } = await openBrand(page)
  const names = ['Bar', 'Solid', 'Strip', 'Text only', 'Two lines']
  await expect(group(page).getByRole('radio')).toHaveCount(5)
  for (const name of names) await expect(group(page).getByRole('radio', { name })).toBeVisible()
  await expect(group(page).getByRole('radio', { name: 'Bar' })).toHaveAttribute('aria-checked', 'true')
  await group(page).getByRole('radio', { name: 'Solid' }).click()
  await expect(group(page).getByRole('radio', { name: 'Solid' })).toHaveAttribute('aria-checked', 'true')
  expect(patches).toEqual([{ name_style: 'solid' }])
})

test('Keyboard: the right arrow chooses and applies the next style; each preview has an accessible name', async ({
  page,
}) => {
  const { patches } = await openBrand(page)
  await group(page).getByRole('radio', { name: 'Bar' }).focus()
  await page.keyboard.press('ArrowRight')
  await expect(group(page).getByRole('radio', { name: 'Solid' })).toBeFocused()
  await expect(group(page).getByRole('radio', { name: 'Solid' })).toHaveAttribute('aria-checked', 'true')
  await page.keyboard.press('ArrowRight')
  await expect(group(page).getByRole('radio', { name: 'Strip' })).toHaveAttribute('aria-checked', 'true')
  expect(patches).toEqual([{ name_style: 'solid' }, { name_style: 'strip' }])
  for (const r of await group(page).getByRole('radio').all()) expect(await r.getAttribute('aria-label')).toBeNull()
  // the name comes from the card's text; the picture is hidden from screen readers
  await expect(group(page).locator('canvas')).toHaveCount(5)
  for (const c of await group(page).locator('canvas').all())
    expect(await c.evaluate((e) => !!e.closest('[aria-hidden=true]'))).toBe(true)
})

test('The preview is the program: each preview is drawn by the program function, in the broadcast colour', async ({
  page,
}) => {
  await openBrand(page)
  // each preview canvas against the same function drawing on a fresh canvas of its size
  const same = (accent: string) =>
    page.evaluate(async (accent) => {
      const url = '/src/core/compositor.ts' // the app's own module, served by the dev server
      const m = await import(/* @vite-ignore */ url)
      const out: Record<string, boolean> = {}
      for (const c of document.querySelectorAll<HTMLCanvasElement>('canvas[data-preview]')) {
        const ref = Object.assign(document.createElement('canvas'), { width: c.width, height: c.height })
        m.drawNameTagPreview(ref, 'Your name', 'Second line', accent, c.dataset.preview)
        const a = c.getContext('2d')!.getImageData(0, 0, c.width, c.height).data
        const b = ref.getContext('2d')!.getImageData(0, 0, c.width, c.height).data
        out[c.dataset.preview!] = a.length === b.length && a.every((v, i) => v === b[i])
      }
      return out
    }, accent)
  const all = { bar: true, solid: true, strip: true, text: true, 'two-line': true }
  await expect.poll(() => same('#3b46d8')).toEqual(all)
  // the solid preview's plate is the broadcast's accent; a new accent redraws every preview
  await page.getByRole('radio', { name: '#4ade80' }).click()
  await expect.poll(() => same('#4ade80')).toEqual(all)
})

test('Depends on the first switch: names off disables the screens switch, which says why', async ({ page }) => {
  const { patches } = await openBrand(page)
  const names = page.getByRole('switch', { name: "Show each person's name on the program" })
  const screens = page.getByRole('switch', { name: 'Show names on shared screens' })
  await expect(names).toHaveAttribute('data-name-tags', '')
  await expect(names).toHaveAttribute('aria-checked', 'true')
  await screens.click()
  await expect(screens).toHaveAttribute('aria-checked', 'false')
  await names.click()
  await expect(names).toHaveAttribute('aria-checked', 'false')
  await expect(screens).toBeDisabled()
  await expect(screens).toHaveAccessibleDescription('The names are off on the program.')
  await names.click()
  await expect(screens).toBeEnabled()
  expect(patches).toEqual([{ hide_screen_names: true }, { hide_names: true }, { hide_names: false }])
})

test('Both languages: the Brand tab in Brazilian Portuguese', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('streamgo.language', 'pt-BR'))
  const b = broadcast('Ao vivo')
  await fakeApi(page, { ...ANA, language: 'pt-BR' }, true, { broadcasts: [b] })
  await page.route('**/studio-token', () => {})
  await page.goto(`/studio/${b.id}`)
  await page.getByRole('tab', { name: 'Marca' }).click()
  const g = page.getByRole('radiogroup', { name: 'Estilo das etiquetas de nome' })
  for (const name of ['Barra', 'Sólido', 'Faixa', 'Só texto', 'Duas linhas'])
    await expect(g.getByRole('radio', { name })).toBeVisible()
  await expect(page.getByRole('switch', { name: 'Mostrar o nome de cada pessoa no programa' })).toBeVisible()
  await expect(page.getByRole('switch', { name: 'Mostrar os nomes nas telas compartilhadas' })).toBeVisible()
})
