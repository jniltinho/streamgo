// web-shell: the sign-in, the signed-in shell, the broadcasts and destinations pages, their language switch, their
// feedback and their scale on large screens (task 3.F). The API is the fake of pages-api.ts.
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'
import { ANA } from './helpers'
import { broadcast, fakeApi, PASSWORD } from './pages-api'

test.beforeEach(({ page }) => {
  page.on('dialog', (d) => {
    throw new Error(`native ${d.type()} dialog: ${d.message()}`)
  })
})

async function signIn(page: Page, password = PASSWORD) {
  await page.getByLabel('E-mail').fill(ANA.email)
  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: 'Sign in' }).click()
}

test.describe('sign-in', () => {
  test('Deep link: a signed-out visitor who opens /studio/<id> lands there after signing in', async ({ page }) => {
    const api = await fakeApi(page, ANA, false)
    await page.goto('/studio/01JDEEPLINK')
    await expect(page).toHaveURL(/\/login\?/)
    expect(new URL(page.url()).searchParams.get('next')).toBe('/studio/01JDEEPLINK')
    await signIn(page)
    await expect(page).toHaveURL('/studio/01JDEEPLINK')
    await expect(page.getByRole('heading', { name: 'Studio' })).toBeVisible()
    expect(api.bodies).toEqual([{ route: 'POST /auth/login', body: { email: ANA.email, password: PASSWORD } }])
  })

  test('wrong credentials show a themed error and keep the visitor on the page', async ({ page }) => {
    await fakeApi(page, ANA, false)
    await page.goto('/login')
    await signIn(page, 'wrong')
    await expect(page.locator('.alert-error')).toHaveText('Invalid e-mail or password.')
    await expect(page.getByLabel('Password')).toHaveValue('')
    await expect(page).toHaveURL('/login')
  })

  test('the rate limit shows a themed error', async ({ page }) => {
    const api = await fakeApi(page, ANA, false)
    api.rateLimited = true
    await page.goto('/login')
    await signIn(page)
    await expect(page.locator('.alert-error')).toHaveText('Too many attempts. Try again later.')
  })

  test('language and theme can be chosen before signing in', async ({ page }) => {
    const api = await fakeApi(page, ANA, false)
    await page.goto('/login')
    await page.getByRole('button', { name: 'Theme and language' }).click()
    await page.getByRole('tab', { name: 'Language' }).click()
    await page.getByRole('combobox', { name: 'Language' }).selectOption('pt-BR')
    await expect(page.locator('h1')).toHaveText('Entrar') // behind the modal, so not in the accessibility tree
    await page.getByRole('tab', { name: 'Tema' }).click()
    await page.locator('[data-theme-id=matrix]').click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'matrix')
    expect(api.bodies).toEqual([]) // nothing saved while signed out
  })
})

