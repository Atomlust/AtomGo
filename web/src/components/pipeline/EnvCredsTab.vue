<script setup lang="ts">
/**
 * EnvCredsTab — 环境与凭据 tab (Story 2.4).
 * Environment list (name + target-server placeholder + env vars) + image-registry
 * binding (type + url + vault credential). Edits a local Environment[] copy and emits
 * `update`; the parent (ProjectPipeline) owns save.
 *
 * Target servers reference the real server registry (Story 4-1); reference
 * existence validation is still pending server-side (non-empty check only).
 * Secret env vars / registry credentials reference a vault credentialId — plaintext is
 * never entered, stored, or shown; only the server-computed mask appears.
 */
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  Environment,
  BuildVar,
  RegistryType,
} from '../../api/pipelineSettings'
import type { Credential } from '../../api/credentials'
import type { Server } from '../../api/servers'
import type { User } from '../../api/users'
import AppSelect from '../ui/AppSelect.vue'
import AppMultiSelect from '../ui/AppMultiSelect.vue'

interface Props {
  environments: Environment[]
  credentials: Credential[]
  /** 服务器列表(设置 · 服务器),供「目标服务器」多选。 */
  servers: Server[]
  /** 用户列表(设置 · 用户管理),供「审批人」多选;审批人按用户名(身份)匹配。 */
  users: User[]
  disabled?: boolean
}
const props = defineProps<Props>()

const { t } = useI18n()

const emit = defineEmits<{
  update: [environments: Environment[]]
}>()

// ─── Local editable copy ────────────────────────────────────────────────────────

let keySeq = 0
interface VarRow extends BuildVar {
  _key: number
}
interface EnvRow {
  _key: number
  id: string
  name: string
  /** Whether this environment participates in the promotion chain. */
  inChain: boolean
  /** Whether promoting to this environment requires manual approval. */
  gated: boolean
  /** Approver identities (usernames) allowed to approve promotion into this env (gated only). */
  approverIds: string[]
  /** Target server ids for this environment. */
  targetServerIds: string[]
  envVars: VarRow[]
  registryType: RegistryType | ''
  registryUrl: string
  registryCredentialId: string
  /** Moving channel tag pushed alongside the commit tag on successful build (empty = disabled). */
  channelTag: string
  /** Per-environment container name for promotion deploy (docker run --name). */
  containerName: string
  /** Per-environment port mappings (comma/space separated). */
  ports: string
  /** Per-environment extra docker run args. */
  runArgs: string
}

function toEnvRow(e: Environment): EnvRow {
  return {
    _key: keySeq++,
    id: e.id,
    name: e.name,
    inChain: e.inChain,
    gated: e.gated,
    approverIds: [...(e.approvers ?? [])],
    targetServerIds: [...(e.targetServerIds ?? [])],
    envVars: e.envVars.map((v) => ({ ...v, _key: keySeq++ })),
    registryType: e.imageRegistry.type,
    registryUrl: e.imageRegistry.url,
    registryCredentialId: e.imageRegistry.credentialId ?? '',
    channelTag: e.channelTag ?? '',
    containerName: e.containerName ?? '',
    ports: e.ports ?? '',
    runArgs: e.runArgs ?? '',
  }
}

const envs = ref<EnvRow[]>(props.environments.map(toEnvRow))

watch(
  () => props.environments,
  (list) => {
    if (envsKey(list) === envsKey(compose())) return
    envs.value = list.map(toEnvRow)
  },
)

