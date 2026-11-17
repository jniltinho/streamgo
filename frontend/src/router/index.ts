// Portions adapted from MeetHub (MIT): frontend/src/router/index.ts
import { nextTick } from 'vue'
import { createRouter, createWebHistory, type RouteLocationNormalized } from 'vue-router'
import { setUnauthorizedHandler } from '@/api/client'
import { useSession } from '@/stores/session'
import { cancelDialogs } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

declare module 'vue-router' {
  interface RouteMeta {
    /** open without a session (web-shell spec: /login and /join/:token) */
    public?: boolean
  }
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true } },
    { path: '/join/:token', name: 'join', component: () => import('@/views/JoinView.vue'), meta: { public: true } },
    { path: '/studio/:id', name: 'studio', component: () => import('@/views/StudioView.vue') },
    {
      path: '/',
      component: () => import('@/layouts/AppLayout.vue'),
      children: [
        { path: '', redirect: '/broadcasts' },
        { path: 'broadcasts', name: 'broadcasts', component: () => import('@/views/BroadcastsView.vue') },
        { path: 'destinations', name: 'destinations', component: () => import('@/views/DestinationsView.vue') },
      ],
    },
    // the feedback gallery: development and tests only (the build fails if it reaches a production bundle)
    ...(import.meta.env.DEV || import.meta.env.MODE === 'test'
      ? [
          {
            path: '/__gallery',
            name: 'gallery',
            component: () => import('@/views/GalleryView.vue'),
            meta: { public: true },
          },
        ]
      : []),
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

// safeNext keeps a post-sign-in target only when it is a path of this app (no open redirect).
export function safeNext(next: unknown): string {
  return typeof next === 'string' && next.startsWith('/') && !next.startsWith('//') && !next.includes('\\') ? next : '/'
}

let restored = false

router.beforeEach(async (to: RouteLocationNormalized) => {
  const session = useSession()
  if (!session.loggedIn && !restored) {
    restored = true
    await session.loadMe()
  }
  if (to.meta.public) return session.loggedIn && to.name === 'login' ? { path: safeNext(to.query.next) } : true
  if (!session.loggedIn) return { name: 'login', query: to.fullPath !== '/' ? { next: to.fullPath } : {} }
  return true
})

// route changes move focus to the new page's heading (screen readers and keyboard users)
router.afterEach(async () => {
  await nextTick()
  const h = document.querySelector<HTMLElement>('main h1, [data-page-heading]')
  if (h) {
    h.tabIndex = -1
    h.focus({ preventScroll: true })
  }
})

// a session ended by the server: cancel pending confirmations, tell the user, go to the sign-in page
setUnauthorizedHandler((code) => {
  const session = useSession()
  if (!session.loggedIn) return
  cancelDialogs()
  session.clear()
  toast.warning(errorMessage(code, t('Your session ended.')))
  const here = router.currentRoute.value.fullPath
  void router.push({ name: 'login', query: here !== '/' ? { next: here } : {} })
})
