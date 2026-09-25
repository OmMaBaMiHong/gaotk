<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.kimiOAuth.connect')"
    width="normal"
    :close-on-escape="step !== 'creating'"
    @close="handleClose"
  >
    <div v-if="error" class="mb-4 flex items-start gap-2 rounded-lg border border-red-300 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/40 dark:bg-red-500/10 dark:text-red-400">
      <Icon name="exclamationCircle" size="sm" class="mt-0.5 shrink-0" />
      <span>{{ error }}</span>
    </div>

    <!-- 开始授权 -->
    <div v-if="step === 'start'" class="space-y-4">
      <p class="text-sm text-gray-600 dark:text-gray-300">
        {{ t('admin.accounts.kimiOAuth.startDesc') }}
      </p>
      <button type="button" class="btn btn-primary w-full" :disabled="starting" @click="startFlow">
        {{ starting ? '…' : t('admin.accounts.kimiOAuth.start') }}
      </button>
    </div>

    <!-- 等待授权 -->
    <div v-else-if="step === 'waiting'" class="space-y-4">
      <div class="flex items-center gap-3 rounded-lg border border-amber-300/60 bg-amber-50 p-3 dark:border-amber-500/30 dark:bg-amber-500/10">
        <div class="h-4 w-4 shrink-0 animate-spin rounded-full border-2 border-amber-500 border-t-transparent" />
        <span class="text-sm text-amber-800 dark:text-amber-200">
          {{ t('admin.accounts.kimiOAuth.waiting') }}
        </span>
      </div>

      <div>
        <label class="input-label">{{ t('admin.accounts.kimiOAuth.userCodeLabel') }}</label>
        <div class="flex items-center gap-2">
          <code class="flex-1 select-all rounded-lg border border-gray-300 bg-gray-100 px-3 py-2 text-lg font-mono font-bold tracking-widest text-center text-gray-900 dark:border-dark-600 dark:bg-dark-800 dark:text-white">
            {{ device?.user_code || '--' }}
          </code>
          <button type="button" class="btn btn-secondary shrink-0" @click="copyCode">
            {{ copied ? t('admin.accounts.kimiOAuth.copied') : t('admin.accounts.kimiOAuth.copyCode') }}
          </button>
        </div>
        <p v-if="expiresIn > 0" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.kimiOAuth.expiresIn', { seconds: expiresIn }) }}
        </p>
      </div>

      <a
        class="btn btn-primary w-full"
        :href="verifyUrl"
        target="_blank"
        rel="noopener noreferrer"
      >
        {{ t('admin.accounts.kimiOAuth.openVerification') }}
      </a>

      <div class="flex items-center justify-between">
        <span v-if="polling" class="text-xs text-gray-400">
          {{ t('admin.accounts.kimiOAuth.autoPolling') }}
        </span>
        <button type="button" class="btn btn-secondary ml-auto" :disabled="polling" @click="pollOnce">
          {{ t('admin.accounts.kimiOAuth.pollAgain') }}
        </button>
      </div>
    </div>

    <!-- 授权完成，创建账号 -->
    <div v-else-if="step === 'ready'" class="space-y-4">
      <div class="flex items-center gap-2 rounded-lg border border-green-300/60 bg-green-50 p-3 text-sm text-green-700 dark:border-green-500/40 dark:bg-green-500/10 dark:text-green-400">
        <Icon name="check" size="sm" class="shrink-0" />
        {{ t('admin.accounts.kimiOAuth.ready') }}
      </div>
      <div>
        <label class="input-label">{{ t('admin.accounts.kimiOAuth.accountName') }}</label>
        <input v-model="accountName" type="text" class="input" :placeholder="t('admin.accounts.kimiOAuth.accountNamePlaceholder')" />
      </div>
    </div>

    <!-- 创建中 -->
    <div v-else class="flex items-center justify-center gap-2 py-8 text-gray-500">
      <div class="h-4 w-4 animate-spin rounded-full border-2 border-gray-400 border-t-transparent" />
      {{ t('admin.accounts.kimiOAuth.creating') }}
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button
          type="button"
          class="btn btn-secondary"
          :class="{ 'w-full': step === 'start' }"
          :disabled="creating"
          @click="handleClose"
        >
          {{ step === 'ready' ? t('common.close') : t('common.cancel') }}
        </button>
        <button
          v-if="step === 'ready'"
          type="button"
          class="btn btn-primary"
          :disabled="creating"
          @click="createAccount"
        >
          {{ t('admin.accounts.kimiOAuth.createAccount') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAccountWorkspace } from '@/composables/useAccountWorkspace'
const { kimi: { createKimiAccountFromOAuth, startKimiDeviceFlow, pollKimiDeviceFlow } } = useAccountWorkspace()
import {
  type KimiStartDeviceFlowResult,
} from '@/api/admin/kimiOAuth'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'created', account: unknown): void }>()