const REGISTRY_TYPES = computed<Array<{ key: RegistryType; label: string }>>(() => [
  { key: 'harbor', label: 'Harbor' },
  { key: 'acr', label: t('pipelinePanels.envRegistryAcr') },
  { key: 'dockerhub', label: 'Docker Hub' },
  { key: 'custom', label: t('pipelinePanels.envRegistryCustom') },
])
const vaultOptions = computed(() => props.credentials.map((c) => ({ value: c.id, label: `${c.name} · ${c.maskedValue}` })))
// 目标服务器:从「设置 · 服务器」列表多选(按 id 引用)。
const serverOptions = computed(() =>
  props.servers.map((s) => ({ value: s.id, label: `${s.name} · ${s.host}` })),
)
// 审批人:从「设置 · 用户管理」列表多选(按用户名/身份匹配,与后端审批校验一致)。
const userOptions = computed(() =>
  props.users.map((u) => ({ value: u.username, label: u.username })),
)
const registryOptions = computed(() => [
  { value: '', label: t('pipelinePanels.envRegistryUnbound') },
  ...REGISTRY_TYPES.value.map((r) => ({ value: r.key, label: r.label })),
])
const registryCredentialOptions = computed(() => [
  { value: '', label: t('pipelinePanels.envRegistryCredOptional') },
  ...vaultOptions.value,
])

// ─── Compose + emit on change ───────────────────────────────────────────────────

function compose(): Environment[] {
  return envs.value.map((e) => ({
    id: e.id,
    name: e.name,
    inChain: e.inChain,
    gated: e.gated,
    approvers: [...e.approverIds],
    targetServerIds: [...e.targetServerIds],
    envVars: e.envVars.map(({ _key, ...v }) => {
      void _key
      return v
    }),
    imageRegistry: {
      type: e.registryType,
      url: e.registryUrl.trim(),
      credentialId: e.registryCredentialId || undefined,
    },
    channelTag: e.channelTag.trim() || undefined,
    containerName: e.containerName.trim() || undefined,
    ports: e.ports.trim() || undefined,
    runArgs: e.runArgs.trim() || undefined,
  }))
}

// 规范化内容键用于跳过父组件的等价回写,保留输入行的 _key 和焦点;
// 同时防止本地变更与父组件之间的重复 emit。
function envsKey(list: Environment[]): string {
  return JSON.stringify(
    list.map((e) => ({
      id: e.id ?? '',
      name: e.name,
      inChain: !!e.inChain,
      gated: !!e.gated,
      approvers: [...(e.approvers ?? [])].sort(),
      targetServerIds: [...(e.targetServerIds ?? [])].sort(),
      envVars: e.envVars.map((v) => ({
        id: v.id ?? '',
        key: v.key,
        secret: !!v.secret,
        value: v.value ?? '',
        credentialId: v.credentialId ?? '',
        maskedValue: v.maskedValue ?? '',
      })),
      registryType: e.imageRegistry.type,
      registryUrl: e.imageRegistry.url.trim(),
      registryCredentialId: e.imageRegistry.credentialId || '',
      channelTag: (e.channelTag ?? '').trim(),
      containerName: (e.containerName ?? '').trim(),
      ports: (e.ports ?? '').trim(),
      runArgs: (e.runArgs ?? '').trim(),
    })),
  )
}

watch(
  envs,
  () => {
    const next = compose()
    if (envsKey(next) !== envsKey(props.environments)) emit('update', next)
  },
  { deep: true },
)

// ─── Promotion chain (derived) ─────────────────────────────────────────────
// 链 = 勾选「纳入晋级链」的环境,按数组当前顺序排列;上移/下移即调整数组顺序。
const chainPreview = computed(() =>
  envs.value
    .filter((e) => e.inChain)
    .map((e) => ({ _key: e._key, name: e.name, gated: e.gated })),
)

function moveEnvUp(idx: number): void {
  if (idx <= 0) return
  const arr = [...envs.value]
  ;[arr[idx - 1], arr[idx]] = [arr[idx], arr[idx - 1]]
  envs.value = arr
}

function moveEnvDown(idx: number): void {
  if (idx >= envs.value.length - 1) return
  const arr = [...envs.value]
  ;[arr[idx], arr[idx + 1]] = [arr[idx + 1], arr[idx]]
  envs.value = arr
}

// ─── Env ops ────────────────────────────────────────────────────────────────────

