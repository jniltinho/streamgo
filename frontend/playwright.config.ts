// Portions adapted from MeetHub (MIT): frontend/playwright.config.ts
import { defineConfig, devices } from '@playwright/test'

// Browser tests. With E2E_BASE_URL they run against a running StreamGo; without it they start the dev server (which
// serves the test gallery) and stub the API in each test. The installed Chrome is used (no browser download).
const port = Number(process.env.E2E_PORT ?? 15173)
const external = process.env.E2E_BASE_URL

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [['list']],
  timeout: 60_000,
  use: {
    baseURL: external ?? `http://127.0.0.1:${port}`,
    viewport: { width: 1920, height: 1080 },
    colorScheme: 'dark',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chrome',
      use: { ...devices['Desktop Chrome'], channel: 'chrome', viewport: { width: 1920, height: 1080 } },
    },
  ],
  webServer: external
    ? undefined
    : {
        command: `npx vp dev --host 127.0.0.1 --port ${port} --strictPort`,
        url: `http://127.0.0.1:${port}/login`,
        reuseExistingServer: false,
        env: { STREAMGO_API_URL: 'http://127.0.0.1:9' }, // nothing listens: every API call is stubbed by the tests
      },
})
