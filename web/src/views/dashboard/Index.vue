<template>
  <main class="space-y-6">
    <!-- R125 重设计(示例3 hero 语言): 渐变欢迎卡 + CTA -->
    <section class="rounded-[24px] p-6 sm:p-7 relative overflow-hidden reveal"
             style="background: linear-gradient(135deg, var(--t-brand), color-mix(in srgb, var(--t-brand) 62%, #8f7bff)); color: #fff; box-shadow: 0 14px 40px color-mix(in srgb, var(--t-brand) 30%, transparent);">
      <div class="absolute w-[170px] h-[170px] rounded-full pointer-events-none"
           style="border: 1px solid rgba(255,255,255,0.25); right: -50px; top: -50px; box-shadow: 0 0 0 24px rgba(255,255,255,0.06), 0 0 0 48px rgba(255,255,255,0.03);"></div>
      <div class="relative">
        <div class="text-[10.5px] font-extrabold tracking-[0.13em] uppercase" style="color: rgba(255,255,255,0.75);">{{ greeting }} · 运营概览</div>
        <h2 class="mt-1.5 text-display sm:text-display font-black leading-tight tracking-tight">{{ greeting }}，今天也顺利</h2>
        <p class="mt-1.5 text-caption max-w-[420px]" style="color: rgba(255,255,255,0.85);">账号、任务与监控都在正常运转。点下方按钮创建今天的群发任务。</p>
        <router-link :to="`/workspace/${accId}/broadcast`"
                     class="inline-flex items-center gap-1.5 mt-4 px-4 py-2.5 rounded-[var(--t-r-md)] text-small font-extrabold active:scale-[0.97] transition-transform"
                     style="background: #fff; color: var(--t-brand); box-shadow: 0 6px 18px rgba(0,0,0,0.15);">
          ＋ 创建群发任务 →
        </router-link>
      </div>
    </section>

    <!-- 统计卡片 -->
    <section class="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 gap-4 reveal">
      <StatCard v-for="(stat, i) in stats" :key="stat.label" v-bind="stat"
                class="stat-card-enter" :style="{ animationDelay: (80 + i * 90) + 'ms' }" />
    </section>

    <!-- R126: 快速开始已按"首页只显示仪表盘"要求撤除 —— 功能入口统一在底部 Tab 的"更多"页 -->

    <!-- 发送日历: 每日成功发送量月历热力图 -->
    <section class="reveal reveal-delay-1">
      <SendCalendar />
    </section>

    <!-- 图表区 -->
    <section class="grid grid-cols-1 lg:grid-cols-3 gap-4 reveal reveal-delay-1">
      <!-- R98 仪表盘双栏: 趋势+分布并排(桌面端), 高度占用减半 -->
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <!-- 任务趋势 -->
      <article class="glass rounded-[var(--t-r-lg)] p-5 md:p-6 min-h-[320px] lift">
        <h3 class="text-body font-semibold text-text-primary mb-4">近 7 天任务趋势</h3>
        <div v-if="loading" class="h-56 flex items-center justify-center">
          <span class="w-5 h-5 rounded-full border-2 animate-spin inline-block" style="border-color: var(--t-brand); border-top-color: transparent;"></span>
        </div>
        <div v-else-if="!hasTaskData" class="h-56 flex flex-col items-center justify-center text-text-muted text-sm">
          <!-- Lottie 品牌空状态: 动态插画替代静态图标 -->
          <LottieAnim src="@/assets/lottie/empty-tasks.json" width="150px" height="150px" />
          <p class="mt-1">暂无任务数据，去创建一个新任务吧~</p>
        </div>
        <v-chart v-else class="h-56" :option="taskChartOption" autoresize />
      </article>

      <!-- 账号状态分布 -->
      <article class="glass rounded-[var(--t-r-lg)] p-5 md:p-6 min-h-[320px] lift">
        <h3 class="text-body font-semibold text-text-primary mb-4">账号状态分布</h3>
        <div v-if="loading" class="h-56 flex items-center justify-center">
          <span class="w-5 h-5 rounded-full border-2 animate-spin inline-block" style="border-color: var(--t-brand); border-top-color: transparent;"></span>
        </div>
        <div v-else-if="!hasAccountData" class="empty-pretty h-56">
          <div class="ep-icon">
            <svg class="w-7 h-7" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
              <circle cx="12" cy="8" r="4" /><path d="M4 21c0-4 3.6-6.5 8-6.5s8 2.5 8 6.5" stroke-linecap="round" />
            </svg>
          </div>
          <p class="text-sm">暂无账号</p>
          <router-link to="/accounts" class="text-xs text-cyan hover:underline">去添加第一个 Telegram 账号 →</router-link>
        </div>
        <v-chart v-else class="h-56" :option="accountChartOption" autoresize />
      </article>
      </div><!-- /R98 双栏 -->
    </section>

    <!-- 风险事件 & 最近活动 -->
    <section class="grid grid-cols-1 lg:grid-cols-2 gap-4 reveal reveal-delay-2">
      <!-- 风险事件 -->
      <article class="glass rounded-[var(--t-r-lg)] p-5 md:p-6 lift">
        <div class="flex items-center justify-between mb-5">
          <h3 class="text-body font-semibold text-text-primary">最近风险事件</h3>
          <router-link to="/accounts" class="text-xs text-cyan hover:text-cyan font-medium transition-colors">查看全部</router-link>
        </div>
        <div class="space-y-3">
          <div
            v-for="event in riskEvents"
            :key="event.risk_id"
            class="flex items-start gap-3 p-3.5 rounded-[var(--t-r-md)] bg-ink-01 hover:bg-bg-secondary transition-[transform,opacity,border-color,background-color] duration-300 hover:shadow-cyan-glow/10"
          >
            <AlertTriangle class="w-5 h-5 text-amber shrink-0 mt-0.5" stroke-width="2" />
            <div class="flex-1 min-w-0">
              <p class="text-sm font-medium text-text-primary">{{ riskTypeLabel(event.risk_type) }}</p>
              <!-- R118: 补账号名+原因 —— 之前只有类型和时间, 看不出是哪个号为什么 -->
              <p class="text-xs text-text-secondary mt-0.5 truncate">
                <span v-if="event.account_id" class="text-amber/90">{{ riskAccName(event.account_id) }}</span>
                <template v-if="event.account_id && event.detail"> · </template>
                <span v-if="event.detail">{{ event.detail }}</span>
              </p>
              <p class="text-xs text-text-muted mt-0.5">{{ formatRelativeTime(event.created_at) }}</p>
            </div>
            <span class="text-xs px-2 py-0.5 rounded-full font-medium" :class="severityClass(event.severity)">
              {{ severityLabel(event.severity) }}
            </span>
          </div>
          <EmptyState v-if="!riskEvents.length" icon="Shield" title="目前一切正常" description="最近 24 小时风平浪静，继续保持~" />
        </div>
      </article>

      <!-- 最近活动 -->
      <article class="glass rounded-[var(--t-r-lg)] p-5 md:p-6 lift">
        <div class="flex items-center justify-between mb-5">
          <h3 class="text-body font-semibold text-text-primary">最近活动</h3>
        </div>
        <div class="space-y-3">
          <div
            v-for="log in auditLogs"
            :key="log.log_id"
            class="flex items-center gap-3 p-3.5 rounded-[var(--t-r-md)] bg-ink-01 hover:bg-bg-secondary transition-[transform,opacity,border-color,background-color] duration-300 hover:shadow-cyan-glow/10"
          >
            <Activity class="w-5 h-5 text-cyan shrink-0" stroke-width="2" />
            <div class="flex-1 min-w-0">
              <p class="text-sm font-medium text-text-primary">{{ auditActionLabel(log.action) }}</p>
              <p class="text-xs text-text-secondary">{{ targetLabel(log.target) }}</p>
            </div>
            <span class="text-xs text-text-secondary">{{ formatRelativeTime(log.created_at) }}</span>
          </div>
          <EmptyState v-if="!auditLogs.length" icon="Clock" title="暂无操作记录" description="你的操作会在这里留下足迹~" />
        </div>
      </article>
    </section>
  </main>