const { t } = useI18n()

type Step = 'start' | 'waiting' | 'ready' | 'creating'

const step = ref<Step>('start')
const starting = ref(false)
const creating = ref(false)
const error = ref('')
const copied = ref(false)
const device = ref<KimiStartDeviceFlowResult | null>(null)
const accountName = ref('')
const expiresAt = ref(0)
const expiresIn = ref(0)
const polling = ref(false)
const pollingTimer = ref<number | null>(null)
const countdownTimer = ref<number | null>(null)

const verifyUrl = computed(
  () => device.value?.verification_uri_complete || device.value?.verification_uri || ''
)

function clearTimers() {
  if (pollingTimer.value !== null) {
    clearTimeout(pollingTimer.value)
    pollingTimer.value = null
  }
  if (countdownTimer.value !== null) {
    clearInterval(countdownTimer.value)
    countdownTimer.value = null
  }
}

function reset() {
  clearTimers()
  step.value = 'start'
  starting.value = false
  creating.value = false
  error.value = ''
  copied.value = false
  device.value = null
  accountName.value = ''
  expiresAt.value = 0
  expiresIn.value = 0
  polling.value = false
}

async function startFlow() {
  error.value = ''
  try {
    starting.value = true
    const data = await startKimiDeviceFlow()
    device.value = data
    expiresAt.value = Date.now() + data.expires_in * 1000
    expiresIn.value = data.expires_in
    step.value = 'waiting'
    startCountdown()
    schedulePoll(Math.max(data.interval, 3) * 1000)
  } catch (e) {
    error.value = (e as { message?: string })?.message || String(e)
  } finally {
    starting.value = false
  }
}

function startCountdown() {
  countdownTimer.value = window.setInterval(() => {
    const remaining = Math.floor((expiresAt.value - Date.now()) / 1000)
    expiresIn.value = Math.max(0, remaining)
    if (remaining <= 0) {
      clearTimers()
      error.value = t('admin.accounts.kimiOAuth.expired')
      step.value = 'start'
    }
  }, 1000)
}

async function doPoll() {
  if (!device.value) return
  try {
    polling.value = true
    const result = await pollKimiDeviceFlow(device.value.session_id)
    if (result.pending) {
      schedulePoll()
    } else {
      clearTimers()
      step.value = 'ready'
      accountName.value = accountName.value || t('admin.accounts.kimiOAuth.accountNamePlaceholder')
    }
  } catch (e) {
    polling.value = false
    clearTimers()
    error.value = (e as { message?: string })?.message || String(e)
    step.value = 'start'
  } finally {
    // 仅当仍在等待（未被错误/完成切换）时复位，避免状态闪动
    if (step.value === 'waiting') polling.value = false
  }
}

function schedulePoll(delay?: number) {
  clearTimeout(pollingTimer.value ?? undefined)
  pollingTimer.value = window.setTimeout(doPoll, delay ?? Math.max(device.value?.interval ?? 15, 3) * 1000)
}

async function pollOnce() {
  if (polling.value) return
  clearTimeout(pollingTimer.value ?? undefined)
  await doPoll()
}

async function copyCode() {
  if (!device.value) return
  try {
    await navigator.clipboard.writeText(device.value.user_code)
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  } catch {
    // 忽略剪贴板权限拒绝
  }
}

async function createAccount() {
  if (!device.value) return
  try {
    creating.value = true
    const account = await createKimiAccountFromOAuth({
      session_id: device.value.session_id,
      name: accountName.value || undefined,
    })
    emit('created', account)
    handleClose()
  } catch (e) {
    error.value = (e as { message?: string })?.message || String(e)
  } finally {
    creating.value = false
  }
}

function handleClose() {
  reset()
  emit('close')
}

watch(
  () => props.show,
  (open) => {
    if (open) {
      reset()
      startFlow()
    }
  }
)

onBeforeUnmount(() => clearTimers())
</script>