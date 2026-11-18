// The UI SDK wrappers in the test gallery (add-studio-interactions, design U2-U6): the menu, the tooltip and the
// meter by keyboard and pointer, the bounded menu in a small window, the state motion, and every theme with a menu
// open and a tooltip shown. The API is stubbed; the studio's own checks are in 20-studio.
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { themeCatalog } from '../../src/themes/catalog'
import { stubApi } from './helpers'

async function openGallery(page: Page) {
  await stubApi(page)
  await page.goto('/__gallery')
  await expect(page.getByRole('heading', { name: 'Feedback gallery' })).toBeVisible()
}

const menu = (page: Page, which: string) => page.locator(`[data-gallery-menu=${which}]`)
const openMenu = (page: Page) => page.getByRole('menu')
const tip = (page: Page, text: string) => page.getByRole('tooltip', { name: text })
const focused = (page: Page) => page.evaluate(() => document.activeElement?.getAttribute('data-gallery-menu'))

async function tabTo(page: Page, sel: string) {
  await page.locator('body').click({ position: { x: 2, y: 2 } })
  for (let i = 0; i < 80; i++) {
    await page.keyboard.press('Tab')
    if (await page.locator(sel).evaluate((el) => el === document.activeElement)) return
  }
  throw new Error(`Tab never reached ${sel}`)
}

test.describe('Device menus', () => {
  test.beforeEach(({ page }) => openGallery(page))

  test('Keyboard: Enter opens on Default, two downs and Enter choose Back, focus returns; Escape changes nothing', async ({
    page,
  }) => {
    await tabTo(page, '[data-gallery-menu=camera]')
    await page.keyboard.press('Enter')
    await expect(openMenu(page)).toBeVisible()
    await expect(page.getByRole('menuitemradio', { name: 'Default' })).toBeFocused()
    await expect(page.getByRole('menuitemradio', { name: 'Default' })).toHaveAttribute('aria-checked', 'true')
    await page.keyboard.press('ArrowDown')
    await page.keyboard.press('ArrowDown')
    await page.keyboard.press('Enter')
    await expect(openMenu(page)).toHaveCount(0)
    await expect(page.locator('[data-gallery-chosen]')).toHaveText('back|')
    expect(await focused(page)).toBe('camera')

    await page.keyboard.press('Enter')
    await expect(page.getByRole('menuitemradio', { name: 'Back' })).toHaveAttribute('aria-checked', 'true')
    await page.keyboard.press('ArrowUp')
    await page.keyboard.press('Escape')
    await expect(openMenu(page)).toHaveCount(0)
    await expect(page.locator('[data-gallery-chosen]')).toHaveText('back|')
    expect(await focused(page)).toBe('camera')
  })

  test('A long list in a small window: inside the window, scrolling inside itself, no page scrollbar', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 720 })
    await menu(page, 'mic').click()
    const m = openMenu(page)
    await expect(m.getByRole('menuitemradio')).toHaveCount(13)
    const box = await m.evaluate((e) => {
      const r = e.getBoundingClientRect()
      return {
        inside: r.left >= 0 && r.top >= 0 && r.right <= innerWidth && r.bottom <= innerHeight,
        scrolls: e.scrollHeight > e.clientHeight && getComputedStyle(e).overflowY === 'auto',
        page:
          document.documentElement.scrollHeight <= innerHeight && document.documentElement.scrollWidth <= innerWidth,
      }
    })
    expect(box).toEqual({ inside: true, scrolls: true, page: true })
    // the last item is reached inside the menu
    await page.keyboard.press('End')
    await expect(m.getByRole('menuitemradio').last()).toBeInViewport()
  })

  test('Nothing to choose: focusable, marked disabled, opens nothing, the tooltip says why', async ({ page }) => {
    await tabTo(page, '[data-gallery-menu=none]')
    const arrow = menu(page, 'none')
    await expect(arrow).toHaveAttribute('aria-disabled', 'true')
    await expect(arrow).not.toHaveAttribute('disabled')
    await expect(tip(page, 'No speaker found.')).toBeVisible()
    await expect(arrow).toHaveAccessibleName('Choose a speaker')
    await expect(arrow).toHaveAccessibleDescription('No speaker found.')
    await page.keyboard.press('Enter')
    await page.keyboard.press('Space')
    await expect(openMenu(page)).toHaveCount(0)
  })
})

