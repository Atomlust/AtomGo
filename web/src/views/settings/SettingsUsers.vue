<script setup lang="ts">
/**
 * SettingsUsers — 多用户管理(RBAC)。
 *
 *   - 用户列表(头像 / 用户名 / 角色 / 操作),卡片式栅格,沿用凭证保险库视觉语言
 *   - 创建用户(弹层:用户名 / 口令 / 角色,角色用分段控件)
 *   - 修改角色(行内下拉,禁自降级最后一位管理员的兜底由后端 409 呈现)
 *   - 重置口令(弹层输入新口令)
 *   - 删除用户(ConfirmDialog 二次确认)
 *
 * 仅 admin(users:manage) 可达;非 admin 显示无权限提示。
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { useSessionStore } from '../../stores/session'
import { HttpError } from '../../api/http'
import {
  listUsers,
  createUser,
  setUserRole,
  resetUserPassword,
  deleteUser,
  type User,
  type UserRole,
} from '../../api/users'

const { t } = useI18n()
const toast = useToast()
const confirm = useConfirm()
const sessionStore = useSessionStore()

const canManage = computed(() => (sessionStore.user?.role ?? 'admin') === 'admin')

const users = ref<User[]>([])
const loadState = ref<'idle' | 'loading' | 'ready' | 'error'>('idle')
const loadError = ref('')

const roleOptions: { value: UserRole; labelKey: string }[] = [
  { value: 'admin', labelKey: 'users.roleAdmin' },
  { value: 'operator', labelKey: 'users.roleOperator' },
  { value: 'viewer', labelKey: 'users.roleViewer' },
]

function avatarChar(name: string): string {
  return (name.trim()[0] ?? '?').toUpperCase()
}

function errMessage(err: unknown, fallbackKey: string): string {
  if (err instanceof HttpError) {
    const code = err.apiError?.code
    if (code === 'user_exists') return t('users.errDuplicate')
    if (code === 'weak_password') return t('users.errWeak')
    if (code === 'last_admin') return t('users.errLastAdmin')
    if (code === 'invalid_role') return t('users.errInvalidRole')
    return err.apiError?.message ?? t(fallbackKey)
  }
  return t(fallbackKey)
}

async function load(): Promise<void> {
  loadState.value = 'loading'
  loadError.value = ''
  try {
    const res = await listUsers()
    users.value = res.users
    loadState.value = 'ready'
  } catch (err) {
    loadState.value = 'error'
    loadError.value = errMessage(err, 'users.errLoad')
  }
}

onMounted(load)

// ——— 创建用户(弹层)———
const createModalOpen = ref(false)
const createForm = ref<{ username: string; password: string; role: UserRole }>({
  username: '',
  password: '',
  role: 'viewer',
})
const createErrors = ref({ username: '', password: '' })
const creating = ref(false)

function openCreateModal(): void {
  createForm.value = { username: '', password: '', role: 'viewer' }
  createErrors.value = { username: '', password: '' }
  createModalOpen.value = true
}

function closeCreateModal(): void {
  if (creating.value) return
  createModalOpen.value = false
}

async function submitCreate(): Promise<void> {
  createErrors.value = { username: '', password: '' }
  let ok = true
  if (!createForm.value.username.trim()) {
    createErrors.value.username = t('users.errUsernameRequired')
    ok = false
  }
  if (createForm.value.password.length < 8) {
    createErrors.value.password = t('users.errPasswordShort')
    ok = false
  }
  if (!ok) return
  creating.value = true
  try {
    await createUser({
      username: createForm.value.username.trim(),
      password: createForm.value.password,
      role: createForm.value.role,
    })
    toast.success(t('users.created'))
    createModalOpen.value = false
    await load()
  } catch (err) {
    toast.error(errMessage(err, 'users.errCreate'))
  } finally {
    creating.value = false
  }
}

// ——— 修改角色 ———
const roleSaving = ref('')
async function changeRole(u: User, role: UserRole): Promise<void> {
  if (role === u.role) return
  roleSaving.value = u.id
  try {
    await setUserRole(u.id, role)
    u.role = role
    toast.success(t('users.roleChanged'))
  } catch (err) {
    toast.error(errMessage(err, 'users.errRoleChange'))
    await load() // 回滚到后端真实状态
  } finally {
    roleSaving.value = ''
  }
}

// ——— 重置口令(弹层)———
const resetTarget = ref<User | null>(null)
const resetPassword = ref('')
const resetError = ref('')
const resetting = ref(false)

function openReset(u: User): void {
  resetTarget.value = u
  resetPassword.value = ''
  resetError.value = ''
}

async function submitReset(): Promise<void> {
  if (!resetTarget.value) return
  if (resetPassword.value.length < 8) {
    resetError.value = t('users.errPasswordShort')
    return
  }
  resetting.value = true
  try {
    await resetUserPassword(resetTarget.value.id, resetPassword.value)
    toast.success(t('users.resetDone', { name: resetTarget.value.username }))
    resetTarget.value = null
  } catch (err) {
    resetError.value = errMessage(err, 'users.errReset')
  } finally {
    resetting.value = false
  }
}

// ——— 删除用户 ———
async function removeUser(u: User): Promise<void> {
  const ok = await confirm.open({
    title: t('users.deleteTitle'),
    body: t('users.deleteBody', { name: u.username }),
    confirmLabel: t('users.delete'),
    variant: 'danger',
  })
  if (!ok) return
  try {
    await deleteUser(u.id)
    toast.success(t('users.deleted', { name: u.username }))
    await load()
  } catch (err) {
    toast.error(errMessage(err, 'users.errDelete'))
  }
}
</script>

<template>
  <div class="users-root">
    <!-- ─── section header ──────────────────────────────────────────────── -->
    <div class="section-head">
      <div class="section-head-text">
        <h2 class="section-title">{{ t('users.title') }}</h2>
        <p class="section-desc">{{ t('users.subtitle') }}</p>
      </div>
      <button
        class="btn-primary"
        :disabled="!canManage"
        @click="openCreateModal"
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" aria-hidden="true">
          <path d="M12 5v14M5 12h14" />
        </svg>
        {{ t('users.create') }}
      </button>
    </div>

    <div v-if="!canManage" class="banner banner--warn" role="alert">
      {{ t('users.noPermission') }}
    </div>

    <template v-else>
      <!-- ─── load error banner ─────────────────────────────────────────── -->
      <div
        v-if="loadState === 'error'"
        class="banner banner--error"
        role="alert"
      >
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true">
          <circle cx="12" cy="12" r="9" /><path d="M12 8v4M12 16h.01" />
        </svg>
        <span>{{ loadError }}</span>
        <button class="banner-retry" @click="load">↻ {{ t('users.retry') }}</button>
      </div>

      <!-- ─── users panel ───────────────────────────────────────────────── -->
      <div class="panel">
        <div class="panel-head">
          <span>{{ t('users.listTitle') }}</span>
          <span v-if="loadState === 'ready'" class="panel-meta">
            {{ t('users.countLabel', { n: users.length }) }}
          </span>
        </div>

        <!-- Loading skeleton -->
        <template v-if="loadState === 'loading'">
          <div class="skel-row" v-for="i in 3" :key="i" aria-hidden="true">
            <span class="skel skel--avatar" />
            <span class="skel skel--name" />
            <span class="skel skel--role" />
            <span class="skel skel--ops" />
          </div>
        </template>

        <!-- Empty state -->
        <template v-else-if="loadState === 'ready' && users.length === 0">
          <div class="empty-state">
            <div class="empty-icon" aria-hidden="true">
              <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7">
                <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
                <circle cx="9" cy="7" r="4" />
                <path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13A4 4 0 0 1 16 11" />
              </svg>
            </div>
            <p class="empty-label">{{ t('users.empty') }}</p>
          </div>
        </template>

        <!-- User list -->
        <template v-else-if="loadState === 'ready'">
          <div class="usr-row usr-row--head" aria-hidden="true">
            <span />
            <span>{{ t('users.colUser') }}</span>
            <span>{{ t('users.colRole') }}</span>
            <span />
          </div>

          <div
            v-for="u in users"
            :key="u.id"
            class="usr-row"
          >
            <span class="usr-avatar" :title="u.username" aria-hidden="true">
              {{ avatarChar(u.username) }}
            </span>
            <span class="usr-name">{{ u.username }}</span>
            <span class="usr-role">
              <select
                class="usr-role-select"
                :value="u.role"
                :disabled="roleSaving === u.id"
                :aria-label="t('users.role')"
                @change="changeRole(u, ($event.target as HTMLSelectElement).value as UserRole)"
              >
                <option v-for="opt in roleOptions" :key="opt.value" :value="opt.value">
                  {{ t(opt.labelKey) }}
                </option>
              </select>
            </span>
            <span class="usr-ops">
              <button
                class="op-btn"
                type="button"
                :title="t('users.reset')"
                :aria-label="t('users.reset')"
                @click="openReset(u)"
              >
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9">
                  <path d="M21 2l-2 2m0 0l-9 9m9-9l-4 4-6 6v4h4l6-6 4-4z" />
                  <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
                  <path d="M21 3v6h-6" />
                </svg>
              </button>
              <button
                class="op-btn op-btn--danger"
                type="button"
                :title="t('users.delete')"
                :aria-label="t('users.delete')"
                @click="removeUser(u)"
              >
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9">
                  <path d="M3 6h18M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
                  <path d="M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2" />
                </svg>
              </button>
            </span>
          </div>
        </template>
      </div>
    </template>
  </div>

  <!-- ═══════════════════════════════════════════════════════════════════════
       创建用户弹层(参考凭证保险库弹层样式)
  ════════════════════════════════════════════════════════════════════════ -->
  <Teleport to="body">
    <div
      v-if="createModalOpen"
      class="modal-scrim"
      role="dialog"
      :aria-label="t('users.createTitle')"
      aria-modal="true"
      @keydown.esc="closeCreateModal"
      @click.self="closeCreateModal"
    >
      <div class="modal">
        <div class="modal-head">
          <div class="modal-icon" aria-hidden="true">
            <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7">
              <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
              <circle cx="9" cy="7" r="4" />
              <path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13A4 4 0 0 1 16 11" />
            </svg>
          </div>
          <div>
            <h3 class="modal-title">{{ t('users.createTitle') }}</h3>
            <p class="modal-sub">{{ t('users.createSubtitle') }}</p>
          </div>
          <button
            class="modal-close"
            type="button"
            :aria-label="t('users.cancel')"
            :disabled="creating"
            @click="closeCreateModal"
          >
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M18 6 6 18M6 6l12 12" />
            </svg>
          </button>
        </div>

        <form class="modal-form" novalidate @submit.prevent="submitCreate">
          <!-- Username -->
          <div class="field">
            <label class="field-label" for="nu-username">{{ t('users.username') }}</label>
            <input
              id="nu-username"
              v-model="createForm.username"
              class="field-input"
              :class="{ 'field-input--error': createErrors.username }"
              type="text"
              autocomplete="off"
              :placeholder="t('users.usernamePlaceholder')"
              :disabled="creating"
              :aria-invalid="createErrors.username ? 'true' : undefined"
              @input="createErrors.username = ''"
            />
            <span v-if="createErrors.username" class="field-error" role="alert">{{ createErrors.username }}</span>
          </div>

          <!-- Password -->
          <div class="field">
            <label class="field-label" for="nu-password">
              {{ t('users.password') }}
              <span class="field-optional">{{ t('users.passwordHint') }}</span>
            </label>
            <input
              id="nu-password"
              v-model="createForm.password"
              class="field-input"
              :class="{ 'field-input--error': createErrors.password }"
              type="password"
              autocomplete="new-password"
              :placeholder="t('users.passwordPlaceholder')"
              :disabled="creating"
              :aria-invalid="createErrors.password ? 'true' : undefined"
              @input="createErrors.password = ''"
            />
            <span v-if="createErrors.password" class="field-error" role="alert">{{ createErrors.password }}</span>
          </div>

          <!-- Role (segmented control) -->
          <div class="field">
            <label class="field-label">{{ t('users.role') }}</label>
            <div class="segmented" role="group" :aria-label="t('users.role')">
              <button
                v-for="opt in roleOptions"
                :key="opt.value"
                type="button"
                class="seg-item"
                :class="{ 'seg-item--active': createForm.role === opt.value }"
                :disabled="creating"
                @click="createForm.role = opt.value"
              >{{ t(opt.labelKey) }}</button>
            </div>
          </div>

          <!-- Footer -->
          <div class="modal-footer">
            <button
              type="button"
              class="btn-secondary"
              :disabled="creating"
              @click="closeCreateModal"
            >{{ t('users.cancel') }}</button>
            <button
              type="submit"
              class="btn-primary"
              :disabled="creating"
              :aria-busy="creating"
            >
              <span v-if="creating" class="spinner" aria-hidden="true" />
              {{ t('users.create') }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </Teleport>

  <!-- ═══════════════════════════════════════════════════════════════════════
       重置口令弹层
  ════════════════════════════════════════════════════════════════════════ -->
  <Teleport to="body">
    <div
      v-if="resetTarget"
      class="modal-scrim"
      role="dialog"
      :aria-label="t('users.resetTitle')"
      aria-modal="true"
      @keydown.esc="resetTarget = null"
      @click.self="resetTarget = null"
    >
      <div class="modal modal--sm">
        <div class="modal-head">
          <div class="modal-icon" aria-hidden="true">
            <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7">
              <rect x="4" y="11" width="16" height="9" rx="2" />
              <path d="M8 11V7a4 4 0 0 1 8 0v4" />
            </svg>
          </div>
          <div>
            <h3 class="modal-title">{{ t('users.resetTitle') }}</h3>
            <p class="modal-sub">{{ t('users.resetBody', { name: resetTarget.username }) }}</p>
          </div>
          <button
            class="modal-close"
            type="button"
            :aria-label="t('users.cancel')"
            :disabled="resetting"
            @click="resetTarget = null"
          >
            <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M18 6 6 18M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div class="modal-body">
          <div class="field">
            <label class="field-label" for="nu-reset-pw">{{ t('users.newPassword') }}</label>
            <input
              id="nu-reset-pw"
              v-model="resetPassword"
              class="field-input"
              :class="{ 'field-input--error': resetError }"
              type="password"
              autocomplete="new-password"
              :placeholder="t('users.passwordPlaceholder')"
              :disabled="resetting"
              :aria-invalid="resetError ? 'true' : undefined"
              @input="resetError = ''"
            />
            <span v-if="resetError" class="field-error" role="alert">{{ resetError }}</span>
          </div>
        </div>

        <div class="modal-footer modal-footer--standalone">
          <button
            type="button"
            class="btn-secondary"
            :disabled="resetting"
            @click="resetTarget = null"
          >{{ t('users.cancel') }}</button>
          <button
            type="button"
            class="btn-primary"
            :disabled="resetting"
            :aria-busy="resetting"
            @click="submitReset"
          >
            <span v-if="resetting" class="spinner" aria-hidden="true" />
            {{ t('users.resetConfirm') }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.users-root {
  display: flex;
  flex-direction: column;
  gap: var(--card-gap);
}

/* ─── user list rows ──────────────────────────────────────────────────────── */
.usr-row {
  display: grid;
  grid-template-columns: 32px minmax(160px, 1.4fr) 150px 96px;
  align-items: center;
  gap: 14px;
  padding: 12px 18px;
  border-bottom: 1px solid var(--color-border);
  transition: background-color var(--duration-fast);
}
.usr-row:last-child {
  border-bottom: none;
}
.usr-row:not(.usr-row--head):hover {
  background: var(--color-inset);
}
.usr-row--head {
  height: 34px;
  padding-top: 0;
  padding-bottom: 0;
  font-size: 0.71rem;
  color: var(--color-faint);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  background: var(--color-card-2);
  pointer-events: none;
}