</template>

<script setup lang="ts">
import { riskTypeLabel, severityLabel, auditActionLabel, targetLabel } from '@/utils/labels'


import { ref, onMounted, computed, onUnmounted, onBeforeUnmount, watch } from 'vue'

let scrollObserver: IntersectionObserver | null = null
let scrollRafId: number | null = null
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart, PieChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import VChart from 'vue-echarts'
import { useChartTheme } from '@/composables/useChartTheme'
import { AlertTriangle, Activity, Shield, Clock } from 'lucide-vue-next'

import { dashboardApi } from '@/api/dashboard'
import LottieAnim from '@/components/ui/LottieAnim.vue'
import { toast } from '@/utils/toast'
import { useAccountStore } from '@/stores/account'

// R125 hero: 按时段问候 + workspace 死参数约定(与 Layout/AppShell 同款)
const greeting = (() => {
  const h = new Date().getHours()
  if (h < 6) return '夜深了'
  if (h < 12) return '早上好'
  if (h < 14) return '中午好'
  if (h < 18) return '下午好'
  return '晚上好'
})()
const accId = computed(() => accountStore.currentAccount?.account_id || 'none')

// R126: 快速开始已撤除(功能入口统一在"更多"页) —— 原 quickActions 死代码一并清除

// 账号切换: 仪表盘只统计当前账号的数据
const accountStore = useAccountStore()
const currentAccount = computed(() => accountStore.currentAccount)
import { settingsApi } from '@/api/settings'
import StatCard from '@/components/charts/StatCard.vue'
import SendCalendar from '@/components/SendCalendar.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import { formatRelativeTime, formatDate } from '@/utils/format'
import type { RiskEvent, AuditLog } from '@/types'

