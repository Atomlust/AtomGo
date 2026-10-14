<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { submitSetup, type SetupStatus } from '../api/setup'
import { HttpError } from '../api/http'
import { useSetupStore } from '../stores/setup'

const router = useRouter()
const { t } = useI18n()
const setupStore = useSetupStore()

const loading = ref(false)
const bannerError = ref('')
const done = ref(false)
const generatedKey = ref('')
const copied = ref(false)

const username = ref('admin')
const password = ref('')
const confirmPassword = ref('')
const publicUrl = ref('')
const masterKeyMode = ref<'generate' | 'provide'>('generate')
const masterKeyProvided = ref('')

// 数据库选择
const dbConfigured = ref(false)
const selectedDriver = ref<'sqlite' | 'mysql' | 'postgres'>('sqlite')
const dbHost = ref('')
const dbPort = ref('')
const dbUser = ref('')
const dbPassword = ref('')
const dbName = ref('')
const dbDSN = ref('')
const restarting = ref(false)
const poller = ref<number | null>(null)

const usernameError = ref('')
const passwordError = ref('')
const confirmError = ref('')
const masterKeyError = ref('')
const dbError = ref('')

// 尚未固化数据库时展示数据库选择;已固化则展示锁定摘要。
const showDbSection = computed(() => !dbConfigured.value)
// 管理员步骤:已固化数据库,或当前选了 SQLite(单步完成)。
const showAdminSection = computed(() => dbConfigured.value || selectedDriver.value === 'sqlite')

const canSubmit = computed(() => !loading.value && !done.value && !restarting.value)

onMounted(async () => {
  const st = await setupStore.ensureSetup(true)
  if (!st) {
    bannerError.value = t('setup.errNetwork')
    return
  }
  if (!st.setupRequired) {
    // 已经完成初始化 → 直接去登录。
    await router.replace({ name: 'login' })
    return
  }
  username.value = st.defaults.username || 'admin'
  publicUrl.value = st.defaults.publicUrl || ''
  dbConfigured.value = !!st.dbConfigured
  if (st.dbDriver) selectedDriver.value = st.dbDriver as 'sqlite' | 'mysql' | 'postgres'
})

onBeforeUnmount(() => {
  if (poller.value) window.clearTimeout(poller.value)
})

function dbDriverLabel(d: string): string {
  if (d === 'mysql') return t('setup.dbMysql')
  if (d === 'postgres') return t('setup.dbPostgres')
  return t('setup.dbSqlite')
}

function clearFieldErrors(): void {
  usernameError.value = ''
  passwordError.value = ''
  confirmError.value = ''
  masterKeyError.value = ''
  dbError.value = ''
  bannerError.value = ''
}

function isValidBase64_32(s: string): boolean {
  try {
    const raw = atob(s)
    return raw.length === 32
  } catch {
    return false
  }
}

function validateAdmin(): boolean {
  clearFieldErrors()
  let ok = true
  if (!username.value.trim()) {
    usernameError.value = t('setup.errUsername')
    ok = false
  }
  if (password.value.length < 8 || password.value.length > 72) {
    passwordError.value = t('setup.errPasswordLen')
    ok = false
  }
  if (password.value !== confirmPassword.value) {
    confirmError.value = t('setup.errPasswordMismatch')
    ok = false
  }
  if (masterKeyMode.value === 'provide' && masterKeyProvided.value.trim() && !isValidBase64_32(masterKeyProvided.value.trim())) {
    masterKeyError.value = t('setup.errVault')
    ok = false
  }
  return ok
}

function validateDb(): boolean {
  clearFieldErrors()
  if (!dbConfigured.value && (selectedDriver.value === 'mysql' || selectedDriver.value === 'postgres')) {
    const hasDSN = dbDSN.value.trim().length > 0
    const hasFields = dbHost.value.trim() && dbUser.value.trim() && dbName.value.trim()
    if (!hasDSN && !hasFields) {
      dbError.value = t('setup.errDbRequired')
      return false
    }
  }
  return true
}

