<template>
  <AppLayout>
    <div class="space-y-6">
      <TokenBankShowcase />
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div><h1 class="text-xl font-semibold">{{ t('tokenBank.title') }}</h1><p class="mt-2 text-sm text-gray-500">{{ t('tokenBank.description') }}</p></div>
        <div class="flex flex-wrap gap-2">
          <button class="btn btn-secondary" @click="showImport = true">{{ t('admin.accounts.dataImportTitle') }}</button>
          <button class="btn btn-primary" @click="showCreate = true">{{ t('tokenBank.add') }}</button>
        </div>
      </div>
      <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-4 text-sm text-red-700">{{ error }} <button class="underline" @click="load">{{ t('tokenBank.retry') }}</button></p>
      <div class="grid gap-4 sm:grid-cols-3">
        <div class="card p-5"><p class="text-sm text-gray-500">{{ t('tokenBank.today') }}</p><p class="mt-2 text-2xl font-semibold text-primary-600">{{ money(overview?.today_revenue || 0) }}</p></div>
        <div class="card p-5"><p class="text-sm text-gray-500">{{ t('tokenBank.total') }}</p><p class="mt-2 text-2xl font-semibold">{{ money(overview?.total_revenue || 0) }}</p></div>
        <div class="card p-5"><p class="text-sm text-gray-500">{{ t('tokenBank.accounts') }}</p><p class="mt-2 text-2xl font-semibold">{{ totalAccounts }}</p></div>
      </div>
      <section class="card overflow-hidden">
        <div class="flex flex-wrap items-center justify-between gap-3 p-5">
          <h2 class="font-semibold">{{ t('tokenBank.accounts') }}</h2>
          <form class="flex flex-wrap gap-2" @submit.prevent="applyFilters">
            <input v-model="search" class="input w-auto" :placeholder="t('common.search')" />
            <select v-model="platform" class="input w-auto" :aria-label="t('tokenBank.platform')">
              <option value="">{{ t('tokenBank.allPlatforms') }}</option>
              <option v-for="p in platforms" :key="p" :value="p">{{ p }}</option>
            </select>
            <button class="btn btn-secondary" :disabled="loading">{{ t('tokenBank.filter') }}</button>
          </form>
        </div>
        <DataTable :columns="columns" :data="accounts" :loading="loading">
          <template #cell-name="{ row }"><p class="font-medium">{{ row.name }}</p><PlatformTypeBadge :platform="row.platform" :type="row.type" /></template>
          <template #cell-status="{ row }"><AccountStatusIndicator :account="row" /></template>
          <template #cell-usage="{ row }"><AccountUsageCell :account="row" :today-stats="todayStats[row.id]" /></template>
          <template #cell-today="{ row }"><AccountTodayStatsCell :stats="todayStats[row.id]" /></template>
          <template #cell-actions="{ row }">
            <div class="flex flex-wrap gap-3 text-sm">
              <button class="text-primary-600" @click="openAccount(row.id, 'stats')">{{ t('tokenBank.details') }}</button>
              <button @click="openAccount(row.id, 'edit')">{{ t('common.edit') }}</button>
              <button v-if="(row.type === 'oauth' || row.type === 'setup-token') && ['anthropic', 'openai', 'gemini', 'antigravity', 'grok'].includes(row.platform)" @click="openAccount(row.id, 'reauth')">{{ t('tokenBank.reauthorize') }}</button>
              <button v-if="row.type === 'oauth' || row.type === 'setup-token'" :disabled="busy" @click="refreshCredentials(row.id)">{{ t('admin.accounts.refreshToken') }}</button>
              <button :disabled="busy" @click="toggle(row)">{{ t(row.schedulable ? 'tokenBank.pause' : 'tokenBank.resume') }}</button>
              <button class="text-red-600" @click="deletingAccount = row">{{ t('common.delete') }}</button>
            </div>
          </template>
        </DataTable>
        <div class="flex items-center justify-end gap-4 p-4" v-if="totalAccounts > 20">
          <button class="btn btn-secondary btn-sm" :disabled="accountPage === 1 || loading" @click="changeAccountPage(-1)">{{ t('tokenBank.previous') }}</button>
          <span>{{ accountPage }}</span>
          <button class="btn btn-secondary btn-sm" :disabled="accountPage * 20 >= totalAccounts || loading" @click="changeAccountPage(1)">{{ t('tokenBank.next') }}</button>
        </div>
      </section>
      <section v-if="selectedAccount" class="space-y-3">
        <h2 class="font-semibold">{{ t('tokenBank.usageRecords') }} · #{{ selectedAccount }}</h2>
        <UsageTable :columns="usageColumns" :data="usageLogs" :loading="usageLoading" :show-upstream-endpoint="false" />
        <div class="flex items-center justify-end gap-3" v-if="usageTotal > 20">
          <button class="btn btn-secondary btn-sm" :disabled="usagePage === 1 || usageLoading" @click="changeUsagePage(-1)">{{ t('tokenBank.previous') }}</button>
          <span>{{ usagePage }}</span>
          <button class="btn btn-secondary btn-sm" :disabled="usagePage * 20 >= usageTotal || usageLoading" @click="changeUsagePage(1)">{{ t('tokenBank.next') }}</button>
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
    <CreateAccountModal :show="showCreate" :proxies="[]" :groups="[]" @close="showCreate = false" @created="load" />
    <ImportDataModal :show="showImport" @close="showImport = false" @imported="load" />
    <EditAccountModal :show="activeModal === 'edit'" :account="activeAccount" :proxies="[]" :groups="[]" @close="activeModal = ''" @updated="load" />
    <ReAuthAccountModal :show="activeModal === 'reauth'" :account="activeAccount" @close="activeModal = ''" @reauthorized="load" />
    <AccountStatsModal :show="activeModal === 'stats'" :account="activeAccount" @close="activeModal = ''" />
    <ConfirmDialog :show="!!deletingAccount" :title="t('common.delete')" :message="t('tokenBank.deleteConfirm', { name: deletingAccount?.name })" :loading="busy" @close="deletingAccount = null" @confirm="deleteAccount" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TokenBankShowcase from '@/components/token-bank/TokenBankShowcase.vue'