test.describe('Tooltips', () => {
  test.beforeEach(({ page }) => openGallery(page))

  test('Focus: shown at once in the tooltip colours, no title, the name kept; Escape hides it until entered again', async ({
    page,
  }) => {
    const button = page.locator('[data-gallery-tip]')
    await tabTo(page, '[data-gallery-tip]')
    const t = tip(page, 'Device settings')
    await expect(t).toBeVisible({ timeout: 150 })
    await expect(button).not.toHaveAttribute('title')
    await expect(button).toHaveAccessibleName('Device settings')
    const colours = await t.evaluate((el) => {
      const probe = document.createElement('i')
      document.body.append(probe)
      probe.style.color = 'var(--color-on-tooltip)'
      const fg = getComputedStyle(probe).color
      probe.style.color = 'var(--color-tooltip)'
      const bg = getComputedStyle(probe).color
      probe.remove()
      const s = getComputedStyle(el)
      return [s.color === fg, s.backgroundColor === bg]
    })
    expect(colours).toEqual([true, true])
    expect(await button.evaluate((el) => el === document.activeElement)).toBe(true) // it never takes focus
    await page.keyboard.press('Escape')
    await expect(t).toBeHidden()
    await page.waitForTimeout(700)
    await expect(t).toBeHidden()
    await page.keyboard.press('Shift+Tab')
    await page.keyboard.press('Tab')
    await expect(t).toBeVisible()
  })

  test('Hover: shown after 500 ms of rest, hidden when the pointer leaves', async ({ page }) => {
    // the page's own clock, moved by the test: the delay is measured, not raced
    await page.clock.install()
    await openGallery(page)
    const button = page.locator('[data-gallery-tip]')
    const t = tip(page, 'Device settings')
    await button.hover()
    await page.clock.runFor(450)
    await expect(t).toBeHidden()
    await page.clock.runFor(100)
    await expect(t).toBeVisible()
    await page.mouse.move(2, 2)
    await page.clock.runFor(500)
    await expect(t).toBeHidden()
  })
})

test.describe('Microphone meter', () => {
  test('the meter is role=meter with its label and its text value', async ({ page }) => {
    await openGallery(page)
    const m = page.getByRole('meter', { name: 'Microphone level' })
    await expect(m).toHaveAttribute('aria-valuetext', 'Heard')
    await expect(m).toHaveAttribute('aria-valuenow', '50')
    const fill = await m.locator('[data-slot=meter-indicator]').evaluate((e) => e.getBoundingClientRect().width)
    const track = await m.locator('[data-slot=meter-track]').evaluate((e) => e.clientWidth)
    expect(Math.abs(fill / track - 0.5)).toBeLessThan(0.05)
  })
})

