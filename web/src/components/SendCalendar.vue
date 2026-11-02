<template>
  <div class="glass rounded-[var(--t-r-md)] p-4 lift">
    <div class="flex items-center justify-between mb-4">
      <div>
        <h3 class="font-semibold text-text-primary">发送日历</h3>
        <p class="text-xs text-text-muted mt-0.5">每日成功发送量 · 近 {{ months.length }} 个月</p>
      </div>
      <div class="flex items-center gap-1.5">
        <button class="px-2.5 py-1.5 rounded-lg text-xs hover:bg-ink-01 transition-colors"
                :disabled="offset >= months.length - 1" @click="offset++">←</button>
        <span class="text-sm font-medium text-text-primary min-w-[90px] text-center">{{ currentLabel }}</span>
        <button class="px-2.5 py-1.5 rounded-lg text-xs hover:bg-ink-01 transition-colors"
                :disabled="offset <= 0" @click="offset--">→</button>
      </div>
    </div>

    <div class="grid grid-cols-7 gap-1 mb-1">
      <div v-for="d in ['一','二','三','四','五','六','日']" :key="d"
           class="text-center text-[10px] text-text-muted py-1">{{ d }}</div>
    </div>
    <div class="grid grid-cols-7 gap-1">
      <div v-for="cell in gridCells" :key="cell.key"
           class="h-8 sm:h-9 rounded-md flex flex-col items-center justify-center text-[10px] transition-transform hover:scale-110 cursor-default"
           :class="cellClass(cell)"
           :title="cell.day ? `${cell.day} 成功发送 ${cell.count} 条` : ''">
        <span v-if="cell.day" class="font-medium">{{ cell.date }}</span>
        <span v-if="cell.day && cell.count > 0" class="opacity-80">{{ cell.count > 999 ? '999+' : cell.count }}</span>
      </div>
    </div>

    <div class="flex items-center gap-3 mt-4 text-[10px] text-text-muted">
      <span>少</span>
      <div class="flex gap-0.5">
        <!-- P0-17: Tailwind JIT 只扫静态类名, 动态拼接 bg-cyan/${n} 不生成 CSS 导致格子无底色。
             显式列出 5 档静态类(与 cellClass 档位一致) -->
        <div v-for="(cls, i) in ['bg-cyan/10', 'bg-cyan/25', 'bg-cyan/50', 'bg-cyan/75', 'bg-cyan']" :key="i"
             class="w-3.5 h-3.5 rounded-sm" :class="cls" />
      </div>
      <span>多</span>
      <span class="ml-auto">本月合计 <b class="text-text-primary">{{ monthTotal }}</b> 条</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { request } from '@/utils/request'

const dayData = ref<Record<string, number>>({})
const offset = ref(0)

// 近6个月的月份列表(最新在前)
const months = computed(() => {
  const list: { key: string; label: string; year: number; month: number }[] = []
  const now = new Date()
  for (let i = 0; i < 6; i++) {
    const d = new Date(now.getFullYear(), now.getMonth() - i, 1)
    list.push({
      key: `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`,
      label: `${d.getFullYear()} 年 ${d.getMonth() + 1} 月`,
      year: d.getFullYear(),
      month: d.getMonth(),
    })
  }
  return list
})
const current = computed(() => months.value[Math.min(offset.value, months.value.length - 1)])
const currentLabel = computed(() => current.value?.label || '')

const gridCells = computed(() => {
  const cells: { key: string; day: boolean; date: number; count: number }[] = []
  if (!current.value) return cells
  const { year, month } = current.value
  const first = new Date(year, month, 1)
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  // 周一开头: JS getDay() 周日=0 → 周一索引 = (getDay()+6)%7
  const lead = (first.getDay() + 6) % 7
  for (let i = 0; i < lead; i++) cells.push({ key: `e${i}`, day: false, date: 0, count: 0 })
  for (let d = 1; d <= daysInMonth; d++) {
    const key = `${year}-${String(month + 1).padStart(2, '0')}-${String(d).padStart(2, '0')}`
    cells.push({ key, day: true, date: d, count: dayData.value[key] || 0 })
  }
  return cells
})

const monthTotal = computed(() =>
  gridCells.value.reduce((s, c) => s + (c.count || 0), 0))

function cellClass(cell: { day: boolean; count: number; key: string }) {
  if (!cell.day) return ''
  const today = new Date()
  const isToday = cell.key === `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`
  const lv = cell.count === 0 ? 0 : cell.count < 10 ? 1 : cell.count < 50 ? 2 : cell.count < 200 ? 3 : 4
  // P0-17: 动态拼接的 bg-cyan/${lv*20} 不会被 Tailwind 扫描生成——格子无背景色。
  // 改显式静态类名数组(JIT 可扫到), 5 档对应 0.2/0.4/0.6/0.8 透明度。
  const lvBg = ['', 'bg-cyan/20', 'bg-cyan/40', 'bg-cyan/60', 'bg-cyan/80']
  return [
    lv === 0 ? 'text-text-muted/50' : lvBg[lv] + ' text-text-primary',
    /* R87-M8: 今日选中从荧光绿描边改为品牌淡底块(深色下不再刺眼) */
    isToday ? 'bg-cyan/15 text-cyan font-semibold rounded-md' : '',
  ]
}

async function load() {
  try {
    // P0-24 审计修复: 原 request 是导出的函数(request() 发起请求), request.get
    // 是 undefined——日历永远空。api 对象才有 get/post 方法。
    dayData.value = await api.get<Record<string, number>>('/dashboard/send-calendar', { params: { months: 6 } })
  } catch { /* 统计失败显示空日历 */ }
}
onMounted(load)
</script>