use([CanvasRenderer, LineChart, PieChart, GridComponent, TooltipComponent, LegendComponent])

const loading = ref(true)

const stats = ref([
  { label: '在线账号', value: '0', icon: 'Users', color: 'cyan', trend: '+0' },
  { label: '今日任务', value: '0', icon: 'ListTodo', color: 'cyan', trend: '+0' },
  { label: '总群组', value: '0', icon: 'MessageSquare', color: 'amber', trend: '+0' },
  { label: '风险事件', value: '0', icon: 'AlertTriangle', color: 'rose', trend: '0' },
])

const riskEvents = ref<RiskEvent[]>([])
const auditLogs = ref<AuditLog[]>([])
// R118: 风险事件的账号名映射(account_id → 展示名)
function riskAccName(id: string) {
  const a = (accountStore.accounts || []).find((x: any) => String(x.account_id) === String(id))
  if (!a) return '账号 ' + String(id).slice(0, 6)
  return a.first_name || a.remark || a.phone || '账号 ' + String(id).slice(0, 6)
}
const taskData = ref<number[]>([])
const accountData = ref<{ value: number; name: string }[]>([])
const expiresAt = ref('')
const daysLeft = ref(0)

const hasTaskData = computed(() => (taskData.value?.length || 0) > 0 && taskData.value.some((v) => v > 0))
const hasAccountData = computed(() => (accountData.value?.length || 0) > 0 && (accountData.value || []).some((v) => v.value > 0))

// Phase 6: 图表主题(见 useChartTheme)
const chartTheme = useChartTheme()
onMounted(() => chartTheme.start())
onBeforeUnmount(() => chartTheme.stop())

// Phase 6: 图表颜色走 useChartTheme(CSS 变量→响应式), 切深浅主题/换肤自动跟随。
// 此前硬编码 #f1f5f9/#94a3b8, 浅色主题下文字几乎不可见。
const taskChartOption = computed(() => ({
  backgroundColor: 'transparent',
  tooltip: {
    trigger: 'axis',
    backgroundColor: 'rgba(15, 23, 42, 0.95)',
    borderColor: 'rgba(255, 255, 255, 0.1)',
    borderWidth: 1,
    textStyle: { color: chartTheme.textPrimary.value },
    extraCssText: 'box-shadow: 0 4px 20px rgba(0,0,0,0.4); border-radius: 0.75rem;',
  },
  grid: { left: '2%', right: '3%', bottom: '2%', top: '8%', containLabel: true },
  xAxis: {
    type: 'category',
    data: ['周一', '周二', '周三', '周四', '周五', '周六', '周日'],
    axisLine: { show: false },
    axisTick: { show: false },
    axisLabel: { color: chartTheme.textMuted.value, fontSize: 11 },
  },
  yAxis: {
    type: 'value',
    minInterval: 1, // R87-M6: Y轴强制整数, 0.5 刻度不再出现
    axisLine: { show: false },
    axisTick: { show: false },
    // R87-M6: 网格线淡化(原 40% 透明度太重)
    splitLine: { lineStyle: { color: chartTheme.border.value + '33' } },
    axisLabel: { color: chartTheme.textMuted.value, fontSize: 11 },
  },
  series: [
    {
      name: '完成任务',
      type: 'line',
      smooth: true,
      symbol: 'circle',
      symbolSize: 6,
      showSymbol: false,
      data: taskData.value,
      // R87-M5: 改读图表令牌色板(--chart-1), 切主题自动跟随, 替换旧硬编码蓝 #7ab5e3
      lineStyle: { color: chartTheme.palette.value[0], width: 2.5 },
      itemStyle: { color: chartTheme.palette.value[0], borderWidth: 2, borderColor: 'rgba(15,23,42,0.8)' },
      areaStyle: {
        color: {
          type: 'linear',
          x: 0,
          y: 0,
          x2: 0,
          y2: 1,
          colorStops: [
            { offset: 0, color: `${chartTheme.palette.value[0]}40` },
            { offset: 1, color: `${chartTheme.palette.value[0]}00` },
          ],
        },
      },
    },
  ],
}))

