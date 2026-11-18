// Portions adapted from MeetHub (MIT): frontend/vite.config.ts
import { writeFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite-plus'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// UI SDK (design U1 of add-studio-interactions): only the wrappers under src/components/ui/ and AppModal.vue import
// it, and only by a component's entry (@ui-sdk/vue/menu), never a package root or the core's internals
const uiSdkOutside = {
  paths: ['@ui-sdk/vue', '@ui-sdk/core'].map((name) => ({
    name,
    message: 'UI SDK is imported only by src/components/ui/Ui*.vue and AppModal.vue (design U1).',
  })),
  patterns: [
    {
      group: ['@ui-sdk/**'],
      message: 'UI SDK is imported only by src/components/ui/Ui*.vue and AppModal.vue (design U1).',
    },
  ],
}
const uiSdkInWrappers = {
  paths: ['@ui-sdk/vue', '@ui-sdk/core'].map((name) => ({
    name,
    message: 'Import a UI SDK component from its entry, such as @ui-sdk/vue/menu (design U1a).',
  })),
  patterns: [{ group: ['@ui-sdk/core/internal/*'], message: 'The core internals of UI SDK are not its API.' }],
}

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
  // SIZE_CHECK=1 (make check) writes Vite's manifest for scripts/size-check.mjs, which removes it again
  build: { outDir: '../web/dist', emptyOutDir: true, manifest: !!process.env.SIZE_CHECK },
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
      'no-restricted-imports': ['error', uiSdkOutside],
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
                ...uiSdkOutside.patterns,
              ],
            },
          ],
        },
      },
      {
        files: ['src/components/ui/**', 'src/components/AppModal.vue'],
        rules: { 'no-restricted-imports': ['error', uiSdkInWrappers] },
      },
    ],
  },
}))
