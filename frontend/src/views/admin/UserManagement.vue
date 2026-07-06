<template>
  <!--
    UserManagement — platform-wide user roster management for SystemAdmin.

    Gated server-side by the /api/v1/system/admin/users* route group
    (RequireSystemAdmin middleware) and client-side by the route guard
    (meta.requiresSystemAdmin). Visual contract: a self-contained
    platform page (NOT a Settings-modal pane) because the user table
    needs full-width real estate that the modal's content-wrapper--full
    can't comfortably provide for a data table.

    Operations:
      - List / search users (GET /system/admin/users)
      - Create user       (POST /system/admin/users)
      - Enable / disable  (PUT /system/admin/users/:id/status)
      - Reset password    (POST /system/admin/users/:id/reset-password)
  -->
  <div class="user-mgmt">
    <div class="user-mgmt-header">
      <div class="title-block">
        <h2>{{ t('userManagement.title') }}</h2>
        <p class="subtitle">{{ t('userManagement.subtitle') }}</p>
      </div>
      <div class="header-actions">
        <t-input
          v-model="searchInput"
          :placeholder="t('userManagement.searchPlaceholder')"
          clearable
          class="search-input"
          @enter="onSearch"
          @clear="onSearchClear"
        >
          <template #prefix-icon><t-icon name="search" /></template>
        </t-input>
        <t-button theme="primary" @click="openCreateDialog">
          <template #icon><t-icon name="add" /></template>
          {{ t('userManagement.createUser') }}
        </t-button>
      </div>
    </div>

    <t-table
      :data="users"
      :columns="columns"
      :loading="loading"
      row-key="id"
      :pagination="pagination"
      :hover="true"
      :stripe="true"
      cell-empty-content="-"
      @page-change="onPageChange"
    >
      <template #username="{ row }">
        <div class="user-cell">
          <t-avatar size="32px">{{ avatarLetter(row.username) }}</t-avatar>
          <div class="user-cell-info">
            <span class="user-cell-name">{{ row.username }}</span>
            <span v-if="row.email" class="user-cell-email">{{ row.email }}</span>
          </div>
        </div>
      </template>

      <template #status="{ row }">
        <div class="status-cell">
          <span class="status-dot" :class="row.is_active ? 'status-active' : 'status-disabled'" />
          <span>{{ row.is_active ? t('userManagement.statusActive') : t('userManagement.statusDisabled') }}</span>
        </div>
        <t-tag
          v-if="row.is_system_admin"
          theme="primary"
          variant="light"
          size="small"
          class="role-tag"
        >{{ t('userManagement.roleSystemAdmin') }}</t-tag>
      </template>

      <template #created_at="{ row }">
        {{ formatDate(row.created_at) }}
      </template>

      <template #op="{ row }">
        <div class="op-cell">
          <t-tooltip :content="t('userManagement.resetPassword')" placement="top">
            <t-button variant="text" theme="default" size="small" @click="openResetDialog(row)">
              <template #icon><t-icon name="lock-on" /></template>
            </t-button>
          </t-tooltip>
          <t-tooltip
            v-if="row.is_active"
            :content="t('userManagement.disable')"
            placement="top"
          >
            <t-button
              variant="text"
              theme="default"
              size="small"
              :disabled="row.id === authStore.currentUserId"
              @click="confirmToggle(row, false)"
            >
              <template #icon><t-icon name="minus-circle" /></template>
            </t-button>
          </t-tooltip>
          <t-tooltip v-else :content="t('userManagement.enable')" placement="top">
            <t-button variant="text" theme="default" size="small" @click="confirmToggle(row, true)">
              <template #icon><t-icon name="check-circle" /></template>
            </t-button>
          </t-tooltip>
        </div>
      </template>
    </t-table>

    <!-- Create user dialog -->
    <t-dialog
      v-model:visible="createVisible"
      :header="t('userManagement.createDialog.title')"
      :confirm-btn="{ content: t('userManagement.createDialog.confirm'), loading: createBusy }"
      :cancel-btn="t('userManagement.createDialog.cancel')"
      :close-on-esc-keydown="false"
      :close-on-overlay-click="false"
      @confirm="submitCreate"
    >
      <t-form :data="createForm" :rules="createRules" ref="createFormRef" label-align="top">
        <t-form-item :label="t('userManagement.createDialog.username')" name="username">
          <t-input v-model="createForm.username" :placeholder="t('userManagement.createDialog.usernamePlaceholder')" />
        </t-form-item>
        <t-form-item :label="t('userManagement.createDialog.email')" name="email">
          <t-input v-model="createForm.email" :placeholder="t('userManagement.createDialog.emailPlaceholder')" />
        </t-form-item>
        <t-form-item :label="t('userManagement.createDialog.password')" name="password">
          <t-input
            v-model="createForm.password"
            type="password"
            :placeholder="t('userManagement.createDialog.passwordPlaceholder')"
          />
        </t-form-item>
      </t-form>
    </t-dialog>

    <!-- Reset password dialog -->
    <t-dialog
      v-model:visible="resetVisible"
      :header="t('userManagement.resetDialog.title', { name: resetTarget?.username || '' })"
      :confirm-btn="{ content: t('userManagement.resetDialog.confirm'), loading: resetBusy }"
      :cancel-btn="t('userManagement.resetDialog.cancel')"
      :close-on-esc-keydown="false"
      :close-on-overlay-click="false"
      @confirm="submitReset"
    >
      <p class="reset-warning">
        {{ t('userManagement.resetDialog.warning', { email: resetTarget?.email || '' }) }}
      </p>
      <t-form :data="resetForm" :rules="resetRules" ref="resetFormRef" label-align="top">
        <t-form-item :label="t('userManagement.resetDialog.newPassword')" name="new_password">
          <t-input
            v-model="resetForm.new_password"
            type="password"
            :placeholder="t('userManagement.resetDialog.passwordPlaceholder')"
          />
        </t-form-item>
        <t-form-item :label="t('userManagement.resetDialog.confirmPassword')" name="confirm_password">
          <t-input
            v-model="resetForm.confirm_password"
            type="password"
            :placeholder="t('userManagement.resetDialog.passwordPlaceholder')"
          />
        </t-form-item>
      </t-form>
    </t-dialog>

    <!-- Disable confirm dialog -->
    <t-dialog
      v-model:visible="disableVisible"
      :header="t('userManagement.disableDialog.title')"
      :confirm-btn="{ content: t('userManagement.disableDialog.confirm'), loading: toggleBusy, theme: 'warning' }"
      :cancel-btn="t('userManagement.disableDialog.cancel')"
      @confirm="submitToggle"
    >
      <p class="reset-warning">
        {{ t('userManagement.disableDialog.warning', { name: toggleTarget?.username || '' }) }}
      </p>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import type { PrimaryTableCol, PageInfo } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import { listUsers, adminCreateUser, setUserActive, adminResetPassword } from '@/api/system'
