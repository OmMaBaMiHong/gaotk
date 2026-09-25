<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">
            {{ t('tokenBank.title') }}
          </h1>
          <p class="mt-2 max-w-3xl text-sm text-gray-500 dark:text-dark-400">
            {{
              t(admin ? 'tokenBank.adminDescription' : 'tokenBank.description')
            }}
          </p>
        </div>
        <button
          v-if="!admin"
          class="btn btn-primary"
          :disabled="!policies.length || busy"
          @click="openImport()"
        >
          {{ t('tokenBank.add') }}
        </button>
      </div>
      <p
        v-if="error"
        role="alert"
        class="rounded-xl bg-red-50 p-4 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300"
      >
        {{ error }}
        <button class="underline" @click="load">
          {{ t('tokenBank.retry') }}
        </button>
      </p>
      <div class="grid gap-4 sm:grid-cols-3">
        <div class="card p-5">
          <p class="text-sm text-gray-500">{{ t('tokenBank.today') }}</p>
          <p
            class="mt-2 text-2xl font-semibold text-primary-600 dark:text-primary-400"
          >
            {{ money(overview?.today_revenue || 0) }}
          </p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500">{{ t('tokenBank.total') }}</p>
          <p class="mt-2 text-2xl font-semibold">
            {{ money(overview?.total_revenue || 0) }}
          </p>
        </div>
        <div class="card p-5">
          <p class="text-sm text-gray-500">
            {{ t(admin ? 'tokenBank.platformRevenue' : 'tokenBank.accounts') }}
          </p>
          <p class="mt-2 text-2xl font-semibold">
            {{
              admin
                ? money(overview?.admin_revenue || 0)
                : overview?.total_accounts || 0
            }}
          </p>
        </div>
      </div>

      <section v-if="admin" class="card p-5">
        <h2 class="font-semibold">{{ t('tokenBank.policies') }}</h2>
        <p class="mt-1 text-sm text-gray-500">
          {{ t('tokenBank.policyHint') }}
        </p>
        <form
          v-for="p in policyForms"
          :key="p.platform"
          class="mt-5 grid items-end gap-3 border-t border-gray-100 pt-4 dark:border-dark-700 sm:grid-cols-2 xl:grid-cols-6"
          @submit.prevent="savePolicy(p)"
        >
          <p class="self-center font-medium">{{ platformName(p.platform) }}</p>
          <label class="text-sm xl:col-span-2"
            >{{ t('tokenBank.pool')
            }}<select v-model="p.group_id" class="input mt-1" required>
              <option :value="0" disabled>
                {{ t('tokenBank.selectPool') }}
              </option>
              <option
                v-for="g in groups.filter((g) => g.platform === p.platform)"
                :key="g.id"
                :value="g.id"
              >
                {{ g.name }} (#{{ g.id }})
              </option>
            </select></label
          >
          <label class="text-sm"
            >{{ t('tokenBank.adminRecipient')
            }}<input
              v-model.number="p.admin_user_id"
              class="input mt-1"
              type="number"
              min="1"
              required
          /></label>
          <div>
            <p class="text-sm">
              {{ t('tokenBank.ratio', { rate: p.owner_share_bps / 100 }) }}
            </p>
            <label class="mt-2 flex items-center gap-2 text-sm"
              ><input v-model="p.enabled" type="checkbox" />{{
                t('tokenBank.enabled')
              }}</label
            >
          </div>
          <button class="btn btn-primary" :disabled="busy">
            {{ t('tokenBank.save') }}
          </button>
        </form>
      </section>

      <section class="card overflow-hidden">
        <div class="flex flex-wrap items-center justify-between gap-3 p-5">
          <h2 class="font-semibold">{{ t('tokenBank.accounts') }}</h2>
          <form
            class="flex flex-wrap gap-2"
            @submit.prevent="applyFilters"
          >
            <select
              v-model="platform"
              class="input w-auto"
              :aria-label="t('tokenBank.platform')"
            >
              <option value="">{{ t('tokenBank.allPlatforms') }}</option>
              <option v-for="p in platforms" :key="p" :value="p">
                {{ platformName(p) }}
              </option>
            </select>
            <select
              v-model="rentalStatus"
              class="input w-auto"
              :aria-label="t('tokenBank.status')"
            >
              <option value="">{{ t('tokenBank.allStatuses') }}</option>
              <option value="active">{{ t('tokenBank.enabled') }}</option>
              <option value="paused">
                {{ t('tokenBank.paused') }}
              </option></select
            ><input
              v-if="admin"
              v-model="ownerFilter"
              class="input w-32"
              type="number"
              min="1"
              :placeholder="t('tokenBank.ownerID')"
              :aria-label="t('tokenBank.ownerID')"
            /><button class="btn btn-secondary" :disabled="loading">
              {{ t('tokenBank.filter') }}
            </button>
          </form>
        </div>
        <p v-if="loading" role="status" class="p-8 text-center text-gray-500">
          {{ t('tokenBank.loading') }}
        </p>
        <p
          v-else-if="!overview?.accounts.length"
          class="p-8 text-center text-gray-500"
        >
          {{
            t(
              !admin && !policies.length
                ? 'tokenBank.closed'
                : 'tokenBank.empty'
            )
          }}
        </p>
        <div v-else class="overflow-x-auto">
          <table class="w-full whitespace-nowrap text-left text-sm">
            <thead class="bg-gray-50 text-gray-500 dark:bg-dark-800">
              <tr>
                <th class="px-5 py-3">{{ t('tokenBank.account') }}</th>
                <th class="px-5 py-3">{{ t('tokenBank.status') }}</th>
                <th class="px-5 py-3">{{ t('tokenBank.usage') }}</th>
                <th class="px-5 py-3">{{ t('tokenBank.total') }}</th>
                <th class="px-5 py-3">{{ t('tokenBank.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="a in overview.accounts" :key="a.id">
                <td class="px-5 py-4">
                  <p class="font-medium">{{ a.name }}</p>
                  <p class="mt-1 text-xs text-gray-500">
                    {{ platformName(a.platform) }} · {{ a.type }} · #{{ a.id
                    }}<span v-if="admin">
                      · {{ t('tokenBank.ownerID') }} {{ a.owner_user_id }}</span
                    >
                  </p>
                </td>
                <td class="px-5 py-4">
                  <span
                    :class="
                      a.schedulable && a.status === 'active'
                        ? 'text-emerald-600'
                        : 'text-amber-600'
                    "
                    >{{
                      t(
                        a.rental_status === 'paused'
                          ? 'tokenBank.paused'
                          : a.schedulable && a.status === 'active'
                            ? 'tokenBank.active'
                            : 'tokenBank.unavailable'
                      )
                    }}</span
                  >
                </td>
                <td class="px-5 py-4">
                  {{ a.requests.toLocaleString() }}
                  {{ t('tokenBank.requests') }}
                  <p class="text-xs text-gray-500">
                    {{ a.tokens.toLocaleString() }} Tokens
                  </p>
                </td>
                <td class="px-5 py-4 font-medium text-primary-600">
                  {{ money(a.revenue) }}
                </td>
                <td class="px-5 py-4">
                  <div class="flex gap-3">
                    <button
                      class="text-primary-600 hover:underline"
                      @click="selectAccount(a.id)"
                    >
                      {{ t('tokenBank.details') }}</button
                    ><button
                      v-if="!admin"
                      :disabled="busy"
                      class="hover:underline"
                      @click="toggle(a)"
                    >
                      {{
                        t(
                          a.rental_status === 'paused'
                            ? 'tokenBank.resume'
                            : 'tokenBank.pause'
                        )
                      }}</button
                    ><button
                      v-if="!admin && a.type === 'oauth'"
                      :disabled="busy"
                      class="hover:underline"
                      @click="openImport(a)"
                    >
                      {{ t('tokenBank.reauthorize') }}
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div
          v-if="(overview?.total_accounts || 0) > 20"
          class="flex items-center justify-end gap-4 p-4"
        >
          <button
            :disabled="accountPage === 1 || loading"
            class="btn btn-secondary btn-sm"
            @click="changeAccountPage(-1)"
          >
            {{ t('tokenBank.previous') }}</button
          ><span>{{ accountPage }}</span
          ><button
            :disabled="
              accountPage * 20 >= (overview?.total_accounts || 0) || loading
            "
            class="btn btn-secondary btn-sm"
            @click="changeAccountPage(1)"
          >
            {{ t('tokenBank.next') }}
          </button>
        </div>
      </section>

      <section class="card overflow-hidden">
        <div class="flex items-center justify-between gap-3 p-5">
          <h2 class="font-semibold">
            {{ t('tokenBank.ledger') }}
            <span v-if="selectedAccount" class="text-sm font-normal"
              >#{{ selectedAccount }}</span
            >
          </h2>
          <button
            v-if="selectedAccount"
            class="text-sm text-primary-600"
            @click="selectAccount(0)"
          >
            {{ t('tokenBank.allAccounts') }}
          </button>
        </div>
        <p class="px-5 pb-4 text-xs text-gray-500">
          {{ t('tokenBank.ledgerHint') }}
        </p>
        <div class="overflow-x-auto">
          <table class="w-full whitespace-nowrap text-left text-sm">
            <thead class="bg-gray-50 text-gray-500 dark:bg-dark-800">
              <tr>
                <th class="px-5 py-3">{{ t('tokenBank.timeModel') }}</th>
                <th class="px-5 py-3">Tokens</th>
                <th class="px-5 py-3">{{ t('tokenBank.billed') }}</th>
                <th class="px-5 py-3">{{ t('tokenBank.ownerRevenue') }}</th>
                <th v-if="admin" class="px-5 py-3">
                  {{ t('tokenBank.platformRevenue') }}
                </th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="l in ledger.items" :key="l.id">
                <td class="px-5 py-4">
                  {{ l.model }}
                  <p class="text-xs text-gray-500">
                    {{ formatDateTime(l.created_at) }} · #{{ l.account_id }}
                  </p>
                </td>
                <td class="px-5 py-4">
                  {{
                    (
                      l.input_tokens +
                      l.output_tokens +
                      l.cache_tokens
                    ).toLocaleString()
                  }}
                </td>
                <td class="px-5 py-4">
                  {{ money(l.bill_amount) }}
                  <p class="text-xs text-gray-500">
                    {{
                      t(
                        l.billing_type === 1
                          ? 'tokenBank.subscription'
                          : 'tokenBank.balance'
                      )
                    }}
                  </p>
                </td>
                <td class="px-5 py-4 text-primary-600">
                  {{ money(l.owner_amount) }}
                  <p class="text-xs text-gray-500">
                    {{ l.owner_share_bps / 100 }}%
                  </p>
                </td>
                <td v-if="admin" class="px-5 py-4">
                  {{ money(l.admin_amount) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p
          v-if="!ledger.items.length"
          class="p-8 text-center text-sm text-gray-500"
        >
          {{ t('tokenBank.noRevenue') }}
        </p>
        <div
          v-if="ledger.total > 20"
          class="flex items-center justify-end gap-4 p-4"
        >
          <button
            class="btn btn-secondary btn-sm"
            :disabled="revenuePage === 1 || loading"
            @click="changeRevenuePage(-1)"
          >
            {{ t('tokenBank.previous') }}</button
          ><span>{{ revenuePage }}</span
          ><button
            class="btn btn-secondary btn-sm"
            :disabled="revenuePage * 20 >= ledger.total || loading"
            @click="changeRevenuePage(1)"
          >
            {{ t('tokenBank.next') }}
          </button>
        </div>
      </section>
    </div>

    <BaseDialog
      :show="showImport"
      :title="t(input.account_id ? 'tokenBank.reauthorize' : 'tokenBank.add')"
      @close="closeImport"
    >
      <form class="space-y-4" @submit.prevent="submitImport">
        <label class="block text-sm"
          >{{ t('tokenBank.name')
          }}<input
            v-model="input.name"
            class="input mt-1"
            maxlength="100"
            required
            :disabled="!!auth || !!input.account_id"
        /></label>
        <label class="block text-sm"
          >{{ t('tokenBank.platform')
          }}<select
            v-model="input.platform"
            class="input mt-1"
            :disabled="!!auth || !!input.account_id"
          >
            <option v-for="p in policies" :key="p.platform" :value="p.platform">
              {{ platformName(p.platform) }} ·
              {{ t('tokenBank.ratio', { rate: p.owner_share_bps / 100 }) }}
            </option>
          </select></label
        >
        <label v-if="!auth && !input.account_id" class="block text-sm"
          >{{ t('tokenBank.method')
          }}<select v-model="method" class="input mt-1">
            <option value="key">API Key</option>
            <option v-if="input.platform !== 'deepseek'" value="oauth">
              OAuth
            </option>
          </select></label
        >
        <label v-if="method === 'key'" class="block text-sm"
          >API Key<input
            v-model="input.api_key"
            class="input mt-1"
            type="password"
            autocomplete="off"
            required
        /></label>
        <template v-if="auth"
          ><a
            class="btn btn-secondary w-full"
            :href="auth.auth_url"
            target="_blank"
            rel="noopener noreferrer"
            >{{ t('tokenBank.openAuthorization') }}</a
          ><label class="block text-sm"
            >{{ t('tokenBank.callback')
            }}<textarea
              v-model="callback"
              class="input mt-1"
              rows="3"
              required
            /></label
          ><label class="block text-sm"
            >{{ t('tokenBank.state')
            }}<input v-model="oauthState" class="input mt-1"
          /></label>
          <p class="text-xs text-gray-500">
            {{ t('tokenBank.callbackHint') }}
          </p></template
        >
        <p class="text-xs text-gray-500">{{ t('tokenBank.importHint') }}</p>
        <p v-if="dialogError" role="alert" class="text-sm text-red-600">
          {{ dialogError }}
        </p>
        <button class="btn btn-primary w-full" :disabled="busy">
          {{
            t(
              busy
                ? 'tokenBank.processing'
                : auth || method === 'key'
                  ? 'tokenBank.verifySave'
                  : 'tokenBank.startAuthorization'
            )
          }}
        </button>
      </form>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import {
  tokenBankAPI,
  parseRentalCallback,
  type RentalPolicy,
  type RentalAccount,
  type RentalOverview,
  type RevenuePage,
  type RentalInput,
  type RentalAuth
} from '@/api/tokenBank'
import { groupsAPI } from '@/api/admin/groups'
import type { AdminGroup } from '@/types'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'

const props = withDefaults(defineProps<{ admin?: boolean }>(), {
  admin: false
})
const { t, locale } = useI18n()
const route = useRoute()
const app = useAppStore()
const platforms = ['openai', 'anthropic', 'deepseek']
const platformName = (p: string) =>
  ({ openai: 'OpenAI / GPT', anthropic: 'Claude', deepseek: 'DeepSeek' })[p] ||
  p
const money = (n: number) =>
  new Intl.NumberFormat(locale.value, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 8
  }).format(n)
const overview = ref<RentalOverview>()
const ledger = ref<RevenuePage>({ items: [], total: 0 })
const policies = ref<RentalPolicy[]>([])
const policyForms = ref<RentalPolicy[]>([])
const groups = ref<AdminGroup[]>([])
const loading = ref(false),
  busy = ref(false),
  error = ref(''),
  dialogError = ref('')
const platform = ref(''),
  rentalStatus = ref(''),
  ownerFilter = ref(String(route.query.owner_user_id || '')),
  accountPage = ref(1),
  revenuePage = ref(1),
  selectedAccount = ref(0)
const showImport = ref(false),
  method = ref('key'),
  callback = ref(''),
  oauthState = ref('')
const input = ref<RentalInput>({ name: '', platform: 'openai', api_key: '' })
const auth = ref<RentalAuth>()
const errorMessage = (e: unknown) =>
  (e as { message?: string })?.message || t('tokenBank.failed')
const filters = () => ({
  platform: platform.value,
  rental_status: rentalStatus.value,
  owner_user_id: props.admin ? ownerFilter.value || undefined : undefined
})

function applyFilters() { accountPage.value = 1; revenuePage.value = 1; selectedAccount.value = 0; void load() }
function changeAccountPage(delta: number) { accountPage.value += delta; void load() }
function changeRevenuePage(delta: number) { revenuePage.value += delta; void loadRevenue() }

async function loadRevenue() {
  try {
    ledger.value = await tokenBankAPI.revenue(props.admin, {
      ...filters(),
      account_id: selectedAccount.value || undefined,
      page: revenuePage.value
    })
  } catch (e) {
    error.value = errorMessage(e)
  }
}
async function load() {
  loading.value = true
  error.value = ''
  try {
    overview.value = await tokenBankAPI.overview(props.admin, {
      ...filters(),
      page: accountPage.value
    })
    await loadRevenue()
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}
async function loadPolicies() {
  policies.value = await tokenBankAPI.policies(props.admin)
  if (props.admin) {
    groups.value = await groupsAPI.getAll()
    policyForms.value = platforms.map((platform) => ({
      platform,
      group_id: 0,
      admin_user_id: 0,
      owner_share_bps: 8000,
      enabled: false,
      ...policies.value.find((p) => p.platform === platform)
    }))
  }
}
async function savePolicy(p: RentalPolicy) {
  busy.value = true
  try {
    await tokenBankAPI.savePolicy(p)
    await loadPolicies()
    await load()
    app.showSuccess(t('tokenBank.saved'))
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
async function toggle(a: RentalAccount) {
  busy.value = true
  try {
    await tokenBankAPI.setStatus(
      a.id,
      a.rental_status === 'paused' ? 'active' : 'paused'
    )
    await load()
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
function selectAccount(id: number) {
  selectedAccount.value = id
  revenuePage.value = 1
  void loadRevenue()
}
function openImport(a?: RentalAccount) {
  input.value = {
    name: a?.name || '',
    platform: a?.platform || policies.value[0]?.platform || 'openai',
    account_id: a?.id,
    api_key: ''
  }
  method.value = a ? 'oauth' : 'key'
  auth.value = undefined
  callback.value = ''
  oauthState.value = ''
  dialogError.value = ''
  showImport.value = true
}
function closeImport() {
  if (busy.value) return
  showImport.value = false
  input.value.api_key = ''
  callback.value = ''
  oauthState.value = ''
  auth.value = undefined
}
watch(
  () => input.value.platform,
  (p) => {
    if (p === 'deepseek') method.value = 'key'
  }
)
async function submitImport() {
  busy.value = true
  dialogError.value = ''
  try {
    if (method.value === 'key') await tokenBankAPI.importAccount(input.value)
    else if (!auth.value) {
      auth.value = await tokenBankAPI.startOAuth(input.value)
      oauthState.value =
        new URL(auth.value.auth_url).searchParams.get('state') || ''
      return
    } else {
      const parsed = parseRentalCallback(callback.value, oauthState.value)
      if (!parsed.code || !parsed.state)
        throw new Error(t('tokenBank.callbackHint'))
      await tokenBankAPI.finishOAuth(
        auth.value.session_id,
        parsed.code,
        parsed.state
      )
    }
    input.value.api_key = ''
    showImport.value = false
    callback.value = ''
    auth.value = undefined
    app.showSuccess(t('tokenBank.saved'))
    await load()
  } catch (e) {
    dialogError.value = errorMessage(e)
  } finally {
    busy.value = false
  }
}
onMounted(async () => {
  try {
    await loadPolicies()
    await load()
  } catch (e) {
    error.value = errorMessage(e)
  }
})
</script>