function addEnv(): void {
  envs.value.push({
    _key: keySeq++,
    id: '',
    name: '',
    inChain: false,
    gated: false,
    approverIds: [],
    targetServerIds: [],
    envVars: [],
    registryType: '',
    registryUrl: '',
    registryCredentialId: '',
    channelTag: '',
    containerName: '',
    ports: '',
    runArgs: '',
  })
}

function removeEnv(envKey: number): void {
  envs.value = envs.value.filter((e) => e._key !== envKey)
}

function addVar(env: EnvRow, secret: boolean): void {
  env.envVars.push({
    _key: keySeq++,
    id: '',
    key: '',
    secret,
    value: secret ? undefined : '',
    credentialId: secret ? '' : undefined,
  })
}

function removeVar(env: EnvRow, rowKey: number): void {
  env.envVars = env.envVars.filter((v) => v._key !== rowKey)
}

function toggleSecret(row: VarRow): void {
  row.secret = !row.secret
  if (row.secret) {
    row.value = undefined
    row.credentialId = ''
  } else {
    row.credentialId = undefined
    row.value = ''
  }
}

function maskFor(row: VarRow): string {
  if (row.maskedValue) return row.maskedValue
  const c = props.credentials.find((x) => x.id === row.credentialId)
  return c ? c.maskedValue : '••••'
}
</script>

