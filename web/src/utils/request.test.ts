import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

// Mock request.ts 的外部依赖（auth/admin store、router），隔离被测逻辑
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    token: 'test-token',
    refresh: vi.fn().mockResolvedValue('new-token'),
    logout: vi.fn(),
  }),
}))
vi.mock('@/stores/admin', () => ({
  useAdminStore: () => ({ token: '', logout: vi.fn() }),
}))
vi.mock('@/router', () => ({ default: { push: vi.fn() } }))

import { request } from '@/utils/request'

function mockFetchSequence(responses: Array<{ status: number; body: unknown }>) {
  let call = 0
  const spy = vi.fn().mockImplementation(() => {
    const r = responses[Math.min(call, responses.length - 1)]
    call++
    return Promise.resolve(new Response(JSON.stringify(r.body), { status: r.status }))
  })
  vi.stubGlobal('fetch', spy)
  return spy
}

describe('doFetchWithRetry 行为（经 request 导出验证）', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('GET 5xx 幂等重试：前两次 500 第三次 200 应成功', async () => {
    const spy = mockFetchSequence([
      { status: 500, body: {} },
      { status: 502, body: {} },
      { status: 200, body: { code: 0, data: 'ok' } },
    ])
    const p = request('/x')
    await vi.runAllTimersAsync()
    const result = await p
    expect(result).toBe('ok')
    expect(spy).toHaveBeenCalledTimes(3)
  })

  it('POST 5xx 不重试（F1 修复）：网关 502 但后端可能已入库，重试会重复建任务', async () => {
    const spy = mockFetchSequence([{ status: 500, body: {} }])
    const p = request('/tasks', { method: 'POST', body: '{}' }).catch((e) => e)
    await vi.runAllTimersAsync()
    const err = await p
    expect(err).toBeInstanceOf(Error)
    expect(spy).toHaveBeenCalledTimes(1) // 关键：只调一次
  })

  it('超时定时器被清理（F2 修复）：fetch 正常返回后无悬挂 timer', async () => {
    mockFetchSequence([{ status: 200, body: { code: 0, data: 'ok' } }])
    const p = request('/x')
    await vi.runAllTimersAsync()
    await p
    expect(vi.getTimerCount()).toBe(0)
  })

  it('网络错误（fetch reject）GET 重试后成功', async () => {
    let call = 0
    const spy = vi.fn().mockImplementation(() => {
      call++
      return call === 1 ? Promise.reject(new Error('network down'))
        : Promise.resolve(new Response(JSON.stringify({ code: 0, data: 'ok' }), { status: 200 }))
    })
    vi.stubGlobal('fetch', spy)
    const p = request('/x')
    await vi.runAllTimersAsync()
    expect(await p).toBe('ok')
    expect(spy).toHaveBeenCalledTimes(2)
  })
})
