<template>
  <AppLayout>
    <div class="mx-auto max-w-2xl space-y-6">
      <!-- Today Check-in Card -->
      <div class="card overflow-hidden">
        <div class="bg-gradient-to-br from-primary-500 to-primary-600 px-6 py-8 text-center">
          <div
            class="mb-4 inline-flex h-16 w-16 items-center justify-center rounded-2xl bg-white/20 backdrop-blur-sm"
          >
            <Icon name="calendar" size="xl" class="text-white" />
          </div>
          <p class="text-sm font-medium text-primary-100">{{ t('checkin.title') }}</p>
          <p class="mt-2 text-4xl font-bold text-white">
            {{ t('checkin.streakDays', { days: status?.streak_days ?? 0 }) }}
          </p>
          <p class="mt-2 text-sm text-primary-100">
            {{ todayRangeLabel }}
          </p>
        </div>
      </div>

      <!-- Not Enabled -->
      <div v-if="loaded && status && !status.enabled" class="card">
        <div class="p-6 text-center">
          <p class="text-sm font-medium text-gray-900 dark:text-white">
            {{ t('checkin.notEnabledTitle') }}
          </p>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
            {{ t('checkin.notEnabledDesc') }}
          </p>
        </div>
      </div>

      <!-- Check-in Button -->
      <div v-else class="card">
        <div class="p-6">
          <div v-if="status?.today_checked_in" class="text-center">
            <div
              class="mb-3 inline-flex h-10 w-10 items-center justify-center rounded-xl bg-emerald-100 dark:bg-emerald-900/30"
            >
              <Icon name="checkCircle" size="md" class="text-emerald-600 dark:text-emerald-400" />
            </div>
            <p class="text-sm font-medium text-gray-900 dark:text-white">
              {{ t('checkin.alreadyCheckedIn') }}
            </p>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
              {{ t('checkin.todayAwarded', { amount: formatAmount(status?.today_amount ?? 0) }) }}
            </p>
          </div>
          <button
            v-else
            type="button"
            :disabled="submitting || !status?.enabled"
            class="btn btn-primary w-full py-3"
            @click="handleCheckin"
          >
            <svg
              v-if="submitting"
              class="-ml-1 mr-2 h-5 w-5 animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            <Icon v-else name="sparkles" size="md" class="mr-2" />
            {{ submitting ? t('checkin.checkingIn') : t('checkin.checkinButton') }}
          </button>
        </div>
      </div>

      <!-- Success Message -->
      <transition name="fade">
        <div
          v-if="checkinResult"
          class="card border-emerald-200 bg-emerald-50 dark:border-emerald-800/50 dark:bg-emerald-900/20"
        >
          <div class="p-6">
            <div class="flex items-start gap-4">
              <div
                class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-emerald-100 dark:bg-emerald-900/30"
              >
                <Icon name="checkCircle" size="md" class="text-emerald-600 dark:text-emerald-400" />
              </div>
              <div class="flex-1">
                <h3 class="text-sm font-semibold text-emerald-800 dark:text-emerald-300">
                  {{ t('checkin.checkinSuccess') }}
                </h3>
                <div class="mt-2 space-y-1 text-sm text-emerald-700 dark:text-emerald-400">
                  <p class="font-medium">
                    {{ t('checkin.awarded') }}:
                    {{ formatAmount(checkinResult.amount_awarded) }}
                  </p>
                  <p v-if="checkinResult.streak_bonus_applied">
                    {{ t('checkin.streakBonusHit', { days: checkinResult.streak_days }) }}
                    (+{{ formatAmount(checkinResult.streak_bonus_amount) }})
                  </p>
                  <p>
                    {{ t('checkin.streakDays', { days: checkinResult.streak_days }) }}
                  </p>
                  <p>
                    {{ t('redeem.newBalance') }}:
                    <span class="font-semibold">${{ checkinResult.new_balance.toFixed(2) }}</span>
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>
      </transition>

      <!-- Error Message -->
      <transition name="fade">
        <div
          v-if="errorMessage"
          class="card border-red-200 bg-red-50 dark:border-red-800/50 dark:bg-red-900/20"
        >
          <div class="p-6">
            <div class="flex items-start gap-4">
              <div
                class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-red-100 dark:bg-red-900/30"
              >
                <Icon name="exclamationCircle" size="md" class="text-red-600 dark:text-red-400" />
              </div>
              <div class="flex-1">
                <h3 class="text-sm font-semibold text-red-800 dark:text-red-300">
                  {{ t('checkin.checkinFailed') }}
                </h3>
                <p class="mt-2 text-sm text-red-700 dark:text-red-400">
                  {{ errorMessage }}
                </p>
              </div>
            </div>
          </div>
        </div>
      </transition>

      <!-- Month Calendar -->
      <div class="card">
        <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('checkin.monthCalendar') }}
          </h2>
        </div>
        <div class="p-6">
          <div class="mb-2 grid grid-cols-7 gap-2 text-center text-xs font-medium text-gray-400">
            <span v-for="label in weekdayLabels" :key="label">{{ label }}</span>
          </div>
          <div class="grid grid-cols-7 gap-2">
            <span v-for="blank in monthLeadingBlanks" :key="`blank-${blank}`"></span>
            <div
              v-for="day in monthDays"
              :key="day.date"
              :class="[
                'flex flex-col items-center justify-center rounded-lg px-1 py-2 text-xs',
                day.checked
                  ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                  : 'bg-gray-50 text-gray-400 dark:bg-dark-800 dark:text-dark-400'
              ]"
            >
              <span class="font-medium">{{ day.dayOfMonth }}</span>
              <span v-if="day.checked" class="mt-0.5 text-[10px] leading-none">
                +{{ formatAmount(day.amount) }}
              </span>
            </div>
          </div>
        </div>
      </div>

      <!-- Cumulative Stats -->
      <div class="card">
        <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('checkin.stats.title') }}
          </h2>
        </div>
        <div class="grid grid-cols-2 gap-4 p-6">
          <div class="rounded-xl bg-gray-50 p-4 text-center dark:bg-dark-800">
            <p class="text-2xl font-bold text-gray-900 dark:text-white">
              {{ status?.total_checkin_count ?? 0 }}
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('checkin.stats.totalCount') }}
            </p>
          </div>
          <div class="rounded-xl bg-gray-50 p-4 text-center dark:bg-dark-800">
            <p class="text-2xl font-bold text-gray-900 dark:text-white">
              {{ formatAmount(status?.total_awarded_amount ?? 0) }}
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('checkin.stats.totalAmount') }}
            </p>
          </div>
          <div class="rounded-xl bg-gray-50 p-4 text-center dark:bg-dark-800">
            <p class="text-2xl font-bold text-gray-900 dark:text-white">
              {{ status?.streak_days ?? 0 }}
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('checkin.stats.currentStreak') }}
            </p>
          </div>
          <div class="rounded-xl bg-gray-50 p-4 text-center dark:bg-dark-800">
            <p class="text-2xl font-bold text-gray-900 dark:text-white">
              {{ formatAmount(status?.streak_bonus_amount ?? 0) }}
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('checkin.stats.streakBonus', { days: status?.streak_bonus_days ?? 7 }) }}
            </p>
          </div>
        </div>
      </div>

      <!-- Rules -->
      <div
        class="card border-primary-200 bg-primary-50 dark:border-primary-800/50 dark:bg-primary-900/20"
      >
        <div class="p-6">
          <div class="flex items-start gap-4">
            <div
              class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-xl bg-primary-100 dark:bg-primary-900/30"
            >
              <Icon name="infoCircle" size="md" class="text-primary-600 dark:text-primary-400" />
            </div>
            <div class="flex-1">
              <h3 class="text-sm font-semibold text-primary-800 dark:text-primary-300">
                {{ t('checkin.rules.title') }}
              </h3>
              <ul
                class="mt-2 list-inside list-disc space-y-1 text-sm text-primary-700 dark:text-primary-400"
              >
                <li>{{ t('checkin.rules.rule1') }}</li>
                <li>{{ t('checkin.rules.rule2') }}</li>
                <li>{{ t('checkin.rules.rule3') }}</li>
              </ul>
            </div>
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { checkinAPI, type CheckinStatus, type CheckinResult } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'

