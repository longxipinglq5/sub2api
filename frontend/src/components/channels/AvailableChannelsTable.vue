<template>
  <div
    v-if="loading"
    class="grid grid-cols-1 gap-4 xl:grid-cols-2"
    aria-busy="true"
  >
    <div
      v-for="index in 4"
      :key="index"
      class="card min-h-64 animate-pulse overflow-hidden"
    >
      <div class="border-b border-gray-100 p-5 dark:border-dark-700">
        <div class="h-5 w-36 rounded bg-gray-200 dark:bg-dark-700"></div>
        <div class="mt-3 h-3 w-52 max-w-full rounded bg-gray-100 dark:bg-dark-800"></div>
      </div>
      <div class="space-y-5 p-5">
        <div class="h-6 w-24 rounded bg-gray-100 dark:bg-dark-800"></div>
        <div class="grid grid-cols-2 gap-5">
          <div class="h-14 rounded bg-gray-100 dark:bg-dark-800"></div>
          <div class="h-14 rounded bg-gray-100 dark:bg-dark-800"></div>
        </div>
      </div>
    </div>
  </div>

  <div v-else-if="rows.length === 0" class="card py-14 text-center">
    <Icon name="inbox" size="xl" class="mx-auto mb-3 h-12 w-12 text-gray-400" />
    <p class="text-sm text-gray-500 dark:text-gray-400">{{ emptyLabel }}</p>
  </div>

  <div v-else class="grid grid-cols-1 items-start gap-4 xl:grid-cols-2">
    <article
      v-for="(channel, chIdx) in rows"
      :key="`${channel.name}-${chIdx}`"
      class="card min-w-0 overflow-hidden transition-colors hover:border-gray-300 dark:hover:border-dark-500"
      :aria-label="`${columns.name}: ${channel.name}`"
    >
      <header class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">
          {{ channel.name }}
        </h2>
        <p class="mt-1 min-h-5 text-sm text-gray-500 dark:text-gray-400">
          {{ channel.description || '-' }}
        </p>
      </header>

      <div class="divide-y divide-gray-100 dark:divide-dark-700">
        <section
          v-for="section in channel.platforms"
          :key="`${channel.name}-${section.platform}`"
          class="px-5 py-4"
        >
          <div class="mb-4 flex items-center justify-between gap-3">
            <span
              :class="[
                'inline-flex items-center gap-1 rounded-md border px-2 py-1 text-[11px] font-medium uppercase',
                platformBadgeClass(section.platform),
              ]"
              :title="columns.platform"
            >
              <PlatformIcon :platform="section.platform as GroupPlatform" size="xs" />
              {{ section.platform }}
            </span>
          </div>

          <div class="grid min-w-0 grid-cols-1 gap-5 sm:grid-cols-2">
            <div class="min-w-0">
              <h3 class="mb-2 text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ columns.groups }}
              </h3>
              <div class="flex flex-col gap-2">
                <div
                  v-if="exclusiveGroups(section).length > 0"
                  class="flex flex-wrap items-center gap-1.5"
                >
                  <span
                    class="inline-flex items-center gap-0.5 text-[10px] font-medium text-purple-600 dark:text-purple-400"
                    :title="t('availableChannels.exclusiveTooltip')"
                  >
                    <Icon name="shield" size="xs" class="h-3 w-3" />
                    {{ t('availableChannels.exclusive') }}
                  </span>
                  <GroupBadge
                    v-for="g in exclusiveGroups(section)"
                    :key="`ex-${g.id}`"
                    :name="g.name"
                    :platform="g.platform as GroupPlatform"
                    :subscription-type="(g.subscription_type || 'standard') as SubscriptionType"
                    :rate-multiplier="g.rate_multiplier"
                    :user-rate-multiplier="userGroupRates[g.id] ?? null"
                    always-show-rate
                  />
                </div>
                <div
                  v-if="publicGroups(section).length > 0"
                  class="flex flex-wrap items-center gap-1.5"
                >
                  <span
                    class="inline-flex items-center gap-0.5 text-[10px] font-medium text-gray-500 dark:text-gray-400"
                    :title="t('availableChannels.publicTooltip')"
                  >
                    <Icon name="globe" size="xs" class="h-3 w-3" />
                    {{ t('availableChannels.public') }}
                  </span>
                  <GroupBadge
                    v-for="g in publicGroups(section)"
                    :key="`pub-${g.id}`"
                    :name="g.name"
                    :platform="g.platform as GroupPlatform"
                    :subscription-type="(g.subscription_type || 'standard') as SubscriptionType"
                    :rate-multiplier="g.rate_multiplier"
                    :user-rate-multiplier="userGroupRates[g.id] ?? null"
                    always-show-rate
                  />
                </div>
                <span v-if="section.groups.length === 0" class="text-xs text-gray-400">-</span>
              </div>
            </div>

            <div class="min-w-0">
              <h3 class="mb-2 text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ columns.supportedModels }}
              </h3>
              <div class="flex flex-wrap gap-1.5">
                <SupportedModelChip
                  v-for="m in section.supported_models"
                  :key="`${section.platform}-${m.name}`"
                  :model="m"
                  :pricing-key-prefix="pricingKeyPrefix"
                  :no-pricing-label="noPricingLabel"
                  :show-platform="false"
                  :platform-hint="section.platform"
                />
                <span v-if="section.supported_models.length === 0" class="text-xs text-gray-400">
                  {{ noModelsLabel }}
                </span>
              </div>
            </div>
          </div>
        </section>
      </div>
    </article>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import SupportedModelChip from './SupportedModelChip.vue'
import type { UserAvailableChannel, UserAvailableGroup, UserChannelPlatformSection } from '@/api/channels'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { platformBadgeClass } from '@/utils/platformColors'

const props = defineProps<{
  columns: {
    name: string
    description: string
    platform: string
    groups: string
    supportedModels: string
  }
  rows: UserAvailableChannel[]
  loading: boolean
  pricingKeyPrefix: string
  noPricingLabel: string
  noModelsLabel: string
  emptyLabel: string
  /** 用户专属倍率（group_id → multiplier）；无专属时由 GroupBadge 仅显示默认倍率。 */
  userGroupRates: Record<number, number>
}>()

// Suppress unused warning — props is accessed via template automatically but
// the explicit reference here keeps the linter from flagging userGroupRates.
void props.userGroupRates

const { t } = useI18n()

function exclusiveGroups(section: UserChannelPlatformSection): UserAvailableGroup[] {
  return section.groups.filter((g) => g.is_exclusive)
}

function publicGroups(section: UserChannelPlatformSection): UserAvailableGroup[] {
  return section.groups.filter((g) => !g.is_exclusive)
}
</script>