async function handleSubmit(): Promise<void> {
  // 第一步(数据库未固化)且选了外部数据库:仅提交数据库配置,触发进程自重启。
  const dbStep = showDbSection.value && selectedDriver.value !== 'sqlite' && !dbConfigured.value
  if (dbStep) {
    if (!validateDb()) return
  } else {
    if (!validateAdmin()) return
  }
  loading.value = true
  clearFieldErrors()
  try {
    const token = setupStore.status?.token ?? ''
    const res = await submitSetup({
      token,
      username: username.value.trim(),
      password: password.value,
      masterKey:
        masterKeyMode.value === 'provide' && masterKeyProvided.value.trim()
          ? masterKeyProvided.value.trim()
          : 'generate',
      publicUrl: publicUrl.value.trim(),
      dbDriver: dbStep ? selectedDriver.value : dbConfigured.value ? selectedDriver.value : 'sqlite',
      dbHost: dbHost.value.trim(),
      dbPort: dbPort.value.trim(),
      dbUser: dbUser.value.trim(),
      dbPassword: dbPassword.value,
      dbName: dbName.value.trim(),
      dbDSN: dbDSN.value.trim(),
    })
    if (res.restartRequired) {
      restarting.value = true
      pollUntilConfigured()
      return
    }
    if (res.masterKey) generatedKey.value = res.masterKey
    done.value = true
    // 让路由守卫的 setup 缓存失效,避免提交后还被弹回 /setup。
    await setupStore.ensureSetup(true)
  } catch (err) {
    if (err instanceof HttpError) {
      if (err.status === 410) bannerError.value = t('setup.errAlreadyDone')
      else if (err.status === 403) bannerError.value = t('setup.errToken')
      else if (err.status === 400 && err.apiError) bannerError.value = err.apiError.message
      else if (err.status === 0) bannerError.value = t('setup.errNetwork')
      else bannerError.value = t('setup.errGeneric', { status: err.status })
    } else {
      bannerError.value = t('setup.errNetwork')
    }
  } finally {
    loading.value = false
  }
}

// 数据库固化后进程会自重启以加载新配置;此处轮询直至 dbConfigured 为真,再进入管理员步骤。
function pollUntilConfigured(attempt = 0): void {
  const maxAttempts = 40 // ~60s
  poller.value = window.setTimeout(async () => {
    try {
      const st: SetupStatus | null = await setupStore.ensureSetup(true)
      if (st && st.dbConfigured) {
        dbConfigured.value = true
        if (st.dbDriver) selectedDriver.value = st.dbDriver as 'sqlite' | 'mysql' | 'postgres'
        restarting.value = false
        bannerError.value = ''
        return
      }
    } catch {
      // 服务重启中,网络错误可忽略,继续轮询。
    }
    if (attempt >= maxAttempts) {
      restarting.value = false
      bannerError.value = t('setup.errRestartTimeout')
      return
    }
    pollUntilConfigured(attempt + 1)
  }, 1500)
}

async function copyKey(): Promise<void> {
  try {
    await navigator.clipboard.writeText(generatedKey.value)
    copied.value = true
    setTimeout(() => (copied.value = false), 2000)
  } catch {
    // 剪贴板不可用时静默降级(用户可手动选择文本复制)。
  }
}

function goLogin(): void {
  router.push({ name: 'login' })
}
</script>

