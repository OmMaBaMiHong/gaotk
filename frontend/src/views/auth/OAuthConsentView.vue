<template>
  <div class="flex min-h-screen items-center justify-center bg-gray-50 px-4 py-10 dark:bg-dark-900">
    <div class="w-full max-w-md">
      <div class="card p-6 sm:p-8">
        <!-- 应用标识 -->
        <div class="text-center">
          <div
            class="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-primary-500 text-2xl font-bold text-white"
          >
            {{ appInitial }}
          </div>
          <p class="mt-3 text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
            {{ t('auth.consent.grantTo', { app: appName }) }}
          </p>
          <h1 class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('auth.consent.title') }}
          </h1>
          <p class="mt-2 text-sm text-gray-600 dark:text-gray-400">
            {{ t('auth.consent.intro', { app: appName, site: siteName }) }}
          </p>
        </div>

        <!-- 参数不合法：不渲染任何按钮，避免把用户导向一个无处可回的授权 -->
        <div v-if="paramError" class="mt-6">
          <p class="rounded-lg bg-red-50 px-4 py-3 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-400">
            {{ paramError }}
          </p>
        </div>

        <template v-else>
          <!-- 账号 -->
          <div
            v-if="account"
            class="mt-6 flex items-center gap-3 rounded-lg bg-gray-50 px-4 py-3 dark:bg-dark-800"
          >
            <div
              class="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-primary-100 text-sm font-semibold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300"
            >
              {{ accountInitial }}
            </div>
            <div class="min-w-0">
              <p class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ account }}</p>
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('auth.consent.accountOf', { site: siteName }) }}
              </p>
            </div>
          </div>

          <!-- 授权范围 -->
          <ul class="mt-6 space-y-2.5">
            <li
              v-for="(scope, index) in scopes"
              :key="index"
              class="flex items-start gap-2.5 text-sm text-gray-700 dark:text-gray-300"
            >
              <Icon name="checkCircle" size="sm" class="mt-0.5 shrink-0 text-green-500" />
              <span>{{ scope }}</span>
            </li>
          </ul>

          <div
            v-if="error"
            class="mt-5 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-400"
          >
            {{ error }}
          </div>

          <div v-if="checking" class="mt-6 text-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('auth.consent.checking') }}
          </div>

          <!-- 未登录：先去登录，登录后回到本页（而不是回首页） -->
          <div v-else-if="!account" class="mt-6 text-center">
            <p class="text-sm text-gray-600 dark:text-gray-400">
              {{ t('auth.consent.loginRequired', { site: siteName }) }}
            </p>
            <button class="btn btn-primary mt-3 w-full" @click="goLogin">
              {{ t('auth.consent.goLogin') }}
            </button>
          </div>

          <div v-else class="mt-6 flex gap-3">
            <button class="btn btn-secondary flex-1" :disabled="submitting" @click="handleDeny">
              {{ t('auth.consent.deny') }}
            </button>
            <button class="btn btn-primary flex-1" :disabled="submitting" @click="handleAllow">
              {{ submitting ? t('auth.consent.authorizing') : t('auth.consent.allow') }}
            </button>
          </div>
        </template>

        <p class="mt-6 text-center text-xs leading-relaxed text-gray-500 dark:text-gray-400">
          {{ t('auth.consent.note', { app: appName, site: siteName }) }}
        </p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 第三方应用授权确认页（OAuth Server 的 consent 页）。
 *
 * 从前这是一张**由 nginx alias 出去的静态 HTML**
 * （`location = /oauth/authorize { alias /opt/oauth-authorize/index.html; }`）。
 * 那样部署脆：换服务器、换环境、搭集群都得手工再铺一遍，而且它和后端的
 * 授权契约分处两地，改了一边容易忘了另一边。挪进项目里随版本发布。
 *
 * ── 为什么页面里用 POST 而不是直接跳后端的 GET ──
 *
 * 后端同址有个 GET 版本，它从 **Authorization 头**认人；而浏览器整页跳转
 * 带不了自定义头，所以 GET 版本对已登录用户只能跳登录页，再被路由守卫
 * 送去 dashboard（redirect 参数丢失）——原地打转。页面里用 POST 就没这个
 * 问题：apiClient 会带上 token，后端返回拼好的 redirectUrl，我们再跳。
 */
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { oauthAuthorize, getCurrentUser } from '@/api/auth'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const checking = ref(true)
const submitting = ref(false)
const error = ref('')
const account = ref('')