<template>
  <div class="env-root">
    <!-- ─── Promotion chain preview (derived from the env cards below) ───────── -->
    <section class="card chain-preview">
      <header class="card-head">
        <span class="card-ic" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><path d="M22 4 12 14.01l-3-3"/></svg>
        </span>
        {{ t('pipelinePanels.envChainPreview') }}
        <span class="card-sub">{{ t('pipelinePanels.envChainPreviewSub') }}</span>
      </header>
      <div class="chain-flow" role="img" :aria-label="t('pipelinePanels.envChainPreviewAria')">
        <template v-for="(c, i) in chainPreview" :key="c._key">
          <div class="chain-node" :class="{ 'chain-node--gated': c.gated }">
            <span class="chain-node-name">{{ c.name || t('pipelinePanels.envUnnamed') }}</span>
            <span v-if="c.gated" class="chain-node-gate" aria-hidden="true">
              <svg width="9" height="9" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><rect x="3" y="11" width="18" height="10" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
            </span>
          </div>
          <svg v-if="i < chainPreview.length - 1" class="chain-arrow" width="16" height="10" viewBox="0 0 16 10" fill="none" aria-hidden="true"><path d="M0 5h14M10 1l4 4-4 4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/></svg>
        </template>
        <span v-if="chainPreview.length === 0" class="chain-empty">{{ t('pipelinePanels.envChainEmpty') }}</span>
      </div>
    </section>

    <!-- ─── Environments ────────────────────────────────────────────────────── -->
    <section class="card">
      <header class="card-head">
        <span class="card-ic" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
        </span>
        {{ t('pipelinePanels.envEnvironments') }}
        <span class="card-sub">{{ t('pipelinePanels.envCardSub') }}</span>
        <button type="button" class="head-add" :disabled="disabled" @click="addEnv">{{ t('pipelinePanels.envAddEnv') }}</button>
      </header>

      <div v-if="!envs.length" class="env-empty">
        <p>{{ t('pipelinePanels.envEmpty') }}</p>
        <button type="button" class="addbtn" :disabled="disabled" @click="addEnv">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" aria-hidden="true"><path d="M12 5v14M5 12h14"/></svg>
          {{ t('pipelinePanels.envAddFirst') }}
        </button>
      </div>

      <article v-for="(env, idx) in envs" :key="env._key" class="env">
        <header class="env-h">
          <input
            v-model="env.name"
            class="env-name"
            type="text"
            :placeholder="t('pipelinePanels.envNamePlaceholder')"
            :aria-label="t('pipelinePanels.envNameAria')"
            :disabled="disabled"
          >
          <div class="env-flags">
            <label class="fl">
              <span class="fl-label">{{ t('pipelinePanels.envInChain') }}</span>
              <button
                type="button"
                class="mini-toggle"
                :class="{ 'mini-toggle--on': env.inChain }"
                role="switch"
                :aria-checked="env.inChain"
                :aria-label="t('pipelinePanels.envInChainAria')"
                :disabled="disabled"
                @click="env.inChain = !env.inChain"
              ><span class="mini-knob" aria-hidden="true" /></button>
            </label>
            <label class="fl">
              <span class="fl-label">{{ t('pipelinePanels.envGated') }}</span>
              <button
                type="button"
                class="mini-toggle mini-toggle--gate"
                :class="{ 'mini-toggle--on': env.gated }"
                role="switch"
                :aria-checked="env.gated"
                :aria-label="t('pipelinePanels.envGatedAria')"
                :title="env.inChain ? '' : t('pipelinePanels.envGatedHint')"
                :disabled="disabled || !env.inChain"
                @click="env.gated = !env.gated"
              ><span class="mini-knob" aria-hidden="true" /></button>
            </label>
          </div>
          <div class="env-order">
            <button
              type="button"
              class="ord"
              :aria-label="t('pipelinePanels.envMoveUpAria')"
              :disabled="disabled || idx === 0"
              @click="moveEnvUp(idx)"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" aria-hidden="true"><path d="M18 15l-6-6-6 6"/></svg>
            </button>
            <button
              type="button"
              class="ord"
              :aria-label="t('pipelinePanels.envMoveDownAria')"
              :disabled="disabled || idx === envs.length - 1"
              @click="moveEnvDown(idx)"
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" aria-hidden="true"><path d="M6 9l6 6 6-6"/></svg>
            </button>
          </div>
          <button
            type="button"
            class="env-del"
            :aria-label="t('pipelinePanels.envDelAria')"
            :disabled="disabled"
            @click="removeEnv(env._key)"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14"/></svg>
          </button>
        </header>

        <div class="env-body">
          <!-- target servers -->
          <div class="eg">
            <div class="eg-l">
              {{ t('pipelinePanels.envTargetServers') }}
            </div>
            <AppMultiSelect
              v-model="env.targetServerIds"
              :options="serverOptions"
              :placeholder="t('pipelinePanels.envSelectServers')"
              :empty-text="t('pipelinePanels.envNoServers')"
              :aria-label="t('pipelinePanels.envTargetServersAria')"
              :disabled="disabled"
              min-width="0"
              searchable
              :search-placeholder="t('pipelinePanels.envSearchServers')"
            />
          </div>

          <!-- approvers (only meaningful for gated envs) -->
          <div class="eg">
            <div class="eg-l">
              {{ t('pipelinePanels.envApprovers') }}
              <span class="eg-hint">{{ t('pipelinePanels.envApproversHint') }}</span>
            </div>
            <AppMultiSelect
              v-model="env.approverIds"
              :options="userOptions"
              :placeholder="t('pipelinePanels.envSelectApprovers')"
              :empty-text="t('pipelinePanels.envNoUsers')"
              :aria-label="t('pipelinePanels.envApproversAria')"
              :disabled="disabled || !env.gated"
              min-width="0"
              searchable
              :search-placeholder="t('pipelinePanels.envSearchApprovers')"
            />
          </div>

          <!-- env vars -->
          <div class="eg">
            <div class="eg-l">{{ t('pipelinePanels.envEnvVars') }}</div>
            <div class="evar-list">
              <div v-for="row in env.envVars" :key="row._key" class="evar-row">
                <input v-model="row.key" class="ev-k mono" type="text" placeholder="KEY" :aria-label="t('pipelinePanels.envVarKeyAria')" :disabled="disabled">
                <AppSelect
                  v-if="row.secret"
                  :model-value="row.credentialId ?? ''"
                  :options="vaultOptions"
                  :placeholder="t('pipelinePanels.envSelectVaultCred')"
                  :aria-label="t('pipelinePanels.envVaultCredAria')"
                  :disabled="disabled"
                  min-width="0"
                  portal
                  @update:model-value="row.credentialId = $event"
                />
                <input v-else v-model="row.value" class="ev-v mono" type="text" :placeholder="t('pipelinePanels.envVarValuePlaceholder')" :aria-label="t('pipelinePanels.envVarValueAria')" :disabled="disabled">
                <button
                  type="button"
                  class="vfrom"
                  :class="row.secret ? 'vfrom--vault' : 'vfrom--plain'"
                  :disabled="disabled"
                  :title="row.secret ? t('pipelinePanels.envSwitchToPlain') : t('pipelinePanels.envSwitchToVault')"
                  @click="toggleSecret(row)"
                >
                  <template v-if="row.secret">{{ t('pipelinePanels.envVault') }} <span class="mask mono">{{ maskFor(row) }}</span></template>
                  <template v-else>{{ t('pipelinePanels.envPlain') }}</template>
                </button>
                <button type="button" class="ev-del" :aria-label="t('pipelinePanels.envDelVarAria')" :disabled="disabled" @click="removeVar(env, row._key)">
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14"/></svg>
                </button>
              </div>
              <div class="addrow">
                <button type="button" class="addbtn addbtn--sm" :disabled="disabled" @click="addVar(env, false)">{{ t('pipelinePanels.envAddPlainVar') }}</button>
                <button type="button" class="addbtn addbtn--sm addbtn--vault" :disabled="disabled" @click="addVar(env, true)">{{ t('pipelinePanels.envAddVaultSecret') }}</button>
              </div>
            </div>
          </div>

          <!-- image registry -->
          <div class="eg">
            <div class="eg-l">
              {{ t('pipelinePanels.envImageRegistry') }}
              <span class="eg-hint">{{ t('pipelinePanels.envRegistryHint') }}</span>
            </div>
            <div class="reg-grid">
              <AppSelect :model-value="env.registryType" :options="registryOptions" :aria-label="t('pipelinePanels.envRegistryTypeAria')" :disabled="disabled" min-width="0" portal @update:model-value="env.registryType = $event as RegistryType | ''" />
              <input
                v-model="env.registryUrl"
                class="ev-v mono"
                type="text"
                :placeholder="t('pipelinePanels.envRegistryUrlPlaceholder')"
                :aria-label="t('pipelinePanels.envRegistryUrlAria')"
                :disabled="disabled || env.registryType === ''"
              >
              <AppSelect
                v-model="env.registryCredentialId"
                :options="registryCredentialOptions"
                :aria-label="t('pipelinePanels.envRegistryCredAria')"
                :disabled="disabled || env.registryType === ''"
                min-width="0"
                portal
              />
            </div>
            <!-- 移动别名(通道 tag):绑仓库后成功构建额外推送的稳定引用,供外部拉取/回滚。 -->
            <div class="reg-channel">
              <div class="eg-l">
                {{ t('pipelinePanels.envChannelTag') }}
                <span class="eg-hint">{{ t('pipelinePanels.envChannelTagAria') }}</span>
              </div>
              <input
                v-model="env.channelTag"
                class="ev-v mono reg-channel-input"
                type="text"
                :placeholder="t('pipelinePanels.envChannelTagPlaceholder')"
                :aria-label="t('pipelinePanels.envChannelTagAria')"
                :disabled="disabled || env.registryType === ''"
              >
              <p class="reg-channel-desc">{{ t('pipelinePanels.envChannelTagHint') }}</p>
            </div>
          </div>

          <!-- per-environment deploy params (container name / ports / run args) -->
          <div class="eg eg-deploy">
            <div class="eg-l">
              {{ t('pipelinePanels.envDeployParams') }}
              <span class="eg-hint">{{ t('pipelinePanels.envDeployParamsHint') }}</span>
            </div>
            <div class="dep-grid">
              <label class="dep-field">
                <span class="dep-label">{{ t('pipelinePanels.envContainerName') }}</span>
                <input
                  v-model="env.containerName"
                  class="ev-v mono"
                  type="text"
                  :placeholder="t('pipelinePanels.envContainerNamePlaceholder')"
                  :aria-label="t('pipelinePanels.envContainerName')"
                  :disabled="disabled"
                >
              </label>
              <label class="dep-field">
                <span class="dep-label">{{ t('pipelinePanels.envPorts') }}</span>
                <input
                  v-model="env.ports"
                  class="ev-v mono"
                  type="text"
                  :placeholder="t('pipelinePanels.envPortsPlaceholder')"
                  :aria-label="t('pipelinePanels.envPorts')"
                  :disabled="disabled"
                >
              </label>
              <label class="dep-field dep-field--wide">
                <span class="dep-label">{{ t('pipelinePanels.envRunArgs') }}</span>
                <input
                  v-model="env.runArgs"
                  class="ev-v mono"
                  type="text"
                  :placeholder="t('pipelinePanels.envRunArgsPlaceholder')"
                  :aria-label="t('pipelinePanels.envRunArgs')"
                  :disabled="disabled"
                >
              </label>
            </div>
            <p class="dep-warn">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M12 9v4M12 17h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/></svg>
              {{ t('pipelinePanels.envDeployParamsEmptyHint') }}
            </p>
          </div>
        </div>
      </article>
    </section>

    <!-- ─── Security note ───────────────────────────────────────────────────── -->
    <p class="sec-note">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><rect x="4" y="11" width="16" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></svg>
      {{ t('pipelinePanels.envSecNote') }}
    </p>
  </div>
