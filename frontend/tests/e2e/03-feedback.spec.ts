// Portions adapted from MeetHub (MIT): frontend/tests/e2e/03-feedback.spec.ts
import { expect, test, type Locator, type Page } from '@playwright/test'
import { stubApi } from './helpers'

test.beforeEach(async ({ page }) => {
  // a native dialog would block the page: fail on any
  page.on('dialog', (d) => {
    throw new Error(`native ${d.type()} dialog: ${d.message()}`)
  })
  await stubApi(page)
  await page.goto('/__gallery')
  await expect(page.getByRole('heading', { name: 'Feedback gallery' })).toBeVisible()
})

test('toasts: four kinds, errors announced assertively, close button', async ({ page }) => {
  await page.locator('[data-open=toasts]').click()
  await expect(page.locator('.toast')).toHaveCount(4)
  // announced by the live regions, the error assertively (the cards themselves are not live)
  await expect(page.locator('[aria-live=assertive]')).toContainText('The server could not be reached.')
  await expect(page.locator('[aria-live=polite]')).toContainText('A new version is available.')
  // the info toast carries its own colour, not the success default
  const fill = (sel: string, name: string) =>
    page
      .locator(sel)
      .first()
      .evaluate((el, n) => getComputedStyle(el).getPropertyValue(n).trim(), name)
  expect(await fill('.toast.info', '--k')).toBe(await fill('html', '--color-toast-info'))
  await page.locator('.toast.err').getByRole('button', { name: 'Close' }).click()
  await expect(page.locator('.toast')).toHaveCount(3)
})

test("a toast's close button shows its focus mark in the colour of its cross", async ({ page }) => {
  // opened from the keyboard, so the focus that follows is a visible one
  await page.locator('[data-open=toasts]').focus()
  await page.keyboard.press('Enter')
  const close = page.locator('.toast.err').getByRole('button', { name: 'Close' })
  await close.focus()
  const mark = await close.evaluate((el) => {
    const c = getComputedStyle(el)
    return { style: c.outlineStyle, outline: c.outlineColor, ink: c.color }
  })
  expect(mark.style).toBe('solid')
  expect(mark.outline).toBe(mark.ink)
})