test.describe('signed-in shell', () => {
  test('navigation, the user name and sign-out', async ({ page }) => {
    await fakeApi(page, ANA, true)
    await page.goto('/broadcasts')
    await expect(page.locator('[data-user-name]')).toHaveText(ANA.name)
    // "Profile": the name in the header opens the settings on the Profile tab; the saved name shows at once
    await page.getByRole('button', { name: `Your profile: ${ANA.name}` }).click()
    const settings = page.getByRole('dialog', { name: 'Settings' })
    await settings.getByLabel('Your name').fill('Ana Maria Souza')
    await settings.getByRole('button', { name: 'Save' }).click()
    await expect(page.locator('.toast')).toContainText('Name saved.')
    await settings.getByRole('button', { name: 'Close' }).click()
    await expect(page.locator('[data-user-name]')).toHaveText('Ana Maria Souza')
    await page.getByRole('navigation').getByRole('link', { name: 'Destinations' }).click()
    await expect(page).toHaveURL('/destinations')
    await expect(page.getByRole('heading', { name: 'Destinations' })).toBeVisible()
    await page.getByRole('button', { name: 'Sign out' }).click()
    await expect(page).toHaveURL('/login')
  })

  test('Switch language: the text changes at once and the choice is kept after signing in elsewhere', async ({
    page,
    browser,
  }) => {
    const api = await fakeApi(page, ANA, true)
    await page.goto('/broadcasts')
    await page.getByRole('button', { name: 'Settings' }).click()
    await page.getByRole('tab', { name: 'Language' }).click()
    await page.getByRole('combobox', { name: 'Language' }).selectOption('pt-BR')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('heading', { name: 'Transmissões' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Sair' })).toBeVisible()
    await expect.poll(() => api.bodies).toContainEqual({ route: 'PATCH /me', body: { language: 'pt-BR' } })

    // another browser: nothing stored locally, the preference comes with the user
    const other = await browser.newContext({ locale: 'en-US' })
    const page2 = await other.newPage()
    await fakeApi(page2, api.user, false)
    await page2.goto('/login')
    await expect(page2.getByRole('heading', { name: 'Sign in' })).toBeVisible()
    await signIn(page2)
    await expect(page2).toHaveURL('/broadcasts')
    await expect(page2.getByRole('heading', { name: 'Transmissões' })).toBeVisible()
    await other.close()
  })
})

test.describe('broadcasts', () => {
  test('lists the broadcasts newest first with their state', async ({ page }) => {
    await fakeApi(page, ANA, true, {
      broadcasts: [broadcast('Weekly Product Live', 'live'), broadcast('Launch rehearsal')],
    })
    await page.goto('/broadcasts')
    const rows = page.locator('tbody tr')
    await expect(rows).toHaveCount(2)
    await expect(rows.nth(0)).toContainText('Weekly Product Live')
    await expect(rows.nth(0)).toContainText('Live')
    await expect(rows.nth(1)).toContainText('Launch rehearsal')
    await expect(rows.nth(1)).toContainText('Offline')
  })

  test('an empty list says how to start', async ({ page }) => {
    await fakeApi(page, ANA, true)
    await page.goto('/broadcasts')
    await expect(page.getByText('No broadcasts yet.')).toBeVisible()
  })

  test('Create and open: the studio of the new broadcast loads', async ({ page }) => {
    const api = await fakeApi(page, ANA, true, { broadcasts: [broadcast('Older')] })
    await page.goto('/broadcasts')
    await page.getByRole('button', { name: 'New broadcast' }).click()
    const dialog = page.getByRole('dialog', { name: 'New broadcast' })
    await dialog.getByLabel('Title').fill('  Q3 roadmap  ')
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(dialog).toBeHidden()
    await expect(page.locator('.toast')).toContainText('Broadcast Q3 roadmap created.')
    expect(api.bodies).toEqual([{ route: 'POST /broadcasts', body: { title: 'Q3 roadmap' } }])
    const first = page.locator('tbody tr').first()
    await expect(first).toContainText('Q3 roadmap')
    await first.getByRole('link', { name: 'Open studio' }).click()
    await expect(page).toHaveURL(`/studio/${api.broadcasts[0]!.id}`)
    await expect(page.getByRole('heading', { name: 'Studio' })).toBeVisible()
  })

  test('a blank title is refused before the request', async ({ page }) => {
    const api = await fakeApi(page, ANA, true)
    await page.goto('/broadcasts')
    await page.getByRole('button', { name: 'New broadcast' }).click()
    await page.getByLabel('Title').fill('   ')
    await page.getByRole('button', { name: 'Create' }).click()
    await expect(page.getByRole('dialog', { name: 'New broadcast' })).toBeVisible()
    expect(api.bodies).toEqual([])
  })

  test('rename', async ({ page }) => {
    const api = await fakeApi(page, ANA, true, { broadcasts: [broadcast('Old name')] })
    await page.goto('/broadcasts')
    await page.getByRole('button', { name: 'Rename Old name' }).click()
    const dialog = page.getByRole('dialog', { name: 'Rename broadcast' })
    await expect(dialog.getByLabel('Title')).toHaveValue('Old name')
    await dialog.getByLabel('Title').fill('New name')
    await dialog.getByRole('button', { name: 'Save' }).click()
    await expect(page.locator('tbody tr')).toContainText('New name')
    expect(api.bodies).toEqual([{ route: `PATCH /broadcasts/${api.broadcasts[0]!.id}`, body: { title: 'New name' } }])
  })

  test('Delete confirmation: a themed dialog asks and a toast confirms', async ({ page }) => {
    const api = await fakeApi(page, ANA, true, { broadcasts: [broadcast('To delete')] })
    await page.goto('/broadcasts')
    await page.getByRole('button', { name: 'Delete To delete' }).click()
    const dialog = page.getByRole('alertdialog', { name: 'Confirmation' })
    await expect(dialog).toContainText('Delete the broadcast To delete?')
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    expect(api.bodies).toEqual([])
    await page.getByRole('button', { name: 'Delete To delete' }).click()
    await dialog.getByRole('button', { name: 'Delete' }).click()
    await expect(page.locator('.toast')).toContainText('Broadcast deleted.')
    await expect(page.getByText('No broadcasts yet.')).toBeVisible()
  })

  test('a broadcast that is not offline cannot be deleted: the conflict is a themed error', async ({ page }) => {
    await fakeApi(page, ANA, true, { broadcasts: [broadcast('On air', 'live')] })
    await page.goto('/broadcasts')
    await page.getByRole('button', { name: 'Delete On air' }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete' }).click()
    await expect(page.locator('.toast.err')).toContainText('End the broadcast before deleting it.')
    await expect(page.locator('tbody tr')).toHaveCount(1)
  })
})

test.describe('destinations', () => {
  const youtube = { id: '01JDEST0001', name: 'YouTube', scheme: 'rtmps', host: 'a.rtmp.youtube.com', has_key: true }

  test('create: the list shows the name, scheme, host and that a key is set, never the key', async ({ page }) => {
    const api = await fakeApi(page, ANA, true)
    await page.goto('/destinations')
    await expect(page.getByText('No destinations yet.')).toBeVisible()
    // "Platform presets": a shortcut opens the form with the platform's name and address; "Other" takes them back
    await page.getByRole('group', { name: 'Popular platforms' }).getByRole('button', { name: 'YouTube' }).click()
    const preset = page.getByRole('dialog', { name: 'New destination' })
    await expect(preset.getByLabel('Platform')).toHaveValue('youtube')
    await expect(preset.getByLabel('Name')).toHaveValue('YouTube')
    await expect(preset.getByLabel('Address')).toHaveValue('rtmps://a.rtmps.youtube.com/live2')
    await preset.getByLabel('Platform').selectOption('twitch')
    await expect(preset.getByLabel('Address')).toHaveValue('rtmps://live.twitch.tv/app')
    await preset.getByLabel('Platform').selectOption('')
    await expect(preset.getByLabel('Name')).toHaveValue('')
    await expect(preset.getByLabel('Address')).toHaveValue('')
    await preset.getByRole('button', { name: 'Cancel' }).click()
    await page.getByRole('button', { name: 'New destination' }).click()
    const dialog = page.getByRole('dialog', { name: 'New destination' })
    await dialog.getByLabel('Name').fill('Twitch')
    await dialog.getByLabel('Address').fill('rtmp://live.twitch.tv/app')
    await dialog.getByLabel('Stream key').fill('live_secret_123')
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(page.locator('.toast')).toContainText('Destination Twitch created.')
    expect(api.bodies).toEqual([
      {
        route: 'POST /destinations',
        body: { name: 'Twitch', address: 'rtmp://live.twitch.tv/app', key: 'live_secret_123' },
      },
    ])
    const row = page.locator('tbody tr')
    await expect(row).toContainText('Twitch')
    await expect(row).toContainText('rtmp://live.twitch.tv')
    await expect(row).toContainText('Set')
    await expect(page.locator('body')).not.toContainText('live_secret_123')
    await expect(page.locator('body')).not.toContainText('/app')
  })

  test('Key field: editing shows empty address and key with a note, and saving without typing keeps them', async ({
    page,
  }) => {
    const api = await fakeApi(page, ANA, true, { destinations: [{ ...youtube }] })
    await page.goto('/destinations')
    await page.getByRole('button', { name: 'Edit YouTube' }).click()
    const dialog = page.getByRole('dialog', { name: 'Edit destination' })
    await expect(dialog.getByLabel('Name')).toHaveValue('YouTube')
    await expect(dialog.getByLabel('Address')).toHaveValue('')
    await expect(dialog.getByLabel('Address')).toHaveAccessibleDescription(/An address is set/)
    await expect(dialog.getByLabel('Stream key')).toHaveValue('')
    await expect(dialog.getByLabel('Stream key')).toHaveAccessibleDescription(
      'A key is set. Leave the field empty to keep it.',
    )
    await dialog.getByLabel('Name').fill('YouTube main')
    await dialog.getByRole('button', { name: 'Save' }).click()
    await expect(page.locator('.toast')).toContainText('Destination saved.')
    expect(api.bodies).toEqual([{ route: `PATCH /destinations/${youtube.id}`, body: { name: 'YouTube main' } }])
    await expect(page.locator('tbody tr')).toContainText('YouTube main')
    await expect(page.locator('tbody tr')).toContainText('Set')
  })

  test('a new address and key are sent when typed', async ({ page }) => {
    const api = await fakeApi(page, ANA, true, { destinations: [{ ...youtube }] })
    await page.goto('/destinations')
    await page.getByRole('button', { name: 'Edit YouTube' }).click()
    const dialog = page.getByRole('dialog', { name: 'Edit destination' })
    await dialog.getByLabel('Address').fill('rtmps://b.rtmp.youtube.com/live2')
    await dialog.getByLabel('Stream key').fill('new-key')
    await dialog.getByRole('button', { name: 'Save' }).click()
    await expect(dialog).toBeHidden()
    expect(api.bodies[0]!.body).toEqual({
      name: 'YouTube',
      address: 'rtmps://b.rtmp.youtube.com/live2',
      key: 'new-key',
    })
    await expect(page.locator('tbody tr')).toContainText('rtmps://b.rtmp.youtube.com')
  })

  test('a destination in use: the 409 is a themed message on edit and on delete', async ({ page }) => {
    await fakeApi(page, ANA, true, { destinations: [{ ...youtube }], inUse: new Set([youtube.id]) })
    await page.goto('/destinations')
    await page.getByRole('button', { name: 'Edit YouTube' }).click()
    const dialog = page.getByRole('dialog', { name: 'Edit destination' })
    await dialog.getByRole('button', { name: 'Save' }).click()
    await expect(dialog.locator('.alert-error')).toHaveText('This destination is in use by a broadcast.')
    await dialog.getByRole('button', { name: 'Cancel' }).click()

    await page.getByRole('button', { name: 'Delete YouTube' }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete' }).click()
    await expect(page.locator('.toast.err')).toContainText('This destination is in use by a broadcast.')
    await expect(page.locator('tbody tr')).toHaveCount(1)
  })

  test('delete asks and confirms', async ({ page }) => {
    await fakeApi(page, ANA, true, { destinations: [{ ...youtube }] })
    await page.goto('/destinations')
    await page.getByRole('button', { name: 'Delete YouTube' }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete' }).click()
    await expect(page.locator('.toast')).toContainText('Destination deleted.')
    await expect(page.getByText('No destinations yet.')).toBeVisible()
  })
})

// the cells of the users-page row of an e-mail
const cells = (page: Page, email: string) => page.locator(`[data-user="${email}"] td`)

test.describe('users (add-user-management)', () => {
  const bruno = { id: 'u2', name: 'Bruno', email: 'bruno@example.com', admin: false, disabled: false }

  test('List and create: every account with its role and state, and a new one with a toast', async ({ page }) => {
    const api = await fakeApi(page, ANA, true, { users: [{ ...bruno }] })
    await page.goto('/broadcasts')
    await page.getByRole('navigation').getByRole('link', { name: 'Users' }).click()
    await expect(page).toHaveURL('/users')
    await expect(cells(page, ANA.email)).toHaveText([ANA.name, ANA.email, 'Administrator', 'Active', /Edit/])
    await expect(cells(page, 'bruno@example.com')).toHaveText(['Bruno', 'bruno@example.com', 'User', 'Active', /Edit/])

    await page.getByRole('button', { name: 'New user' }).click()
    const dialog = page.getByRole('dialog', { name: 'New user' })
    await dialog.getByLabel('Name').fill('Carla')
    await dialog.getByLabel('E-mail').fill('carla@example.com')
    await dialog.getByLabel('Password').fill('short')
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(dialog.locator('.field-error')).toHaveText('Too short.') // refused before the request
    expect(api.bodies).toEqual([])
    await dialog.getByLabel('Password').fill('carla-password')
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(page.locator('.toast')).toContainText('User Carla created.')
    await expect(cells(page, 'carla@example.com')).toHaveText(['Carla', 'carla@example.com', 'User', 'Active', /Edit/])
    expect(api.bodies).toEqual([
      {
        route: 'POST /users',
        body: { email: 'carla@example.com', name: 'Carla', password: 'carla-password', admin: false },
      },
    ])

    // a taken e-mail is a themed error in the modal
    await page.getByRole('button', { name: 'New user' }).click()
    await dialog.getByLabel('Name').fill('Bruno again')
    await dialog.getByLabel('E-mail').fill('BRUNO@example.com')
    await dialog.getByLabel('Password').fill('0123456789')
    await dialog.getByRole('button', { name: 'Create' }).click()
    await expect(dialog.locator('.alert-error')).toHaveText('A user with this e-mail already exists.')
  })

  test('Edit, disable, enable: the list follows, and the controls on oneself are disabled with a reason', async ({
    page,
  }) => {
    const api = await fakeApi(page, ANA, true, { users: [{ ...bruno }] })
    await page.goto('/users')
    const row = cells(page, 'bruno@example.com')

    await page.getByRole('button', { name: 'Edit Bruno' }).click()
    const dialog = page.getByRole('dialog', { name: 'Edit user' })
    await expect(dialog.getByLabel('Name')).toHaveValue('Bruno')
    await dialog.getByLabel('Name').fill('Bruno Lima')
    await dialog.getByLabel('Administrator').check()
    await dialog.getByRole('button', { name: 'Save' }).click()
    await expect(page.locator('.toast')).toContainText('User saved.')
    await expect(row).toHaveText(['Bruno Lima', 'bruno@example.com', 'Administrator', 'Active', /Edit/])
    expect(api.bodies.at(-1)).toEqual({ route: 'PATCH /users/u2', body: { name: 'Bruno Lima', admin: true } })

    await page.getByRole('button', { name: 'Disable Bruno Lima' }).click()
    const confirm = page.getByRole('alertdialog', { name: 'Confirmation' })
    await expect(confirm).toContainText('Disable Bruno Lima?')
    await confirm.getByRole('button', { name: 'Cancel' }).click()
    expect(api.bodies).toHaveLength(1)
    await page.getByRole('button', { name: 'Disable Bruno Lima' }).click()
    await confirm.getByRole('button', { name: 'Disable' }).click()
    await expect(page.locator('.toast').last()).toContainText('Bruno Lima disabled.')
    await expect(row.nth(3)).toHaveText('Disabled')
    // one row says "Disable" and the other "Enable": the buttons still line up from row to row
    const left = (name: string) =>
      page.getByRole('button', { name, exact: true }).evaluate((el) => Math.round(el.getBoundingClientRect().left))
    expect(await left(`Edit ${ANA.name}`)).toBe(await left('Edit Bruno Lima'))
    expect(await left(`Disable ${ANA.name}`)).toBe(await left('Enable Bruno Lima'))
    await page.getByRole('button', { name: 'Enable Bruno Lima' }).click()
    await expect(page.locator('.toast').last()).toContainText('Bruno Lima enabled.')
    await expect(row.nth(3)).toHaveText('Active')
    expect(api.bodies.slice(1)).toEqual([
      { route: 'PATCH /users/u2', body: { disabled: true } },
      { route: 'PATCH /users/u2', body: { disabled: false } },
    ])

    // oneself: no disable, no demotion, each with the reason
    const self = page.getByRole('button', { name: `Disable ${ANA.name}` })
    await expect(self).toBeDisabled()
    await expect(self).toHaveAccessibleDescription('You cannot disable your own account.')
    await page.getByRole('button', { name: `Edit ${ANA.name}` }).click()
    const own = page.getByRole('dialog', { name: 'Edit user' })
    await expect(own.getByLabel('Administrator')).toBeDisabled()
    await expect(own.getByLabel('Administrator')).toHaveAccessibleDescription(
      'You cannot remove your own administrator role.',
    )
  })

  test('Hidden from others: no Users link, and /users leads to the broadcasts', async ({ page }) => {
    await fakeApi(page, { ...ANA, admin: false }, true)
    await page.goto('/broadcasts')
    await expect(page.getByRole('heading', { name: 'Broadcasts' })).toBeVisible()
    await expect(page.getByRole('navigation').getByRole('link', { name: 'Users' })).toHaveCount(0)
    await page.goto('/users')
    await expect(page).toHaveURL('/broadcasts')
  })

  test('Change my password: the errors show at their field, success is a toast', async ({ page }) => {
    const api = await fakeApi(page, { ...ANA, admin: false }, true)
    await page.goto('/broadcasts')
    await page.getByRole('button', { name: `Your profile: ${ANA.name}` }).click()
    const form = page.getByRole('dialog', { name: 'Settings' }).getByRole('form', { name: 'Change password' })
    const submit = form.getByRole('button', { name: 'Change password' })

    // a different repetition: shown at the field, no request
    await form.getByLabel('Current password').fill(PASSWORD)
    await form.getByLabel('New password', { exact: true }).fill('battery-staple-horse')
    await form.getByLabel('Repeat the new password').fill('battery-staple-house')
    await submit.click()
    await expect(form.locator('.field-error')).toHaveText('The passwords do not match.')
    expect(api.bodies).toEqual([])

    // a wrong current password: shown at the field
    await form.getByLabel('Repeat the new password').fill('battery-staple-horse')
    await form.getByLabel('Current password').fill('not my password')
    await submit.click()
    await expect(form.locator('.field-error')).toHaveText('This is not your current password.')
    await expect(form.getByLabel('Current password')).toHaveAttribute('aria-invalid', 'true')

    await form.getByLabel('Current password').fill(PASSWORD)
    await submit.click()
    await expect(page.locator('.toast')).toContainText('Password changed. Your other sessions ended.')
    await expect(form.getByLabel('New password', { exact: true })).toHaveValue('')
    expect(api.bodies.at(-1)).toEqual({
      route: 'PUT /me/password',
      body: { current: PASSWORD, password: 'battery-staple-horse' },
    })
  })
})

test.describe('server settings (add-server-settings)', () => {
  async function openServerTab(page: Page) {
    await page.getByRole('button', { name: 'Settings' }).click()
    const settings = page.getByRole('dialog', { name: 'Settings' })
    await settings.getByRole('tab', { name: 'Server' }).click()
    return settings
  }

  test('Change and save: a toast confirms and the studio of an offline broadcast offers the new choices', async ({
    page,
  }) => {
    const api = await fakeApi(page, ANA, true, { broadcasts: [broadcast('Show')] })
    await page.goto('/broadcasts')
    const settings = await openServerTab(page)
    await expect(settings.getByLabel('Highest resolution')).toHaveValue('1080p')
    await settings.getByLabel('Highest resolution').selectOption('1440p')
    await settings.getByLabel('Video bitrate').selectOption({ label: 'Fixed' })
    await settings.getByLabel('kbit/s (500 to 40000)').fill('8000')
    await settings.getByRole('button', { name: 'Save' }).click()
    await expect(page.locator('.toast')).toContainText('Server settings saved.')
    expect(api.bodies.at(-1)).toEqual({
      route: 'PUT /settings',
      body: { output: { resolution: '720p', max_resolution: '1440p', video_bitrate_kbps: 8000 } },
    })

    // a default above the ceiling is refused with the reason, and nothing changes
    await settings.getByLabel('Default resolution of new broadcasts').selectOption('1440p')
    await settings.getByLabel('Highest resolution').selectOption('1080p')
    await settings.getByRole('button', { name: 'Save' }).click()
    await expect(settings.locator('.alert-error')).toHaveText(
      'The default resolution cannot be above the highest resolution.',
    )
    expect(api.settings.max_resolution).toBe('1440p')

    await settings.getByRole('button', { name: 'Close' }).click()
    await page.route('**/studio-token', () => {}) // no LiveKit here: the studio stays connecting, its panel shown
    await page.locator('tbody tr').first().getByRole('link', { name: 'Open studio' }).click()
    await page.getByRole('tab', { name: 'Destinations' }).click()
    await expect(page.locator('#output-quality option')).toHaveText([/^720p/, /^1080p/, /^1440p/])
  })

  test('Locked field: its control is disabled and says the environment sets it', async ({ page }) => {
    const api = await fakeApi(page, ANA, true)
    api.settings.locked = ['max_resolution']
    await page.goto('/broadcasts')
    const settings = await openServerTab(page)
    const ceiling = settings.getByLabel('Highest resolution')
    await expect(ceiling).toBeDisabled()
    await expect(ceiling).toHaveAccessibleDescription(
      'Set by the server environment (STREAMGO_OUTPUT_MAX_RESOLUTION): change it there.',
    )
    await expect(settings.getByLabel('Video bitrate')).toBeEnabled()
    await settings.getByRole('button', { name: 'Save' }).click()
    await expect(page.locator('.toast')).toContainText('Server settings saved.')
    expect(api.bodies.at(-1)).toEqual({
      route: 'PUT /settings',
      body: { output: { resolution: '720p', video_bitrate_kbps: 0 } },
    })
  })

  test('Hidden from others: no Server tab', async ({ page }) => {
    await fakeApi(page, { ...ANA, admin: false }, true)
    await page.goto('/broadcasts')
    await page.getByRole('button', { name: 'Settings' }).click()
    const settings = page.getByRole('dialog', { name: 'Settings' })
    await expect(settings.getByRole('tab', { name: 'Profile' })).toBeVisible()
    await expect(settings.getByRole('tab', { name: 'Server' })).toHaveCount(0)
  })
})

test.describe('no page scrollbar, modals below the top', () => {
  for (const size of [
    { width: 1280, height: 720 },
    { width: 1920, height: 1080 },
  ]) {
    test(`sign-in and the theme and language modal fit ${size.width}x${size.height}`, async ({ page }) => {
      await fakeApi(page, ANA, false)
      await page.setViewportSize(size)
      await page.goto('/login')
      const fits = (sel: string) =>
        page.locator(sel).evaluate((el) => el.scrollHeight <= el.clientHeight && el.scrollWidth <= el.clientWidth)
      expect(await fits('html')).toBe(true)
      expect(await fits('.auth-page')).toBe(true)
      await page.getByRole('button', { name: 'Theme and language' }).click()
      const dialog = page.locator('dialog[open]')
      await expect(dialog).toBeVisible()
      expect(await fits('dialog[open]')).toBe(true) // the 22 themes show without scrolling
      const top = await dialog.evaluate((el) => el.getBoundingClientRect().top)
      expect(Math.abs(top - size.height * 0.05)).toBeLessThanOrEqual(8) // 5% below the top (the rise animation ends there)
    })
  }

  test('the signed-in pages do not scroll the document', async ({ page }) => {
    await fakeApi(page, ANA, true)
    for (const path of ['/broadcasts', '/destinations', '/users']) {
      await page.goto(path)
      await expect(page.locator('h1')).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollHeight <= window.innerHeight)).toBe(true)
    }
  })
})

test.describe('large screens and accessibility', () => {
  const seed = () => ({
    broadcasts: [broadcast('Weekly Product Live', 'live'), broadcast('Launch rehearsal'), broadcast('Q2 review')],
  })

  async function measure(page: Page) {
    const col = (await page.locator('[data-page-column]').boundingBox())!
    const size = await page
      .getByRole('heading', { name: 'Broadcasts' })
      .evaluate((h) => parseFloat(getComputedStyle(h).fontSize))
    return { col, size }
  }

  test('Broadcasts page at 4K: a centred column no wider than half the window, text at twice its Full HD size', async ({
    page,
  }) => {
    await fakeApi(page, ANA, true, seed())
    await page.goto('/broadcasts')
    await expect(page.locator('tbody tr')).toHaveCount(3)
    const fhd = await measure(page)
    await page.screenshot({ path: test.info().outputPath('broadcasts-1920x1080.png') })

    await page.setViewportSize({ width: 3840, height: 2160 })
    await expect.poll(async () => (await measure(page)).size).toBe(fhd.size * 2)
    const uhd = await measure(page)
    expect(uhd.col.width).toBeLessThanOrEqual(3840 / 2)
    const left = uhd.col.x
    const right = 3840 - (uhd.col.x + uhd.col.width)
    expect(Math.abs(left - right)).toBeLessThanOrEqual(1)
    expect(uhd.col.width).toBeCloseTo(fhd.col.width * 2, 0)
    await page.screenshot({ path: test.info().outputPath('broadcasts-3840x2160.png') })
  })

  test('the sign-in box stays a narrow centred column at 4K', async ({ page }) => {
    await page.setViewportSize({ width: 3840, height: 2160 })
    await fakeApi(page, ANA, false)
    await page.goto('/login')
    const box = (await page.locator('.auth-box').boundingBox())!
    expect(box.width).toBeLessThanOrEqual(3840 / 2)
    expect(Math.abs(box.x - (3840 - box.x - box.width))).toBeLessThanOrEqual(1)
  })

  for (const [path, signedIn] of [
    ['/login', false],
    ['/broadcasts', true],
    ['/destinations', true],
    ['/users', true],
  ] as const) {
    test(`axe finds no WCAG A/AA violation on ${path}`, async ({ page }) => {
      await fakeApi(page, ANA, signedIn, {
        ...seed(),
        destinations: [
          { id: '01JDEST0001', name: 'YouTube', scheme: 'rtmps', host: 'a.rtmp.youtube.com', has_key: true },
        ],
      })
      await page.goto(path)
      await expect(page.locator('h1')).toBeVisible()
      if (signedIn) await expect(page.locator('tbody tr').first()).toBeVisible()
      const { violations } = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()
      expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target).join(', ')}`)).toEqual([])
    })
  }
})