</template>

<style scoped>
.env-root { display: flex; flex-direction: column; gap: 16px; width: 100%; }

.card {
  background: var(--color-card);
  border: 1px solid var(--color-border);
  border-radius: var(--rounded-card);
  box-shadow: var(--shadow);
  overflow: hidden;
}
.card-head {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 13px 18px;
  border-bottom: 1px solid var(--color-border);
  font-size: 0.88rem;
  font-weight: 600;
  color: var(--color-text);
}
.card-ic {
  width: 22px; height: 22px;
  border-radius: 6px;
  background: var(--color-primary-soft);
  color: var(--color-primary);
  display: grid; place-items: center;
  flex: none;
}
.card-ic svg { width: 13px; height: 13px; }
.card-sub { margin-left: 4px; font-size: 0.73rem; color: var(--color-faint); font-weight: 400; }
.head-add {
  margin-left: auto;
  font-size: 0.78rem;
  font-weight: 500;
  color: var(--color-primary);
  background: transparent;
  border: 1px solid var(--color-border-strong);
  border-radius: 7px;
  padding: 5px 11px;
  cursor: pointer;
  font-family: var(--font-sans);
}
.head-add:hover:not(:disabled) { border-color: var(--color-primary); background: var(--color-primary-soft); }
.head-add:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; }
.head-add:disabled { opacity: 0.5; cursor: not-allowed; }