test('confirm dialog: modal, safe focus, Escape cancels and focus returns', async ({ page }) => {
  const opener = page.locator('[data-open=confirm]')
  await opener.focus()
  await page.keyboard.press('Enter')
  const dialog = page.getByRole('alertdialog', { name: 'Confirmation' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(opener).toBeFocused()
})

test('typed confirm enables only with the expected text', async ({ page }) => {
  await page.locator('[data-open=typed]').click()
  const dialog = page.getByRole('alertdialog')
  const ok = dialog.getByRole('button', { name: 'Confirm' })
  await expect(ok).toBeDisabled()
  await dialog.getByRole('textbox').fill('ANA@example.com ')
  await expect(ok).toBeEnabled()
  await ok.click()
  await expect(dialog).toBeHidden()
})

test('alert dialog closes with OK', async ({ page }) => {
  await page.locator('[data-open=alert]').click()
  const dialog = page.getByRole('dialog', { name: 'Notice' })
  await expect(dialog.getByRole('button', { name: 'OK' })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(dialog).toBeHidden()
})

test('validation: no browser bubble, the themed invalid state', async ({ page }) => {
  await page.locator('[data-open=validate]').click()
  await expect(page.locator('#g-email')).toHaveClass(/invalid/)
  await expect(page.locator('#g-email')).toHaveAttribute('aria-invalid', 'true')
  await page.locator('#g-email').fill('x')
  await expect(page.locator('#g-email')).not.toHaveClass(/invalid/)
})

// studio-interactions "Dialogs and toasts on UI SDK" (design U10): the gallery's modal, the confirmations, the alerts
// and the toasts behave as they did on the native <dialog> and the popover list.
test.describe('Dialogs and toasts on UI SDK', () => {
  // runs fn(m) in the page with the app's own feedback modules (dialogs.ts and toasts.ts); with `keep`, the promise
  // it returns is kept on the window under that name, to be answered later
  const run = (page: Page, src: string, keep = '') =>
    page.evaluate(
      async ([code, name, dialogs, toasts]) => {
        const m = { ...(await import(dialogs!)), ...(await import(toasts!)) }
        const out = new Function('m', code!)(m)
        if (name) (window as unknown as Record<string, unknown>)[name] = out
        else return out
      },
      [src, keep, '/src/feedback/dialogs.ts', '/src/feedback/toasts.ts'],
    )
  const answer = (page: Page, name: string) =>
    page.evaluate((n) => (window as unknown as Record<string, Promise<unknown>>)[n], name)
  // true while focus is inside the element
  const holdsFocus = (loc: Locator) => loc.evaluate((el) => el.contains(document.activeElement))
  // a press on the page beside the dialog, where the gallery's Toasts button is
  const clickOutside = async (page: Page) => {
    const b = (await page.locator('[data-open=toasts]').boundingBox())!
    await page.mouse.click(b.x + b.width / 2, b.y + b.height / 2)
  }

  test('A modal behaves as before', async ({ page }) => {
    const opener = page.locator('[data-open=modal]')
    await opener.focus()
    await page.keyboard.press('Enter')
    const modal = page.getByRole('dialog', { name: 'Gallery modal 1' })
    await expect(modal).toBeVisible()
    // the first control of the body, not the close control of the title bar
    await expect(modal.getByLabel('Name')).toBeFocused()
    // Tab never leaves it
    for (let i = 0; i < 8; i++) {
      await page.keyboard.press('Tab')
      expect(await holdsFocus(modal)).toBe(true)
    }
    // the page behind cannot be clicked, and a click outside does not close it
    await clickOutside(page)
    await expect(modal).toBeVisible()
    await expect(page.locator('.toast')).toHaveCount(0)
    // no page scrollbar while it is open
    expect(await page.evaluate(() => document.documentElement.scrollHeight <= window.innerHeight)).toBe(true)
    await page.keyboard.press('Escape')
    await expect(modal).toBeHidden()
    await expect(opener).toBeFocused()
  })

  test('A modal over a modal', async ({ page }) => {
    await page.locator('[data-open=modal]').click()
    const lower = page.getByRole('dialog', { name: 'Gallery modal 1' })
    await lower.locator('[data-open=upper]').click()
    const upper = page.getByRole('dialog', { name: 'Gallery modal 2' })
    await expect(upper).toBeVisible()
    await expect(upper.getByLabel('Name')).toBeFocused()
    const titleIds = () =>
      page.locator('.pnl').evaluateAll((els) => els.map((el) => el.getAttribute('aria-labelledby')))
    expect(new Set(await titleIds()).size).toBe(2)
    await page.keyboard.press('Escape')
    await expect(upper).toBeHidden()
    await expect(lower).toBeVisible()
    await expect(lower.locator('[data-open=upper]')).toBeFocused()
    // the lower one is still modal: Tab stays inside, the page cannot be clicked
    for (let i = 0; i < 6; i++) {
      await page.keyboard.press('Tab')
      expect(await holdsFocus(lower)).toBe(true)
    }
    await clickOutside(page)
    await expect(lower).toBeVisible()
    await expect(page.locator('.toast')).toHaveCount(0)

    // a confirmation over it: Escape answers no and closes only the confirmation
    await lower.locator('[data-open=ask]').click()
    const confirm = page.getByRole('alertdialog', { name: 'Confirmation' })
    await expect(confirm).toBeVisible()
    await expect(confirm.getByRole('button', { name: 'Cancel' })).toBeFocused()
    expect(new Set(await titleIds()).size).toBe(2)
    await page.keyboard.press('Escape')
    await expect(confirm).toBeHidden()
    await expect(lower).toBeVisible()
    await expect(lower.locator('[data-gallery-answer]')).toHaveText('false')
    await expect(lower.locator('[data-open=ask]')).toBeFocused()
    for (let i = 0; i < 6; i++) {
      await page.keyboard.press('Tab')
      expect(await holdsFocus(lower)).toBe(true)
    }
    await page.keyboard.press('Escape')
    await expect(lower).toBeHidden()
  })

  test('A modal unmounted by its parent while its confirmation shows', async ({ page }) => {
    // what the page hides from input and from assistive technology on its own, with no dialog
    const hidden = () =>
      page.evaluate(() =>
        [...document.querySelectorAll('[inert], [aria-hidden=true], [data-inert]')].map(
          (el) => `${el.tagName}.${el.getAttribute('class')}:${el.hasAttribute('inert')}`,
        ),
      )
    const before = await hidden()
    await page.locator('[data-open=modal]').click()
    const lower = page.getByRole('dialog', { name: 'Gallery modal 1' })
    await lower.locator('[data-open=upper]').click()
    const upper = page.getByRole('dialog', { name: 'Gallery modal 2' })
    await upper.locator('[data-open=orphan]').click()
    const confirm = page.getByRole('alertdialog', { name: 'Confirmation' })
    await expect(confirm).toBeVisible()
    await page.evaluate(() => (window as unknown as { galleryLeave: () => void }).galleryLeave())
    await expect(upper).toBeHidden()
    // the confirmation still shows, keeps the focus, and settles
    await expect(confirm).toBeVisible()
    expect(await holdsFocus(confirm)).toBe(true)
    await page.keyboard.press('Escape')
    await expect(confirm).toBeHidden()
    expect(await answer(page, 'galleryAnswer')).toBe(false)
    // the focus lands in the modal still open, which is still modal
    await expect.poll(() => holdsFocus(lower)).toBe(true)
    for (let i = 0; i < 6; i++) {
      await page.keyboard.press('Tab')
      expect(await holdsFocus(lower)).toBe(true)
    }
    await page.keyboard.press('Escape')
    await expect(lower).toBeHidden()
    // nothing stays inert: the page answers clicks and keys again
    expect(await hidden()).toEqual(before)
    await page.locator('[data-open=modal]').click()
    await expect(lower).toBeVisible()
  })

  test('A confirmation behaves as before', async ({ page }) => {
    await page.locator('[data-open=confirm]').focus()
    await run(
      page,
      "return m.askConfirm('Delete the broadcast Weekly Product Live?', { danger: true, ok: 'Delete' })",
      'q',
    )
    const dialog = page.getByRole('alertdialog', { name: 'Confirmation' })
    await expect(dialog).toBeVisible()
    await expect(dialog).toHaveAccessibleDescription('Delete the broadcast Weekly Product Live?')
    // OK comes first in the markup, focus starts on Cancel
    await expect(dialog.getByRole('button')).toHaveText(['Delete', 'Cancel'])
    await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
    await clickOutside(page)
    await expect(dialog).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    expect(await answer(page, 'q')).toBe(false)
    await expect(page.locator('[data-open=confirm]')).toBeFocused()
  })

  test('A typed confirmation', async ({ page }) => {
    await run(page, "return m.confirmTyped('This cannot be undone.', 'ana@example.com')", 'q')
    const dialog = page.getByRole('alertdialog', { name: 'Safety confirmation' })
    const input = dialog.getByRole('textbox')
    await expect(input).toBeFocused()
    const ok = dialog.getByRole('button', { name: 'Confirm' })
    await expect(ok).toBeDisabled()
    await input.pressSequentially('  ANA@Example')
    await expect(ok).toBeDisabled()
    await input.pressSequentially('.COM ')
    await expect(ok).toBeEnabled()
    await ok.click()
    await expect(dialog).toBeHidden()
    expect(await answer(page, 'q')).toBe(true)
  })

  test('An alert', async ({ page }) => {
    await run(page, "return m.showAlert('Maintenance tonight at 22:00.')", 'one')
    const dialog = page.getByRole('dialog', { name: 'Notice' })
    await expect(dialog).toBeVisible()
    await expect(page.getByRole('alertdialog')).toHaveCount(0)
    await dialog.getByRole('button', { name: 'OK' }).click()
    await expect(dialog).toBeHidden()
    expect(await answer(page, 'one')).toBeUndefined()
    await run(page, "return m.showAlert('Maintenance tonight at 22:00.')", 'two')
    await expect(dialog).toBeVisible()
    await expect(dialog.getByRole('button', { name: 'OK' })).toBeFocused()
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    expect(await answer(page, 'two')).toBeUndefined()
  })

  test('Requests wait their turn', async ({ page }) => {
    await run(page, "return m.askConfirm('First question?')", 'first')
    await run(page, "return m.askConfirm('Second question?')", 'second')
    await run(page, "return m.askConfirm('Third question?')", 'third')
    const dialog = page.getByRole('alertdialog', { name: 'Confirmation' })
    await expect(dialog).toHaveCount(1)
    await expect(dialog).toHaveAccessibleDescription('First question?')
    await dialog.getByRole('button', { name: 'OK' }).click()
    // the first one leaving answers nothing for the next
    await expect(dialog).toHaveAccessibleDescription('Second question?')
    await expect(dialog.getByRole('button', { name: 'Cancel' })).toBeFocused()
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveAccessibleDescription('Third question?')
    await dialog.getByRole('button', { name: 'OK' }).click()
    await expect(dialog).toBeHidden()
    expect([await answer(page, 'first'), await answer(page, 'second'), await answer(page, 'third')]).toEqual([
      true,
      false,
      true,
    ])
  })

  test('Toasts behave as before', async ({ page }) => {
    await page.clock.install()
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Feedback gallery' })).toBeVisible()
    await page.clock.pauseAt(Date.now() + 1000)
    await run(page, 'for (let i = 1; i <= 6; i++) m.toast.info(`Message ${i}`)')
    // five show, the oldest is gone; from the top of the window down, the oldest first
    const shown = () =>
      page
        .locator('.toast')
        .evaluateAll((els) =>
          els
            .sort((a, b) => a.getBoundingClientRect().top - b.getBoundingClientRect().top)
            .map((el) => el.querySelector('.toastBody')?.textContent),
        )
    await expect.poll(shown).toEqual(['Message 2', 'Message 3', 'Message 4', 'Message 5', 'Message 6'])
    await run(page, 'm.toastState.items.slice().forEach((it) => m.closeToast(it.id))')
    await expect(page.locator('.toast')).toHaveCount(0)

    // an error stays 8 s, the other kinds 4 s
    await run(page, "m.toast.success('Saved.'); m.toast.error('Failed.')")
    await expect(page.locator('.toast')).toHaveCount(2)
    await page.clock.runFor(3_900)
    await expect(page.locator('.toast')).toHaveCount(2)
    await page.clock.runFor(600)
    await expect(page.locator('.toast')).toHaveText([/Failed\./])
    await page.clock.runFor(3_400)
    await expect(page.locator('.toast')).toHaveCount(1)
    await page.clock.runFor(600)
    await expect(page.locator('.toast')).toHaveCount(0)

    // the time stops while the pointer is on a toast, and while the focus is in it
    await run(page, "m.toast.warning('Hold on.')")
    const card = page.locator('.toast.warn')
    await card.hover()
    await page.clock.runFor(30_000)
    await expect(card).toBeVisible()
    await page.mouse.move(5, 5)
    await page.clock.runFor(4_500)
    await expect(card).toHaveCount(0)
    await run(page, "m.toast.warning('Hold on.')")
    await card.getByRole('button', { name: 'Close' }).focus()
    await page.mouse.move(5, 5)
    await page.clock.runFor(30_000)
    await expect(card).toBeVisible()

    // each closes by its button
    await card.getByRole('button', { name: 'Close' }).click()
    await expect(card).toHaveCount(0)
    await page.locator('[data-open=toasts]').click()
    for (const kind of ['.err', '.warn', '.info', ':not(.err, .warn, .info)']) {
      const one = page.locator(`.toast${kind}`)
      await one.getByRole('button', { name: 'Close' }).click()
      await expect(one).toHaveCount(0)
    }
  })

  test('Announced once', async ({ page }) => {
    await run(page, "m.toast.error('The server could not be reached.'); m.toast.info('A new version is available.')")
    const assertive = page.locator('[aria-live=assertive]')
    const polite = page.locator('[aria-live=polite]')
    await expect(assertive).toHaveCount(1)
    await expect(polite).toHaveCount(1)
    await expect(assertive).toContainText('The server could not be reached.')
    await expect(polite).toContainText('A new version is available.')
    // each once, in its own region, and the visible cards are not live regions
    const count = (text: string) =>
      page
        .locator('[aria-live]')
        .evaluateAll((els, t) => els.reduce((n, el) => n + (el.textContent ?? '').split(t).length - 1, 0), text)
    expect(await count('The server could not be reached.')).toBe(1)
    expect(await count('A new version is available.')).toBe(1)
    await expect(page.locator('.toast')).toHaveCount(2)
    for (const card of await page.locator('.toast').all()) {
      await expect(card).not.toHaveAttribute('aria-live')
      expect(await card.evaluate((el) => !!el.closest('[aria-live]') || !!el.querySelector('[aria-live]'))).toBe(false)
      expect((await card.getAttribute('role')) ?? '').not.toMatch(/^(alert|status|log)$/)
    }
  })

  test('A toast over a modal', async ({ page }) => {
    // the top-most element at the centre of the close control is the control itself
    const onTop = (loc: Locator) =>
      loc.evaluate((el) => {
        const r = el.getBoundingClientRect()
        const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2)
        return !!hit && el.contains(hit)
      })
    // a toast shown while the modal is open
    await page.locator('[data-open=modal]').click()
    const modal = page.getByRole('dialog', { name: 'Gallery modal 1' })
    await modal.locator('[data-open=toast]').click()
    const card = page.locator('.toast', { hasText: 'Shown over the modal.' })
    await expect(card).toBeVisible()
    const close = card.getByRole('button', { name: 'Close' })
    expect(await onTop(close)).toBe(true)
    await close.click()
    await expect(card).toHaveCount(0)
    await expect(modal).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(modal).toBeHidden()

    // a modal opened while a toast is shown
    await run(page, "m.toast.info('Shown before the modal.')")
    const before = page.locator('.toast', { hasText: 'Shown before the modal.' })
    await expect(before).toBeVisible()
    await page.locator('[data-open=modal]').click()
    await expect(modal).toBeVisible()
    const close2 = before.getByRole('button', { name: 'Close' })
    expect(await onTop(close2)).toBe(true)
    await close2.click()
    await expect(before).toHaveCount(0)
    await expect(modal).toBeVisible()
  })

  // F6 reaches the toasts over a trapping modal, F6 again goes back, Tab moves inside the toast, Escape closes the
  // toast only and the focus returns into the modal
  for (const order of ['toast after the modal', 'toast before the modal'] as const)
    test(`A toast over a modal, by keyboard: ${order}`, async ({ page }) => {
      const modal = page.getByRole('dialog', { name: 'Gallery modal 1' })
      const text = order === 'toast after the modal' ? 'Shown over the modal.' : 'Shown before the modal.'
      if (order === 'toast before the modal') await run(page, `m.toast.info('${text}')`)
      await page.locator('[data-open=modal]').focus()
      await page.keyboard.press('Enter')
      await expect(modal.getByLabel('Name')).toBeFocused()
      if (order === 'toast after the modal') {
        await modal.locator('[data-open=toast]').focus()
        await page.keyboard.press('Enter')
        await modal.getByLabel('Name').focus()
      }
      const card = page.locator('.toast', { hasText: text })
      await expect(card).toBeVisible()
      await page.keyboard.press('F6')
      await expect.poll(() => holdsFocus(card)).toBe(true)
      await page.keyboard.press('F6')
      await expect(modal.getByLabel('Name')).toBeFocused()
      await page.keyboard.press('F6')
      await expect(card).toBeFocused() // the newest toast itself
      await page.keyboard.press('Tab')
      await expect(card.getByRole('button', { name: 'Close' })).toBeFocused()
      await page.keyboard.press('Escape')
      await expect(card).toHaveCount(0)
      await expect(modal).toBeVisible() // Escape closed the toast, not the modal
      await expect.poll(() => holdsFocus(modal)).toBe(true)
      await expect(modal.getByLabel('Name')).toBeFocused()
      for (let i = 0; i < 4; i++) {
        await page.keyboard.press('Tab')
        expect(await holdsFocus(modal)).toBe(true)
      }
      await page.keyboard.press('Escape')
      await expect(modal).toBeHidden()
    })
})
