<template>
  <section class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('tokenBank.settingTitle') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('tokenBank.settingHint') }}</p>
    </div>
    <div class="space-y-3 p-6">
      <div class="flex items-center justify-between gap-4">
        <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('tokenBank.settingEnabled') }}</span>
        <Toggle :model-value="enabled" :disabled="loading || saving || !loaded" :aria-label="t('tokenBank.settingEnabled')" @update:model-value="save" />
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400" role="status">{{ t(saving ? 'tokenBank.processing' : saved ? 'tokenBank.saved' : 'tokenBank.settingImmediate') }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">
        {{ t('tokenBank.failed') }}
        <button v-if="!loaded" type="button" data-testid="setting-retry" class="underline" @click="load">{{ t('tokenBank.retry') }}</button>
      </p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { useAppStore } from '@/stores/app'
import { tokenBankAPI } from '@/api/tokenBank'

const { t } = useI18n()
const appStore = useAppStore()
const enabled = ref(false)
const loading = ref(true)
const loaded = ref(false)
const saving = ref(false)
const saved = ref(false)
const error = ref(false)
async function load() {
  loading.value = true
  error.value = false
  try { enabled.value = (await tokenBankAPI.config()).enabled; loaded.value = true }
  catch { error.value = true }
  finally { loading.value = false }
}
async function save(value: boolean) {
  if (!loaded.value || saving.value) return
  saving.value = true
  saved.value = false
  error.value = false
  try { enabled.value = (await tokenBankAPI.updateConfig(value)).enabled; saved.value = true; await appStore.fetchPublicSettings(true) }
  catch { error.value = true }
  finally { saving.value = false }
}
onMounted(load)
</script>
