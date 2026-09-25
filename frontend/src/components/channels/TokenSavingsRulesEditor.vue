<template>
  <div class="space-y-3">
    <p class="text-sm">{{ t('tokenBank.receivingGroups') }}</p>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('tokenBank.receivingRulesHint') }}</p>
    <p v-if="!groups.length" class="text-xs text-gray-500 dark:text-gray-400">{{ t('tokenBank.selectChannelGroupsFirst') }}</p>
    <div v-for="group in groups" :key="group.id" class="space-y-3">
      <label class="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          :data-testid="`receiving-group-${group.id}`"
          :checked="groupIds.includes(group.id)"
          @change="selectGroup(group.id, ($event.target as HTMLInputElement).checked)"
        />
        <span>{{ group.platform }} · {{ group.name }}</span>
      </label>
      <div v-if="groupIds.includes(group.id)" class="ml-6 grid gap-3 sm:grid-cols-2">
        <label class="block text-sm">
          {{ t('tokenBank.receivingPriority') }}
          <input
            type="number" min="0" max="1000" step="1" required class="input mt-1"
            :data-testid="`receiving-priority-${group.id}`"
            :value="ruleFor(group.id).priority"
            @input="updateRule(group.id, { priority: ($event.target as HTMLInputElement).valueAsNumber })"
          />
        </label>
        <label v-if="group.platform === 'openai'" class="block text-sm">
          {{ t('tokenBank.receivingAllowedPlans') }}
          <select
            multiple required :size="4" class="input mt-1"
            :data-testid="`receiving-plans-${group.id}`"
            @change="updatePlans(group.id, $event)"
          >
            <option v-for="plan in OPENAI_SAVINGS_PLANS" :key="plan.value" :value="plan.value" :selected="ruleFor(group.id).allowed_plans.includes(plan.value)">{{ plan.label }}</option>
          </select>
          <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('tokenBank.receivingAllowedPlansHint') }}</span>
        </label>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { OPENAI_SAVINGS_PLANS, type ReceivingGroup, type ReceivingRule } from './tokenSavingsRules'

const props = defineProps<{ groups: ReceivingGroup[]; groupIds: number[]; rules: ReceivingRule[] }>()
const emit = defineEmits<{
  (event: 'update:groupIds', value: number[]): void
  (event: 'update:rules', value: ReceivingRule[]): void
}>()
const { t } = useI18n()
const ruleFor = (id: number): ReceivingRule => props.rules.find(rule => rule.group_id === id) || { group_id: id, priority: 0, allowed_plans: [] }

function selectGroup(id: number, selected: boolean) {
  emit('update:groupIds', selected ? [...props.groupIds, id] : props.groupIds.filter(groupId => groupId !== id))
  emit('update:rules', selected ? [...props.rules.filter(rule => rule.group_id !== id), ruleFor(id)] : props.rules.filter(rule => rule.group_id !== id))
}
function updateRule(id: number, patch: Partial<ReceivingRule>) {
  const next = { ...ruleFor(id), ...patch }
  emit('update:rules', props.rules.some(rule => rule.group_id === id)
    ? props.rules.map(rule => rule.group_id === id ? next : rule)
    : [...props.rules, next])
}
function updatePlans(id: number, event: Event) {
  updateRule(id, { allowed_plans: Array.from((event.target as HTMLSelectElement).selectedOptions, option => option.value) })
}
</script>
