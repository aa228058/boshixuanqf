<template>
  <span class="absolute top-2 right-2 text-xs text-amber bg-amber/10 px-2 py-0.5 rounded-full z-10">演示数据</span>
  <!-- R128 relative: 演示数据角标(第2行)绝对定位需要定位上下文 -->
  <main class="relative space-y-6">
    <!-- 首页不显示面包屑 -->
    <div class="grid grid-cols-1 md:grid-cols-3 lg:grid-cols-6 gap-4">
      <StatCard
        v-for="stat in stats"
        :key="stat.label"
        v-bind="stat"
      />
    </div>

    <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <div class="glass rounded-[var(--t-r-md)] p-5 lift">
        <h3 class="text-sm font-medium text-text-secondary mb-4">租户增长趋势 <span class="ml-2 text-xs px-1.5 py-0.5 rounded bg-warning/15 text-warning">示例数据</span></h3>
        <v-chart class="h-64" :option="tenantChartOption" autoresize />
      </div>
      <div class="glass rounded-[var(--t-r-md)] p-5 lift">
        <h3 class="text-sm font-medium text-text-secondary mb-4">收入趋势 <span class="ml-2 text-xs px-1.5 py-0.5 rounded bg-warning/15 text-warning">示例数据</span></h3>
        <v-chart class="h-64" :option="revenueChartOption" autoresize />
      </div>
    </div>

    <div class="glass rounded-[var(--t-r-md)] p-5 lift">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-sm font-medium text-text-secondary">最近注册租户</h3>
        <router-link to="/admin/tenants" class="text-xs text-cyan hover:underline">查看全部</router-link>
      </div>
      <!-- 桌面端表格 -->
      <div class="hidden sm:block">
        <t-table
          row-key="tenant_id"
          :columns="tenantColumns.map(c => ({ colKey: c.key, title: c.title }))"
          :data="recentTenants"
          hover
        >
          <template #status="{ row }">
            <span class="inline-flex items-center gap-1.5 text-xs px-2 py-0.5 rounded-full font-bold whitespace-nowrap" :class="statusClass(row.status as string)">
              <i class="w-1.5 h-1.5 rounded-full bg-current opacity-80"></i>{{ row.status === 'active' ? '正常' : row.status === 'suspended' ? '已停用' : '未激活' }}
            </span>
          </template>
          <template #plan_id="{ row }">
            <span class="text-xs">{{ planLabel(row.plan_id as string) }}</span>
          </template>
        </t-table>
      </div>
      <!-- 移动端卡片 -->
      <div class="sm:hidden space-y-3">
        <div
          v-for="t in recentTenants"
          :key="t.tenant_id"
          class="p-3 rounded-lg bg-bg-card space-y-1"
        >
          <div class="flex items-center justify-between">
            <span class="font-medium text-sm">{{ t.name }}</span>
            <span class="text-xs px-2 py-0.5 rounded-full" :class="statusClass(t.status)">
              {{ t.status === 'active' ? '正常' : t.status === 'suspended' ? '已停用' : '未激活' }}
            </span>
          </div>
          <div class="flex items-center gap-3 text-xs text-text-secondary">
            <span>{{ planLabel(t.plan_id) }}</span>
            <span>{{ new Date(t.created_at).toLocaleDateString('zh-CN') }}</span>
          </div>
        </div>
        <EmptyState v-if="!recentTenants.length" icon="Users" title="暂无租户" description="还没有租户注册" />
      </div>
    </div>
  </main>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { LineChart, BarChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import VChart from 'vue-echarts'
import { useChartTheme } from '@/composables/useChartTheme'

// Phase 6: 图表主题(见 useChartTheme)——切主题文字/轴/分割线自动跟随
const chartTheme = useChartTheme()
onMounted(() => chartTheme.start())
onBeforeUnmount(() => chartTheme.stop())
import { adminApiMod } from '@/api/admin'
import Breadcrumb from '@/components/ui/Breadcrumb.vue'
import { toast } from '@/utils/toast'
import StatCard from '@/components/charts/StatCard.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import type { Tenant } from '@/types'

use([CanvasRenderer, LineChart, BarChart, GridComponent, TooltipComponent, LegendComponent])

const stats = ref([
  { label: '总租户', value: '0', icon: 'Users', color: 'cyan', trend: '+0' },
  { label: '活跃租户', value: '0', icon: 'Activity', color: 'cyan', trend: '+0' },
  { label: '总账号', value: '0', icon: 'User', color: 'amber', trend: '+0' },
  { label: '卡密总数', value: '0', icon: 'Key', color: 'rose', trend: '+0' },
  { label: '未使用卡密', value: '0', icon: 'KeyRound', color: 'cyan', trend: '+0' },
  { label: '今日收入', value: '¥0', icon: 'DollarSign', color: 'emerald', trend: '+0' },
])

const recentTenants = ref<Tenant[]>([])

const tenantColumns = [
  { key: 'name', title: '租户名称' },
  { key: 'plan_id', title: '套餐' },
  { key: 'status', title: '状态' },
  { key: 'created_at', title: '注册时间', format: (v: any) => new Date(v).toLocaleDateString('zh-CN') },
]

// TODO(数据接入): 以下两张图表的 x 轴与 series 均为占位示例数据（写死的周标签+编造的数值），
// 管理后台展示的不是真实趋势。后端暂无 trend 接口；接入前应给图表加"示例数据"标注或隐藏，
// 避免管理员依据假数据做决策。
// Phase 6: 图表颜色走 useChartTheme(切主题自动跟随), 不再硬编码深色值
const tenantChartOption = computed(() => ({
  backgroundColor: 'transparent',
  tooltip: { trigger: 'axis', backgroundColor: 'rgba(17,24,39,0.9)', borderColor: 'rgba(255,255,255,0.1)', textStyle: { color: chartTheme.textPrimary.value } },
  grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
  xAxis: { type: 'category', data: ['周一', '周二', '周三', '周四', '周五', '周六', '周日'], axisLine: { lineStyle: { color: chartTheme.border.value + '66' } }, axisLabel: { color: chartTheme.textMuted.value } },
  yAxis: { type: 'value', axisLine: { show: false }, splitLine: { lineStyle: { color: chartTheme.border.value + '55' } }, axisLabel: { color: chartTheme.textMuted.value } },
  series: [{
    type: 'line', smooth: true, data: [2, 5, 3, 8, 6, 10, 7],
    lineStyle: { color: '#38bdf8', width: 2 }, itemStyle: { color: '#38bdf8' }, // P0-23: ECharts 不认 Tailwind 类名
    areaStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: 'rgba(90,184,232,0.2)' }, { offset: 1, color: 'rgba(90,184,232,0)' }] } },
  }]
}))

