import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import UserCreateModal from '../UserCreateModal.vue'

const { createUser } = vi.hoisted(() => ({
  createUser: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      create: createUser
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

describe('UserCreateModal', () => {
  beforeEach(() => {
    createUser.mockReset()
    createUser.mockResolvedValue({})
  })

  it('submits an empty RPM limit as numeric zero', async () => {
    const wrapper = mount(UserCreateModal, {
      props: {
        show: true
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>'
          },
          Icon: true
        }
      }
    })

    await wrapper.get('input[type="email"]').setValue('bruce@qitus.cn')
    await wrapper.get('input[type="text"]').setValue('8883084lxp')

    const numberInputs = wrapper.findAll('input[type="number"]')
    await numberInputs[0].setValue('100')
    await numberInputs[1].setValue('1')
    await numberInputs[2].setValue('')

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(createUser).toHaveBeenCalledWith(
      expect.objectContaining({
        balance: 100,
        concurrency: 1,
        rpm_limit: 0
      })
    )
  })
})
