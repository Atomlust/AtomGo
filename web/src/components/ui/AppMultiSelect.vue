<script setup lang="ts">
/**
 * AppMultiSelect — 多选下拉(从列表勾选,避免手动输入出错)。
 * 视觉与交互对齐 AppSelect(同一套设计 token + Portal 定位),但 modelValue 为 string[],
 * 触发器以 chip 展示已选项,下拉以勾选框多选,并提供「全选 / 清空」。
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

export interface SelectOption {
  value: string
  label: string
}

const props = withDefaults(
  defineProps<{
    modelValue: string[]
    options: SelectOption[]
    placeholder?: string
    disabled?: boolean
    ariaLabel?: string
    minWidth?: string
    height?: string
    searchable?: boolean
    searchPlaceholder?: string
    emptyText?: string
  }>(),
  {
    placeholder: '',
    disabled: false,
    ariaLabel: undefined,
    minWidth: '180px',
    height: undefined,
    searchable: false,
    searchPlaceholder: '',
    emptyText: '',
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
}>()

const root = ref<HTMLElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const open = ref(false)
const query = ref('')
const activeIndex = ref(-1)
const menuStyle = ref<Record<string, string>>({})

const selectedOptions = computed(() =>
  props.options.filter((o) => props.modelValue.includes(o.value)),
)
const visibleOptions = computed(() =>
  props.searchable && query.value
    ? props.options.filter((o) =>
        (o.label + ' ' + o.value)
          .toLowerCase()
          .includes(query.value.toLowerCase()),
      )
    : props.options,
)
const allVisibleSelected = computed(
  () =>
    visibleOptions.value.length > 0 &&
    visibleOptions.value.every((o) => props.modelValue.includes(o.value)),
)

function isSelected(v: string): boolean {
  return props.modelValue.includes(v)
}

function toggle(v: string): void {
  if (props.disabled) return
  const set = new Set(props.modelValue)
  if (set.has(v)) set.delete(v)
  else set.add(v)
  emit('update:modelValue', [...set])
}

function toggleAll(): void {
  const visible = visibleOptions.value
  const set = new Set(props.modelValue)
  if (allVisibleSelected.value) visible.forEach((o) => set.delete(o.value))
  else visible.forEach((o) => set.add(o.value))
  emit('update:modelValue', [...set])
}

function clearAll(): void {
  emit('update:modelValue', [])
}

function close(): void {
  open.value = false
  activeIndex.value = -1
  query.value = ''
}

function toggleOpen(): void {
  if (props.disabled) return
  if (open.value) {
    close()
    return
  }
  positionMenu()
  open.value = true
}

function positionMenu(): void {
  if (!root.value) return
  const rect = root.value.getBoundingClientRect()
  const desiredHeight = Math.min(
    280,
    props.options.length * 38 + 12 + (props.searchable ? 38 : 0) + 44,
  )
  const availableBelow = window.innerHeight - rect.bottom - 8
  const availableAbove = rect.top - 8
  const above =
    availableBelow < desiredHeight && availableAbove > availableBelow
  const maxHeight = Math.max(
    0,
    Math.min(280, above ? availableAbove : availableBelow),
  )
  menuStyle.value = {
    left: `${rect.left}px`,
    width: `${rect.width}px`,
    top: above ? 'auto' : `${rect.bottom + 6}px`,
    bottom: above ? `${window.innerHeight - rect.top + 6}px` : 'auto',
    maxHeight: `${maxHeight}px`,
  }
}

watch(query, () => {
  activeIndex.value = 0
})
watch(
  () => props.disabled,
  (d) => {
    if (d) close()
  },
  { flush: 'sync' },
)
watch(
  visibleOptions,
  () => {
    if (open.value) positionMenu()
  },
  { flush: 'sync' },
)

function handleKeydown(event: KeyboardEvent): void {
  if (props.disabled || event.isComposing || event.keyCode === 229) return
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    if (!open.value) toggleOpen()
    else
      activeIndex.value =
        (activeIndex.value +
          (event.key === 'ArrowDown' ? 1 : -1) +
          visibleOptions.value.length) %
        visibleOptions.value.length
  } else if (event.key === 'Enter' && open.value) {
    event.preventDefault()
    const option = visibleOptions.value[activeIndex.value]
    if (option) toggle(option.value)
  } else if (event.key === 'Escape' && open.value) {
    event.preventDefault()
    close()
  }
}

function handlePointerdown(event: PointerEvent): void {
  if (
    open.value &&
    root.value &&
    !root.value.contains(event.target as Node) &&
    !menu.value?.contains(event.target as Node)
  )
    close()
}

onMounted(() => {
  document.addEventListener('pointerdown', handlePointerdown)
  window.addEventListener('resize', positionMenu)
  window.addEventListener('scroll', positionMenu, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', handlePointerdown)
  window.removeEventListener('resize', positionMenu)
  window.removeEventListener('scroll', positionMenu, true)
})
</script>

<template>
  <div
    ref="root"
    class="app-multi"
    :class="{ 'app-multi--open': open }"
    :style="{ minWidth, '--app-multi-height': height }"
  >
    <div
      class="app-multi__trigger"
      role="combobox"
      :aria-label="ariaLabel"
      :aria-expanded="open"
      :aria-controls="`${ariaLabel ?? 'ms'}-options`"
      aria-haspopup="listbox"
      :aria-disabled="disabled"
      :aria-multiselectable="true"
      tabindex="0"
      @click="toggleOpen"
      @keydown="handleKeydown"
    >
      <span v-if="!selectedOptions.length" class="app-multi__placeholder">{{ placeholder }}</span>
      <span v-else class="app-multi__chips">
        <span v-for="o in selectedOptions" :key="o.value" class="app-multi__chip">
          {{ o.label }}
          <button
            type="button"
            class="app-multi__chip-x"
            :aria-label="`${o.label} 移除`"
            :disabled="disabled"
            @click.stop="toggle(o.value)"
          >
            <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg>
          </button>
        </span>
      </span>
      <svg
        class="app-multi__chevron"
        width="15"
        height="15"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
        aria-hidden="true"
      >
        <path d="m6 9 6 6 6-6" />
      </svg>
    </div>

    <Teleport to="body">
      <div
        v-if="open"
        :id="`${ariaLabel ?? 'ms'}-options`"
        ref="menu"
        class="app-multi__menu app-multi__menu--portal"
        :style="menuStyle"
        role="listbox"
        aria-multiselectable="true"
      >
        <input
          v-if="searchable"
          v-model="query"
          class="app-multi__search"
          type="search"
          :aria-label="searchPlaceholder"
          :placeholder="searchPlaceholder"
          @keydown="
            ['ArrowDown', 'ArrowUp', 'Enter', 'Escape'].includes($event.key) &&
            handleKeydown($event)
          "
        >
        <button
          type="button"
          class="app-multi__bulk"
          :disabled="!visibleOptions.length"
          @click="toggleAll"
        >
          {{ allVisibleSelected ? $t('pipelinePanels.msDeselectAll') : $t('pipelinePanels.msSelectAll') }}
        </button>
        <button
          type="button"
          class="app-multi__option"
          :class="{
            'app-multi__option--active': index === activeIndex,
            'app-multi__option--selected': isSelected(option.value),
          }"
          v-for="(option, index) in visibleOptions"
          :key="option.value"
          role="option"
          :aria-selected="isSelected(option.value)"
          @mouseenter="activeIndex = index"
          @click="toggle(option.value)"
        >
          <span class="app-multi__check" :class="{ 'app-multi__check--on': isSelected(option.value) }" aria-hidden="true">
            <svg v-if="isSelected(option.value)" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6"><path d="m5 12 4 4L19 6"/></svg>
          </span>
          <span class="app-multi__option-label">{{ option.label }}</span>
        </button>
        <div v-if="!visibleOptions.length" class="app-multi__empty">{{ emptyText || placeholder }}</div>
        <div v-if="selectedOptions.length" class="app-multi__footer">
          {{ $t('pipelinePanels.msSelected', { n: selectedOptions.length }) }}
          <button type="button" class="app-multi__clear" @click="clearAll">{{ $t('pipelinePanels.msClear') }}</button>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.app-multi {
  position: relative;
  width: 100%;
}
.app-multi__trigger {
  position: relative;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 5px;
  width: 100%;
  min-height: var(--app-multi-height, 38px);
  padding: 4px 34px 4px 10px;
  border: 1px solid var(--color-border-strong);
  border-radius: var(--rounded);
  background: var(--color-inset);
  color: var(--color-text);
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: border-color var(--duration-fast), box-shadow var(--duration-fast), background var(--duration-fast);
}
.app-multi__trigger:hover:not(:disabled) {
  border-color: var(--color-faint);
  background: var(--color-inset);
}
.app-multi--open .app-multi__trigger,
.app-multi__trigger:focus-visible {
  outline: none;
  border-color: var(--color-primary);
  box-shadow: 0 0 0 3px var(--color-primary-soft);
}
.app-multi__trigger[aria-disabled='true'] {
  opacity: 0.55;
  cursor: not-allowed;
}
.app-multi__placeholder {
  color: var(--color-faint);
  font-size: 0.79rem;
}
.app-multi__chips { display: flex; flex-wrap: wrap; gap: 5px; flex: 1; min-width: 0; }
.app-multi__chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
  padding: 2px 4px 2px 8px;
  border-radius: 100px;
  background: var(--color-primary-soft);
  color: var(--color-primary);
  border: 1px solid var(--color-primary-line, var(--color-border-strong));
  font-size: 0.73rem;
  font-weight: 500;
  line-height: 1.4;
}
.app-multi__chip-x {
  display: grid;
  place-items: center;
  width: 16px;
  height: 16px;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: inherit;
  cursor: pointer;
  padding: 0;
}
.app-multi__chip-x:hover:not(:disabled) { background: rgb(0 0 0 / 12%); }
.app-multi__chip-x:disabled { cursor: not-allowed; }
.app-multi__chevron {
  position: absolute;
  right: 10px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--color-faint);
  transition: transform var(--duration-fast), color var(--duration-fast);
  flex: none;
}
.app-multi--open .app-multi__chevron { transform: translateY(-50%) rotate(180deg); color: var(--color-primary); }

.app-multi__menu {
  box-sizing: border-box;
  position: fixed;
  z-index: 10000;
  padding: 5px;
  border: 1px solid var(--color-border-strong);
  border-radius: var(--rounded);
  background: var(--color-card-2);
  box-shadow: 0 12px 28px rgb(0 0 0 / 24%);
}
.app-multi__search {
  width: 100%;
  box-sizing: border-box;
  min-height: 36px;
  padding: 7px 9px;
  margin-bottom: 4px;
  border: 1px solid var(--color-border);
  border-radius: var(--rounded);
  background: var(--color-inset);
  color: var(--color-text);
  font: inherit;
}
.app-multi__bulk {
  display: block;
  width: 100%;
  text-align: left;
  padding: 7px 9px;
  margin-bottom: 4px;
  border: 1px dashed var(--color-border-strong);
  border-radius: calc(var(--rounded) - 2px);
  background: transparent;
  color: var(--color-dim);
  font: inherit;
  font-size: 0.76rem;
  font-weight: 500;
  cursor: pointer;
}
.app-multi__bulk:hover:not(:disabled) { border-color: var(--color-primary); color: var(--color-primary); background: var(--color-primary-soft); }
.app-multi__bulk:disabled { opacity: 0.5; cursor: not-allowed; }
.app-multi__option {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  min-height: 38px;
  padding: 8px 9px;
  border: 0;
  border-radius: calc(var(--rounded) - 2px);
  background: transparent;
  color: var(--color-text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.app-multi__option:hover,
.app-multi__option--active { background: var(--color-inset); }
.app-multi__option--selected { color: var(--color-text); }
.app-multi__check {
  display: grid;
  place-items: center;
  width: 18px;
  height: 18px;
  flex: none;
  border: 1px solid var(--color-border-strong);
  border-radius: 5px;
  background: var(--color-card);
  color: #fff;
}
.app-multi__check--on { background: var(--color-primary); border-color: var(--color-primary); }
.app-multi__option-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.app-multi__empty { padding: 10px 9px; color: var(--color-faint); font-size: 0.8rem; }
.app-multi__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 9px 4px;
  margin-top: 2px;
  border-top: 1px solid var(--color-border);
  font-size: 0.74rem;
  color: var(--color-faint);
}
.app-multi__clear {
  border: none;
  background: none;
  color: var(--color-primary);
  font: inherit;
  font-size: 0.74rem;
  font-weight: 500;
  cursor: pointer;
  padding: 0;
  text-decoration: underline;
  text-underline-offset: 2px;
}
</style>
