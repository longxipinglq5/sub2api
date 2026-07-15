import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import TablePageLayout from '../TablePageLayout.vue'

describe('TablePageLayout', () => {
  it('keeps the framed container for table content by default', () => {
    const wrapper = mount(TablePageLayout, {
      slots: { table: '<div>Rows</div>' },
    })

    expect(wrapper.find('.table-scroll-container').exists()).toBe(true)
    expect(wrapper.find('.cards-scroll-container').exists()).toBe(false)
  })

  it('uses a padded unframed container for card content', () => {
    const wrapper = mount(TablePageLayout, {
      props: { contentVariant: 'cards' },
      slots: { table: '<div>Cards</div>' },
    })

    const container = wrapper.get('.cards-scroll-container')
    expect(wrapper.find('.table-scroll-container').exists()).toBe(false)
    expect(container.exists()).toBe(true)
    expect(container.classes()).not.toContain('card')
  })
})