.env-empty { padding: 40px 18px; text-align: center; display: flex; flex-direction: column; align-items: center; gap: 14px; }
.env-empty p { font-size: 0.84rem; color: var(--color-faint); }

/* ─── Single environment ─────────────────────────────────────────────────── */
.env { border-bottom: 1px solid var(--color-border); }
.env:last-of-type { border-bottom: none; }
.env-h {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  padding: 12px 18px;
  background: var(--color-card-2);
}
.env-name {
  flex: 1;
  height: 32px;
  border: 1px solid transparent;
  background: transparent;
  color: var(--color-text);
  font-size: 0.9rem;
  font-weight: 600;
  border-radius: 7px;
  padding: 0 8px;
}
.env-name:hover:not(:disabled) { background: var(--color-inset); }
.env-name:focus { outline: none; border-color: var(--color-primary); background: var(--color-inset); box-shadow: 0 0 0 3px var(--color-primary-soft); }
.env-del {
  width: 30px; height: 30px;
  border: 1px solid var(--color-border);
  background: transparent;
  color: var(--color-faint);
  border-radius: 7px;
  cursor: pointer;
  display: grid; place-items: center;
  flex: none;
}
.env-del:hover:not(:disabled) { color: var(--color-red); border-color: var(--color-red-line); }
.env-del:focus-visible { outline: 2px solid var(--color-primary); }

