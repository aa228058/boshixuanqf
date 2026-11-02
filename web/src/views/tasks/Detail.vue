<template>
  <main class="max-w-4xl mx-auto space-y-6">
    <Breadcrumb :items="[{ path: `/workspace/${route.params.accountId}/tasks`, label: '任务列表' }, { path: '', label: '任务详情' }]" />
    <!-- 页面级错误态: 加载失败可重试 -->
    <EmptyState v-if="pageError && !task" icon="ListTodo" :title="pageError" description="任务详情暂时无法显示">
      <button @click="fetchTask" class="mt-3 px-4 py-1.5 rounded-lg text-xs border border-border text-text-secondary hover:text-cyan hover:border-cyan/40 transition-colors">重新加载</button>
    </EmptyState>
    <div v-else-if="pageLoading && !task" class="glass rounded-[var(--t-r-lg)] p-12 text-center text-sm text-text-muted lift">加载中...</div>
    <div v-if="task" class="flex flex-wrap items-center gap-3">
      <button @click="$router.push(`/workspace/${route.params.accountId}/tasks`)" class="p-2 rounded-lg hover:bg-bg-hover transition-colors" aria-label="返回">
        <ArrowLeft class="w-5 h-5" />
      </button>
      <div>
        <!-- R87-M8: 隐藏任务编号(#hash 对租户无意义) -->
        <h2 class="text-xl tracking-tight font-semibold text-text-primary truncate">{{ taskTypeLabel(task?.type || '') }}</h2>
        <p class="text-xs text-text-secondary">创建于 {{ task ? formatDateTime(task.created_at) : '' }}</p>
      </div>
      <span
        class="ml-auto text-xs px-2 py-0.5 rounded-full border"
        :class="task ? taskStatusClass(task.status) : ''"
      >
        {{ task ? taskStatusLabel(task.status) : '' }}
      </span>
    </div>

    <!-- R106: 桌面端两栏 —— 左列进度卡(固定宽), 右列实时日志(自适应); 移动端保持单列堆叠 -->
    <div class="grid grid-cols-1 lg:grid-cols-[minmax(0,340px)_minmax(0,1fr)] gap-4 items-start">
    <div class="space-y-4 min-w-0">
    <!-- 进度 -->
    <div class="glass rounded-[var(--t-r-md)] p-6 lift">
      <div class="flex items-center justify-between mb-4">
        <div class="flex items-center gap-5">
          <!-- R126 示例2 ring 进度环: conic-gradient 环 + 内圈百分比 -->
          <div class="relative w-[92px] h-[92px] rounded-full grid place-items-center shrink-0"
               :style="{ background: `conic-gradient(var(--t-brand) ${progressPercent * 3.6}deg, var(--t-surface-2) 0deg)` }">
            <div class="absolute inset-[9px] rounded-full grid place-items-center" style="background: var(--t-bg);">
              <span class="text-title font-black tabular-nums">{{ progressPercent }}<span class="text-[11px] font-bold">%</span></span>
            </div>
          </div>
          <div>
            <p class="text-sm text-text-secondary max-w-[60ch]">
              <span class="tabular-nums">{{ formatNumber((task?.progress.done || 0) + (task?.progress.failed || 0)) }}</span>/<span class="tabular-nums">{{ formatNumber(task?.progress.total || 0) }}</span> 已处理
              <span class="text-emerald ml-2"><span class="tabular-nums">{{ formatNumber(task?.progress.done || 0) }}</span> 成功</span>
              <span v-if="task?.progress.failed" class="text-rose ml-2"><span class="tabular-nums">{{ formatNumber(task.progress.failed) }}</span> 失败</span>
            </p>
          </div>
        </div>
        <div class="flex flex-wrap gap-2">
          <button v-if="task?.status === 'running'" @click="pause" class="px-3 py-2 rounded-lg bg-amber/10 text-amber border border-amber/20 text-sm">暂停</button>
          <button v-else-if="task?.status === 'paused'" @click="resume" class="px-3 py-2 rounded-lg bg-emerald/10 text-emerald border border-emerald/20 text-sm">恢复</button>
          <button v-if="['running','paused','pending'].includes(task?.status || '')" @click="cancel" class="px-3 py-2 rounded-lg bg-rose/10 text-rose border border-rose/20 text-sm" aria-label="取消">取消</button>
          <button v-if="['success','failed','cancelled'].includes(task?.status || '') && (task?.progress?.failed || 0) > 0" @click="retryFailed" class="px-3 py-2 rounded-lg bg-cyan/10 text-cyan border border-cyan/20 text-sm">重试失败目标 ({{ task?.progress?.failed }})</button>
        </div>
      </div>
      <div class="w-full h-3 bg-bg-card rounded-full overflow-hidden">
        <div
          class="h-full rounded-full transition-[transform,opacity,border-color,background-color] duration-300 relative"
          :class="progressColor"
          :style="{ width: `${progressPercent}%` }"
        >
          <div v-if="task?.status === 'running'" class="absolute inset-0 overflow-hidden">
            <div class="shimmer-bar" />
          </div>
        </div>
      </div>
      <!-- R118: 等待倒计时 —— task_wait 帧驱动, 等待过程每秒可见 -->
      <div v-if="waitState && task?.status === 'running'" class="mt-3 flex items-center gap-2 text-xs text-text-secondary">
        <span class="relative flex h-2 w-2 shrink-0">
          <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-amber opacity-60" />
          <span class="relative inline-flex rounded-full h-2 w-2 bg-amber" />
        </span>
        <span class="tabular-nums text-amber font-medium">{{ waitState.remaining }}s</span>
        <span>·</span>
        <span class="truncate">账号 {{ accountMap[waitState.account_id] || '...' }}</span>
        <span>等待后进入下一目标</span>
        <span class="truncate text-text-muted">→ {{ groupMap[normId(waitState.target_id)] || waitState.target_id }}<template v-if="waitState.total">（{{ waitState.next_index }}/{{ waitState.total }}）</template></span>
      </div>
    </div>

    <!-- 成员采集结果 -->
    <div v-if="task?.type === 'scrape_members'" class="glass rounded-[var(--t-r-md)] p-5 lift">
      <div class="flex items-center justify-between mb-3">
        <div class="flex items-center gap-2">
          <Users class="w-4 h-4 text-text-muted" />
          <h3 class="text-sm font-medium">采集到的成员</h3>
          <span class="text-xs text-text-secondary">({{ members.length }})</span>
        </div>
        <button @click="fetchMembers" :disabled="membersLoading" class="text-xs px-3 py-1 rounded-lg border border-border hover:border-cyan/40 transition-colors disabled:opacity-40">
          {{ membersLoading ? '加载中...' : '刷新' }}
        </button>
      </div>
      <div v-if="membersLoading" class="py-6 text-center text-sm text-text-muted">加载中...</div>
      <div v-else-if="membersError" class="py-6 text-center text-sm text-red">{{ membersError }}</div>
      <div v-else-if="!members.length" class="py-6 text-center text-sm text-text-muted">暂无成员数据</div>
      <div v-else class="space-y-1 max-h-80 overflow-y-auto">
        <div v-for="m in members" :key="m.member_id || m.tg_user_id" class="flex items-center gap-3 px-2 py-1.5 rounded hover:bg-bg-hover/50 text-sm">
          <span class="text-cyan font-mono text-xs w-32 shrink-0">{{ m.tg_user_id }}</span>
          <span class="truncate">{{ m.username || m.first_name || '未设置用户名' }}</span>
        </div>
      </div>
    </div>

    </div><!-- /左列 -->
    <div class="min-w-0">
    <!-- 实时日志 -->
    <div class="glass rounded-[var(--t-r-md)] p-5 lift">
      <div class="flex items-center justify-between mb-3">
        <div class="flex items-center gap-2">
          <Clock class="w-4 h-4 text-text-muted" />
          <h3 class="text-sm font-medium">实时日志</h3>
          <span class="text-xs text-text-secondary">({{ logs.length }})</span>
        </div>
        <div class="flex items-center gap-2">
          <span v-if="connState === 'connected'" class="flex items-center gap-1 text-xs text-emerald">
            <span class="w-1.5 h-1.5 rounded-full bg-emerald animate-pulse" />
            实时连接中
          </span>
          <span v-else-if="connState === 'connecting'" class="flex items-center gap-1 text-xs text-text-secondary">
            <span class="w-1.5 h-1.5 rounded-full bg-cyan animate-pulse" />
            连接中…
          </span>
          <span v-else class="text-xs text-text-secondary">已断开,3秒后重连</span>
          <!-- R128 语义修正: 仅清除前端显示, 不动服务端已保存的记录 -->
          <button @click="clearLogs" class="text-xs px-2.5 py-1 rounded-lg border border-border text-text-muted hover:text-red hover:border-red/40 transition-colors" title="仅清除当前显示">清除显示</button>
        </div>
      </div>
      <div ref="logContainer" class="max-h-[420px] overflow-y-auto space-y-1.5">
        <div
          v-for="(log, i) in displayLogs"
          :key="log.log_id || `${log.executed_at}-${i}`"
          class="flex items-start gap-2.5 py-1.5 px-2 rounded-lg text-xs"
          :class="logCardClass(log)"
        >
          <!-- 状态图标 -->
          <div class="shrink-0 mt-0.5">
            <CheckCircle2 v-if="log.status === 'success'" class="w-3.5 h-3.5 text-emerald" />
            <XCircle v-else-if="log.status === 'failed'" class="w-3.5 h-3.5 text-rose" />
            <AlertCircle v-else class="w-3.5 h-3.5 text-text-muted" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="text-text-muted/70 font-mono text-[10.5px] shrink-0">{{ formatTime(log.executed_at) }}</span>
              <!-- R87-M8: 操作标签弱化(去底色, 与内容拉开主次) -->
              <span class="text-[10.5px] font-medium text-text-muted/70 shrink-0 w-14">{{ getActionLabel(log.action) }}</span>
              <span class="flex items-center gap-1 text-text-primary truncate">
                <Send class="w-3 h-3 text-cyan shrink-0" />
                <span class="truncate">{{ log.target_name || groupMap[normId(log.target_id)] || log.target_id }}</span>
              </span>
            </div>
            <!-- message 渲染(2026-10-22): 成功行的"耗时 X.Xs"和限速行的"N 秒后进入下一目标"
                 写在 message 字段但此前模板没渲染, 用户看不到——统一展示 -->
            <div v-if="log.message || log._waitMsg" class="mt-1 text-text-muted text-xs inline-block">
              {{ log.message }}
              <span v-if="log._waitMsg" class="ml-1 text-text-muted/60">⏳ {{ log._waitMsg }}</span>
            </div>
            <div v-if="log.error_code" class="mt-1 text-rose text-xs bg-rose/5 rounded px-2 py-1 inline-block">
              {{ statusLabel(log.error_code) }}
            </div>
          </div>
        </div>
        <div v-if="!filteredLogs.length" class="text-text-muted text-center py-10 text-sm">
          <Clock class="w-8 h-8 mx-auto mb-2 opacity-30" />
          {{ logs.length ? '当前筛选条件下没有日志' : '暂无执行日志，任务运行后会在这里显示~' }}
        </div>
      </div>
    </div>
    </div><!-- /右列 -->
    </div><!-- /R106 两栏 -->
  </main>
</template>

<script setup lang="ts">
import { statusLabel } from '@/utils/labels'
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, CheckCircle2, XCircle, AlertCircle, Send, Clock, Users } from 'lucide-vue-next'
import { taskApi } from '@/api/task'
import { accountApi } from '@/api/account'
import { groupApi } from '@/api/group'
import { ask } from '@/utils/confirm'
import { useAccountStore } from '@/stores/account'
const pageError = ref('')
const pageLoading = ref(true)
const accountStore = useAccountStore()
import { formatDateTime, formatNumber, taskStatusClass, taskStatusLabel, taskTypeLabel } from '@/utils/format'
import { toast } from '@/utils/toast'
import { useAuthStore } from '@/stores/auth'
import Breadcrumb from '@/components/ui/Breadcrumb.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import { useTaskEvents } from '@/composables/useTaskEvents'
import type { Task, TaskLog } from '@/types'