<template>
  <div class="setup-page">
    <!-- aurora background -->
    <div class="aurora" aria-hidden="true">
      <span class="blob b1" />
      <span class="blob b2" />
      <span class="blob b3" />
      <span class="blob b4" />
    </div>
    <div class="grain" aria-hidden="true" />

    <!-- top-left brand -->
    <div class="brand">
      <div class="brand-logo mono" aria-hidden="true">⚛</div>
      <div class="brand-name">AtomGo</div>
    </div>

    <div class="wrap">
      <div class="card" role="main">
        <div class="card__pool" aria-hidden="true" />
        <div class="card__sheen" aria-hidden="true" />

        <!-- 完成态:主密钥保存提示 -->
        <template v-if="done">
          <span class="eyebrow"><span class="eyebrow__dot" aria-hidden="true" />{{ t('setup.eyebrow') }}</span>
          <h1 class="headline">{{ t('setup.successTitle') }}</h1>
          <p class="lede">{{ t('setup.successLed') }}</p>

          <div v-if="generatedKey" class="keybox">
            <div class="keybox__title">{{ t('setup.masterKeySavedTitle') }}</div>
            <p class="keybox__hint">{{ t('setup.masterKeySavedHint') }}</p>
            <div class="keybox__row">
              <code class="keybox__value">{{ generatedKey }}</code>
              <button type="button" class="keybox__copy" @click="copyKey">
                {{ copied ? t('setup.copied') : t('setup.copy') }}
              </button>
            </div>
          </div>

          <button type="button" class="submit" @click="goLogin">
            <span class="submit__shine" aria-hidden="true" />
            <span>{{ t('setup.goLogin') }}</span>
            <svg
              class="submit__arrow"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2.4"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <path d="M5 12h14M13 6l6 6-6 6" />
            </svg>
          </button>
        </template>

        <!-- 表单态 -->
        <template v-else>
          <span class="eyebrow"><span class="eyebrow__dot" aria-hidden="true" />{{ t('setup.eyebrow') }}</span>
          <h1 class="headline">{{ t('setup.title') }}</h1>
          <p class="lede">{{ t('setup.lede') }}</p>

          <div
            v-if="bannerError"
            class="banner"
            role="alert"
            aria-live="assertive"
          >
            <span class="banner__icon" aria-hidden="true">⚠</span>
            <span>{{ bannerError }}</span>
          </div>

          <div
            v-if="restarting"
            class="banner banner--info"
            role="status"
            aria-live="polite"
          >
            <span class="banner__icon" aria-hidden="true">⟳</span>
            <span>{{ t('setup.dbRestarting') }}</span>
          </div>

          <form class="setup-form" novalidate aria-label="setup form" @submit.prevent="handleSubmit">
            <!-- 数据库选择(未固化时) -->
            <fieldset v-if="showDbSection" class="section">
              <legend class="section__legend">{{ t('setup.dbSection') }}</legend>
              <div class="radio">
                <label class="radio__opt" :class="{ 'radio__opt--on': selectedDriver === 'sqlite' }">
                  <input v-model="selectedDriver" type="radio" value="sqlite" :disabled="loading" />
                  <span class="radio__title">{{ t('setup.dbSqlite') }}</span>
                  <span class="radio__desc">{{ t('setup.dbSqliteHint') }}</span>
                </label>
                <label class="radio__opt" :class="{ 'radio__opt--on': selectedDriver === 'mysql' }">
                  <input v-model="selectedDriver" type="radio" value="mysql" :disabled="loading" />
                  <span class="radio__title">{{ t('setup.dbMysql') }}</span>
                  <span class="radio__desc">{{ t('setup.dbExternalHint') }}</span>
                </label>
                <label class="radio__opt" :class="{ 'radio__opt--on': selectedDriver === 'postgres' }">
                  <input v-model="selectedDriver" type="radio" value="postgres" :disabled="loading" />
                  <span class="radio__title">{{ t('setup.dbPostgres') }}</span>
                  <span class="radio__desc">{{ t('setup.dbExternalHint') }}</span>
                </label>
              </div>

              <template v-if="selectedDriver !== 'sqlite'">
                <span v-if="dbError" class="field__error" role="alert">{{ dbError }}</span>
                <div class="field">
                  <label class="field__label" for="su-dbhost">{{ t('setup.dbHost') }}</label>
                  <div class="inwrap">
                    <input
                      id="su-dbhost"
                      v-model="dbHost"
                      class="field__input"
                      type="text"
                      autocomplete="off"
                      spellcheck="false"
                      :placeholder="t('setup.dbHostPlaceholder')"
                      :disabled="loading"
                    />
                  </div>
                </div>
                <div class="grid-2">
                  <div class="field">
                    <label class="field__label" for="su-dbport">{{ t('setup.dbPort') }}</label>
                    <div class="inwrap">
                      <input
                        id="su-dbport"
                        v-model="dbPort"
                        class="field__input"
                        type="text"
                        autocomplete="off"
                        :placeholder="t('setup.dbPortPlaceholder')"
                        :disabled="loading"
                      />
                    </div>
                  </div>
                  <div class="field">
                    <label class="field__label" for="su-dbuser">{{ t('setup.dbUser') }}</label>
                    <div class="inwrap">
                      <input
                        id="su-dbuser"
                        v-model="dbUser"
                        class="field__input"
                        type="text"
                        autocomplete="off"
                        spellcheck="false"
                        :disabled="loading"
                      />
                    </div>
                  </div>
                </div>
                <div class="grid-2">
                  <div class="field">
                    <label class="field__label" for="su-dbpass">{{ t('setup.dbPassword') }}</label>
                    <div class="inwrap">
                      <input
                        id="su-dbpass"
                        v-model="dbPassword"
                        class="field__input"
                        type="password"
                        autocomplete="new-password"
                        :disabled="loading"
                      />
                    </div>
                  </div>
                  <div class="field">
                    <label class="field__label" for="su-dbname">{{ t('setup.dbName') }}</label>
                    <div class="inwrap">
                      <input
                        id="su-dbname"
                        v-model="dbName"
                        class="field__input"
                        type="text"
                        autocomplete="off"
                        spellcheck="false"
                        :placeholder="t('setup.dbNamePlaceholder')"
                        :disabled="loading"
                      />
                    </div>
                  </div>
                </div>
                <div class="field">
                  <label class="field__label" for="su-dbdsn">{{ t('setup.dbDSN') }}</label>
                  <div class="inwrap">
                    <input
                      id="su-dbdsn"
                      v-model="dbDSN"
                      class="field__input mono"
                      type="text"
                      autocomplete="off"
                      spellcheck="false"
                      :placeholder="t('setup.dbDSNPlaceholder')"
                      :disabled="loading"
                    />
                  </div>
                  <p class="field__hint">{{ t('setup.dbDSNHint') }}</p>
                </div>
              </template>
            </fieldset>

            <!-- 已固化数据库:锁定摘要 -->
            <fieldset v-else class="section">
              <legend class="section__legend">{{ t('setup.dbSection') }}</legend>
              <p class="field__hint">{{ t('setup.dbConfiguredSummary', { driver: dbDriverLabel(selectedDriver) }) }}</p>
            </fieldset>

            <!-- 管理员账户 / 对外地址 / 保险库(管理员步骤) -->
            <template v-if="showAdminSection">
              <fieldset class="section">
                <legend class="section__legend">{{ t('setup.accountSection') }}</legend>

                <div class="field">
                  <label class="field__label" for="su-username">{{ t('setup.username') }}</label>
                  <div class="inwrap" :class="{ 'inwrap--error': usernameError }">
                    <input
                      id="su-username"
                      v-model="username"
                      class="field__input"
                      type="text"
                      autocomplete="username"
                      autocorrect="off"
                      autocapitalize="off"
                      spellcheck="false"
                      :placeholder="t('setup.usernamePlaceholder')"
                      :disabled="loading"
                      :aria-invalid="usernameError ? 'true' : undefined"
                      @input="usernameError = ''"
                    />
                  </div>
                  <span v-if="usernameError" class="field__error" role="alert">{{ usernameError }}</span>
                </div>

                <div class="field">
                  <label class="field__label" for="su-password">{{ t('setup.password') }}</label>
                  <div class="inwrap" :class="{ 'inwrap--error': passwordError }">
                    <input
                      id="su-password"
                      v-model="password"
                      class="field__input"
                      type="password"
                      autocomplete="new-password"
                      :placeholder="t('setup.passwordPlaceholder')"
                      :disabled="loading"
                      :aria-invalid="passwordError ? 'true' : undefined"
                      @input="passwordError = ''"
                    />
                  </div>
                  <span v-if="passwordError" class="field__error" role="alert">{{ passwordError }}</span>
                </div>

                <div class="field">
                  <label class="field__label" for="su-confirm">{{ t('setup.confirmPassword') }}</label>
                  <div class="inwrap" :class="{ 'inwrap--error': confirmError }">
                    <input
                      id="su-confirm"
                      v-model="confirmPassword"
                      class="field__input"
                      type="password"
                      autocomplete="new-password"
                      :placeholder="t('setup.confirmPasswordPlaceholder')"
                      :disabled="loading"
                      :aria-invalid="confirmError ? 'true' : undefined"
                      @input="confirmError = ''"
                    />
                  </div>
                  <span v-if="confirmError" class="field__error" role="alert">{{ confirmError }}</span>
                </div>
              </fieldset>

              <fieldset class="section">
                <legend class="section__legend">{{ t('setup.publicUrlSection') }}</legend>
                <div class="field">
                  <label class="field__label" for="su-public">{{ t('setup.publicUrl') }}</label>
                  <div class="inwrap">
                    <input
                      id="su-public"
                      v-model="publicUrl"
                      class="field__input"
                      type="url"
                      autocomplete="off"
                      spellcheck="false"
                      :placeholder="t('setup.publicUrlPlaceholder')"
                      :disabled="loading"
                    />
                  </div>
                  <p class="field__hint">{{ t('setup.publicUrlHint') }}</p>
                </div>
              </fieldset>

              <fieldset class="section">
                <legend class="section__legend">{{ t('setup.vaultSection') }}</legend>
                <div class="radio">
                  <label class="radio__opt" :class="{ 'radio__opt--on': masterKeyMode === 'generate' }">
                    <input v-model="masterKeyMode" type="radio" value="generate" :disabled="loading" />
                    <span class="radio__title">{{ t('setup.vaultGenerate') }}</span>
                    <span class="radio__desc">{{ t('setup.vaultGenerateHint') }}</span>
                  </label>
                  <label class="radio__opt" :class="{ 'radio__opt--on': masterKeyMode === 'provide' }">
                    <input v-model="masterKeyMode" type="radio" value="provide" :disabled="loading" />
                    <span class="radio__title">{{ t('setup.vaultProvide') }}</span>
                    <span class="radio__desc">{{ t('setup.vaultProvideHint') }}</span>
                  </label>
                </div>
                <div v-if="masterKeyMode === 'provide'" class="field">
                  <div class="inwrap" :class="{ 'inwrap--error': masterKeyError }">
                    <input
                      v-model="masterKeyProvided"
                      class="field__input mono"
                      type="text"
                      autocomplete="off"
                      spellcheck="false"
                      :placeholder="t('setup.vaultProvidePlaceholder')"
                      :disabled="loading"
                      :aria-invalid="masterKeyError ? 'true' : undefined"
                      @input="masterKeyError = ''"
                    />
                  </div>
                  <span v-if="masterKeyError" class="field__error" role="alert">{{ masterKeyError }}</span>
                </div>
              </fieldset>

              <p class="step-hint">{{ t('setup.stepHint') }}</p>
            </template>

            <button type="submit" class="submit" :disabled="!canSubmit" :aria-busy="loading">
              <span class="submit__shine" aria-hidden="true" />
              <span v-if="loading" class="spinner" aria-hidden="true" />
              <span>{{ loading ? t('setup.submitting') : t('setup.submit') }}</span>
              <svg
                v-if="!loading"
                class="submit__arrow"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2.4"
                stroke-linecap="round"
                stroke-linejoin="round"
              >
                <path d="M5 12h14M13 6l6 6-6 6" />
              </svg>
            </button>
          </form>
        </template>
      </div>
    </div>

    <div class="trust" aria-hidden="true">
      <span>{{ t('login.trustSelfHosted') }}</span><span class="trust__s">·</span>
      <span>{{ t('login.trustNoEgress') }}</span><span class="trust__s">·</span>
      <span>{{ t('login.trustSingleBinary') }}</span>
    </div>
  </div>