test.describe('State motion', () => {
  const animations = (page: Page) =>
    page.locator('[data-gallery-motion]').evaluate((root) =>
      root
        .getAnimations({ subtree: true })
        // the three effects; the buttons' own press transitions are not part of this change
        .filter((a) => a instanceof CSSAnimation)
        .map((a) => {
          const effect = a.effect as KeyframeEffect
          const props = [
            ...new Set(
              effect
                .getKeyframes()
                .flatMap((k) =>
                  Object.keys(k).filter((p) => !['offset', 'easing', 'composite', 'computedOffset'].includes(p)),
                ),
            ),
          ]
          const target = effect.target as Element
          return {
            what: target.closest('[data-gallery-toggle]')
              ? 'control'
              : target.closest('[data-gallery-speaking]')
                ? 'speaking'
                : 'live',
            duration: Number(effect.getTiming().duration),
            props,
          }
        }),
    )

  test('A control changes: its icon moves for at most 300 ms, transform and opacity only', async ({ page }) => {
    await openGallery(page)
    // read when the animation starts: a 300 ms animation may be over by the time a later read runs
    await page.locator('[data-gallery-toggle]').evaluate((button) => {
      const w = window as unknown as { started: { duration: number; props: string[] }[] }
      w.started = []
      ;(button as HTMLElement).addEventListener('animationstart', (e) => {
        const a = (e.target as Element)
          .getAnimations()
          .find((x) => x instanceof CSSAnimation && x.animationName === e.animationName)!
        const effect = a.effect as KeyframeEffect
        const skip = ['offset', 'easing', 'composite', 'computedOffset']
        const props = [
          ...new Set(effect.getKeyframes().flatMap((k) => Object.keys(k).filter((p) => !skip.includes(p)))),
        ]
        w.started.push({ duration: Number(effect.getTiming().duration), props })
      })
    })
    await page.locator('[data-gallery-toggle]').click()
    await expect
      .poll(() => page.evaluate(() => (window as unknown as { started: unknown[] }).started.length))
      .toBeGreaterThan(0)
    const control = await page.evaluate(
      () => (window as unknown as { started: { duration: number; props: string[] }[] }).started,
    )
    expect(control.length).toBe(1)
    expect(control[0]!.duration).toBeLessThanOrEqual(300)
    expect(control[0]!.props.sort()).toEqual(['opacity', 'transform'])
  })

  test('Speaking and live: the speaking bars move and the live mark pulses, transform and opacity only', async ({
    page,
  }) => {
    await openGallery(page)
    const all = await animations(page)
    expect(all.filter((a) => a.what === 'speaking').length).toBe(4)
    expect(all.filter((a) => a.what === 'live').length).toBe(1)
    for (const a of all)
      expect(
        a.props.every((p) => p === 'transform' || p === 'opacity'),
        a.what,
      ).toBe(true)
  })

  test('Reduced motion and flat mode: none of the three runs, also when that starts while they run', async ({
    page,
  }) => {
    await openGallery(page)
    expect((await animations(page)).length).toBeGreaterThan(0)
    await page.emulateMedia({ reducedMotion: 'reduce' })
    expect(await animations(page)).toEqual([])
    await page.locator('[data-gallery-toggle]').click()
    expect(await animations(page)).toEqual([])
    // every state still told by its icon and text
    await expect(page.locator('[data-gallery-toggle]')).toHaveAttribute('aria-pressed', 'false')
    await expect(page.getByRole('img', { name: 'Speaking' })).toBeVisible()
    await page.emulateMedia({ reducedMotion: 'no-preference' })
    expect((await animations(page)).length).toBeGreaterThan(0)
    await page.evaluate(() => document.documentElement.setAttribute('data-flat', 'true'))
    expect(await animations(page)).toEqual([])
    await page.locator('[data-gallery-toggle]').click()
    expect(await animations(page)).toEqual([])
    // the meter still shows the level
    await expect(page.getByRole('meter', { name: 'Microphone level' })).toHaveAttribute('aria-valuenow', '50')
  })
})

