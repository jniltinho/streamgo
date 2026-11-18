// Portions adapted from MeetHub (MIT): frontend/tests/e2e/03-feedback.spec.ts
import { expect, test } from '@playwright/test'
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
  await expect(page.locator('.toast.err')).toHaveAttribute('aria-live', 'assertive')
  await expect(page.locator('.toast.info')).toHaveAttribute('aria-live', 'polite')
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
