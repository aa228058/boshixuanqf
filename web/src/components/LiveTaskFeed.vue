<template>
  <!-- 活动任务: 实时进度面板(只显示当前任务进度, 不展示日志流水) -->
  <div v-if="isActive" class="glass rounded-[var(--t-r-md)] p-4 border border-cyan/20">
    <div class="flex items-center justify-between mb-3">
      <div class="flex items-center gap-2">
        <span class="relative flex h-2.5 w-2.5">
          <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-cyan opacity-60" />
          <span class="relative inline-flex rounded-full h-2.5 w-2.5 bg-cyan" />
        </span>
        <span class="text-sm font-medium">{{ typeLabel }} · {{ statusText }}</span>
      </div>
      <span class="text-xs text-text-muted">
        <template v-if="task.status === 'queued'">已等待 {{ elapsed(task.created_at) }}</template>
        <template v-else>已运行 {{ elapsed(task.started_at || task.created_at) }}</template>
      </span>
    </div>

    <!-- 大进度条 -->
    <div class="mb-2">
      <div class="flex items-center justify-between text-sm mb-1.5">
        <span class="text-text-secondary">{{ prog.done }} / {{ prog.total }}</span>
        <span class="font-semibold text-cyan">{{ percent }}%</span>
      </div>
      <div class="h-2.5 rounded-full bg-bg-hover overflow-hidden">
        <div class="h-full rounded-full transition-all duration-500 bg-cyan" :style="{ width: percent + '%' }" />
      </div>
    </div>

    <!-- 当前动作: 单行实时步骤, 不再刷日志列表 -->
    <div class="flex items-center gap-2 text-xs text-text-secondary min-h-5">
      <Loader2 class="w-3.5 h-3.5 shrink-0 animate-spin text-cyan" />
      <span class="truncate">{{ currentAction }}</span>
      <span v-if="waitRemain > 0" class="ml-2 px-2 py-0.5 rounded-full bg-amber/15 text-amber text-xs shrink-0">
        下次发送 {{ waitRemain }}s
      </span>
    </div>

    <div class="flex justify-end mt-2">
      <button @click="$router.push(`/workspace/${accountId}/tasks/${task.task_id}`)"
              class="text-xs px-3 py-1.5 rounded-lg border border-border text-text-secondary hover:border-cyan/40 hover:text-cyan transition-colors">
        查看详情
      </button>
    </div>
  </div>

  <!-- 无活动任务: 一行极简总结(最近一个同类型任务的最终结果), 不占地方 -->
  <div v-else-if="lastDone" class="flex items-center justify-between rounded-[var(--t-r-md)] px-4 py-2.5 border border-border/60 bg-ink-01">
    <div class="flex items-center gap-2 text-xs text-text-muted">
      <CheckCircle2 v-if="lastDone.status === 'completed'" class="w-3.5 h-3.5 text-emerald" />
      <XCircle v-else class="w-3.5 h-3.5 text-rose" />
      <span>上次{{ typeLabel }}已{{ lastDone.status === 'completed' ? '完成' : '结束' }} · {{ lastDoneProg.done }} / {{ lastDoneProg.total }}</span>
    </div>
    <button @click="$router.push(`/workspace/${accountId}/tasks/${lastDone.task_id}`)"
            class="text-xs text-text-muted hover:text-cyan transition-colors shrink-0">
      查看详情
    </button>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { CheckCircle2, XCircle, Loader2 } from 'lucide-vue-next'
import { taskApi } from '@/api/task'
import { useTaskEvents } from '@/composables/useTaskEvents'
import confetti from 'canvas-confetti'

const props = defineProps<{ accountId: string; taskType: string }>()
const task = ref<any>(null)
const lastDone = ref<any>(null)
let timer: ReturnType<typeof setInterval> | null = null

const isActive = computed(() => ['queued', 'running', 'pending'].includes(task.value?.status))

// 后端 progress 可能是 JSON 字符串或对象, 统一解析
function parseProgress(p: any): { total: number; done: number; failed: number } {
  if (!p) return { total: 0, done: 0, failed: 0 }
  if (typeof p === 'string') {
    try { return { total: 0, done: 0, failed: 0, ...JSON.parse(p) } } catch { return { total: 0, done: 0, failed: 0 } }
  }
  return p
}
const prog = computed(() => parseProgress(task.value?.progress))
const lastDoneProg = computed(() => parseProgress(lastDone.value?.progress))

const percent = computed(() => {
  const total = prog.value.total
  if (!total) return 0
  return Math.min(100, Math.round((prog.value.done / total) * 100))
})

const typeNames: Record<string, string> = {
  broadcast: '群发任务', join_groups: '加群任务', scrape_members: '成员采集',
  scrape_links: '链接采集', scheduled_broadcast: '定时广播',
}
const typeLabel = computed(() => typeNames[props.taskType] || '任务')

const statusMap: Record<string, string> = {
  queued: '排队中', running: '进行中', pending: '等待调度',
}
const statusText = computed(() => statusMap[task.value?.status] || '')

// 实时日志流: 保留最近 6 条, 新日志在前
const liveLogs = ref<any[]>([])
let celebrated = false
// P2-52: 原 celebrated 是模块级全局标志——任务 A 完成后任务 B 不再撒花。
// 本组件是单任务视图, 切任务(或同 ID 重载)即重置, 每个任务都能撒。
watch(() => (task.value as any)?.task_id, () => { celebrated = false })