// "In every theme" / "Contrast": a menu open (an item highlighted, one checked, one focused), a tooltip shown and
// the meter half full, in each theme: no axe violation, and the painted pairs meet their ratios.
test('Contrast: every theme with a menu open, a tooltip shown and the meter half full', async ({ page }) => {
  test.setTimeout(180_000)
  await openGallery(page)
  await page.setViewportSize({ width: 1280, height: 900 })
  await tabTo(page, '[data-gallery-menu=camera]')
  await page.keyboard.press('Enter')
  await page.keyboard.press('ArrowDown') // Front highlighted and focused; Default checked
  await expect(page.getByRole('menuitemradio', { name: 'Front' })).toHaveAttribute('data-highlighted', '')
  await page.locator('[data-gallery-tip]').hover()
  await expect(tip(page, 'Device settings')).toBeVisible()
  await expect(openMenu(page)).toBeVisible()
  const failures: string[] = []
  for (const theme of themeCatalog) {
    await page.evaluate((id) => document.documentElement.setAttribute('data-theme', id), theme.id)
    const r = await new AxeBuilder({ page }).include('[data-gallery-ui]').include('[role=menu]').analyze()
    for (const v of r.violations) for (const n of v.nodes) failures.push(`${theme.id}: ${v.id} ${n.target}`)
    const ratios = await page.evaluate(() => {
      const rgb = (c: string) =>
        c
          .match(/[\d.]+/g)!
          .slice(0, 3)
          .map(Number)
      const ch = (v: number) => {
        const s = v / 255
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
      }
      const lum = (c: number[]) => 0.2126 * ch(c[0]!) + 0.7152 * ch(c[1]!) + 0.0722 * ch(c[2]!)
      const ratio = (a: string, b: string) => {
        const [x, y] = [lum(rgb(a)), lum(rgb(b))].sort((p, q) => q - p)
        return (x! + 0.05) / (y! + 0.05)
      }
      const s = (sel: string) => getComputedStyle(document.querySelector(sel)!)
      const hl = s('[role=menuitemradio][data-highlighted]')
      const checked = s('[role=menuitemradio][data-checked]')
      const menuBg = s('[role=menu]').backgroundColor
      const tipS = s('[role=tooltip]:not([hidden])')
      return {
        highlighted: ratio(hl.color, hl.backgroundColor),
        checked: ratio(checked.color, menuBg),
        tooltip: ratio(tipS.color, tipS.backgroundColor),
        fill: ratio(
          s('[data-gallery-meter] [data-slot=meter-indicator]').backgroundColor,
          s('[data-gallery-meter] [data-slot=meter-track]').backgroundColor,
        ),
      }
    })
    const need = { highlighted: 4.5, checked: 4.5, tooltip: 4.5, fill: 3 }
    for (const [k, v] of Object.entries(ratios))
      if (v < need[k as keyof typeof need] - 0.01) failures.push(`${theme.id}: ${k} ${v.toFixed(2)}`)
  }
  expect(failures).toEqual([])
})

// The wrappers of add-name-tag-styles (design N5): UiSwitch, UiRadioGroup and UiPopover in the gallery.
const tagsState = (page: Page) => page.locator('[data-gallery-tags-state]')

test.describe('Switches, radio cards and the popover', () => {
  test.beforeEach(({ page }) => openGallery(page))

  test('Switch: named, turned by a click and by Space; Depends on the first switch: disabled and says why', async ({
    page,
  }) => {
    const names = page.getByRole('switch', { name: 'Show names', exact: true })
    const screens = page.getByRole('switch', { name: 'Show names on screens' })
    await expect(names).toHaveAttribute('aria-checked', 'true')
    await expect(screens).toBeEnabled()
    await names.click()
    await expect(names).toHaveAttribute('aria-checked', 'false')
    await expect(tagsState(page)).toHaveText('false|true|bar|')
    // the second switch is disabled while the first is off, and its reason is shown and describes it
    await expect(screens).toBeDisabled()
    await expect(screens).toHaveAccessibleDescription('The names are off.')
    await expect(page.locator('[data-gallery-tags]').getByText('The names are off.')).toBeVisible()
    await names.focus()
    await page.keyboard.press('Space')
    await expect(names).toHaveAttribute('aria-checked', 'true')
    await expect(screens).toBeEnabled()
    await page.getByText('Show names on screens').click() // the label turns it too
    await expect(tagsState(page)).toHaveText('true|false|bar|')
  })

  test('Radio cards: Keyboard, one tab stop, the right arrow chooses the next, each card named', async ({ page }) => {
    const group = page.getByRole('radiogroup', { name: 'Style' })
    await expect(group.getByRole('radio')).toHaveCount(3)
    for (const name of ['Bar', 'Solid', 'Strip']) await expect(group.getByRole('radio', { name })).toBeVisible()
    await expect(group.getByRole('radio', { name: 'Bar' })).toHaveAttribute('aria-checked', 'true')
    await tabTo(page, '[data-gallery-radio] [data-value=bar]')
    await page.keyboard.press('ArrowRight')
    await expect(group.getByRole('radio', { name: 'Solid' })).toBeFocused()
    await expect(group.getByRole('radio', { name: 'Solid' })).toHaveAttribute('aria-checked', 'true')
    await expect(tagsState(page)).toHaveText('true|true|solid|')
    await page.keyboard.press('Tab') // one tab stop: the next control is the popover's button
    await expect(page.locator('[data-gallery-popover]')).toBeFocused()
    await group.getByRole('radio', { name: 'Strip' }).click()
    await expect(tagsState(page)).toHaveText('true|true|strip|')
  })

  test('Popover: opens on its field, saves with Enter and returns focus; Escape and a press outside close it', async ({
    page,
  }) => {
    const button = page.locator('[data-gallery-popover]')
    await expect(button).toHaveAccessibleName('Edit the second line')
    await button.click()
    const dialog = page.getByRole('dialog', { name: 'Second line' })
    await expect(dialog).toBeVisible()
    await expect(dialog.getByLabel('Role or company')).toBeFocused()
    await page.keyboard.type('Head of Product')
    await page.keyboard.press('Enter')
    await expect(dialog).toBeHidden()
    await expect(tagsState(page)).toHaveText('true|true|bar|Head of Product')
    await expect(button).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(dialog).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(button).toBeFocused()
    await button.click()
    await expect(dialog).toBeVisible()
    await page.mouse.click(2, 2)
    await expect(dialog).toBeHidden()
    // inside the window, no page scrollbar
    await page.setViewportSize({ width: 1280, height: 720 })
    await button.click()
    const box = await dialog.evaluate((e) => {
      const r = e.getBoundingClientRect()
      return r.left >= 0 && r.top >= 0 && r.right <= innerWidth && r.bottom <= innerHeight
    })
    expect(box).toBe(true)
  })
})