import type { AdminUser } from '@/api/system'

const { t } = useI18n()
const authStore = useAuthStore()

// --- list state ---
const users = ref<AdminUser[]>([])
const loading = ref(false)
const searchInput = ref('')
const activeSearch = ref('')
const pagination = ref({
  current: 1,
  pageSize: 20,
  total: 0,
  showJumper: true,
  showPageSize: true,
})

const columns = computed<PrimaryTableCol<AdminUser>[]>(() => [
  { colKey: 'username', title: t('userManagement.colUser'), width: 260 },
  { colKey: 'status', title: t('userManagement.colStatus'), width: 180 },
  { colKey: 'created_at', title: t('userManagement.colCreatedAt'), width: 180 },
  { colKey: 'op', title: t('userManagement.colActions'), width: 140, align: 'left', fixed: 'right' },
])

async function fetchUsers() {
  loading.value = true
  try {
    const offset = (pagination.value.current - 1) * pagination.value.pageSize
    const resp = await listUsers({
      search: activeSearch.value || undefined,
      offset,
      limit: pagination.value.pageSize,
    })
    users.value = resp.users || []
    pagination.value.total = resp.total || 0
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('userManagement.messages.loadFailed'))
    users.value = []
    pagination.value.total = 0
  } finally {
    loading.value = false
  }
}

function onPageChange(pageInfo: PageInfo) {
  pagination.value.current = pageInfo.current
  pagination.value.pageSize = pageInfo.pageSize
  fetchUsers()
}

function onSearch() {
  activeSearch.value = searchInput.value.trim()
  pagination.value.current = 1
  fetchUsers()
}

function onSearchClear() {
  searchInput.value = ''
  activeSearch.value = ''
  pagination.value.current = 1
  fetchUsers()
}

// --- create dialog ---
const createVisible = ref(false)
const createBusy = ref(false)
const createFormRef = ref()
const createForm = ref({ username: '', email: '', password: '' })
const createRules = {
  username: [
    { required: true, message: t('userManagement.validation.usernameRequired'), trigger: 'blur' },
    { min: 2, max: 50, message: t('userManagement.validation.usernameLength'), trigger: 'blur' },
  ],
  email: [
    { required: true, message: t('userManagement.validation.emailRequired'), trigger: 'blur' },
    { email: true, message: t('userManagement.validation.emailInvalid'), trigger: 'blur' },
  ],
  password: [
    { required: true, message: t('userManagement.validation.passwordRequired'), trigger: 'blur' },
    { min: 6, message: t('userManagement.validation.passwordLength'), trigger: 'blur' },
  ],
}