import DataTable from '@/components/common/DataTable.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'
import CreateAccountModal from '@/components/account/CreateAccountModal.vue'
import EditAccountModal from '@/components/account/EditAccountModal.vue'
import ImportDataModal from '@/components/admin/account/ImportDataModal.vue'
import ReAuthAccountModal from '@/components/admin/account/ReAuthAccountModal.vue'
import AccountStatsModal from '@/components/admin/account/AccountStatsModal.vue'
import AccountUsageCell from '@/components/account/AccountUsageCell.vue'
import AccountTodayStatsCell from '@/components/account/AccountTodayStatsCell.vue'
import AccountStatusIndicator from '@/components/account/AccountStatusIndicator.vue'
import UsageTable from '@/components/admin/usage/UsageTable.vue'
import { provideAccountWorkspace } from '@/composables/useAccountWorkspace'
import { createAccountsAPI } from '@/api/admin/accounts'
import { apiClient } from '@/api/client'
import { tokenBankAPI, type RentalOverview, type RevenuePage } from '@/api/tokenBank'
import type { Account, AccountListItem, AdminUsageLog, WindowStats } from '@/types'
import { formatDateTime } from '@/utils/format'

provideAccountWorkspace('user')
const accountAPI = createAccountsAPI('user')
const { t, locale } = useI18n()
const platforms = ['anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go']
const money = (n: number) => new Intl.NumberFormat(locale.value, { style: 'currency', currency: 'USD', maximumFractionDigits: 8 }).format(n)
const accounts = ref<AccountListItem[]>([])
const totalAccounts = ref(0)
const todayStats = ref<Record<string, WindowStats>>({})
const overview = ref<RentalOverview>()
const ledger = ref<RevenuePage>({ items: [], total: 0 })
const loading = ref(false), busy = ref(false), error = ref('')
const platform = ref(''), search = ref(''), accountPage = ref(1), revenuePage = ref(1), selectedAccount = ref(0)
const showCreate = ref(false), showImport = ref(false)
const activeAccount = ref<Account | null>(null), activeModal = ref('')
const deletingAccount = ref<AccountListItem | null>(null)
const usageLogs = ref<AdminUsageLog[]>([]), usageTotal = ref(0), usagePage = ref(1), usageLoading = ref(false)
const columns = computed(() => [
  { key: 'name', label: t('tokenBank.account') },
  { key: 'status', label: t('tokenBank.status') },
  { key: 'usage', label: t('admin.accounts.columns.usageWindows') },
  { key: 'today', label: t('admin.accounts.columns.todayStats') },
  { key: 'actions', label: t('tokenBank.actions') }
])
const usageColumns = computed(() => [
  { key: 'created_at', label: t('tokenBank.timeModel') },
  { key: 'model', label: t('usage.model') },
  { key: 'tokens', label: 'Tokens' },
  { key: 'cost', label: t('tokenBank.billed') },
  { key: 'latency', label: t('usage.duration') }
])
const errorMessage = (e: unknown) => (e as { message?: string })?.message || t('tokenBank.failed')
async function loadRevenue() {
  ledger.value = await tokenBankAPI.revenue(false, { account_id: selectedAccount.value || undefined, page: revenuePage.value })
}
async function load() {
  loading.value = true
  error.value = ''
  try {
    const [result, savings] = await Promise.all([
      accountAPI.list(accountPage.value, 20, { platform: platform.value, search: search.value }),
      tokenBankAPI.overview(false, {})
    ])
    accounts.value = result.items
    totalAccounts.value = result.total
    overview.value = savings
    const [stats] = await Promise.all([result.items.length ? accountAPI.getBatchTodayStats(result.items.map(a => a.id)) : Promise.resolve({ stats: {} }), loadRevenue()])
    todayStats.value = stats.stats
  } catch (e) { error.value = errorMessage(e) } finally { loading.value = false }
}
function applyFilters() { accountPage.value = 1; void load() }
function changeAccountPage(delta: number) { accountPage.value += delta; void load() }
async function changeRevenuePage(delta: number) { revenuePage.value += delta; try { await loadRevenue() } catch (e) { error.value = errorMessage(e) } }
async function selectAccount(id: number) {
  selectedAccount.value = id
  revenuePage.value = 1
  try { await loadRevenue() } catch (e) { error.value = errorMessage(e) }
}
async function loadUsage() {
  if (!selectedAccount.value) return
  usageLoading.value = true
  try {
    const { data } = await apiClient.get(`/user/accounts/${selectedAccount.value}/usage-logs`, { params: { page: usagePage.value, page_size: 20 } })
    usageLogs.value = data.items
    usageTotal.value = data.total
  } catch (e) { error.value = errorMessage(e) } finally { usageLoading.value = false }
}
function changeUsagePage(delta: number) { usagePage.value += delta; void loadUsage() }
async function openAccount(id: number, modal: string) {
  try {
    activeAccount.value = await accountAPI.getById(id)
    activeModal.value = modal
    if (modal === 'stats') {
      usagePage.value = 1
      await selectAccount(id)
      await loadUsage()
    }
  } catch (e) { error.value = errorMessage(e) }
}
async function refreshCredentials(id: number) {
  busy.value = true
  try { await accountAPI.refreshCredentials(id); await load() } catch (e) { error.value = errorMessage(e) } finally { busy.value = false }
}
async function toggle(a: AccountListItem) {
  busy.value = true
  try { await accountAPI.setSchedulable(a.id, !a.schedulable); await load() } catch (e) { error.value = errorMessage(e) } finally { busy.value = false }
}
async function deleteAccount() {
  if (!deletingAccount.value) return
  busy.value = true
  try { await accountAPI.delete(deletingAccount.value.id); deletingAccount.value = null; await load() } catch (e) { error.value = errorMessage(e) } finally { busy.value = false }
}
onMounted(load)
</script>
