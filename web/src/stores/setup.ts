/**
 * Setup store — 缓存首次启动初始化向导状态,供路由守卫在「setup 模式」下把
 * 除 /setup 外的所有路由重定向到向导,避免把未初始化的实例暴露给业务页面。
 *
 * 与 session store 类似的「缓存一次」语义:首次 ensureSetup() 命中
 * /api/setup/status,之后直接返回缓存;传 force=true(如 /setup 路由自身、
 * 或提交成功后)可强制刷新以确认初始化已完成。
 */

import { defineStore } from 'pinia'
import { ref } from 'vue'
import { http } from '../api/http'
import type { SetupStatus } from '../api/setup'

export const useSetupStore = defineStore('setup', () => {
  /** null = 尚未探测 / 探测失败;否则为最近一次状态快照。 */
  const status = ref<SetupStatus | null>(null)
  let fetched = false

  /**
   * 获取(或返回缓存的)初始化状态。
   *
   * - 返回非 null 且 setupRequired=true  → 系统处于 setup 模式
   * - 返回非 null 且 setupRequired=false → 已完成初始化
   * - 返回 null                          → 探测失败(后端不可达),调用方按「非 setup 模式」兜底
   */
  async function ensureSetup(force = false): Promise<SetupStatus | null> {
    if (fetched && !force) return status.value
    try {
      const data = await http.get<SetupStatus>('/api/setup/status')
      status.value = data
    } catch {
      // 探测失败不阻断导航:视为「非 setup 模式」,交给 session 守卫走正常流程。
      status.value = null
    }
    fetched = true
    return status.value
  }

  return { status, ensureSetup }
})