const route = useRoute()
const router = useRouter()
const taskId = ((Array.isArray(route.params.id) ? route.params.id[0] : route.params.id) as string) || ''

function isValidUUID(id: string): boolean {
  const uuidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i
  return uuidRegex.test(id)
}

const actionLabels: Record<string, string> = {
  send_message: '发送消息',
  join_group: '加入群组',
  scrape_members: '采集成员',
  scrape_links: '采集链接',
  forward_message: '转发消息',
  reply_message: '回复消息',
  broadcast: '群发执行',
  scrape: '采集执行',
  scrape_private_link: '私密链接解析',
  scrape_public_group: '公开群解析',
  monitor_start: '监听启动',
  monitor_complete: '监听完成',
  // 群发执行细分(修复: 这些码缺失导致任务详情显示英文)
  // 2026-10-21: send_message 映射表上方已有('发送消息'), 此前重复声明导致构建失败;
  // 日志"全是等待发送"的真因是 target_name 列缺失+COPY 列数不匹配(见 r76 修复), 非映射表
  send_wait: '限速等待',
  send_success: '发送成功',
  send_failed: '发送失败',
  send_skip_duplicate: '跳过重复',
  send_skip_restricted: '跳过受限',
  wait: '等待',
  send: '发送',
}

function getActionLabel(action: string): string {
  return actionLabels[action] || action
}