.usr-avatar {
  width: 32px;
  height: 32px;
  border-radius: var(--rounded);
  display: grid;
  place-items: center;
  flex-shrink: 0;
  background: var(--color-primary-soft);
  color: var(--color-primary);
  font-weight: 600;
  font-size: 0.84rem;
}

.usr-name {
  font-size: 0.85rem;
  font-weight: 500;
  color: var(--color-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
}

.usr-role-select {
  height: 30px;
  padding: 0 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--rounded);
  background: var(--color-inset);
  color: var(--color-text);
  font-family: var(--font-sans);
  font-size: 0.8rem;
}
.usr-role-select:focus {
  outline: none;
  border-color: var(--color-primary);
  box-shadow: 0 0 0 3px var(--color-primary-soft);
}
.usr-role-select:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.usr-ops {
  display: flex;
  justify-content: flex-end;
  gap: 5px;
}
.op-btn {
  width: 28px;
  height: 26px;
  border: 1px solid var(--color-border);
  background: transparent;
  color: var(--color-faint);
  border-radius: var(--rounded-md);
  cursor: pointer;
  display: grid;
  place-items: center;
  transition:
    color var(--duration-fast),
    border-color var(--duration-fast),
    background-color var(--duration-fast),
    transform var(--duration-fast);
}
.op-btn:hover {
  color: var(--color-text);
  border-color: var(--color-faint);
}
.op-btn:focus-visible {
  outline: 2px solid var(--color-primary);
  outline-offset: 2px;
}
.op-btn--danger:hover {
  color: var(--color-red);
  border-color: var(--color-red-line);
  background: var(--color-red-soft);
}

/* Loading skeleton grid alignment with the list columns */
.skel-row {
  grid-template-columns: 32px minmax(160px, 1.4fr) 150px 96px;
}
</style>
