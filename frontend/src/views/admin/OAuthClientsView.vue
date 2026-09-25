<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="flex-1">
            <p class="text-sm text-gray-500 dark:text-dark-400">
              {{ t('admin.oauthClients.pageHint') }}
            </p>
          </div>
          <div class="flex flex-1 flex-wrap items-center justify-end gap-2">
            <button
              @click="loadClients"
              :disabled="loading"
              class="btn btn-secondary"
              :title="t('common.refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button @click="openCreateDialog" class="btn btn-primary">
              <Icon name="plus" size="md" class="mr-1" />
              {{ t('admin.oauthClients.create') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="clients" :loading="loading">
          <template #cell-client_id="{ value }">
            <code class="rounded bg-gray-100 px-1.5 py-0.5 text-xs dark:bg-dark-700">{{ value }}</code>
          </template>

          <template #cell-secret_last4="{ value }">
            <span class="font-mono text-xs text-gray-500 dark:text-dark-400">••••{{ value }}</span>
          </template>

          <template #cell-redirect_uris="{ row }">
            <div class="max-w-xs">
              <div class="truncate text-sm text-gray-600 dark:text-gray-300" :title="row.redirect_uris.join('\n')">
                {{ row.redirect_uris[0] }}
              </div>
              <div v-if="row.redirect_uris.length > 1" class="text-xs text-gray-400">
                +{{ row.redirect_uris.length - 1 }}
              </div>
            </div>
          </template>

          <template #cell-flags="{ row }">
            <div class="flex flex-wrap items-center gap-1">
              <span :class="['badge', row.enabled ? 'badge-success' : 'badge-gray']">
                {{ row.enabled ? t('admin.oauthClients.enabled') : t('admin.oauthClients.disabled') }}
              </span>
              <span v-if="row.allow_localhost" class="badge badge-warning">
                {{ t('admin.oauthClients.allowLocalhost') }}
              </span>
            </div>
          </template>

          <template #cell-updated_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center space-x-1">
              <button
                @click="toggleEnabled(row)"
                :disabled="saving"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-600 dark:hover:text-gray-300"
                :title="row.enabled ? t('admin.oauthClients.disable') : t('admin.oauthClients.enable')"
              >
                <Icon :name="row.enabled ? 'eye' : 'eyeOff'" size="sm" />
              </button>
              <button
                @click="openEditDialog(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-blue-50 hover:text-blue-600 dark:hover:bg-blue-900/20 dark:hover:text-blue-400"
                :title="t('common.edit')"
              >
                <Icon name="edit" size="sm" />
              </button>
              <button
                @click="openDeleteDialog(row)"
                class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                :title="t('common.delete')"
              >
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('empty.noData')"
              :description="t('admin.oauthClients.createFirst')"
              :action-text="t('admin.oauthClients.create')"
              @action="openCreateDialog"
            />
          </template>
        </DataTable>
      </template>
    </TablePageLayout>

    <!-- Create/Edit Dialog -->
    <BaseDialog
      :show="showEditDialog"
      :title="isEditing ? t('admin.oauthClients.edit') : t('admin.oauthClients.create')"
      width="wide"
      @close="closeEdit"
    >
      <!-- 新建/换密钥成功：一次性展示密钥 -->
      <div v-if="issuedSecret" class="space-y-4">
        <div class="rounded-lg border border-green-200 bg-green-50 p-4 dark:border-green-800 dark:bg-green-900/20">
          <p class="mb-2 font-medium text-green-800 dark:text-green-300">
            {{ t('admin.oauthClients.secretIssued') }}
          </p>
          <code class="block break-all rounded bg-white p-2 font-mono text-xs dark:bg-dark-700">{{ issuedSecret }}</code>
          <button @click="copySecret" class="btn btn-secondary mt-2">{{ t('common.copy') }}</button>
        </div>
        <div class="flex justify-end">
          <button @click="closeEdit" class="btn btn-primary">{{ t('common.confirm') }}</button>
        </div>
      </div>

      <form v-else id="oauth-client-form" @submit.prevent="handleSave" class="space-y-4">
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.oauthClients.fields.name') }}</label>
            <input v-model="form.name" type="text" class="input" required />
          </div>
          <div>
            <label class="input-label">{{ t('admin.oauthClients.fields.clientId') }}</label>
            <input v-model="form.client_id" type="text" class="input" required :disabled="isEditing" />
            <p v-if="isEditing" class="input-hint">{{ t('admin.oauthClients.fields.clientIdHint') }}</p>
          </div>
        </div>

        <div>
          <label class="input-label">{{ t('admin.oauthClients.fields.redirectUris') }}</label>
          <textarea v-model="form.redirect_uris" rows="4" class="input font-mono" required></textarea>
          <p class="input-hint">{{ t('admin.oauthClients.fields.redirectUrisHint') }}</p>
        </div>

        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="flex items-center gap-2 text-sm">
              <input v-model="form.allow_localhost" type="checkbox" class="h-4 w-4" />
              {{ t('admin.oauthClients.fields.allowLocalhost') }}
            </label>
            <p class="input-hint">{{ t('admin.oauthClients.fields.allowLocalhostHint') }}</p>
          </div>
          <div v-if="isEditing">
            <label class="flex items-center gap-2 text-sm">
              <input v-model="form.regenerate_secret" type="checkbox" class="h-4 w-4" />
              {{ t('admin.oauthClients.fields.regenerateSecret') }}
            </label>
            <p class="input-hint">{{ t('admin.oauthClients.fields.regenerateSecretHint') }}</p>
          </div>
        </div>

        <div>
          <label class="input-label">{{ t('admin.oauthClients.fields.remark') }}</label>
          <input v-model="form.remark" type="text" class="input" />
        </div>

        <div v-if="formError" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-600 dark:border-red-800 dark:bg-red-900/20 dark:text-red-400">
          {{ formError }}
        </div>

        <div class="flex justify-end gap-3">
          <button type="button" @click="closeEdit" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" form="oauth-client-form" :disabled="saving" class="btn btn-primary">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </form>
    </BaseDialog>

    <!-- Delete Confirmation -->
    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.oauthClients.delete')"
      :message="t('admin.oauthClients.deleteConfirm')"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTime } from '@/utils/format'
import type { Column } from '@/components/common/types'
import oauthClientsAPI from '@/api/admin/oauthClients'
import type { OAuthClientView } from '@/api/admin/oauthClients'

const { t } = useI18n()

const clients = ref<OAuthClientView[]>([])
const loading = ref(false)
const saving = ref(false)
const showEditDialog = ref(false)
const showDeleteDialog = ref(false)
const isEditing = ref(false)
const editingId = ref<number | null>(null)
const deletingId = ref<number | null>(null)
const issuedSecret = ref('')
const formError = ref('')

const form = ref({
  name: '',
  client_id: '',
  redirect_uris: '',
  allow_localhost: false,
  regenerate_secret: false,
  remark: ''
})

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.oauthClients.fields.name') },
  { key: 'client_id', label: t('admin.oauthClients.fields.clientId') },
  { key: 'secret_last4', label: t('admin.oauthClients.fields.secret') },
  { key: 'redirect_uris', label: t('admin.oauthClients.fields.redirectUris') },
  { key: 'flags', label: t('admin.oauthClients.flags') },
  { key: 'updated_at', label: t('admin.oauthClients.fields.updatedAt') },
  { key: 'actions', label: t('admin.oauthClients.actions') }
])

