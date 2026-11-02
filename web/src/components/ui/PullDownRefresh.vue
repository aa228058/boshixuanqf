<template>
  <!--
    P0-01 真正的修复(2026-10-23): 桌面版 tdesign-vue-next 不导出 PullDownRefresh
    (它是移动端组件), 此前 5 个工作区页面的 <pull-down-refresh> 标签是未注册
    透传, 下拉刷新从未生效。此组件按页面既有契约手写实现:
      v-model="pullRefreshing"  —  刷新中状态(父组件在 finally 里复位)
      @refresh="onPullRefresh"  —  释放过阈值时触发
      默认插槽                   —  页面列表内容, 属性(如 class)透传到根元素
    触发条件: 触摸点位于列表滚动容器顶部(scrollTop<=0)向下拉, 拉动距离经阻尼
    换算, 超过 THRESHOLD 释放即触发; 刷新期间锁定防重复触发。
  -->
  <div ref="wrapEl" class="pdr-wrap">
    <div class="pdr-indicator" :style="{ height: offset + 'px' }">
      <div v-show="offset > 4 || refreshing" class="pdr-indicator-inner">
        <svg v-if="refreshing" class="pdr-spin" viewBox="0 0 24 24" fill="none">
          <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-opacity=".25" stroke-width="3" />
          <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" stroke-width="3" stroke-linecap="round" />
        </svg>
        <span class="pdr-text">{{ refreshing ? '刷新中…' : (releasing ? '释放立即刷新' : '下拉可以刷新') }}</span>
      </div>
    </div>
    <div
      @touchstart.passive="onStart"
      @touchmove="onMove"
      @touchend="onEnd"
      @touchcancel="onEnd"
      @mousedown="onMouseStart"
    >
      <slot />
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'

const props = defineProps<{ modelValue?: boolean }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void; (e: 'refresh'): void }>()

const THRESHOLD = 60 // 触发刷新的下拉距离(px)
const DAMP = 0.45 // 阻尼系数——拉得越深越费力, 手感自然
const MAX_PULL = 120 // 指示器最大高度

const wrapEl = ref<HTMLElement | null>(null)
const pullY = ref(0) // 原始下拉位移
const refreshing = computed(() => !!props.modelValue)

const releasing = computed(() => !refreshing.value && pullY.value >= THRESHOLD)
const offset = computed(() => {
  if (refreshing.value) return 56
  return Math.min(Math.round(pullY.value * DAMP), MAX_PULL)
})

let startY = 0
let tracking = false
let scrollParent: HTMLElement | null = null

// 找到最近的滚动容器(下拉手势只有在它顶部时才生效, 否则让位给正常滚动)
function findScrollParent(el: HTMLElement | null): HTMLElement | null {
  let n = el?.parentElement
  while (n) {
    const s = getComputedStyle(n)
    if (/(auto|scroll)/.test(s.overflowY) && n.scrollHeight > n.clientHeight) return n
    n = n.parentElement
  }
  return null
}

function canPull(): boolean {
  if (refreshing.value) return false
  if (!scrollParent) scrollParent = findScrollParent(wrapEl.value)
  if (!scrollParent) return true // 无滚动容器时总能拉(整页场景)
  return scrollParent.scrollTop <= 0
}

function onStart(e: TouchEvent) {
  if (e.touches.length !== 1) return
  if (!canPull()) return
  tracking = true
  startY = e.touches[0].clientY
  pullY.value = 0
}

function onMove(e: TouchEvent) {
  if (!tracking) return
  const delta = e.touches[0].clientY - startY
  if (delta <= 0) {
    // 向上滑——交还滚动, 停止跟踪
    if (pullY.value > 0) pullY.value = 0
    tracking = false
    return
  }
  // 下拉阶段阻止默认滚动(橡皮筋), 把位移记在组件上
  e.preventDefault()
  pullY.value = delta
}

function onEnd() {
  if (!tracking) return
  tracking = false
  if (pullY.value >= THRESHOLD) {
    // 过阈值: 进入刷新态并通知父组件真正干活
    emit('update:modelValue', true)
    emit('refresh')
    pullY.value = 0
  } else {
    pullY.value = 0
  }
}

// 桌面端鼠标兜底——方便非触屏设备测试同样的交互
function onMouseStart(e: MouseEvent) {
  if (e.button !== 0) return
  if (!canPull()) return
  const move = (ev: MouseEvent) => {
    const delta = ev.clientY - e.clientY
    if (delta > 0) {
      ev.preventDefault()
      pullY.value = delta
      tracking = true
    }
  }
  const up = () => {
    window.removeEventListener('mousemove', move)
    window.removeEventListener('mouseup', up)
    onEnd()
  }
  window.addEventListener('mousemove', move)
  window.addEventListener('mouseup', up)
}
</script>

<style scoped>
.pdr-wrap {
  position: relative;
}
.pdr-indicator {
  overflow: hidden;
  transition: height 0.18s ease-out;
  display: flex;
  align-items: flex-end;
  justify-content: center;
  color: var(--text-muted, #94a3b8);
}
.pdr-indicator-inner {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-bottom: 8px;
}
.pdr-text {
  font-size: 12px;
}
.pdr-spin {
  width: 14px;
  height: 14px;
  animation: pdr-rot 0.9s linear infinite;
}
@keyframes pdr-rot {
  to { transform: rotate(360deg); }
}
</style>
