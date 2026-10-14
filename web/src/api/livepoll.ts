/**
 * Live metrics cache API.
 *
 * GET  /api/system/live-poll        → { intervalSeconds }
 * PUT  /api/system/live-poll        → { intervalSeconds }   (needs CSRF + settings:manage)
 * POST /api/system/live-poll/refresh → { ok: true }          (needs CSRF + settings:manage)
 *
 * The background collector periodically gathers server metrics (CPU/memory/disk) and
 * container lists into an in-process cache; the dashboard / servers / containers pages
 * read from it instead of SSH on every request. The interval is configurable here.
 */

import { http } from './http'

export interface LivePollConfig {
  /** Background collection interval, in seconds (range 10–3600). */
  intervalSeconds: number
}

export async function getLivePoll(): Promise<LivePollConfig> {
  return http.get<LivePollConfig>('/api/system/live-poll')
}

export async function setLivePoll(input: LivePollConfig): Promise<LivePollConfig> {
  return http.put<LivePollConfig>('/api/system/live-poll', input)
}

export async function refreshLiveCache(): Promise<{ ok: boolean }> {
  return http.post<{ ok: boolean }>('/api/system/live-poll/refresh')
}