const accountChartOption = computed(() => ({
  backgroundColor: 'transparent',
  tooltip: {
    backgroundColor: 'rgba(15, 23, 42, 0.95)',
    borderColor: 'rgba(255, 255, 255, 0.1)',
    borderWidth: 1,
    textStyle: { color: chartTheme.textPrimary.value },
    extraCssText: 'box-shadow: 0 4px 20px rgba(0,0,0,0.4); border-radius: 0.75rem;',
  },
  legend: {
    bottom: 0,
    itemWidth: 8,
    itemHeight: 8,
    itemGap: 16,
    textStyle: { color: getComputedStyle(document.documentElement).getPropertyValue('--text-secondary') || 'text-text-muted', fontSize: 11 },
  },
  series: [
    {
      type: 'pie',
      radius: ['42%', '72%'],
      center: ['50%', '44%'],
      avoidLabelOverlap: false,
      padAngle: 3,
      itemStyle: { borderRadius: 6, borderColor: 'rgba(15,23,42,0.8)', borderWidth: 2 },
      data: accountData.value,
      label: { show: false },
      emphasis: {
        label: { show: true, fontSize: 13, fontWeight: 'bold', color: chartTheme.textPrimary.value },
        itemStyle: { shadowBlur: 10, shadowColor: 'rgba(0,0,0,0.1)' },
      },
    },
  ],
}))

function severityClass(s: string) {
  const m: Record<string, string> = {
    low: 'bg-text-muted/15 text-text-secondary',
    medium: 'bg-amber/10 text-amber',
    high: 'bg-rose/10 text-rose',
    critical: 'bg-rose/20 text-rose',
  }
  return m[s] || ''
}

// Scroll Reveal：苹果产品页滚动进入动画
function initScrollReveal() {
  scrollObserver = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          entry.target.classList.add('revealed')
          scrollObserver!.unobserve(entry.target)
        }
      })
    },
    { threshold: 0.08, rootMargin: '0px 0px -40px 0px' }
  )

  document.querySelectorAll('.reveal').forEach((el) => scrollObserver?.observe(el))
}

onMounted(async () => {  // 先触发动画（首屏内容直接显示）
  scrollRafId = requestAnimationFrame(() => {
    document.querySelectorAll('.reveal').forEach((el) => el.classList.add('revealed'))
  })

  // 侧边栏切换账号: 仪表盘数据跟随切换, 只显示该账号的统计
  watch(currentAccount, () => { loadDashboardData() })
  loadDashboardData()

  // 后续页面切换时重新绑定
  initScrollReveal()
})

// ===== 诊断埋点: 页面上直接可见, 截图即真相 =====
// 拉取仪表盘数据(首载与切账号共用, 避免整页刷新)
async function loadDashboardData() {
  try {
    loading.value = true
    const s = await dashboardApi.getStats(currentAccount.value?.account_id)
    if (s) {
      stats.value[0].value = String(s.online_accounts ?? 0)
      stats.value[1].value = String(s.completed_tasks_today ?? 0)
      stats.value[2].value = String(s.total_groups ?? 0)
      stats.value[3].value = String(s.risk_events_24h ?? 0)
      taskData.value = s.task_trend || []
      accountData.value = s.account_distribution || []
    }

    const re = await dashboardApi.getRiskEvents({ limit: 5 })
    riskEvents.value = (re as any)?.list || (Array.isArray(re) ? re : [])
    const al = await dashboardApi.getAuditLogs({ limit: 5 })
    auditLogs.value = (al )?.list || []
    try {
      const profile = await settingsApi.getProfile()
      if (profile.expires_at) {
        expiresAt.value = formatDate(profile.expires_at)
        const diff = new Date(profile.expires_at).getTime() - Date.now()
        daysLeft.value = Math.max(0, Math.ceil(diff / (1000 * 60 * 60 * 24)))
      }
    } catch (e) { /* ignore */ }
  } catch (e: unknown) {
    // 数据加载失败必须可见, 否则用户看到全 0 不知道原因
    toast({ type: 'error', title: '仪表盘数据加载失败', message: (e instanceof Error ? e.message : '请检查网络或联系管理员') })
  } finally {
    loading.value = false
  }
}
onUnmounted(() => {
  if (scrollObserver) { scrollObserver?.disconnect(); scrollObserver = null }
  if (scrollRafId) { cancelAnimationFrame(scrollRafId); scrollRafId = null }
})
</script>
