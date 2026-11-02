<script lang="ts">
// 模块级共享计数器: 所有弹窗实例共用一个, 叠层递增才真正生效
// (写在这里而非 script setup, 是因为 setup 内的变量每实例独立, 递增会失效)
let modalCounter = 0
</script>

<template>
  <VueFinalModal
    :model-value="modelValue"
    @update:model-value="emit('update:modelValue', $event)"
    :z-index-fn="zIndexFn"
    :click-to-close="true"
    :esc-to-close="true"
    :lock-scroll="true"
    :hide-overlay="false"
    overlay-transition=""
    :content-transition="{
      enterActiveClass: 'modal-enter-active',
      enterFromClass: 'modal-enter-from',
      enterToClass: 'modal-enter-to',
      leaveActiveClass: 'modal-leave-active',
      leaveFromClass: 'modal-leave-from',
      leaveToClass: 'modal-leave-to',
    }"
    class="modal-wrapper"
    content-class="modal-content"
    overlay-class="modal-overlay"
  >
    <!-- R128 键盘吸底: 输入框聚焦时把聚焦元素滚进可视区 —— 移动端键盘弹起后
         弹窗自动跟随滚动, 底部提交按钮不被顶出屏幕 -->
    <div class="modal-card" @focusin="onFocusIn">
      <!-- 顶部拖拽把手 -->
      <div class="modal-handle" />

      <!-- 头部 -->
      <div v-if="title" class="modal-header">
        <h3 class="modal-title">{{ title }}</h3>
        <button class="modal-close" @click="close" aria-label="关闭">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
      </div>

      <!-- 内容区 -->
      <div class="modal-body">
        <slot />
      </div>

      <!-- 底部 -->
      <div v-if="$slots.footer" class="modal-footer">
        <slot name="footer" />
      </div>
    </div>
  </VueFinalModal>
</template>

<script setup lang="ts">
import { ref, watch, onUnmounted } from 'vue'
import { VueFinalModal } from 'vue-final-modal'

const props = defineProps<{
  modelValue: boolean
  title?: string
  zIndex?: number
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
}>()

// 本实例的打开序位, 叠加到共享计数器上 → 后开的弹窗永远压住先开的
const order = ref(0)

// P2-02 审计修复: 原实现在 zIndexFn 里 +1——该函数渲染期会被多次调用,
// 每次渲染计数器都涨(打开期间 N 次渲染 = +N 次), 而卸载只 -1 一次,
// 计数器单调膨胀, z-index 随渲染次数无限抬高( eventually 盖过 toast)。
// 改为 watch 打开瞬间 +1 一次; zIndexFn 退化为纯查询, 卸载 -1 保持配对。
watch(() => props.modelValue, (v) => {
  if (v) {
    modalCounter += 1
    order.value = modalCounter
  }
})
function zIndexFn() {
  return props.zIndex ?? (2500 + order.value * 20)
}

function close() {
  emit('update:modelValue', false)
}

// R128 键盘吸底(编译必需): 聚焦输入控件时把它滚进弹窗可视区 —— 移动端键盘弹起后
// 弹窗跟随滚动, 底部提交按钮不被顶出屏幕。与下方 visualViewport 方案互补(双保险)。
function onFocusIn(e: FocusEvent) {
  const t = e.target as HTMLElement | null
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT')) {
    t.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  }
}

/* ===== 键盘弹起适配(规范 §8): H5 弹窗输入时按钮被顶飞的根治 =====
   监听 visualViewport: 键盘弹起时其 height 缩小,
   把差值写成 CSS 变量 --modal-kb, 移动端弹窗整体上移同样距离 */
const KB_VAR = '--modal-kb'
function syncKbOffset() {
  const vv = window.visualViewport
  if (!vv) return
  const kb = Math.max(0, window.innerHeight - vv.height - vv.offsetTop)
  document.documentElement.style.setProperty(KB_VAR, kb > 80 ? `${kb}px` : '0px')
}
function attachKbGuard() {
  if (!window.visualViewport) return
  syncKbOffset()
  vv?.addEventListener('resize', syncKbOffset)
  vv?.addEventListener('scroll', syncKbOffset)
}
function detachKbGuard() {
  if (!vv) return
  vv.removeEventListener('resize', syncKbOffset)
  vv.removeEventListener('scroll', syncKbOffset)
  document.documentElement.style.setProperty(KB_VAR, '0px')
}
let vv: VisualViewport | null = null
if (typeof window !== 'undefined') vv = window.visualViewport

