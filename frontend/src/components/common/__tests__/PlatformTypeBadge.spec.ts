import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import PlatformTypeBadge from '../PlatformTypeBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

describe('PlatformTypeBadge', () => {
  it('labels Qwen accounts as Qwen instead of falling back to Gemini', () => {
    const wrapper = mount(PlatformTypeBadge, {
      props: {
        platform: 'qwen',
        type: 'apikey',
      },
      global: {
        stubs: {
          PlatformIcon: true,
          Icon: true,
        },
      },
    })

    expect(wrapper.text()).toContain('Qwen')
    expect(wrapper.text()).not.toContain('Gemini')
  })
})
