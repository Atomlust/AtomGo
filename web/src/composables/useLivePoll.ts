/**
 * useLivePoll — 后台统一采集间隔(秒)。
 *
 * 服务器 / 容器 / 概览的实时数据由后台按「设置 → 系统 → 实时采集缓存」里配置的
 * 间隔周期采集并写入缓存(见 internal/livecache)。前端轮询节奏应与该间隔保持一致:
 *  - 比它更快没有意义(缓存还没更新,读到的是同一份快照);
 *  - 页面上「每 N 秒自动刷新」的提示也必须显示同一个 N,而不是写死的 12。
 *
 * 读配置失败时回落默认 30s(与后端 defaultLivePollInterval 一致),
 * 保证页面不会因配置接口异常而彻底不刷新。
 */

import { ref, type Ref } from 'vue'
import { getLivePoll } from '../api/livepoll'

/** 与后端 defaultLivePollInterval 一致的兜底间隔(秒)。 */
const DEFAULT_SECONDS = 30

export interface UseLivePoll {
  /** 采集/轮询间隔(秒);供文案展示与 setInterval 使用。 */
  pollSeconds: Ref<number>
  /** 拉取一次配置(通常在 onMounted 里 await 后再启动轮询)。 */
  loadPollInterval: () => Promise<void>
}

export function useLivePoll(): UseLivePoll {
  const pollSeconds = ref<number>(DEFAULT_SECONDS)

  async function loadPollInterval(): Promise<void> {
    try {
      const cfg = await getLivePoll()
      const n = Number(cfg?.intervalSeconds)
      if (Number.isFinite(n) && n >= 10) {
        pollSeconds.value = Math.floor(n)
      }
    } catch {
      // 忽略:保持默认间隔,不阻断页面。
    }
  }

  return { pollSeconds, loadPollInterval }
}