// "Contrast" of the name-tag controls: the switches (one on, one off and one disabled), the radio cards with one
// chosen and the popover open, in each theme: no axe violation, and the painted pairs meet their ratios.
test('Contrast: every theme with the switches, the radio cards and the popover open', async ({ page }) => {
  test.setTimeout(180_000)
  await openGallery(page)
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.getByRole('switch', { name: 'Show names on screens' }).click() // off
  await page.locator('[data-gallery-popover]').click()
  await expect(page.getByRole('dialog', { name: 'Second line' })).toBeVisible()
  const failures: string[] = []
  for (const theme of themeCatalog) {
    await page.evaluate((id) => document.documentElement.setAttribute('data-theme', id), theme.id)
    const r = await new AxeBuilder({ page }).include('[data-gallery-tags]').include('[role=dialog]').analyze()
    for (const v of r.violations) for (const n of v.nodes) failures.push(`${theme.id}: ${v.id} ${n.target}`)
    const ratios = await page.evaluate(() => {
      const rgb = (c: string) =>
        c
          .match(/[\d.]+/g)!
          .slice(0, 3)
          .map(Number)
      const ch = (v: number) => {
        const s = v / 255
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
      }
      const lum = (c: number[]) => 0.2126 * ch(c[0]!) + 0.7152 * ch(c[1]!) + 0.0722 * ch(c[2]!)
      const ratio = (a: string, b: string) => {
        const [x, y] = [lum(rgb(a)), lum(rgb(b))].sort((p, q) => q - p)
        return (x! + 0.05) / (y! + 0.05)
      }
      const s = (sel: string) => getComputedStyle(document.querySelector(sel)!)
      const card = s('[data-gallery-tags]').backgroundColor
      const on = s('[data-gallery-switch=names]')
      const off = s('[data-gallery-switch=screens]')
      const pop = s('[role=dialog]')
      return {
        edgeOn: ratio(on.borderTopColor, card),
        edgeOff: ratio(off.borderTopColor, card),
        thumbOn: ratio(s('[data-gallery-switch=names] [data-checked]').backgroundColor, on.backgroundColor),
        thumbOff: ratio(s('[data-gallery-switch=screens] > *').backgroundColor, off.backgroundColor),
        chosen: ratio(s('[data-gallery-radio] [aria-checked=true]').borderTopColor, card),
        popover: ratio(pop.color, pop.backgroundColor),
        hint: ratio(s('[role=dialog] .studio-meta').color, pop.backgroundColor),
      }
    })
    const need = { edgeOn: 3, edgeOff: 3, thumbOn: 3, thumbOff: 3, chosen: 3, popover: 4.5, hint: 4.5 }
    for (const [k, v] of Object.entries(ratios))
      if (v < need[k as keyof typeof need] - 0.01) failures.push(`${theme.id}: ${k} ${v.toFixed(2)}`)
  }
  expect(failures).toEqual([])
})
