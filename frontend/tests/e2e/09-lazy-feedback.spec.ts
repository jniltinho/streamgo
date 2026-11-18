// studio-interactions "The library is contained", scenario "Not in the first paint" (design U8 and U10): the toast
// stack and the dialog host are loaded after the entry. Their modules are held back, or refused, by a route handler.
import { expect, test, type Page, type Route } from '@playwright/test'
import { stubApi } from './helpers'

const HOSTS = /\/src\/components\/ui\/(ToastStack|DialogHost)\.vue(\?|$)/

// holdHosts keeps the hosts' requests waiting until release()
async function holdHosts(page: Page) {
  const held: Route[] = []
  let open = false
  await page.route(HOSTS, (r) => (open ? r.continue() : void held.push(r)))
  return {
    held: () => held.length,
    release: async () => {
      open = true
      for (const r of held.splice(0)) await r.continue()
    },
  }
}

// runs code in the page with the app's own feedback modules; with `keep`, its promise is kept on the window
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
const noScrollbar = (page: Page) =>
  page.evaluate(() => document.documentElement.scrollHeight <= document.documentElement.clientHeight)

test('the sign-in page paints and works while the hosts are held back', async ({ page }) => {
  await stubApi(page)
  const hosts = await holdHosts(page)
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
  await expect.poll(hosts.held).toBe(2) // both requested as soon as the entry ran
  const email = page.getByLabel('E-mail')
  await email.fill('ana@example.com')
  await page.getByLabel('Password').fill('secret')
  await expect(email).toHaveValue('ana@example.com')
  const box = await page.locator('.auth-box').boundingBox()

  // a confirmation asked meanwhile waits for the host; the focus was on the e-mail field
  await email.focus()
  await run(page, "return m.askConfirm('Leave the page?')", 'early')
  await page.getByLabel('Password').focus() // moved meanwhile: it still goes back to the e-mail field

  await hosts.release()
  const dlg = page.getByRole('alertdialog', { name: 'Confirmation' })
  await expect(dlg).toBeVisible()
  await expect(dlg.getByRole('button', { name: 'Cancel' })).toBeFocused()
  await page.keyboard.press('Enter')
  expect(await answer(page, 'early')).toBe(false)
  await expect(dlg).toHaveCount(0)
  await expect(email).toBeFocused()

  // the hosts arrived without moving the page or growing a scrollbar
  expect(await page.locator('.auth-box').boundingBox()).toEqual(box)
  expect(await noScrollbar(page)).toBe(true)

  // and work as usual
  await run(page, "m.toast.success('Saved.')")
  await expect(page.locator('.toast.show')).toHaveText(/Saved\./)
  await run(page, "return m.askConfirm('Delete it?')", 'late')
  await dlg.getByRole('button', { name: 'OK' }).click()
  expect(await answer(page, 'late')).toBe(true)
  expect(await noScrollbar(page)).toBe(true)
})

test('a toast pushed while the stack loads is shown after it and stays its full time', async ({ page }) => {
  await page.clock.install()
  await stubApi(page)
  const hosts = await holdHosts(page)
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
  await page.clock.pauseAt(Date.now() + 1000)
  await run(page, "m.toast.info('Welcome.')")
  await page.clock.runFor(30_000) // nothing can show it: its time does not run
  await expect(page.locator('.toast')).toHaveCount(0)

  await hosts.release()
  await expect(page.locator('.toast')).toHaveCount(1)
  await page.clock.runFor(100)
  await expect(page.locator('.toast.show')).toHaveText(/Welcome\./)
  await page.clock.runFor(3_800)
  await expect(page.locator('.toast.show')).toHaveCount(1)
  await page.clock.runFor(700)
  await expect(page.locator('.toast')).toHaveCount(0)
})

test('hosts that cannot be loaded: confirmations settle as no, nothing hangs', async ({ page }) => {
  await stubApi(page)
  let tries = 0
  await page.route(HOSTS, (r) => (tries++, r.abort('internetdisconnected')))
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
  await run(page, "return m.askConfirm('Delete it?')", 'pending')
  await run(page, "return m.showAlert('Saved.')", 'alert')
  expect(await answer(page, 'pending')).toBe(false) // settles after the tries, about 3 s
  expect(await answer(page, 'alert')).toBeUndefined()
  expect(await run(page, "return m.askConfirm('Later?')")).toBe(false)
  expect(tries).toBeGreaterThan(2) // tried again
  // the page still works
  await page.getByLabel('E-mail').fill('ana@example.com')
  await expect(page.getByLabel('E-mail')).toHaveValue('ana@example.com')
})

test('a modal that cannot be loaded says so, and opens once the network is back', async ({ page }) => {
  await stubApi(page)
  let offline = true
  await page.route(/\/src\/components\/PreferencesModal\.vue(\?|$)/, (r) =>
    offline ? r.abort('internetdisconnected') : r.continue(),
  )
  await page.goto('/login')
  const open = page.getByRole('button', { name: 'Theme and language' })
  await open.click()
  await expect(page.locator('.toast.err')).toContainText('Could not load this window', { timeout: 10_000 })
  await expect(page.getByRole('dialog')).toHaveCount(0)

  offline = false
  await open.click()
  await expect(page.getByRole('dialog', { name: 'Theme and language' })).toBeVisible()
})
