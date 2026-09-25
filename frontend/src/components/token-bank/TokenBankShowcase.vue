<template>
  <div
    v-if="showcase?.enabled"
    data-testid="showcase"
    class="card flex min-w-0 items-center gap-3 px-4 py-3 text-sm"
    @mouseenter="hovered = true"
    @mouseleave="hovered = false"
  >
    <Icon name="speakerWave" class="shrink-0 text-primary-600 dark:text-primary-400" aria-hidden="true" />
    <div class="min-w-0 flex-1 overflow-hidden">
      <Transition name="broadcast" mode="out-in">
        <p :key="broadcast" class="truncate text-gray-700 dark:text-gray-300" :title="broadcast">{{ broadcast }}</p>
      </Transition>
    </div>
    <button
      v-if="showcase.recent.length > 1 && !reducedMotion"
      type="button"
      data-testid="broadcast-pause"
      class="shrink-0 text-gray-500 hover:text-primary-600 focus-visible:outline-primary-500 dark:text-gray-400"
      :aria-label="t(paused ? 'tokenBank.showcaseResume' : 'tokenBank.showcasePause')"
      :aria-pressed="paused"
      @click="paused = !paused"
    ><Icon :name="paused ? 'play' : 'pause'" size="sm" /></button>
    <button
      type="button"
      data-testid="leaderboard-open"
      class="shrink-0 whitespace-nowrap font-medium text-primary-600 hover:underline focus-visible:outline-primary-500 dark:text-primary-400"
      @click="showLeaderboard = true"
    >{{ t('tokenBank.showcaseLeaderboard') }}</button>
  </div>
  <BaseDialog :show="showLeaderboard && !!showcase?.enabled" :title="t('tokenBank.showcaseLeaderboard')" @close="showLeaderboard = false">
    <p class="mb-4 text-sm text-gray-500 dark:text-gray-400">{{ t('tokenBank.showcaseLeaderboardHint') }}</p>
    <table v-if="showcase?.leaderboard.length" class="w-full text-left text-sm">
      <thead class="text-gray-500 dark:text-gray-400">
        <tr><th scope="col" class="py-3 pr-3">{{ t('tokenBank.showcaseRank') }}</th><th scope="col" class="py-3 pr-3">{{ t('tokenBank.showcaseUser') }}</th><th scope="col" class="py-3 text-right">{{ t('tokenBank.total') }}</th></tr>
      </thead>
      <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
        <tr v-for="entry in showcase.leaderboard.slice(0, 10)" :key="entry.rank">
          <td class="py-3 pr-3 text-gray-500">{{ entry.rank }}</td>
          <td class="py-3 pr-3">{{ entry.name }}</td>
          <td class="py-3 text-right tabular-nums text-primary-600 dark:text-primary-400">{{ money(entry.amount) }}</td>
        </tr>
      </tbody>
    </table>
    <p v-else class="py-6 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('tokenBank.showcaseEmpty') }}</p>
    <p v-if="showcase?.updated_at" class="mt-4 text-xs text-gray-500 dark:text-gray-400">{{ t('tokenBank.showcaseUpdated', { time: formatDateTime(showcase.updated_at) }) }}</p>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { tokenBankAPI, type TokenBankShowcase } from '@/api/tokenBank'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
import { formatDateTime } from '@/utils/format'

const { t, locale } = useI18n()
const showcase = ref<TokenBankShowcase>()
const showLeaderboard = ref(false)
const index = ref(0)
const paused = ref(false)
const hovered = ref(false)
const reducedMotion = ref(false)
const money = (n: number) => new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', maximumFractionDigits: 8 }).format(n)
const broadcast = computed(() => {
  const entry = showcase.value?.recent[index.value]
  if (!entry) return t('tokenBank.showcaseEmpty')
  const platform = CONCRETE_PLATFORM_OPTIONS.find(p => p.value === entry.platform)?.label || entry.platform
  return t('tokenBank.showcaseBroadcast', { name: entry.name, platform, amount: money(entry.amount) })
})
let refreshTimer: ReturnType<typeof setInterval> | undefined
let rotationTimer: ReturnType<typeof setInterval> | undefined
let controller: AbortController | undefined
let motionQuery: MediaQueryList | undefined
let disposed = false

async function refresh() {
  if (document.hidden || disposed) return
  controller?.abort()
  const request = new AbortController()
  controller = request
  try {
    const result = await tokenBankAPI.showcase(request.signal)
    if (request.signal.aborted || disposed) return
    showcase.value = result
    index.value %= Math.max(result.recent.length, 1)
    if (!result.enabled) showLeaderboard.value = false
  } catch {
    if (request.signal.aborted || disposed) return
    showcase.value = undefined
    showLeaderboard.value = false
  }
}
function stopTimers() {
  clearInterval(refreshTimer)
  clearInterval(rotationTimer)
}
function onVisibilityChange() {
  stopTimers()
  if (document.hidden) { controller?.abort(); return }
  void refresh()
  refreshTimer = setInterval(() => { void refresh() }, 60_000)
  rotationTimer = setInterval(() => {
    const length = showcase.value?.recent.length || 0
    if (length > 1 && !paused.value && !hovered.value && !reducedMotion.value && !showLeaderboard.value) index.value = (index.value + 1) % length
  }, 8_000)
}
function onMotionChange() { reducedMotion.value = !!motionQuery?.matches }
onMounted(() => {
  motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
  onMotionChange()
  motionQuery.addEventListener('change', onMotionChange)
  document.addEventListener('visibilitychange', onVisibilityChange)
  onVisibilityChange()
})
onUnmounted(() => {
  disposed = true
  controller?.abort()
  stopTimers()
  motionQuery?.removeEventListener('change', onMotionChange)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<style scoped>
.broadcast-enter-active, .broadcast-leave-active { transition: transform 180ms ease, opacity 180ms ease; }
.broadcast-enter-from { transform: translateY(100%); opacity: 0; }
.broadcast-leave-to { transform: translateY(-100%); opacity: 0; }
@media (prefers-reduced-motion: reduce) {
  .broadcast-enter-active, .broadcast-leave-active { transition: none; }
  .broadcast-enter-from, .broadcast-leave-to { transform: none; }
}
</style>