const { t, locale } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const status = ref<CheckinStatus | null>(null)
const loaded = ref(false)
const submitting = ref(false)
const checkinResult = ref<CheckinResult | null>(null)
const errorMessage = ref('')

const formatAmount = (value: number) => `$${value.toFixed(2)}`

const todayRangeLabel = computed(() => {
  if (!status.value) return ''
  return t('checkin.rewardRange', {
    min: formatAmount(status.value.min_amount),
    max: formatAmount(status.value.max_amount)
  })
})

// 本月日历：格子按浏览器本地月份生成，签到标记以服务端返回的 YYYY-MM-DD 精确匹配。
const monthRecordsByDate = computed(() => {
  const map = new Map<string, number>()
  for (const record of status.value?.month_records ?? []) {
    map.set(record.date, record.amount)
  }
  return map
})

const formatMonthDate = (year: number, month: number, day: number) => {
  const monthStr = String(month + 1).padStart(2, '0')
  const dayStr = String(day).padStart(2, '0')
  return `${year}-${monthStr}-${dayStr}`
}

interface CalendarDay {
  date: string
  dayOfMonth: number
  checked: boolean
  amount: number
}

const monthDays = computed<CalendarDay[]>(() => {
  const now = new Date()
  const year = now.getFullYear()
  const month = now.getMonth()
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  const days: CalendarDay[] = []
  for (let day = 1; day <= daysInMonth; day++) {
    const date = formatMonthDate(year, month, day)
    const amount = monthRecordsByDate.value.get(date)
    days.push({
      date,
      dayOfMonth: day,
      checked: amount !== undefined,
      amount: amount ?? 0
    })
  }
  return days
})

const monthLeadingBlanks = computed(() => {
  const now = new Date()
  // getDay(): 0=Sunday，与 weekdayLabels 的起点一致
  return new Date(now.getFullYear(), now.getMonth(), 1).getDay()
})

const weekdayLabels = computed(() => {
  // 以周日为一周起点，与日历网格的排布保持一致
  const keys = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat']
  return keys.map((key) => t(`checkin.weekdays.${key}`))
})

const loadStatus = async () => {
  try {
    status.value = await checkinAPI.getStatus()
  } catch (error) {
    console.error('Failed to load check-in status:', error)
    appStore.showError(t('checkin.loadFailed'))
  } finally {
    loaded.value = true
  }
}

const handleCheckin = async () => {
  submitting.value = true
  errorMessage.value = ''
  checkinResult.value = null

  try {
    const result = await checkinAPI.checkin()
    checkinResult.value = result
    appStore.showSuccess(
      t('checkin.checkinSuccessToast', { amount: formatAmount(result.amount_awarded) })
    )
    // 刷新面板数据与用户余额（余额在页头/侧栏展示）
    await loadStatus()
    try {
      await authStore.refreshUser()
    } catch (error) {
      console.error('Failed to refresh user after check-in:', error)
    }
  } catch (error: any) {
    errorMessage.value = error.response?.data?.detail || t('checkin.failedToCheckin')
    appStore.showError(t('checkin.checkinFailed'))
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  loadStatus()
  // locale 仅用于保持 i18n 依赖显式（weekday 文案随语言切换）
  void locale
})
</script>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: all 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