</template>

<style scoped>
/* 沉浸式深色门厅:与 Login 同一套品牌色(电光蓝族),不随全局浅/深主题翻转。
   色值显式写死、字体走项目 token、无外部资源(self-hosted「数据不出网」)。 */
.setup-page {
  --sp-bg: #060a14;
  --sp-blue: #3b82f6;
  --sp-deep: #2563eb;
  --sp-indigo: #6366f1;
  --sp-cyan: #22d3ee;
  --sp-sky: #38bdf8;
  --sp-text: #eef4ff;
  --sp-dim: #aebbd6;
  --sp-faint: #74809c;
  --sp-glass: rgba(255, 255, 255, 0.045);
  --sp-brd: rgba(255, 255, 255, 0.12);

  position: fixed;
  inset: 0;
  overflow: auto;
  background: var(--sp-bg);
  color: var(--sp-text);
  font-family: var(--font-sans);
  font-size: 0.933rem;
  line-height: 1.5;
}

.mono {
  font-family: var(--font-mono, ui-monospace, monospace);
}

/* ——— aurora background ——— */
.aurora {
  position: fixed;
  inset: 0;
  z-index: 0;
  overflow: hidden;
  filter: saturate(1.25);
  pointer-events: none;
}
.blob {
  position: absolute;
  border-radius: 50%;
  filter: blur(70px);
  opacity: 0.8;
  mix-blend-mode: screen;
}
.b1 {
  width: 540px;
  height: 540px;
  background: radial-gradient(circle, var(--sp-blue), transparent 65%);
  top: -160px;
  left: -120px;
  animation: float1 18s ease-in-out infinite alternate;
}
.b2 {
  width: 500px;
  height: 500px;
  background: radial-gradient(circle, var(--sp-deep), transparent 65%);
  bottom: -180px;
  right: -100px;
  animation: float2 22s ease-in-out infinite alternate;
}
.b3 {
  width: 420px;
  height: 420px;
  background: radial-gradient(circle, var(--sp-cyan), transparent 65%);
  top: 20%;
  right: 14%;
  animation: float3 16s ease-in-out infinite alternate;
}
.b4 {
  width: 460px;
  height: 460px;
  background: radial-gradient(circle, var(--sp-indigo), transparent 65%);
  bottom: 8%;
  left: 10%;
  animation: float1 24s ease-in-out infinite alternate-reverse;
}
@keyframes float1 {
  0% { transform: translate(0, 0) scale(1); }
  100% { transform: translate(140px, 120px) scale(1.22); }
}
@keyframes float2 {
  0% { transform: translate(0, 0) scale(1.1); }
  100% { transform: translate(-150px, -90px) scale(0.88); }
}
@keyframes float3 {
  0% { transform: translate(0, 0) scale(0.9); }
  100% { transform: translate(-120px, 130px) scale(1.18); }
}
.aurora::after {
  content: '';
  position: absolute;
  inset: 0;
  background: radial-gradient(120% 120% at 50% 40%, transparent 30%, rgba(6, 10, 20, 0.55) 75%, rgba(6, 10, 20, 0.92) 100%);
}
.grain {
  position: fixed;
  inset: 0;
  z-index: 1;
  pointer-events: none;
  opacity: 0.5;
  background-image: radial-gradient(rgba(255, 255, 255, 0.04) 1px, transparent 1px);
  background-size: 3px 3px;
}