async function loadClients() {
  loading.value = true
  try {
    const res = await oauthClientsAPI.list()
    clients.value = res.items
  } finally {
    loading.value = false
  }
}

function openCreateDialog() {
  isEditing.value = false
  editingId.value = null
  issuedSecret.value = ''
  formError.value = ''
  form.value = { name: '', client_id: '', redirect_uris: '', allow_localhost: false, regenerate_secret: false, remark: '' }
  showEditDialog.value = true
}

function openEditDialog(row: OAuthClientView) {
  isEditing.value = true
  editingId.value = row.id
  issuedSecret.value = ''
  formError.value = ''
  form.value = {
    name: row.name,
    client_id: row.client_id,
    redirect_uris: row.redirect_uris.join('\n'),
    allow_localhost: row.allow_localhost,
    regenerate_secret: false,
    remark: row.remark
  }
  showEditDialog.value = true
}

function closeEdit() {
  showEditDialog.value = false
  issuedSecret.value = ''
}

async function handleSave() {
  saving.value = true
  formError.value = ''
  try {
    if (isEditing.value && editingId.value) {
      const res = await oauthClientsAPI.update(editingId.value, {
        name: form.value.name,
        redirect_uris: form.value.redirect_uris.split('\n').map(s => s.trim()).filter(Boolean),
        allow_localhost: form.value.allow_localhost,
        regenerate_secret: form.value.regenerate_secret || undefined,
        remark: form.value.remark
      })
      issuedSecret.value = res.client_secret || ''
    } else {
      const res = await oauthClientsAPI.create({
        name: form.value.name,
        client_id: form.value.client_id,
        redirect_uris: form.value.redirect_uris.split('\n').map(s => s.trim()).filter(Boolean),
        allow_localhost: form.value.allow_localhost,
        remark: form.value.remark
      })
      issuedSecret.value = res.client_secret
    }
    await loadClients()
  } catch (e) {
    formError.value = (e as { message?: string })?.message || t('common.error')
  } finally {
    saving.value = false
  }
}

async function toggleEnabled(row: OAuthClientView) {
  saving.value = true
  try {
    await oauthClientsAPI.update(row.id, { enabled: !row.enabled })
    row.enabled = !row.enabled
  } finally {
    saving.value = false
  }
}

function openDeleteDialog(row: OAuthClientView) {
  deletingId.value = row.id
  showDeleteDialog.value = true
}

async function confirmDelete() {
  if (deletingId.value) {
    await oauthClientsAPI.remove(deletingId.value)
    await loadClients()
  }
  showDeleteDialog.value = false
}

async function copySecret() {
  try {
    await navigator.clipboard.writeText(issuedSecret.value)
  } catch {
    /* 剪贴板不可用时用户可手动复制 */
  }
}

onMounted(loadClients)
</script>
