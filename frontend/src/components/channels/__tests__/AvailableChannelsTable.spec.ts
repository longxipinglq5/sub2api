import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import AvailableChannelsTable from '../AvailableChannelsTable.vue'
import type { UserAvailableChannel } from '@/api/channels'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

const rows: UserAvailableChannel[] = [
  {
    name: 'ChatGPT Channel',
    description: 'GPT models',
    platforms: [
      {
        platform: 'openai',
        groups: [
          {
            id: 1,
            name: 'openai-default',
            platform: 'openai',
            subscription_type: 'standard',
            rate_multiplier: 1,
            is_exclusive: false,
          },
        ],
        supported_models: [{ name: 'gpt-5.5', platform: 'openai', pricing: null }],
      },
    ],
  },
  {
    name: 'Qwen',
    description: 'Wan image models',
    platforms: [
      {
        platform: 'qwen',
        groups: [],
        supported_models: [{ name: 'wan2.6-t2i', platform: 'qwen', pricing: null }],
      },
    ],
  },
]

const baseProps = {
  columns: {
    name: 'Channel',
    description: 'Description',
    platform: 'Platform',
    groups: 'Groups',
    supportedModels: 'Models',
  },
  rows,
  loading: false,
  pricingKeyPrefix: 'availableChannels.pricing',
  noPricingLabel: 'No pricing',
  noModelsLabel: 'No models',
  emptyLabel: 'No channels',
  userGroupRates: {},
}

function render(overrides: Partial<typeof baseProps> = {}) {
  return mount(AvailableChannelsTable, {
    props: { ...baseProps, ...overrides },
    global: {
      stubs: {
        Icon: true,
        PlatformIcon: true,
        GroupBadge: {
          props: ['name'],
          template: '<span data-testid="group">{{ name }}</span>',
        },
        SupportedModelChip: {
          props: ['model'],
          template: '<span data-testid="model">{{ model.name }}</span>',
        },
      },
    },
  })
}

describe('AvailableChannelsTable', () => {
  it('renders one card per channel instead of table rows', () => {
    const wrapper = render()

    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.findAll('article')).toHaveLength(2)
    expect(wrapper.get('[data-testid="channel-card-grid"]').classes()).toContain('items-stretch')
    expect(wrapper.findAll('article').every((card) => card.classes().includes('h-full'))).toBe(true)
    expect(wrapper.text()).toContain('ChatGPT Channel')
    expect(wrapper.text()).toContain('openai-default')
    expect(wrapper.text()).toContain('gpt-5.5')
    expect(wrapper.text()).toContain('Qwen')
    expect(wrapper.text()).toContain('wan2.6-t2i')
  })

  it('renders a stable card skeleton while loading', () => {
    const wrapper = render({ loading: true })

    expect(wrapper.findAll('article')).toHaveLength(0)
    expect(wrapper.findAll('.animate-pulse')).toHaveLength(4)
  })

  it('renders the empty state when no channels are available', () => {
    const wrapper = render({ rows: [] })

    expect(wrapper.findAll('article')).toHaveLength(0)
    expect(wrapper.text()).toContain('No channels')
  })
})