/* ——— brand ——— */
.brand {
  position: fixed;
  top: 22px;
  left: 26px;
  z-index: 3;
  display: flex;
  align-items: center;
  gap: 10px;
}
.brand-logo {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border-radius: 10px;
  font-size: 1.333rem;
  color: var(--sp-text);
  background: linear-gradient(160deg, var(--sp-blue), var(--sp-deep));
  box-shadow: 0 6px 18px rgba(37, 99, 235, 0.45);
}
.brand-name {
  font-weight: 700;
  font-size: 1.2rem;
  letter-spacing: 0.2px;
}

/* ——— card ——— */
.wrap {
  position: relative;
  z-index: 2;
  min-height: 100%;
  display: grid;
  place-items: center;
  padding: 88px 20px 64px;
}
.card {
  position: relative;
  /* 内容宽度 520px → 720px(+200px):原先卡片偏窄,表单与说明文字挤在一起。
     仍以 100% 兜底,窄屏不会溢出。 */
  width: min(720px, 100%);
  padding: 34px 34px 30px;
  border-radius: 22px;
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.07), rgba(255, 255, 255, 0.03));
  border: 1px solid var(--sp-brd);
  box-shadow: 0 30px 80px rgba(0, 0, 0, 0.5), inset 0 1px 0 rgba(255, 255, 255, 0.08);
  backdrop-filter: blur(18px);
  overflow: hidden;
}
.card__pool {
  position: absolute;
  inset: -1px;
  background: radial-gradient(120% 80% at 50% -10%, rgba(59, 130, 246, 0.18), transparent 60%);
  pointer-events: none;
}
.card__sheen {
  position: absolute;
  top: 0;
  left: -40%;
  width: 40%;
  height: 100%;
  background: linear-gradient(100deg, transparent, rgba(255, 255, 255, 0.08), transparent);
  transform: skewX(-18deg);
  pointer-events: none;
}

.eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 0.8rem;
  letter-spacing: 1.4px;
  text-transform: uppercase;
  color: var(--sp-cyan);
}
.eyebrow__dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--sp-cyan);
  box-shadow: 0 0 10px var(--sp-cyan);
}
.headline {
  margin: 12px 0 6px;
  font-size: 1.867rem;
  font-weight: 760;
  letter-spacing: -0.4px;
  line-height: 1.15;
}
.lede {
  margin: 0 0 18px;
  color: var(--sp-dim);
  font-size: 0.933rem;
}

/* ——— banner ——— */
.banner {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  padding: 11px 13px;
  border-radius: 12px;
  margin-bottom: 16px;
  background: rgba(248, 113, 113, 0.12);
  border: 1px solid rgba(248, 113, 113, 0.35);
  color: #ffd9d9;
  font-size: 0.867rem;
}
.banner__icon {
  flex: none;
  font-size: 1rem;
}
.banner--info {
  background: rgba(56, 189, 248, 0.12);
  border-color: rgba(56, 189, 248, 0.35);
  color: #d6f1ff;
}

/* ——— form ——— */
.setup-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.section {
  border: 1px solid var(--sp-brd);
  border-radius: 14px;
  padding: 14px 14px 16px;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 12px;
  background: rgba(255, 255, 255, 0.02);
}
.section__legend {
  padding: 0 8px;
  font-size: 0.833rem;
  font-weight: 600;
  letter-spacing: 0.3px;
  color: var(--sp-sky);
}
.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.field__label {
  font-size: 0.833rem;
  color: var(--sp-dim);
}
.field__hint {
  margin: 0;
  font-size: 0.8rem;
  color: var(--sp-faint);
  line-height: 1.45;
}
.field__error {
  font-size: 0.8rem;
  color: #ffb4b4;
}
.grid-2 {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.inwrap {
  display: flex;
  align-items: center;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--sp-brd);
  border-radius: 11px;
  padding: 0 12px;
  transition: border-color 0.15s, box-shadow 0.15s;
}
.inwrap:focus-within {
  border-color: var(--sp-blue);
  box-shadow: 0 0 0 3px rgba(59, 130, 246, 0.22);
}
.inwrap--error {
  border-color: rgba(248, 113, 113, 0.6);
}
.field__input {
  flex: 1;
  width: 100%;
  border: none;
  outline: none;
  background: transparent;
  color: var(--sp-text);
  font-size: 0.933rem;
  padding: 11px 0;
}
.field__input::placeholder {
  color: var(--sp-faint);
}