watch(() => props.modelValue, v => {
  if (v) attachKbGuard()
  else detachKbGuard()
}, { immediate: true })
onUnmounted(() => { detachKbGuard(); modalCounter = Math.max(0, modalCounter - 1) })
</script>

<style scoped>
/* ===== 弹窗卡片(本组件模板内元素, scoped 正常生效) ===== */
.modal-card {
  width: 100%;
  max-height: inherit;
  /* R128 实底进 tokens: --t-modal-bg 三主题统一定义(tokens.css 各主题块) */
  background: var(--t-modal-bg, #fff);
  border-radius: 20px 20px 0 0;
  box-shadow: 0 -8px 32px rgba(0, 0, 0, 0.18);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  position: relative;
}

/* 顶部拖拽把手 */
.modal-handle {
  position: absolute;
  top: 10px;
  left: 50%;
  transform: translateX(-50%);
  width: 40px;
  height: 4px;
  border-radius: 9999px;
  background: rgba(128, 128, 128, 0.35);
  pointer-events: none;
  z-index: 2;
}

/* 头部 */
.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 24px 20px 12px;
  flex-shrink: 0;
  position: relative;
}

.modal-title {
  font-size: 18px;
  font-weight: 700;
  letter-spacing: 0.02em;
  color: var(--td-text-color-primary, #1a1a2e);
  margin: 0;
  line-height: 1.3;
}

.modal-close {
  width: 32px;
  height: 32px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--td-text-color-secondary, #666);
  background: transparent;
  border: none;
  cursor: pointer;
  transition: all 0.2s;
  flex-shrink: 0;
  margin-left: 12px;
}
.modal-close:hover {
  background: rgba(128, 128, 128, 0.10);
  color: var(--td-text-color-primary, #1a1a2e);
}

/* 内容区 */
.modal-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  -webkit-overflow-scrolling: touch;
  padding: 8px 20px;
  scrollbar-width: none;
}
.modal-body::-webkit-scrollbar {
  display: none;
}

/* 底部 */
.modal-footer {
  flex-shrink: 0;
  padding: 12px 20px calc(20px + env(safe-area-inset-bottom, 0px));
  border-top: 1px solid rgba(128, 128, 128, 0.10);
  display: flex;
  flex-direction: column;
  gap: 8px;
  /* 滚动指示: 提示上方还有内容(规范 §5) */
  box-shadow: 0 -4px 12px rgba(0, 0, 0, 0.04);
  position: relative;
  z-index: 1;
  background: var(--modal-bg, var(--td-bg-color-container, #fff));
}

/* ===== 桌面端: 圆角全包 ===== */
@media (min-width: 769px) {
  .modal-card {
    border-radius: 20px;
    box-shadow: 0 20px 60px rgba(0, 0, 0, 0.18);
  }
  .modal-handle {
    display: none;
  }
}

/* ===== 深色模式适配 =====
   R86 修复: 同 DynamicBackground —— ":global(.theme-dark) .modal-card" 会被编译成
   ".theme-dark"(选择器后半被丢弃), 把背景/文字色直接打到 <html> 上。整条必须放进 :global() */
/* R128 实底进 tokens: --t-modal-bg 三主题统一定义(tokens.css),
   此处不再用 td 玻璃变量(它会在深色下把弹窗打回 6% 透明——"模态框太花"的根因) */
:global(.theme-dark .modal-card) {
  background: var(--t-modal-bg, #0d1230);
}
:global(.theme-dark .modal-title) {
  color: var(--td-text-color-primary, #e8eaf6);
}
:global(.theme-dark .modal-close) {
  color: var(--td-text-color-secondary, #9ca3af);
}
:global(.theme-dark .modal-close:hover) {
  background: rgba(255, 255, 255, 0.08);
  color: var(--td-text-color-primary, #e8eaf6);
}
</style>