.env-body { padding: 14px 18px; display: grid; grid-template-columns: 1fr 1fr; gap: 16px; align-items: start; }
.eg-l {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: var(--text-label);
  font-weight: 500;
  color: var(--color-dim);
  margin-bottom: 7px;
}
.eg-hint { font-size: 0.7rem; color: var(--color-faint); font-weight: 400; }

.eg-input, .ev-k, .ev-v, .ev-sel {
  height: 32px;
  border: 1px solid var(--color-border-strong);
  background: var(--color-inset);
  color: var(--color-text);
  border-radius: 8px;
  padding: 0 10px;
  font-size: 0.79rem;
  width: 100%;
}
.eg-input:focus, .ev-k:focus, .ev-v:focus, .ev-sel:focus { outline: none; border-color: var(--color-primary); box-shadow: 0 0 0 3px var(--color-primary-soft); }
.ev-sel { font-family: var(--font-sans); cursor: pointer; }
.mono { font-family: var(--font-mono); }

.evar-list { display: flex; flex-direction: column; gap: 8px; }
.evar-row { display: grid; grid-template-columns: 160px 1fr 124px 30px; gap: 8px; align-items: center; }

.vfrom {
  height: 28px;
  border-radius: 7px;
  font-size: 0.69rem;
  font-weight: 500;
  padding: 0 8px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  white-space: nowrap;
  font-family: var(--font-sans);
}
.vfrom--plain { color: var(--color-dim); background: var(--color-inset); border: 1px solid var(--color-border); }
.vfrom--vault { color: var(--color-cyan); background: var(--color-cyan-soft); border: 1px solid var(--color-cyan-line); }
.vfrom:hover:not(:disabled) { filter: brightness(1.1); }
.vfrom:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 1px; }
.vfrom .mask { letter-spacing: 0.06em; }

.ev-del {
  width: 28px; height: 28px;
  border: 1px solid var(--color-border);
  background: transparent;
  color: var(--color-faint);
  border-radius: 7px;
  cursor: pointer;
  display: grid; place-items: center;
}
.ev-del:hover:not(:disabled) { color: var(--color-red); border-color: var(--color-red-line); }
.ev-del:focus-visible { outline: 2px solid var(--color-primary); }

.reg-grid { display: grid; grid-template-columns: 150px 1fr 200px; gap: 8px; }
.reg-channel { display: flex; flex-direction: column; align-items: stretch; gap: 7px; margin-top: 12px; }
.reg-channel-input { max-width: 280px; width: 100%; }
.reg-channel-desc { margin: 0; font-size: 0.74rem; color: var(--color-faint); line-height: 1.5; }