const actionLabels: Record<string, string> = {
  send_message: '发送消息', join_group: '加入群聊', scrape_members: '采集成员',
}
function actionLabel(a: string) { return actionLabels[a] || a }

// 模板 32 行展示的"当前动作"文本（此前引用了不存在的 currentAction——
// 这类未定义引用被运行时 catch 吞掉，模板一直显示空）。
const taskTypeLabels: Record<string, string> = {
  broadcast: '群发消息中',
  scrape_members: '采集成员中',
  scrape_links: '监听链接中',
  join_groups: '加入群聊中',
  monitor_scrape: '监控采集中',
  scheduled_broadcast: '定时群发中',
  invite_members: '邀请成员中',
}
const currentAction = computed(() => {
  if (!task.value) return ''
  const t = task.value as any
  return taskTypeLabels[t.type] || t.type || ''
})

function logText(l: any): string {
  if (l.message) return l.message
  const target = l.target_id || l.target_name || l.targetName || ''
  let text = actionLabel(l.action)
  if (target) text += ` → ${target}`
  if (l.error_code) text += ` (${l.error_code})`
  return text
}

function levelColor(status: string): string {
  if (status === 'success' || status === 'completed') return 'bg-emerald'
  if (status === 'failed' || status === 'error') return 'bg-rose'
  if (status === 'warn' || status === 'pending_approval') return 'bg-amber'
  return 'bg-cyan'
}

function formatTime(iso: string): string {
  if (!iso) return '--:--:--'
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function elapsed(iso: string): string {
  if (!iso) return '0 秒'
  const s = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 60) return `${s} 秒`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} 分 ${s % 60} 秒`
  return `${Math.floor(m / 60)} 时 ${m % 60} 分`
}

async function load() {
  if (!props.accountId || !props.taskType) return
  try {
    const res: any = await taskApi.list({ account_id: props.accountId, type: props.taskType, page: 1, page_size: 5 })
    const arr = res.list || res.items || []
    const active = arr.find((t: any) => ['queued', 'running', 'pending'].includes(t.status))
    if (active) {
      // 切换到新任务时清空当前动作
      if (task.value?.task_id !== active.task_id) liveLogs.value = []
      task.value = active
      lastDone.value = null
      // 运行中才拉取最新一条日志作为当前动作
      const lr: any = await taskApi.getLogs(active.task_id)
      const logs = lr?.list || lr?.data || (Array.isArray(lr) ? lr : [])
      if (logs.length) liveLogs.value = logs.slice(-6).reverse()
    } else {
      task.value = null
      liveLogs.value = []
      // 记录最近一个已结束任务, 用于一行总结
      const done = arr.find((t: any) => ['completed', 'failed', 'cancelled'].includes(t.status))
      if (lastDone.value?.task_id !== done?.task_id) lastDone.value = done || null
    }
  } catch { /* 静默, 下轮再试 */ }
}

// SSE 实时事件: 进度/日志亚秒级推送, 轮询降为兜底(10 秒)
useTaskEvents((evt: any) => {
  if (!task.value) return
  if (evt.task_id && evt.task_id !== task.value.task_id) return
  // 实时日志
  if (evt.type === 'task_log' && evt.log) {
    const l = evt.log
    if (!liveLogs.value.some((x: any) => x.log_id === l.log_id)) {
      liveLogs.value = [l, ...liveLogs.value].slice(0, 6)
    }
    return
  }
  // 实时进度: 后端每处理一个目标推送一次
  if (evt.progress) {
    task.value = { ...task.value, progress: evt.progress }
    if (evt.progress.done + (evt.progress.failed || 0) >= (evt.progress.total || 0) && evt.progress.total > 0) {
      setTimeout(load, 800) // 接近完成时主动拉一次终态
      // 全部目标完成: 撒花庆祝(每个任务只放一次)
      if (!celebrated) {
        celebrated = true
        confetti({ particleCount: 100, spread: 80, origin: { y: 0.3 }, zIndex: 3000 })
      }
    }
  }
})

onMounted(() => { load(); timer = setInterval(load, 10000) })
// 下次发送倒计时: 最新一条日志是"等待 N 秒..."时, 解析秒数实时倒数
const waitRemain = ref(0)
let waitTimer: ReturnType<typeof setInterval> | null = null
watch(liveLogs, (logs: any[]) => {
  const l = logs?.[0]
  const m = l && /等待\s*(\d+)\s*秒/.exec(l.message || '')
  if (m) {
    waitRemain.value = parseInt(m[1], 10)
    if (waitTimer) clearInterval(waitTimer)
    waitTimer = setInterval(() => {
      if (waitRemain.value > 0) waitRemain.value--
      else if (waitTimer) { clearInterval(waitTimer); waitTimer = null }
    }, 1000)
  } else if (waitTimer) {
    clearInterval(waitTimer); waitTimer = null; waitRemain.value = 0
  }
})

onUnmounted(() => { if (timer) clearInterval(timer); if (waitTimer) clearInterval(waitTimer) })
watch(() => [props.accountId, props.taskType], load)
</script>
