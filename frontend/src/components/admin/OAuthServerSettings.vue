<template>
  <section class="card" aria-labelledby="oauth-server-heading">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 id="oauth-server-heading" class="text-lg font-semibold text-gray-900 dark:text-white">{{ copy.title }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ copy.description }}</p>
    </div>
    <div class="space-y-4 p-6">
      <p v-if="loading" role="status">{{ copy.loading }}</p>
      <template v-else-if="settings">
        <dl class="grid gap-2 text-sm sm:grid-cols-2">
          <div><dt class="text-gray-500">Client ID</dt><dd>{{ settings.client_id || '—' }}</dd></div>
          <div><dt class="text-gray-500">{{ copy.secret }}</dt><dd>{{ settings.secret_configured ? copy.configured : copy.missing }}</dd></div>
        </dl>
        <label for="oauth-server-redirects" class="mb-2 block text-sm font-medium">{{ copy.addresses }}</label>
        <textarea id="oauth-server-redirects" v-model="addresses" class="input font-mono text-sm" rows="5" spellcheck="false"
          placeholder="https://skoob.cc/api/v1/account/oauth/callback" :disabled="saving" />
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ copy.help }}</p>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ settings.source === 'database' ? copy.database : copy.environment }}</p>
        <button type="button" class="btn btn-primary" :disabled="saving" @click="save">{{ saving ? copy.saving : copy.save }}</button>
      </template>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }} <button v-if="!settings" type="button" class="underline" @click="load">{{ copy.retry }}</button></p>
      <p v-if="success" role="status" class="text-sm text-green-600">{{ copy.saved }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import apiClient from '@/api/client'

interface Settings { client_id: string; secret_configured: boolean; redirect_uris: string[]; source: 'environment' | 'database' }
const { locale } = useI18n()
const copy = computed(() => locale.value.startsWith('zh') ? {
  title: '应用授权 · OAuth 回调', description: '管理 Skoob 等应用登录后允许返回的地址，保存后立即生效。',
  loading: '正在加载授权配置…', secret: '客户端密钥', configured: '已配置', missing: '尚未配置',
  addresses: '允许的回调地址', help: '每行一个完整 HTTPS 地址，按完整地址精确匹配，不支持通配符。本地 localhost / 127.0.0.1 / ::1 的 HTTP 端口回调继续支持；清空列表会禁用全部回调。',
  database: '当前使用后台保存的配置。', environment: '当前使用部署环境默认值；首次保存后由后台管理。',
  saving: '保存中…', save: '保存授权回调', saved: '授权回调已保存并生效。', retry: '重试', failed: '授权配置请求失败',
} : {
  title: 'Application authorization · OAuth callbacks', description: 'Manage allowed login callbacks for applications such as Skoob. Changes take effect immediately.',
  loading: 'Loading authorization settings…', secret: 'Client secret', configured: 'Configured', missing: 'Not configured',
  addresses: 'Allowed callback URLs', help: 'One complete HTTPS URL per line. Exact matches only; no wildcards. HTTP callbacks to localhost / 127.0.0.1 / ::1 ports remain supported. An empty list disables all callbacks.',
  database: 'Using saved administration settings.', environment: 'Using deployment defaults; saving transfers management to this page.',
  saving: 'Saving…', save: 'Save OAuth callbacks', saved: 'OAuth callbacks saved and active.', retry: 'Retry', failed: 'Could not load or save authorization settings',
})
const settings = ref<Settings | null>(null)
const addresses = ref('')
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const success = ref(false)
function message(e: unknown): string {
  const value = e as { response?: { data?: { message?: string } }; message?: string }
  return value?.response?.data?.message || value?.message || copy.value.failed
}
async function load() {
  loading.value = true; error.value = ''
  try { settings.value = (await apiClient.get<Settings>('/admin/settings/oauth-server')).data; addresses.value = settings.value.redirect_uris.join('\n') }
  catch (e) { error.value = message(e) }
  finally { loading.value = false }
}
async function save() {
  saving.value = true; error.value = ''; success.value = false
  try {
    const redirect_uris = addresses.value.split(/\r?\n/).map(line => line.trim()).filter(Boolean)
    settings.value = (await apiClient.put<Settings>('/admin/settings/oauth-server', { redirect_uris })).data
    addresses.value = settings.value.redirect_uris.join('\n'); success.value = true
  } catch (e) { error.value = message(e) }
  finally { saving.value = false }
}
onMounted(load)
</script>