const task = ref<Task | null>(null)
const members = ref<any[]>([])
const membersLoading = ref(false)
const membersError = ref('')

async function fetchMembers() {
  const t = task.value as any
  const groupId = t?.payload?.group_id || t?.target_ids?.[0]
  if (!groupId) { membersError.value = '任务缺少群组信息'; return }
  membersLoading.value = true
  membersError.value = ''
  try {
    const res: any = await groupApi.getMembers(groupId)
    members.value = res?.members || res?.data?.members || res?.list || res?.items || (Array.isArray(res) ? res : [])
  } catch (e: unknown) {
    membersError.value = e instanceof Error ? e.message : '加载成员失败'
  } finally {
    membersLoading.value = false
  }
}

const logs = ref<TaskLog[]>([])

// R87-M10: 100% 后状态收敛兜底 —— SSE 事件丢失时, 进度满后 3s 自动补拉任务状态,
// 避免"发完了还显示执行中"必须手动刷新
watch(() => [(task.value as any)?.progress?.done, (task.value as any)?.progress?.failed, (task.value as any)?.status], () => {
  const t = task.value as any
  if (!t || t.status !== 'running') return
  const processed = (t.progress?.done || 0) + (t.progress?.failed || 0)
  if (t.progress?.total && processed >= t.progress.total) {
    setTimeout(() => {
      const cur = task.value as any
      if (cur && cur.status === 'running') fetchTask()
    }, 3000)
  }
})

