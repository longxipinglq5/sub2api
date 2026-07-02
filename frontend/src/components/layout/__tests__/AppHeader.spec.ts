import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AppHeader from '../AppHeader.vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import type { User } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => ({
        'nav.adminDashboard': '管理后台',
        'nav.profile': '个人资料',
        'nav.apiKeys': 'API 密钥',
        'nav.github': 'GitHub',
        'nav.logout': '退出登录',
        'common.balance': '余额',
        'common.contactSupport': '联系支持',
        'onboarding.restartTour': '重新引导'
      }[key] ?? key)
    })
  }
})

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/profile', component: { template: '<div />' } },
      { path: '/keys', component: { template: '<div />' } },
      { path: '/admin/dashboard', component: { template: '<div />' } },
      { path: '/login', component: { template: '<div />' } }
    ]
  })
}

function makeUser(role: User['role']): User {
  return {
    id: 1,
    username: 'alice',
    email: 'alice@example.com',
    role,
    balance: 0,
    concurrency: 0,
    status: 'active',
    allowed_groups: null,
    balance_notify_enabled: false,
    balance_notify_threshold: null,
    balance_notify_extra_emails: [],
    created_at: '2026-06-30T00:00:00Z',
    updated_at: '2026-06-30T00:00:00Z'
  }
}

async function mountHeaderFor(role: User['role']) {
  const pinia = createPinia()
  setActivePinia(pinia)

  const authStore = useAuthStore()
  authStore.$patch({
    user: makeUser(role),
    token: 'token'
  })

  const appStore = useAppStore()
  appStore.publicSettingsLoaded = true
  appStore.cachedPublicSettings = { backend_mode_enabled: false, custom_menu_items: [] } as any

  const router = createTestRouter()

  await router.push('/')
  await router.isReady()

  const wrapper = mount(AppHeader, {
    global: {
      plugins: [pinia, router],
      stubs: {
        AnnouncementBell: true,
        LocaleSwitcher: true,
        SubscriptionProgressMini: true
      },
      mocks: {
        $t: (key: string) => ({
          'onboarding.restartTour': '重新引导'
        }[key] ?? key)
      }
    }
  })

  await wrapper.get('button[aria-label="User Menu"]').trigger('click')
  await wrapper.vm.$nextTick()

  return wrapper
}

describe('AppHeader admin entry', () => {
  beforeEach(() => {
    window.localStorage.clear()
  })

  it('shows an admin dashboard link in the user dropdown for admins', async () => {
    const wrapper = await mountHeaderFor('admin')

    const adminLink = wrapper.find('a[href="/admin/dashboard"]')

    expect(adminLink.exists()).toBe(true)
    expect(adminLink.text()).toContain('管理后台')
  })

  it('does not show the admin dashboard link for normal users', async () => {
    const wrapper = await mountHeaderFor('user')

    expect(wrapper.find('a[href="/admin/dashboard"]').exists()).toBe(false)
  })
})
