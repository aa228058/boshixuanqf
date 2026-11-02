import { onMounted, onUnmounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { setSseStatus, bumpSseRetry } from '@/stores/sse'

// ===== 任务实时事件(SSE) · 常驻连接版 =====
// 交互层完善(2026-10-20):
//   1) 30秒宽限期——页面切走连接保留, 切回直接复用(租户感知=一直实时连着, 不再每次等几秒)
//   2) 重连上限后转 60s 低频无限重试——挂一晚上断网也能自动恢复, 不再"8次后永久断流"
//   3) 连接状态同步 sseStatus store——侧边栏实时显示 绿(正常)/黄(重连中)/红(断开)
// 帧格式: event: events  data: {task_id, progress} 或 {type:"task_log", task_id, log}

let es: EventSource | null = null
const listeners = new Set<(evt: any) => void>()
let refs = 0
let reconnects = 0
let retryTimer: ReturnType<typeof setTimeout> | null = null
let disconnectTimer: ReturnType<typeof setTimeout> | null = null
const DISCONNECT_GRACE_MS = 30000
const MAX_RETRIES = 8

// 审计P0-03: 先带JWT换60秒ticket, EventSource用ticket连接(JWT不进URL/日志)
async function fetchTicket(): Promise<string | null> {
  try {
    // F-20: token 改为纯内存存储（stores/auth），不再读 localStorage
    const token = useAuthStore().token
    if (!token) return null
    const res = await fetch('/api/v1/events/ticket', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
    })
    if (!res.ok) return null
    const data = await res.json()
    return data.ticket || null
  } catch { return null }
}

// N-03: 统一重连调度——指数退避(10s/20s/30s封顶)
// 连接建立(onopen)即重置预算（原逻辑靠首条消息归零，空闲连接预算被冻结）；
// 换票失败也安排重试（原逻辑静默放弃导致 token 过期后永久断流且无感知）。
// 改进: 满 8 次后不暂停, 转 60s 无限重试(服务恢复自动接上)
function scheduleReconnect() {
  if (refs <= 0) return
  if (reconnects >= MAX_RETRIES) {
    console.warn('[task-events] SSE 重连已达上限, 转 60s 低频无限重试')
    scheduleRetry(60000)
    return
  }
  reconnects++
  scheduleRetry(Math.min(10000 * reconnects, 30000))
}

function scheduleRetry(ms: number) {
  if (retryTimer) clearTimeout(retryTimer)
  retryTimer = setTimeout(() => { retryTimer = null; ensureConnected() }, ms)
}

async function ensureConnected() {
  if (es) return
  const base = (import.meta as any).env?.VITE_API_BASE_URL || '/api/v1'
  const ticket = await fetchTicket()
  if (!ticket) {
    // 换票失败（多为 access token 过期待刷新），不能静默放弃
    scheduleReconnect()
    return
  }
  es = new EventSource(`${base}/events?ticket=${encodeURIComponent(ticket)}`)
  es.onopen = () => {
    reconnects = 0
    setSseStatus('connected')
  }
  // R87-M9 + R118: 全局 SSE 看门狗 —— 45s 无消息判定假活, 主动断开走既有重连逻辑
  // R118: 30s→45s —— 服务端心跳间隔正好 30s(注释行不触发前端监听),
  // 旧值会每 30s 把健康连接误判假活强杀一次; 45s 给心跳留足余量。
  let sseWatchdog: ReturnType<typeof setTimeout> | null = null
  const resetSseWatchdog = () => {
    if (sseWatchdog) clearTimeout(sseWatchdog)
    sseWatchdog = setTimeout(() => {
      console.warn('[useTaskEvents] SSE 45s 无消息, 判定假活, 强制重连')
      try { es.close() } catch { /* 忽略 */ }
      scheduleReconnect()
    }, 45000)
  }

  es.addEventListener('events', (m: any) => {
    try {
      resetSseWatchdog()
      const raw = JSON.parse(m.data)
      // R99 解包: 全局事件流帧为 broker Event{topic,payload} 结构,
      // 真正的业务事件(type/task_id/progress...)在 payload 里。不解包则
      // 各页 task_status/progress 分支全是死代码(任务列表不实时)。
      const evt = raw && raw.payload ? raw.payload : raw
      listeners.forEach((fn) => { try { fn(evt) } catch { /* 单个回调异常不影响其他 */ } })
    } catch { /* 非 JSON 帧忽略 */ }
  })
  es.onerror = () => {
    // EventSource 原生重连会带着【旧 ticket】一直 401，这里主动重建换新票
    console.warn('[task-events] SSE 异常, 退避重建')
    setSseStatus('reconnecting')
    bumpSseRetry()
    const dead = es
    es = null
    try { dead && dead.close() } catch { /* 忽略 */ }
    scheduleReconnect()
  }
}

function teardown() {
  setSseStatus('disconnected')
  if (es) { es.close(); es = null }
  if (retryTimer) { clearTimeout(retryTimer); retryTimer = null }
  if (disconnectTimer) { clearTimeout(disconnectTimer); disconnectTimer = null }
  refs = 0
  reconnects = 0
}

export function useTaskEvents(onEvent: (evt: any) => void) {
  onMounted(() => {
    listeners.add(onEvent)
    refs++
    // 宽限期内有组件回来: 取消待执行的断开, 直接复用连接
    if (disconnectTimer) { clearTimeout(disconnectTimer); disconnectTimer = null }
    ensureConnected()
  })
  onUnmounted(() => {
    listeners.delete(onEvent)
    refs--
    if (refs <= 0) {
      refs = 0
      // 不立即断开: 30 秒宽限期, 期间切回任何页面直接复用(=一直实时连接的体感)
      if (disconnectTimer) clearTimeout(disconnectTimer)
      disconnectTimer = setTimeout(() => {
        disconnectTimer = null
        if (refs <= 0) teardown()
      }, DISCONNECT_GRACE_MS)
    }
  })
}

// R118: 手动立即重连 —— 左下角连接状态点击时调用, 打断退避等待立刻重连
export function reconnectNow() {
  if (retryTimer) { clearTimeout(retryTimer); retryTimer = null }
  reconnects = 0
  ensureConnected()
}