// R87-M8: 日志合并展示 —— "限速等待"行不再独立占行,
// 其"等待N秒后进入下一目标"信息并入上一条日志显示(时间-消息-群组-结果 的干净时间线)
const displayLogs = computed(() => {
  const out: (TaskLog & { _waitMsg?: string })[] = []
  for (const log of logs.value) {
    const isWait = log.action === '限速等待' || (log.message || '').includes('秒后进入下一目标')
    if (isWait) {
      if (out.length && log.message) out[out.length - 1] = { ...out[out.length - 1], _waitMsg: log.message }
      continue
    }
    out.push({ ...log })
  }
  return out
})

// 日志级别筛选: 全部/成功/失败/其他
const logFilter = ref<'all' | 'success' | 'failed' | 'other'>('all')
const logPills = [
  { value: 'all', label: '全部' },
  { value: 'success', label: '成功' },
  { value: 'failed', label: '失败' },
  { value: 'other', label: '其他' },
] as const
const filteredLogs = computed(() => {
  if (logFilter.value === 'all') return logs.value
  return logs.value.filter((log: any) => {
    // 与 logCardClass 保持同一套状态归一化(任务完成时的 wait 视为成功)
    const s = (task.value?.status === 'completed' && (log.status === 'wait' || log.status === 'pending')) ? 'success' : log.status
    if (logFilter.value === 'success') return s === 'success'
    if (logFilter.value === 'failed') return s === 'failed'
    return s !== 'success' && s !== 'failed'
  })
})

// R118: 等待倒计时态 —— task_wait 帧(每秒)更新, 不刷日志列表
// {account_id, target_id, remaining, next_index, total}
const waitState = ref<{ account_id: string; target_id: string; remaining: number; next_index: number; total: number } | null>(null)

