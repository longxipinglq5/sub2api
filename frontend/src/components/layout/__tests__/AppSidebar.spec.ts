import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AppSidebar from '../AppSidebar.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

function makeUser(): User {
  return {
    id: 1,
    username: 'admin',
    email: 'admin@example.com',
    role: 'admin',
    balance: 0,
    concurrency: 1,
    status: 'active',
    allowed_groups: null,
    balance_notify_enabled: false,
    balance_notify_threshold: null,
    balance_notify_extra_emails: [],
    created_at: '2026-07-07T00:00:00Z',
    updated_at: '2026-07-07T00:00:00Z'
  }
}

async function mountSidebar() {
  const pinia = createPinia()
  setActivePinia(pinia)

  const appStore = useAppStore()
  appStore.$patch({
    publicSettingsLoaded: true,
    siteName: '企数Token',
    siteVersion: 'v0.1.139',
    cachedPublicSettings: { backend_mode_enabled: false, custom_menu_items: [] }
  })

  const authStore = useAuthStore()
  authStore.$patch({
    user: makeUser(),
    token: 'token'
  })

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/admin/dashboard', component: { template: '<div />' } }
    ]
  })
  await router.push('/admin/dashboard')
  await router.isReady()

  return mount(AppSidebar, {
    global: {
      plugins: [pinia, router],
      stubs: {
        VersionBadge: {
          template: '<span data-testid="version-badge">version</span>'
        }
      }
    }
  })
}

describe('AppSidebar branding', () => {
  beforeEach(() => {
    window.localStorage.clear()
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn()
      }))
    })
  })

  it('shows the branded product name without an upgrade/version badge', async () => {
    const wrapper = await mountSidebar()

    expect(wrapper.text()).toContain('企数Token')
    expect(wrapper.text()).not.toContain('Sub2API')
    expect(wrapper.find('[data-testid="version-badge"]').exists()).toBe(false)
  })
})