const clientId = String(route.query.client_id ?? '')
const redirectUri = String(route.query.redirect_uri ?? '')
const state = String(route.query.state ?? '')

/**
 * 参数缺失就**什么按钮都不给**。
 *
 * 少了 redirect_uri，「拒绝」无处可回、「同意」拿到 code 也送不出去；
 * 与其让人点一个必然失败的按钮，不如直接说清楚缺什么。
 */
const paramError = computed(() => {
  if (!clientId) return t('auth.consent.missingParam', { param: 'client_id' })
  if (!redirectUri) return t('auth.consent.missingParam', { param: 'redirect_uri' })
  return ''
})

/** 应用名。client_id 是标识不是展示名，首字母大写只为好看，不参与任何判断。 */
const appName = computed(() => (clientId ? clientId.charAt(0).toUpperCase() + clientId.slice(1) : 'App'))
const appInitial = computed(() => appName.value.charAt(0).toUpperCase())
const siteName = computed(() => appStore.siteName || window.location.hostname)
const accountInitial = computed(() => (account.value || 'U').charAt(0).toUpperCase())

const scopes = computed(() => [
  t('auth.consent.scopeProfile'),
  t('auth.consent.scopeSubscription'),
  t('auth.consent.scopeContent'),
])

/**
 * 确认登录状态。
 *
 * 不只信 store 里的 isAuthenticated：本地可能留着一把过期 token，
 * 那样会让人点了「授权」才在下一步失败。这里实打实问一次 /auth/me。
 */
async function checkAccount(): Promise<void> {
  if (!authStore.isAuthenticated) {
    checking.value = false
    return
  }
  try {
    const res = await getCurrentUser()
    const user = (res as { data?: { email?: string; username?: string } })?.data ?? null
    account.value = user?.email || user?.username || t('auth.consent.signedInAccount')
  } catch {
    // token 失效：当作未登录，走「去登录」那条路，别让人卡在一个假的已登录态。
    account.value = ''
  } finally {
    checking.value = false
  }
}

function goLogin(): void {
  // 登录后回到**本页连同 query**，否则回来时 client_id/state 全丢了。
  void router.push({ path: '/login', query: { redirect: route.fullPath } })
}

async function handleAllow(): Promise<void> {
  submitting.value = true
  error.value = ''
  try {
    const res = await oauthAuthorize({ client_id: clientId, redirect_uri: redirectUri, state })
    const url = (res as { data?: { redirectUrl?: string } })?.data?.redirectUrl
    if (!url) {
      error.value = t('auth.consent.failed')
      return
    }
    // 离开本站回到第三方应用，用整页跳转而不是 router。
    window.location.href = url
  } catch (e) {
    error.value = (e as { message?: string })?.message || t('auth.consent.failed')
  } finally {
    submitting.value = false
  }
}

/**
 * 拒绝也要**回到发起方**并带上 error=access_denied。
 * 静默停在本页的话，第三方那边会一直等着，用户以为自己卡住了。
 */
function handleDeny(): void {
  const sep = redirectUri.includes('?') ? '&' : '?'
  window.location.href = `${redirectUri}${sep}error=access_denied&state=${encodeURIComponent(state)}`
}

onMounted(() => {
  if (paramError.value) {
    checking.value = false
    return
  }
  void checkAccount()
})
</script>
