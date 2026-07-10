import { describe, expect, it } from 'vitest'

import { createGroupPlatformOptions, createGroupPlatformFilterOptions } from '../groupsPlatformOptions'

describe('groupsPlatformOptions', () => {
  it('includes Qwen when creating or editing a group', () => {
    expect(createGroupPlatformOptions()).toContainEqual({ value: 'qwen', label: 'Qwen' })
  })

  it('includes Qwen in the group platform filter', () => {
    expect(createGroupPlatformFilterOptions()).toContainEqual({ value: 'qwen', label: 'Qwen' })
  })
})
