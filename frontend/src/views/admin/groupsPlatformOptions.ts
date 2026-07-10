const GROUP_PLATFORM_OPTIONS = [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'qwen', label: 'Qwen' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
  { value: 'grok', label: 'Grok' },
] as const

export function createGroupPlatformOptions() {
  return GROUP_PLATFORM_OPTIONS.map((option) => ({ ...option }))
}

export function createGroupPlatformFilterOptions(allPlatformsLabel = 'All Platforms') {
  return [
    { value: '', label: allPlatformsLabel },
    ...createGroupPlatformOptions(),
  ]
}