const revenueChartOption = computed(() => ({
  backgroundColor: 'transparent',
  tooltip: { trigger: 'axis', backgroundColor: 'rgba(17,24,39,0.9)', borderColor: 'rgba(255,255,255,0.1)', textStyle: { color: chartTheme.textPrimary.value } },
  grid: { left: '3%', right: '4%', bottom: '3%', containLabel: true },
  xAxis: { type: 'category', data: ['周一', '周二', '周三', '周四', '周五', '周六', '周日'], axisLine: { lineStyle: { color: chartTheme.border.value + '66' } }, axisLabel: { color: chartTheme.textMuted.value } },
  yAxis: { type: 'value', axisLine: { show: false }, splitLine: { lineStyle: { color: chartTheme.border.value + '55' } }, axisLabel: { color: chartTheme.textMuted.value } },
  series: [{
    type: 'bar', data: [150, 230, 180, 320, 280, 450, 380],
    itemStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: '#a3d2f2' }, { offset: 1, color: 'rgba(163,210,242,0.3)' }] }, borderRadius: [4, 4, 0, 0] },
  }]
}))

function statusClass(s: string) {
  const map: Record<string, string> = { active: 'bg-emerald/10 text-emerald', inactive: 'bg-text-muted/15 text-text-muted', suspended: 'bg-rose/10 text-rose' }
  return map[s] || ''
}

function planLabel(p: string) {
  const map: Record<string, string> = { free: '免费版', basic: '基础版', pro: '专业版', enterprise: '企业版' }
  return map[p] || p
}

onMounted(async () => {
  try {
    const s = await adminApiMod.getStats()
    stats.value[0].value = String(s.total_tenants || 0)
    stats.value[1].value = String(s.active_tenants || 0)
    stats.value[2].value = String(s.total_accounts || 0)
    stats.value[3].value = String(s.total_codes || 0)
    stats.value[4].value = String(s.unused_codes || 0)
    stats.value[5].value = `¥${s.today_revenue || 0}`

    const res = await adminApiMod.listTenants({ limit: 5 })
    recentTenants.value = res.list || []
  } catch (e: unknown) {
    // P2-60: 原空 catch 静默——管理仪表盘首屏失败只显示全 0 数字, 管理员无感知
    console.error('[admin-dashboard] 加载失败:', e)
    toast({ type: 'error', title: '仪表盘加载失败', message: (e as Error)?.message || '请刷新重试' })
  }
})
</script>
