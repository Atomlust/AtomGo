import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PromotionPanel from './PromotionPanel.vue'

// 晋升面板:晋级动作 + 审批门 + 历史,以及「批准即部署」的目标环境部署结果展示。
// 这里聚焦最后一块(本次新增):deployRunId / deployTargets / deploySkipped 的呈现。

const api = vi.hoisted(() => ({
  getEnvironments: vi.fn(),
  promoteRun: vi.fn(),
  listRunPromotions: vi.fn(),
}))

vi.mock('../../api/promotion', () => ({
  getEnvironments: api.getEnvironments,
  promoteRun: api.promoteRun,
  listRunPromotions: api.listRunPromotions,
}))

const approveStage = vi.hoisted(() => vi.fn())
const rejectStage = vi.hoisted(() => vi.fn())
vi.mock('../../api/runs', () => ({ approveStage, rejectStage }))

const PROMOTION = {
  id: 'promo-1',
  projectId: 'proj-1',
  sourceRunId: 'run-1',
  fromEnvironment: 'staging',
  targetEnvironment: 'prod',
  status: 'promoted' as const,
  promotedBy: 'admin',
  createdAt: '2026-01-01T00:00:00Z',
  decidedAt: '2026-01-01T00:00:00Z',
}

function panel() {
  return mount(PromotionPanel, {
    props: { runId: 'run-1', projectId: 'proj-1', runStatus: 'success' },
  })
}

describe('PromotionPanel · 晋升部署结果', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getEnvironments.mockResolvedValue({ environments: [{ name: 'prod', gated: false }] })
    api.listRunPromotions.mockResolvedValue({ items: [] })
  })

  it('非门禁晋级:展示目标环境逐机部署结果', async () => {
    api.promoteRun.mockResolvedValue({
      ...PROMOTION,
      deployRunId: 'promo-run-1',
      deployTargets: [
        { serverId: 'srv-1', serverName: 'Web 1', status: 'success', message: 'released', startedAt: '2026-01-01T00:00:00Z', finishedAt: '2026-01-01T00:00:02Z' },
        { serverId: 'srv-2', serverName: 'Web 2', status: 'failed', message: 'health check failed', startedAt: '2026-01-01T00:00:00Z' },
      ],
    })

    const wrapper = panel()
    await flushPromises()
    await wrapper.get('.promo-btn--promote').trigger('click')
    await flushPromises()

    expect(api.promoteRun).toHaveBeenCalledWith('run-1', { targetEnvironment: 'prod' })
    const deploy = wrapper.get('.promo-deploy')
    expect(deploy.get('.promo-deploy-run').text()).toBe('promo-run-1')
    const items = wrapper.findAll('.pd-item')
    expect(items).toHaveLength(2)
    expect(items[0]!.text()).toContain('Web 1')
    expect(items[0]!.text()).toContain('成功')
    expect(items[1]!.text()).toContain('Web 2')
    expect(items[1]!.text()).toContain('失败')
    expect(items[1]!.text()).toContain('health check failed')
    wrapper.unmount()
  })

  it('无目标机:展示本地化的「已跳过部署」说明', async () => {
    api.promoteRun.mockResolvedValue({
      ...PROMOTION,
      deploySkipped: 'no target servers for environment',
    })

    const wrapper = panel()
    await flushPromises()
    await wrapper.get('.promo-btn--promote').trigger('click')
    await flushPromises()

    const skipped = wrapper.get('.promo-deploy-skipped')
    expect(skipped.text()).toContain('目标环境未配置目标机')
    expect(wrapper.find('.promo-deploy-run').exists()).toBe(false)
    wrapper.unmount()
  })

  it('部分变量未注入:展示告警(仅 key,不含明文)', async () => {
    api.promoteRun.mockResolvedValue({
      ...PROMOTION,
      deployRunId: 'promo-run-3',
      deployTargets: [
        { serverId: 'srv-1', serverName: 'Web 1', status: 'success', message: 'released', startedAt: '2026-01-01T00:00:00Z', finishedAt: '2026-01-01T00:00:02Z' },
      ],
      deployEnvUnresolved: ['DB_PASS', 'API_KEY'],
    })

    const wrapper = panel()
    await flushPromises()
    await wrapper.get('.promo-btn--promote').trigger('click')
    await flushPromises()

    const warn = wrapper.get('.promo-deploy-unresolved')
    expect(warn.text()).toContain('DB_PASS')
    expect(warn.text()).toContain('API_KEY')
    expect(warn.text()).not.toContain('s3cr3t')
    wrapper.unmount()
  })

  it('门禁晋级:批准后 inflight 响应中的部署结果被就地呈现', async () => {
    api.getEnvironments.mockResolvedValue({ environments: [{ name: 'prod', gated: true }] })
    // 历史先留空 → 面板渲染「申请晋级」按钮;晋级请求发出后服务端才登记 pending。
    api.listRunPromotions.mockResolvedValue({ items: [] })

    // 门禁晋级会阻塞(POST /promote 挂起直到审批决定)→ 用受控 promise 模拟批准后的结算。
    let resolvePromote!: (v: unknown) => void
    api.promoteRun.mockReturnValue(
      new Promise((res) => {
        resolvePromote = res
      }),
    )

    const wrapper = panel()
    await flushPromises()
    // gated → 先弹确认,再提交
    await wrapper.get('.promo-btn--promote').trigger('click')
    await flushPromises()
    await wrapper.get('.promo-confirm-actions .promo-btn--gate').trigger('click')
    await flushPromises()
    expect(api.promoteRun).toHaveBeenCalledWith('run-1', { targetEnvironment: 'prod' })
    expect(wrapper.find('.promo-deploy').exists()).toBe(false)

    resolvePromote({
      ...PROMOTION,
      deployRunId: 'promo-run-2',
      deployTargets: [
        { serverId: 'srv-9', serverName: 'Web 9', status: 'success', message: '', startedAt: '2026-01-01T00:00:00Z' },
      ],
    })
    await flushPromises()

    const deploy = wrapper.get('.promo-deploy')
    expect(deploy.get('.promo-deploy-run').text()).toBe('promo-run-2')
    expect(wrapper.findAll('.pd-item')).toHaveLength(1)
    wrapper.unmount()
  })
})