/* ——— radio ——— */
.radio {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.radio__opt {
  display: grid;
  grid-template-columns: auto 1fr;
  grid-template-rows: auto auto;
  column-gap: 10px;
  align-items: center;
  padding: 11px 12px;
  border-radius: 12px;
  border: 1px solid var(--sp-brd);
  background: rgba(255, 255, 255, 0.03);
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s;
}
.radio__opt--on {
  border-color: var(--sp-blue);
  background: rgba(59, 130, 246, 0.1);
}
.radio__opt input {
  grid-row: 1 / 3;
  accent-color: var(--sp-blue);
  width: 16px;
  height: 16px;
}
.radio__title {
  font-size: 0.9rem;
  font-weight: 600;
  color: var(--sp-text);
}
.radio__desc {
  grid-column: 2;
  font-size: 0.8rem;
  color: var(--sp-faint);
  line-height: 1.4;
}

.step-hint {
  margin: 2px 0 0;
  font-size: 0.8rem;
  color: var(--sp-faint);
}

/* ——— submit ——— */
.submit {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  margin-top: 4px;
  padding: 13px 18px;
  border: none;
  border-radius: 13px;
  font-size: 1rem;
  font-weight: 650;
  color: #fff;
  cursor: pointer;
  background: linear-gradient(135deg, var(--sp-blue), var(--sp-deep));
  box-shadow: 0 12px 30px rgba(37, 99, 235, 0.45);
  overflow: hidden;
  transition: transform 0.12s, box-shadow 0.15s, opacity 0.15s;
}
.submit:hover:not(:disabled) {
  transform: translateY(-1px);
  box-shadow: 0 16px 38px rgba(37, 99, 235, 0.55);
}
.submit:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.submit__shine {
  position: absolute;
  top: 0;
  left: -40%;
  width: 40%;
  height: 100%;
  background: linear-gradient(100deg, transparent, rgba(255, 255, 255, 0.28), transparent);
  transform: skewX(-18deg);
  animation: shine 3.2s ease-in-out infinite;
}
@keyframes shine {
  0% { left: -40%; }
  60%, 100% { left: 120%; }
}
.submit__arrow {
  width: 18px;
  height: 18px;
}
.spinner {
  width: 17px;
  height: 17px;
  border-radius: 50%;
  border: 2px solid rgba(255, 255, 255, 0.4);
  border-top-color: #fff;
  animation: spin 0.7s linear infinite;
}
@keyframes spin {
  to { transform: rotate(360deg); }
}

/* ——— key box (success) ——— */
.keybox {
  margin: 6px 0 18px;
  padding: 14px;
  border-radius: 13px;
  border: 1px solid rgba(34, 211, 238, 0.35);
  background: rgba(34, 211, 238, 0.08);
}
.keybox__title {
  font-size: 0.867rem;
  font-weight: 650;
  color: var(--sp-cyan);
  margin-bottom: 4px;
}
.keybox__hint {
  margin: 0 0 10px;
  font-size: 0.8rem;
  color: var(--sp-dim);
  line-height: 1.45;
}
.keybox__row {
  display: flex;
  gap: 8px;
  align-items: stretch;
}
.keybox__value {
  flex: 1;
  font-size: 0.767rem;
  word-break: break-all;
  padding: 9px 10px;
  border-radius: 9px;
  background: rgba(0, 0, 0, 0.35);
  color: var(--sp-text);
  border: 1px solid var(--sp-brd);
}
.keybox__copy {
  flex: none;
  padding: 0 14px;
  border: 1px solid var(--sp-brd);
  border-radius: 9px;
  background: rgba(255, 255, 255, 0.06);
  color: var(--sp-text);
  font-size: 0.867rem;
  cursor: pointer;
  transition: background 0.15s;
}
.keybox__copy:hover {
  background: rgba(255, 255, 255, 0.12);
}

/* ——— trust bar ——— */
.trust {
  position: fixed;
  bottom: 18px;
  left: 0;
  right: 0;
  z-index: 2;
  display: flex;
  justify-content: center;
  gap: 10px;
  font-size: 0.8rem;
  color: var(--sp-faint);
  pointer-events: none;
}
.trust__s {
  opacity: 0.5;
}
</style>