/* ─── Per-environment deploy params ───────────────────────────────── */
.eg-deploy { grid-column: 1 / -1; }
.dep-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px 12px; }
.dep-field { display: flex; flex-direction: column; gap: 5px; }
.dep-field--wide { grid-column: 1 / -1; }
.dep-warn { display: flex; align-items: flex-start; gap: 6px; margin: 9px 0 0; font-size: 0.72rem; line-height: 1.55; color: var(--color-warning, #854F0B); }
.dep-warn svg { flex: none; margin-top: 1px; }
.dep-label { font-size: 0.72rem; font-weight: 500; color: var(--color-dim); }
@media (max-width: 760px) { .dep-grid { grid-template-columns: 1fr; } }
@media (max-width: 760px) { .env-body { grid-template-columns: 1fr; } .reg-grid { grid-template-columns: 1fr; } .evar-row { grid-template-columns: 1fr; } }

.addrow { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 2px; }
.addbtn {
  display: inline-flex; align-items: center; gap: 6px;
  font-size: 0.79rem; font-weight: 500;
  color: var(--color-primary);
  background: transparent;
  border: 1px dashed var(--color-border-strong);
  border-radius: 8px;
  padding: 7px 12px;
  cursor: pointer;
  font-family: var(--font-sans);
}
.addbtn--sm { padding: 5px 10px; font-size: 0.75rem; }
.addbtn:hover:not(:disabled) { border-color: var(--color-primary); background: var(--color-primary-soft); }
.addbtn:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; }
.addbtn--vault { color: var(--color-cyan); }
.addbtn--vault:hover:not(:disabled) { border-color: var(--color-cyan-line); background: var(--color-cyan-soft); }

.sec-note {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.76rem;
  color: var(--color-faint);
  line-height: 1.5;
  padding: 4px 2px;
}
.sec-note svg { color: var(--color-cyan); flex: none; }
.sec-note b { color: var(--color-dim); font-weight: 600; }

/* ─── Promotion chain preview ──────────────────────────────────────── */
.chain-preview .card-head { border-bottom: 1px solid var(--color-border); }
.chain-flow {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 14px 18px;
  background: var(--color-inset);
  flex-wrap: wrap;
  min-height: 52px;
}
.chain-node {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px;
  border-radius: var(--rounded-md);
  background: var(--color-card-2);
  border: 1px solid var(--color-border-strong);
  font-size: 0.78rem;
  font-weight: 500;
  color: var(--color-text);
  white-space: nowrap;
}
.chain-node--gated {
  border-color: var(--color-amber-line);
  background: var(--color-amber-soft);
  color: var(--color-amber);
}
.chain-node-name { }
.chain-node-gate { color: inherit; display: flex; align-items: center; }
.chain-arrow { color: var(--color-faint); flex-shrink: 0; display: flex; align-items: center; }
.chain-empty { font-size: 0.78rem; color: var(--color-faint); font-style: italic; }

/* ─── Per-env chain flags + ordering ───────────────────────────────── */
.env-flags { display: flex; align-items: center; gap: 12px; }
.eg-hint { margin-left: 6px; font-size: 0.733rem; font-weight: 400; color: var(--color-text-tertiary); }
.fl {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 0.73rem;
  color: var(--color-dim);
  cursor: default;
}
.fl-label { white-space: nowrap; }
.mini-toggle {
  position: relative;
  width: 32px;
  height: 17px;
  border-radius: var(--rounded-full);
  background: var(--color-border-strong);
  border: none;
  cursor: pointer;
  flex-shrink: 0;
  transition: background-color var(--duration-fast);
}
.mini-toggle--on { background: var(--color-primary); }
.mini-toggle--gate.mini-toggle--on { background: var(--color-amber); }
.mini-knob {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 13px;
  height: 13px;
  border-radius: var(--rounded-full);
  background: #fff;
  transition: transform var(--duration-fast);
  pointer-events: none;
  box-shadow: 0 1px 3px oklch(0% 0 0 / 0.3);
}
.mini-toggle--on .mini-knob { transform: translateX(15px); }
.mini-toggle:disabled { opacity: 0.4; cursor: not-allowed; }
@media (prefers-reduced-motion: reduce) { .mini-toggle, .mini-knob { transition: none; } }

.env-order { display: inline-flex; align-items: center; gap: 3px; }
.ord {
  width: 26px;
  height: 26px;
  border: 1px solid var(--color-border);
  background: transparent;
  color: var(--color-faint);
  border-radius: var(--rounded-md);
  cursor: pointer;
  display: grid;
  place-items: center;
  transition: color var(--duration-fast), border-color var(--duration-fast), background-color var(--duration-fast);
}
.ord:hover:not(:disabled) { color: var(--color-primary); border-color: var(--color-primary); background: var(--color-primary-soft); }
.ord:disabled { opacity: 0.25; cursor: not-allowed; }
</style>
