// Portions adapted from MeetHub (MIT): frontend/vite.config.ts
import { writeFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite-plus'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// The StreamGo SPA.
// - vp build → ../web/dist (embedded in the Go binary through go:embed)
// - vp dev   → proxies /api to the Go server (STREAMGO_API_URL, default http://127.0.0.1:8080)
const apiTarget = process.env.STREAMGO_API_URL ?? 'http://127.0.0.1:8080'

export default defineConfig(({ mode }) => ({
  plugins: [
    vue(),
    tailwindcss(),
    // go:embed needs at least one file in web/dist even before a build: recreate the versioned placeholder
    {
      name: 'gitkeep',
      closeBundle: () => writeFileSync(fileURLToPath(new URL('../web/dist/.gitkeep', import.meta.url)), ''),
    },
    // the /__gallery page is for tests only: a production bundle that contains it fails the build
    {
      name: 'no-gallery-in-production',
      apply: 'build',
      generateBundle(_, bundle) {
        if (mode !== 'production') return
        for (const [name, out] of Object.entries(bundle)) {
          const code = out.type === 'chunk' ? out.code : String(out.source)
          if (/__gallery|Feedback gallery/.test(code))
            this.error(`the test gallery leaked into the production bundle (${name})`)
        }
      },
    },
  ],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  build: { outDir: '../web/dist', emptyOutDir: true },
  server: { host: '0.0.0.0', port: 5173, proxy: { '/api': { target: apiTarget, changeOrigin: false } } },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts', 'tests/unit/**/*.test.ts'],
    exclude: ['tests/e2e/**', 'node_modules/**'],
  },
  fmt: { singleQuote: true, semi: false, printWidth: 120 },
  lint: {
    rules: {
      // themed toasts, alerts and dialogs only: the browser's native ones are banned
      'no-alert': 'error',
      'no-restricted-globals': ['error', 'alert', 'confirm', 'prompt'],
      'no-restricted-properties': [
        'error',
        { object: 'window', property: 'alert' },
        { object: 'window', property: 'confirm' },
        { object: 'window', property: 'prompt' },
      ],
    },
    overrides: [
      {
        // the layering rule (build-and-deploy spec): src/core is plain TypeScript, without the UI framework
        files: ['src/core/**'],
        rules: {
          'no-restricted-imports': [
            'error',
            {
              paths: ['vue', 'pinia', 'vue-router'].map((name) => ({
                name,
                message: 'src/core must not import the UI framework (layering rule).',
              })),
              patterns: [
                {
                  group: ['vue/*', 'pinia/*', 'vue-router/*', '@vue/*'],
                  message: 'src/core must not import the UI framework (layering rule).',
                },
              ],
            },
          ],
        },
      },
    ],
  },
}))