// SSE 实时: 本任务的日志/进度/状态亚秒级推送, 打开详情页即可盯着日志滚动
useTaskEvents((evt: any) => {
  if (evt.task_id !== taskId) return
  if (evt.type === 'task_wait') {
    waitState.value = {
      account_id: String(evt.account_id || ''),
      target_id: String(evt.target_id || ''),
      remaining: Number(evt.remaining || 0),
      next_index: Number(evt.next_index || 0),
      total: Number(evt.total || 0),
    }
    return
  }
  if (evt.type === 'task_log' && evt.log) {
    const l = evt.log
    if (!logs.value.some((x: any) => x.log_id === l.log_id)) {
      logs.value = [...logs.value, l]
      requestAnimationFrame(() => {
        if (logContainer.value) logContainer.value.scrollTop = logContainer.value.scrollHeight
      })
    }
    return
  }
  if (evt.progress && task.value) {
    task.value = { ...task.value, progress: evt.progress }
    return
  }
  if (evt.type === 'task_status') {
    fetchTask()
  }
})

async function clearLogs() {
  try {
    await taskApi.deleteTaskLogs((task.value as any).task_id)
    logs.value = []
    toast({ type: 'success', title: '日志已清空' })
  } catch { toast({ type: 'error', title: '清空失败' }) }
}
// 2026-09-23 修复: 初始态是"连接中"而非"已断开"——换ticket+建连要1-2秒,
// 原实现初始 false 直接显示"已断开", 用户每次打开详情都看到一次假断线
const connState = ref<'connecting' | 'connected' | 'disconnected'>('connecting')
const isConnected = computed(() => connState.value === 'connected')
const logContainer = ref<HTMLDivElement>()
const accountMap = ref<Record<string, string>>({})
const groupMap = ref<Record<string, string>>({})

// 规范化群ID: 去掉 -100 前缀, 兼容 groups.tg_chat_id 与日志 target_id 的两种存法
const normId = (id: any): string => String(id ?? '').replace(/^-100/, '')
let eventSource: EventSource | null = null
let refreshTimer: ReturnType<typeof setInterval>
let onVisCleanup: (() => void) | null = null
// R87-M9: SSE 看门狗 —— 某些代理/网络下连接"假活"(不断开也不推数据),
// onerror 永不触发导致日志假死。30秒无任何消息则判定假活, 主动断开重连。
let sseWatchdog: ReturnType<typeof setTimeout> | null = null
function resetSseWatchdog() {
  if (sseWatchdog) clearTimeout(sseWatchdog)
  sseWatchdog = setTimeout(() => {
    console.warn('[task-detail] SSE 45s 无消息, 判定假活, 强制重连')
    try { eventSource?.close() } catch { /* 忽略 */ }
    eventSource = null
    connectSSE()
  }, 45000)
}

const // 2026-10-21 修正: 进度按"已处理"(成功+失败)计算——失败也是处理结果, 不应让进度条永远卡住
progressPercent = computed(() => {
  if (!task.value?.progress.total) return 0
  return Math.round(((task.value.progress.done + (task.value.progress.failed || 0)) / task.value.progress.total) * 100)
})

const progressColor = computed(() => {
  if (!task.value) return 'bg-cyan'
  if (task.value.status === 'failed') return 'bg-rose'
  if (task.value.status === 'completed') return 'bg-emerald'
  return 'bg-cyan'
})

function logCardClass(log: TaskLog) {
  // 如果任务已完成，wait/pending 的日志视为成功
  const effectiveStatus = (task.value?.status === 'completed' && (log.status === 'wait' || log.status === 'pending'))
    ? 'success' : log.status
  if (effectiveStatus === 'success') return 'bg-emerald/5 hover:bg-emerald/10 border border-emerald/10'
  if (effectiveStatus === 'failed') return 'bg-rose/5 hover:bg-rose/10 border border-rose/10'
  return 'bg-ink-01 hover:bg-bg-card border border-transparent'
}