function openCreateDialog() {
  createForm.value = { username: '', email: '', password: '' }
  createVisible.value = true
}

async function submitCreate() {
  const valid = await createFormRef.value?.validate?.()
  if (valid !== true) return
  createBusy.value = true
  try {
    await adminCreateUser({ ...createForm.value })
    MessagePlugin.success(t('userManagement.messages.createSuccess'))
    createVisible.value = false
    pagination.value.current = 1
    await fetchUsers()
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('userManagement.messages.createFailed'))
  } finally {
    createBusy.value = false
  }
}

// --- reset password dialog ---
const resetVisible = ref(false)
const resetBusy = ref(false)
const resetFormRef = ref()
const resetTarget = ref<AdminUser | null>(null)
const resetForm = ref({ new_password: '', confirm_password: '' })
const resetRules = {
  new_password: [
    { required: true, message: t('userManagement.validation.passwordRequired'), trigger: 'blur' },
    { min: 6, message: t('userManagement.validation.passwordLength'), trigger: 'blur' },
  ],
  confirm_password: [
    { required: true, message: t('userManagement.validation.passwordRequired'), trigger: 'blur' },
    {
      validator: (val: string) => val === resetForm.value.new_password,
      message: t('userManagement.validation.passwordMismatch'),
      trigger: 'blur',
    },
  ],
}

function openResetDialog(row: AdminUser) {
  resetTarget.value = row
  resetForm.value = { new_password: '', confirm_password: '' }
  resetVisible.value = true
}

async function submitReset() {
  if (!resetTarget.value) return
  const valid = await resetFormRef.value?.validate?.()
  if (valid !== true) return
  resetBusy.value = true
  try {
    await adminResetPassword(resetTarget.value.id, resetForm.value.new_password)
    MessagePlugin.success(t('userManagement.messages.resetSuccess'))
    resetVisible.value = false
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('userManagement.messages.resetFailed'))
  } finally {
    resetBusy.value = false
  }
}

// --- enable / disable ---
const disableVisible = ref(false)
const toggleBusy = ref(false)
const toggleTarget = ref<AdminUser | null>(null)

function confirmToggle(row: AdminUser, active: boolean) {
  if (active) {
    // Enabling is safe; skip the confirm dialog.
    doToggle(row, true)
    return
  }
  // Disabling needs a confirm.
  toggleTarget.value = row
  disableVisible.value = true
}

async function submitToggle() {
  if (!toggleTarget.value) return
  await doToggle(toggleTarget.value, false)
  disableVisible.value = false
}

async function doToggle(row: AdminUser, active: boolean) {
  toggleBusy.value = true
  try {
    await setUserActive(row.id, active)
    MessagePlugin.success(
      active ? t('userManagement.messages.enableSuccess') : t('userManagement.messages.disableSuccess'),
    )
    await fetchUsers()
  } catch (e: any) {
    MessagePlugin.error(
      e?.message ||
        (active ? t('userManagement.messages.enableFailed') : t('userManagement.messages.disableFailed')),
    )
  } finally {
    toggleBusy.value = false
  }
}

// --- helpers ---
function avatarLetter(name: string): string {
  if (!name) return '?'
  return name.charAt(0).toUpperCase()
}

function formatDate(iso: string): string {
  if (!iso) return '-'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

onMounted(() => {
  fetchUsers()
})
</script>

<style lang="less" scoped>
.user-mgmt {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  padding: 20px 24px;
  box-sizing: border-box;
  overflow: auto;
}

.user-mgmt-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
  flex-wrap: wrap;
}

.title-block {
  h2 {
    margin: 0;
    font-size: 20px;
    font-weight: 600;
  }
  .subtitle {
    margin: 4px 0 0;
    color: var(--td-text-color-secondary);
    font-size: 13px;
  }
}

.header-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.search-input {
  width: 280px;
}

.user-cell {
  display: flex;
  align-items: center;
  gap: 10px;
}

.user-cell-info {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.user-cell-name {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-cell-email {
  font-size: 12px;
  color: var(--td-text-color-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.status-cell {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.status-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  &.status-active {
    background: var(--td-success-color);
  }
  &.status-disabled {
    background: var(--td-text-color-disabled);
  }
}

.role-tag {
  margin-left: 8px;
}

.op-cell {
  display: flex;
  align-items: center;
  gap: 4px;
}

.reset-warning {
  margin: 0 0 16px;
  color: var(--td-warning-color);
  font-size: 13px;
  line-height: 1.6;
}
</style>
