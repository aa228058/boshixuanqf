<template>
  <main class="space-y-6">
    <Breadcrumb :items="[{ path: '/admin/dashboard', label: '仪表盘' }, { path: '', label: '系统监控' }]" />
    <!-- 资源监控 -->
    <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
      <div class="glass rounded-[var(--t-r-md)] p-5 lift">
        <div class="flex items-center justify-between mb-4">
          <span class="text-sm text-text-secondary">CPU 使用率</span>
          <Cpu class="w-5 h-5 text-cyan" />
        </div>
        <p class="text-3xl tracking-tight font-bold font-mono">{{ (metrics.cpu || 0).toFixed(1) }}%</p>
        <div class="w-full h-2 bg-bg-card rounded-full mt-3 overflow-hidden">
          <div class="h-full bg-gradient-to-r rounded-full glow-progress transition-[width,background-color] duration-500" :class="usageColor(metrics.cpu)" :style="{ width: `${Math.min(metrics.cpu, 100)}%` }" />
        </div>
      </div>
      <div class="glass rounded-[var(--t-r-md)] p-5 lift">
        <div class="flex items-center justify-between mb-4">
          <span class="text-sm text-text-secondary">内存使用</span>
          <MemoryStick class="w-5 h-5 text-cyan" />
        </div>
        <p class="text-3xl tracking-tight font-bold font-mono">{{ (metrics.memory || 0).toFixed(1) }}%</p>
        <p class="text-xs text-text-secondary mt-1">{{ (metrics.memoryUsed || 0).toFixed(1) }} / {{ (metrics.memoryTotal || 0).toFixed(1) }} GB</p>
        <div class="w-full h-2 bg-bg-card rounded-full mt-2 overflow-hidden">
          <div class="h-full rounded-full transition-[width,background-color] duration-500" :class="usageBarColor(metrics.memory)" :style="{ width: `${Math.min(metrics.memory, 100)}%` }" />
        </div>
      </div>
      <div class="glass rounded-[var(--t-r-md)] p-5 lift">
        <div class="flex items-center justify-between mb-4">
          <span class="text-sm text-text-secondary">磁盘使用</span>
          <HardDrive class="w-5 h-5 text-amber" />
        </div>
        <p class="text-3xl tracking-tight font-bold font-mono">{{ (metrics.disk || 0).toFixed(1) }}%</p>
        <!-- P1-36: diskTotal 为 0(未加载或后端未给)时显示占位, 不造假容量 -->
        <p class="text-xs text-text-secondary mt-1">{{ metrics.diskTotal > 0 ? (metrics.diskUsed || 0).toFixed(1) + ' / ' + metrics.diskTotal.toFixed(1) + ' GB' : '—' }}</p>
        <t-progress :percentage="Math.round((metrics.disk || 0) * 10) / 10"  class="mt-2" />
      </div>
    </div>

    <!-- 在线连接 + 队列 -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <div class="glass rounded-[var(--t-r-md)] p-5 lift">
        <h3 class="text-sm font-medium text-text-secondary mb-4">在线 TG 连接</h3>
        <div class="flex items-center gap-4">
          <div class="w-20 h-20 rounded-full border-4 border-cyan/20 flex items-center justify-center">
            <span class="text-2xl tracking-tight font-bold font-mono text-cyan">{{ metrics.onlineConnections }}</span>
          </div>
          <div class="space-y-1">
            <p class="text-sm max-w-[65ch]">在线账号</p>
            <p class="text-xs text-text-secondary">总账号: {{ metrics.totalAccounts }}</p>
            <p class="text-xs text-text-secondary">在线率: {{ (metrics.onlineRate || 0).toFixed(1) }}%</p>
          </div>
        </div>
      </div>
      <div class="glass rounded-[var(--t-r-md)] p-5 lift">
        <h3 class="text-sm font-medium text-text-secondary mb-4">任务队列</h3>
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <span class="text-sm">排队中</span>
            <span class="font-mono text-amber">{{ metrics.queuedTasks }}</span>
          </div>
          <div class="flex items-center justify-between">
            <span class="text-sm">执行中</span>
            <span class="font-mono text-cyan">{{ metrics.runningTasks }}</span>
          </div>
          <div class="flex items-center justify-between">
            <span class="text-sm">今日完成</span>
            <span class="font-mono text-emerald">{{ metrics.completedToday }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 实时日志 -->
    <div class="glass rounded-[var(--t-r-md)] p-5 lift">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-sm font-medium text-text-secondary">系统日志</h3>
        <span class="flex items-center gap-1 text-xs text-emerald">
          <span class="w-1.5 h-1.5 rounded-full bg-emerald animate-pulse" />
          实时
        </span>
      </div>
      <div class="max-h-[300px] overflow-y-auto space-y-1 font-mono text-xs bg-bg-card rounded-lg p-4">
        <div v-for="(log, i) in systemLogs" :key="`${log.time}-${i}`" class="flex items-start gap-2 py-0.5">
          <span class="text-text-muted shrink-0">{{ log.time }}</span>
          <span class="shrink-0" :class="logLevelClass(log.level)">[{{ log.level }}]</span>
          <span class="text-text-secondary">{{ log.message }}</span>
        </div>
      </div>
    </div>
  </main>
</template>

<script setup lang="ts">
import Breadcrumb from '@/components/ui/Breadcrumb.vue'
import { ref, onMounted, onUnmounted } from 'vue'
import { Cpu, MemoryStick, HardDrive } from 'lucide-vue-next'
import { adminApiMod } from '@/api/admin'

const metrics = ref({
  cpu: 0, memory: 0, memoryUsed: 0, memoryTotal: 4,
  // P1-36: 原 diskTotal 硬编码 100——首屏闪"0.0 / 100.0 GB"假容量, 后端字段
  // 缺失/请求失败时永远显示假容量。初值改 0, 模板按 0 显示占位符, 真值到达后覆盖。
  disk: 0, diskUsed: 0, diskTotal: 0,
  onlineConnections: 0, totalAccounts: 0, onlineRate: 0,
  queuedTasks: 0, runningTasks: 0, completedToday: 0,
})

const systemLogs = ref<Array<{ time: string; level: string; message: string }>>([])
let refreshTimer: ReturnType<typeof setInterval>

// 平滑采样：内存历史值（5次采样）
const memoryHistory = ref<number[]>([])
const alertCooldown = ref(false)
const ALERT_COOLDOWN_MS = 60000 // 警报冷却1分钟

function logLevelClass(level: string) {
  const map: Record<string, string> = { INFO: 'text-cyan', WARN: 'text-amber', ERROR: 'text-rose', DEBUG: 'text-text-muted' }
  return map[level] || 'text-text-secondary'
}

function pushLog(level: string, message: string) {
  systemLogs.value.unshift({
    time: new Date().toLocaleTimeString('zh-CN'),
    level,
    message,
  })
  if (systemLogs.value.length > 50) systemLogs.value.pop()
}

function checkAlert(key: string, value: number, threshold: number) {
  if (alertCooldown.value) return
  if (key === 'memory') {
    memoryHistory.value.push(value)
    if (memoryHistory.value.length > 5) memoryHistory.value.shift()
    // 5次采样中至少3次超过阈值才报警
    const exceedCount = memoryHistory.value.filter(v => v > threshold).length
    if (exceedCount >= 3) {
      pushLog('WARN', `内存使用率连续异常: ${value.toFixed(1)}%`)
      triggerCooldown()
    }
  } else if (value > threshold) {
    pushLog('WARN', `${key.toUpperCase()} 使用率过高: ${value.toFixed(1)}%`)
    triggerCooldown()
  }
}

function triggerCooldown() {
  alertCooldown.value = true
  setTimeout(() => { alertCooldown.value = false }, ALERT_COOLDOWN_MS)
}

// 动态颜色：根据使用率返回对应颜色类
function usageColor(percent: number): string {
  if (percent >= 90) return 'from-rose to-rose'
  if (percent >= 75) return 'from-amber to-amber'
  return 'from-cyan to-cyan-bright'
}

function usageBarColor(percent: number): string {
  if (percent >= 90) return 'bg-rose'
  if (percent >= 75) return 'bg-amber'
  return 'bg-cyan'
}

async function fetchMetrics() {
  try {
    const data = await adminApiMod.getSystemMetrics()
    metrics.value = { ...metrics.value, ...data }
    metrics.value.totalAccounts = (data as any).total_accounts ?? 0
    metrics.value.onlineRate = (data as any).online_rate ?? 0
    // 平滑采样警报检查
    checkAlert('memory', metrics.value.memory, 85)
    checkAlert('cpu', metrics.value.cpu, 90)
    checkAlert('disk', metrics.value.disk, 90)
    // P1-39: 原 systemLogs 只在超阈值警报时写入——"系统日志"面板永远空。
    // 每次成功拉取记一条 DEBUG(50 条环形缓冲, 不堆积)。
    pushLog('DEBUG', `指标已更新 CPU ${metrics.value.cpu}% 内存 ${metrics.value.memory}%`)
  } catch (e: unknown) {
    pushLog('ERROR', `指标拉取失败: ${(e as Error)?.message || e}`)
  }
}

// 审计UI-P1-04: visibility感知轮询(页面隐藏暂停, 可见立即刷新)
// 修复：原代码定义了 startAutoRefresh 却忘记调用，监控页只加载一次就永远静止
const tickHandler = () => { if (!document.hidden) fetchMetrics() }

function startAutoRefresh() {
  refreshTimer = setInterval(tickHandler, 5000)
  document.addEventListener('visibilitychange', tickHandler)
}

onMounted(() => {
  fetchMetrics()
  startAutoRefresh()
})

onUnmounted(() => {
  clearInterval(refreshTimer)
  document.removeEventListener('visibilitychange', tickHandler)
})
</script>
