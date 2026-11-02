<!-- 进度环(交互示例28): 环形进度 + 居中百分比, 适合任务卡片等紧凑场景。
     用法: <ProgressRing :percent="51" :size="44" :stroke="4" /> -->
<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  percent: number          // 0-100, 已处理口径(成功+失败)
  size?: number            // 环外径 px
  stroke?: number          // 环粗细 px
  success?: number         // 成功占比 0-100(画绿色弧, 剩余为青)
}>(), { size: 44, stroke: 4, success: 0 })

const r = computed(() => (props.size - props.stroke) / 2)
const circ = computed(() => 2 * Math.PI * r.value)
// 总进度弧长
const dashTotal = computed(() => circ.value * (1 - Math.min(100, props.percent) / 100))
// 成功段弧长(在总进度里切出绿色段)
const successLen = computed(() => circ.value * (Math.min(props.success, props.percent) / 100))
</script>

<template>
  <div class="relative inline-grid place-items-center shrink-0" :style="{ width: size + 'px', height: size + 'px' }">
    <svg :width="size" :height="size" class="-rotate-90">
      <circle :cx="size/2" :cy="size/2" :r="r" fill="none" stroke-width="stroke"
              class="stroke-bg-primary" />
      <!-- 底进度(青/主色) -->
      <circle :cx="size/2" :cy="size/2" :r="r" fill="none" stroke-width="stroke"
              stroke-linecap="round" stroke="currentColor" class="text-cyan transition-all duration-500"
              :stroke-dasharray="circ" :stroke-dashoffset="dashTotal" />
      <!-- 成功段(绿, 从起点覆盖) -->
      <circle v-if="success > 0" :cx="size/2" :cy="size/2" :r="r" fill="none" :stroke-width="stroke"
              stroke-linecap="round" :stroke="'var(--color-emerald, #4cb892)'" class="transition-all duration-500"
              :stroke-dasharray="`${successLen} ${circ}`" stroke-dashoffset="0" />
    </svg>
    <span class="absolute text-[10px] font-bold tabular-nums text-text-primary">{{ Math.round(percent) }}%</span>
  </div>
</template>
