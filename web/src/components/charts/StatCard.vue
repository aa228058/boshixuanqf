<template>
  <!-- R87-M7: KPI 卡改 demo 范式"纯文字三层"(label/大数字/趋势), 去掉彩色图标块 —— 色彩纪律: 数字才是主角 -->
  <div class="glass rounded-[var(--t-r-lg)] p-3 sm:p-4 min-w-0 transition-[transform,border-color] duration-300 lift">
    <p class="text-[10.5px] font-medium text-text-muted truncate">{{ label }}</p>
    <p class="text-title leading-tight font-bold text-text-primary tracking-tight mt-1.5"><CountUp :value="value" /></p>
    <p v-if="trend" class="text-[10.5px] mt-1 font-medium" :class="trendClass">{{ trend }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
// P2-53: 补 Database/Server/Cpu/HardDrive——原 iconMap 缺这几个, 后端传对应 icon 名时全部回退 Activity
import { Users, User, ListTodo, MessageSquare, AlertTriangle, Shield, Radio, TrendingUp, Activity, Key, KeyRound, DollarSign, Database, Server, Cpu, HardDrive } from 'lucide-vue-next'
import CountUp from '@/components/ui/CountUp.vue'

const props = defineProps<{
  label: string
  value: string
  icon: string
  color: string
  trend?: string
}>()

// 图标名 → Lucide 组件(仪表盘传字符串名, 映射渲染; 未知名字回退 Activity)
const iconMap: Record<string, any> = {
  Users, User, ListTodo, MessageSquare, AlertTriangle, Shield, Radio, TrendingUp, Activity, Key, KeyRound, DollarSign,
  Database, Server, Cpu, HardDrive,
}
const iconComp = computed(() => iconMap[props.icon] || Activity)

const bgClass = computed(() => {
  const m: Record<string, string> = {
    cyan: 'bg-cyan-dim',
    purple: 'bg-cyan/10',
    amber: 'bg-amber/10',
    rose: 'bg-rose/10',
    emerald: 'bg-emerald/10',
    indigo: 'bg-cyan/15',
  }
  return m[props.color] || 'bg-cyan-dim'
})

const textClass = computed(() => {
  const m: Record<string, string> = {
    cyan: 'text-cyan',
    purple: 'text-cyan',
    amber: 'text-amber',
    rose: 'text-rose',
    emerald: 'text-emerald',
    indigo: 'text-cyan/80',
  }
  return m[props.color] || 'text-cyan'
})

const trendClass = computed(() => {
  if (!props.trend) return ''
  return props.trend.startsWith('+') ? 'text-emerald' : 'text-text-muted'
})
</script>
