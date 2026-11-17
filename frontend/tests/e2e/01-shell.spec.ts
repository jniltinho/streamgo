// web-shell: client routes and the session guard (the sign-in form itself comes with task 3.F).
import { expect, test } from '@playwright/test'
import { ANA, stubApi } from './helpers'

test.describe('signed out', () => {
  for (const path of ['/broadcasts', '/destinations', '/studio/01JABC']) {
    test(`${path} sends the visitor to /login and remembers where they were`, async ({ page }) => {
      await stubApi(page)
      await page.goto(path)
      await expect(page).toHaveURL(/\/login\?/)
      expect(new URL(page.url()).searchParams.get('next')).toBe(path)
      await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
    })
  }

  test('/ is the broadcasts page, behind the sign-in', async ({ page }) => {
    await stubApi(page)
    await page.goto('/')
    await expect(page).toHaveURL(/\/login\?/)
    expect(new URL(page.url()).searchParams.get('next')).toBe('/broadcasts')
  })

  test('/join/:token is open without a session', async ({ page }) => {
    await stubApi(page)
    await page.goto('/join/tok123')
    await expect(page).toHaveURL('/join/tok123')
    await expect(page.getByRole('heading', { name: 'Join the studio' })).toBeVisible()
  })
})

test.describe('signed in', () => {
  test('every client route opens its view', async ({ page }) => {
    await stubApi(page, ANA)
    for (const [path, heading] of [
      ['/broadcasts', 'Broadcasts'],
      ['/destinations', 'Destinations'],
      ['/studio/01JABC', 'Studio'],
      ['/join/tok123', 'Join the studio'],
    ]) {
      await page.goto(path!)
      await expect(page).toHaveURL(path!)
      await expect(page.getByRole('heading', { name: heading })).toBeVisible()
    }
  })

  test('/login sends a signed-in user on to the remembered deep link', async ({ page }) => {
    await stubApi(page, ANA)
    await page.goto('/login?next=%2Fstudio%2F01JABC')
    await expect(page).toHaveURL('/studio/01JABC')
  })

  test('/login refuses a next that leaves the app', async ({ page }) => {
    await stubApi(page, ANA)
    await page.goto('/login?next=%2F%2Fevil.example')
    await expect(page).toHaveURL('/broadcasts')
  })

  test('signing out returns to /login', async ({ page }) => {
    await stubApi(page, ANA)
    await page.goto('/broadcasts')
    await page.getByRole('button', { name: 'Sign out' }).click()
    await expect(page).toHaveURL('/login')
  })
})