function formatTime(date: string | undefined | null) {
  if (!date) return '--:--:--'
  const d = new Date(date)
  if (isNaN(d.getTime())) return '--:--:--'
  return new Date(date).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

async function fetchTask() {
  pageLoading.value = true
  pageError.value = ''
  try {
    const res = await taskApi.getById(taskId)
    // 兼容: 直接对象 / {data: {...}}
    if (res && typeof res === 'object') {
      task.value = (res as any).data || res
    } else {
      task.value = res as Task
    }
  } catch (e: unknown) {
    pageError.value = e instanceof Error ? e.message : '请稍后重试'
    toast({ type: 'error', title: '获取任务失败', message: pageError.value })
  } finally {
    pageLoading.value = false
  }
}

async function fetchLogs() {
  try {
    const res = await taskApi.getLogs(taskId)
    // 兼容: 直接数组 / {list: [...]} / {data: [...]}
    let data: TaskLog[] = []
    if (Array.isArray(res)) {
      data = res
    } else if (res && typeof res === 'object') {
      data = (res as any).list || (res as any).data || []
    }
    logs.value = data
    await nextTick()
    if (logContainer.value) {
      logContainer.value.scrollTop = logContainer.value.scrollHeight
    }
  } catch (e: unknown) {
    toast({ type: 'error', title: '获取日志失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

async function connectSSE() {
  const authStore = useAuthStore(); const token = authStore.token
  // 审计P0-03: 短期ticket替代URL里的JWT
  let ticket = ''
  try {
    // F-20: 使用内存中的 token（authStore 已在函数头部获取）
    // P2-04: 同 Layout.vue——硬编码 /api/v1 改为可配置 base
    const apiBase = import.meta.env.VITE_API_BASE_URL || '/api/v1'
    const tres = await fetch(`${apiBase}/events/ticket`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } })
    ticket = (await tres.json()).ticket || ''
  } catch { /* 静默 */ }
  const apiBase2 = import.meta.env.VITE_API_BASE_URL || '/api/v1'
  eventSource = new EventSource(`${apiBase2}/tasks/${taskId}/progress/stream?ticket=${encodeURIComponent(ticket)}`)
  eventSource.onopen = () => { connState.value = 'connected'; resetSseWatchdog() }
  eventSource.onmessage = (e) => {
    try {
      resetSseWatchdog() // R87-M9: 收到任何消息都重置看门狗
      const data = JSON.parse(e.data)
      if (!data || !task.value) return

      // Handle real-time task log
      // 精读修复: useTaskEvents 全局通道也会推同一条日志, 去重防双份
      if (data.type === 'task_log' && data.log) {
        if (!logs.value.some((x: any) => x.log_id === data.log.log_id)) {
          logs.value.push(data.log)
        }
        // Keep max 500 logs in memory to prevent UI lag
        if (logs.value.length > 500) {
          logs.value = logs.value.slice(-500)
        }
        return
      }

      // 2026-09-23 修复: 任务状态变更(完成/失败/取消)时整任务刷新,
      // 否则详情页永远停在"执行中"(原实现只处理日志和进度, 漏了状态事件)
      if (data.type === 'task_status') {
        fetchTask()
        return
      }

      // Handle task progress update
      const tp = task.value.progress || {}
      task.value.progress = {
        total: data.total ?? data.progress?.total ?? tp.total ?? 0,
        done: data.done ?? data.progress?.done ?? tp.done ?? 0,
        failed: data.failed ?? data.progress?.failed ?? tp.failed ?? 0,
        current_target: data.current_target ?? data.progress?.current_target ?? tp.current_target ?? '',
        account_progress: data.account_progress ?? data.progress?.account_progress ?? tp.account_progress ?? []
      }
    } catch (err) { /* ignore malformed SSE data */ }
  }
  eventSource.onerror = () => {
    connState.value = 'disconnected'
    eventSource?.close()
    // Auto-reconnect after 3 seconds
    setTimeout(() => {
      if (connState.value !== 'connected') connectSSE()
    }, 3000)
  }
}

async function pause() {
  try { await taskApi.pause(taskId); await fetchTask(); toast({ type: 'success', title: '任务已暂停' }) }
  catch (e: unknown) { toast({ type: 'error', title: '暂停失败', message: (e instanceof Error ? e.message : '请稍后重试') }) }
}
async function resume() {
  try { await taskApi.resume(taskId); await fetchTask(); toast({ type: 'success', title: '任务已恢复' }) }
  catch (e: unknown) { toast({ type: 'error', title: '恢复失败', message: (e instanceof Error ? e.message : '请稍后重试') }) }
}
async function cancel() {
  try { await taskApi.cancel(taskId); await fetchTask(); toast({ type: 'success', title: '任务已取消' }) }
  catch (e: unknown) { toast({ type: 'error', title: '取消失败', message: (e instanceof Error ? e.message : '请稍后重试') }) }
}

// 重试失败目标: 后端 scope=failed 克隆新任务(仅含失败目标, 续传防重保证不重复)
async function retryFailed() {
  if (!task.value) return
  const failed = task.value.progress?.failed || 0
  const ok = await ask(`仅对 ${failed} 个失败目标重新发送？将创建新任务`, { title: '重试失败目标' })
  if (!ok) return
  try {
    await taskApi.retry(taskId, 'failed')
    toast({ type: 'success', title: '已创建重试任务', message: '可在任务列表查看进度' })
    router.push(`/workspace/${route.params.accountId}/tasks`)
  } catch (e: unknown) {
    toast({ type: 'error', title: '重试失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

watch(() => logs.value.length, async () => {
  await nextTick()
  if (logContainer.value) logContainer.value.scrollTop = logContainer.value.scrollHeight
})

onMounted(async () => {
  if (!isValidUUID(taskId)) {
    router.push(`/workspace/${route.params.accountId}/tasks`)
    return
  }
  await fetchTask()
  await fetchLogs()
  try {
    const accs = await accountApi.list()
    const accData = (accs as any).data || accs || []
    accountMap.value = Object.fromEntries((accData).map((a: any) => [a.account_id, a.remark || a.phone]))
  } catch (e: unknown) { /* ignore */ }
  try {
    // R87-M10: 不再按"当前账号"过滤 —— 任务可能跨账号执行, 按账号过滤会漏群导致日志显示原始ID。
    // 群库是租户级数据, 全量拉取建立 群ID/群TG_ID → 群名 映射。
    const grps = await groupApi.list()
    // R86 热修: groupApi.list 返回 {list:[...], count} 结构, 原 (grps).data||grps 拿到对象,
    // forEach 抛异常被静默吞掉 → groupMap 为空 → 日志全显示原始ID
    const raw: any = grps
    const grpData = Array.isArray(raw) ? raw
      : Array.isArray(raw?.data) ? raw.data
      : Array.isArray(raw?.data?.list) ? raw.data.list
      : Array.isArray(raw?.list) ? raw.list : []
    // 同时用 group_id 和 tg_chat_id 做 key，确保能匹配日志中的 target_id
    const entries: [string, string][] = []
    grpData.forEach((g: any) => {
      const name = g.title || g.name || g.username || g.group_id
      entries.push([normId(g.group_id), name])
      if (g.tg_chat_id) entries.push([normId(g.tg_chat_id), name])
      if (g.username) entries.push([g.username, name])
    })
    groupMap.value = Object.fromEntries(entries)
  } catch (e: unknown) { /* ignore */ }
  if (task.value && (task.value as any).type === 'scrape_members') fetchMembers()
  connectSSE()
  // R118: 回前台立即补拉 —— 手机浏览器后台冻结 EventSource,
  // 切回前台时立刻拉全量日志追平, 不用等重连周期
  const onVis = () => {
    if (document.visibilityState === 'visible') {
      fetchLogs()
      if (connState.value !== 'connected') connectSSE()
    }
  }
  document.addEventListener('visibilitychange', onVis)
  onVisCleanup = () => document.removeEventListener('visibilitychange', onVis)
  // 精读修复: 断线轮询退避 1s→5s, 避免断线期间高频打接口
  refreshTimer = setInterval(() => {
    if (!isConnected.value) {
      fetchTask()
      fetchLogs()
    }
  }, 5000)
})

onUnmounted(() => {
  eventSource?.close()
  clearInterval(refreshTimer)
  if (onVisCleanup) onVisCleanup()
})
</script>

<style scoped>
.shimmer-bar {
  width: 100%;
  height: 100%;
  background: linear-gradient(90deg, transparent, rgba(255,255,255,0.3), transparent);
  animation: shimmer-slide 1.5s infinite;
}
@keyframes shimmer-slide {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(100%); }
}
</style>
