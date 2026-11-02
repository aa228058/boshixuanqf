<template>
  <div class="min-h-[100dvh] text-text-primary relative">
    <!-- 动态背景 -->
    <!-- 断网横幅(交互层: 断网操作不再神秘失败; 用统一的 isOnline 状态) -->
    <div v-if="!isOnline" class="fixed top-0 left-0 right-0 z-[200] bg-amber/95 text-amber-950 text-center text-xs py-1.5 font-medium">
      网络连接已断开, 操作将在恢复后自动重试
    </div>

    <!-- R87-M7: DynamicBackground 已移除 —— demo 范式=纯色底+单光斑(body::before), 多层网格背景已删 -->
    <!-- R129 灵动岛 Toast: 紧凑胶囊+毛玻璃+顶栏下方悬浮; 2.5s 自动消失, 可滑走/点掉 -->
    <Toaster
      position="top-center"
      :offset="80"
      :duration="2500"
      richColors
      :toastOptions="{
        style: { fontSize: '13px' },
        classNames: { toast: 'ts-toast', title: 'ts-toast-title' }
      }"
    />
    <!-- 全局请求进度条: 任何 API 请求进行中顶部滑动显示(200ms防抖), 内联动画不依赖Tailwind自定义类 -->
    <div
      v-if="globalLoading"
      class="fixed top-0 left-0 right-0 z-[70] h-[3px] overflow-hidden pointer-events-none"
      style="background: rgba(124, 92, 255, 0.15);"
    >
      <div
        class="h-full w-2/5"
        style="background: linear-gradient(90deg, transparent, #7c5cff 50%, transparent); animation: global-load-slide 1s infinite ease-in-out;"
      />
    </div>
    <Suspense>
      <template #default>
        <router-view v-slot="{ Component }">
          <transition name="page" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </template>
      <template #fallback>
        <!-- 路由懒加载骨架: chunk 下载期间显示流光骨架而不是白屏 -->
        <div class="route-skeleton">
          <div class="skeleton-shimmer sk-line w40"></div>
          <div class="skeleton-shimmer sk-line w70"></div>
          <div class="skeleton-shimmer sk-card"></div>
          <div class="skeleton-shimmer sk-line w55"></div>
          <div class="skeleton-shimmer sk-line w85"></div>
        </div>
      </template>
    </Suspense>
    <ConfirmHost />
    <!-- P1-07: ToastContainer 已删——真实 toast 走 vue-sonner 的 <Toaster />,
         自研 ToastContainer 读的水数组列从来没人写, 是死组件(双系统并存) -->
    <SupportFloat />
    <!-- P1-08: 原此处还有第二条断网横幅(z-70), 与顶部 z-200 那条重复叠加——删除,
         全站只保留顶部一条。WifiOff 图标如他处不再使用可随 lint 清理。 -->
    <!-- R86 排障条已随雾罩根因(:global编译错位)定位而移除 -->
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { Toaster } from 'vue-sonner'
// 2026-10-22 审计修复: 此前此处有一套重复的 online 状态+两个 window 监听器
// 且从未 removeEventListener(监听器泄漏)——已删除, 统一用下方 isOnline 一套
import { WifiOff } from 'lucide-vue-next'
import { globalLoading } from '@/utils/request'
import SupportFloat from '@/components/ui/SupportFloat.vue'
import ConfirmHost from '@/components/ui/ConfirmHost.vue'
// R87-M7: DynamicBackground 已移除(网格背景层删除, 纯令牌底色)
// P1-07: ToastContainer 已删(组件仍在但不再引用)——真实 toast 由 vue-sonner 的
// <Toaster /> 渲染; 自研 ToastContainer 读的 toasts 数组从来没人写, 是死组件。
// 上面那条"精读修复"注释是其历史背景, 当时以为 ToastContainer 是真渲染者。

const isOnline = ref(true)

function updateOnlineStatus() {
  isOnline.value = navigator.onLine
}

// R86: 排障逻辑已随雾罩根因定位移除, 保留注释存档

// 动态切换 theme-color
function updateThemeColor() {
  const isDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) {
    meta.setAttribute('content', isDark ? '#020510' : '#ffffff')
  }
}

// R136: toast 垂直位置由 ui-kit.css 的"防弹版"CSS 全权锁定(top:110px + flex 居中),
// :offset 只作兜底初值。JS 钉扎/transform 居中均已废弃(与 sonner 动画打架)。

// R140: 灵动岛动画 JS 接管(WAAPI) —— CSS 进场动画在该环境多次实测不生效,
// 不再猜原因; MutationObserver 监听 sonner 容器, 新岛出现即用 Web Animations API
// 播"10号系统回弹"(与 CSS 层叠/选择器优先级完全无关, 必定执行); 2.3s 后 JS 再
// 驱动吸出退场(CSS toast-auto-hide 并行兜底, 双保险)。
let islandObserver: MutationObserver | null = null
const playIslandDrop = (el: Element) => {
  const h = el as HTMLElement
  h.animate?.(
    [
      { opacity: 0, transform: 'translateY(-40px)' },
      { opacity: 1, transform: 'translateY(4px)', offset: 0.7 },
      { opacity: 1, transform: 'translateY(0)' },
    ],
    { duration: 550, easing: 'cubic-bezier(0.175, 0.885, 0.32, 1.275)' }
  )
  setTimeout(() => {
    h.animate?.(
      [
        { opacity: 1, transform: 'translateY(0) scale(1)' },
        { opacity: 0, transform: 'translateY(-20px) scale(0.85)' },
      ],
      { duration: 250, easing: 'cubic-bezier(0.6, -0.28, 0.735, 0.045)', fill: 'forwards' }
    )
  }, 2300)
}
const bindIslandAnim = () => {
  if (islandObserver) return
  const toaster = document.querySelector('[data-sonner-toaster]')
  if (!toaster) return
  toaster.querySelectorAll('[data-sonner-toast]').forEach(playIslandDrop)
  islandObserver = new MutationObserver((muts) => {
    muts.forEach((m) => m.addedNodes.forEach((n) => {
      if (n.nodeType === 1 && (n as Element).hasAttribute?.('data-sonner-toast')) playIslandDrop(n)
    }))
  })
  islandObserver.observe(toaster, { childList: true })
}

onMounted(() => {
  isOnline.value = navigator.onLine
  window.addEventListener('online', updateOnlineStatus)
  window.addEventListener('offline', updateOnlineStatus)
  updateThemeColor()
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', updateThemeColor)
  bindIslandAnim()
  setTimeout(bindIslandAnim, 600)
  setTimeout(bindIslandAnim, 1600)
})

onUnmounted(() => {
  window.removeEventListener('online', updateOnlineStatus)
  window.removeEventListener('offline', updateOnlineStatus)
  window.matchMedia('(prefers-color-scheme: dark)').removeEventListener('change', updateThemeColor)
})
</script>

<style>

/* ===== 主题切换颜色渐变 ===== */
.theme-animating, .theme-animating *, .theme-animating *::before, .theme-animating *::after {
  transition: background-color .45s ease, color .45s ease, border-color .45s ease,
              box-shadow .45s ease, fill .45s ease, stroke .45s ease !important;
}

/* ===== 侧边栏激活指示条 ===== */
.menu-active { position: relative; }
.menu-active::before {
  content: ''; position: absolute; left: -14px; top: 50%; translate: 0 -50%;
  width: 3px; height: 60%; border-radius: 99px;
  background: linear-gradient(180deg, rgb(6 182 212), rgb(6 182 212 / .4));
  box-shadow: 0 0 8px rgb(6 182 212 / .6);
  animation: menu-indicator-in .25s cubic-bezier(.22,1,.36,1) both;
}
@keyframes menu-indicator-in { from { opacity: 0; height: 20%; } to { opacity: 1; height: 60%; } }

/* ===== 运行中进度条流光 ===== */
@keyframes bar-shimmer { to { background-position: -200% 0; } }
.bar-animated {
  background-image: linear-gradient(90deg, currentColor 0%, rgb(255 255 255 / .35) 50%, currentColor 100%);
  background-size: 200% 100%;
  animation: bar-shimmer 1.6s linear infinite;
}

/* ===== 表格斑马纹工具类 ===== */
.zebra > *:nth-child(even) { background-color: rgb(128 140 160 / .045); }
.zebra > *:nth-child(even):hover { background-color: rgb(128 140 160 / .09); }

/* ===== 在线状态呼吸圆点 ===== */
.dot-online { position: relative; }
.dot-online::after {
  content: ''; position: absolute; inset: -3px; border-radius: 50%;
  border: 1.5px solid rgb(52 211 153 / .5);
  animation: dot-breathe 2s ease-out infinite;
}
@keyframes dot-breathe {
  0% { transform: scale(.6); opacity: 1; }
  70%, 100% { transform: scale(1.6); opacity: 0; }
}

/* ===== 路由懒加载骨架屏 ===== */
.route-skeleton { min-height: 60vh; padding: 24px; display: flex; flex-direction: column; gap: 16px; }
.route-skeleton .sk-line { height: 14px; border-radius: 6px; }
.route-skeleton .sk-line.w40 { width: 40%; } .route-skeleton .sk-line.w70 { width: 70%; }
.route-skeleton .sk-line.w55 { width: 55%; } .route-skeleton .sk-line.w85 { width: 85%; }
.route-skeleton .sk-card { height: 120px; border-radius: 16px; }

/* ===== 页面切换过渡(UI升级包) ===== */
.page-enter-active { transition: opacity .22s ease, transform .22s cubic-bezier(.22,1,.36,1); }
.page-leave-active { transition: opacity .14s ease; }
.page-enter-from { opacity: 0; transform: translateY(10px); }
.page-leave-to { opacity: 0; }

/* ===== 按钮按压反馈(全局微交互) ===== */
button:not(:disabled):active { transform: scale(.97); }
button { transition: transform .12s ease, background-color .2s ease, color .2s ease, border-color .2s ease, opacity .2s ease, box-shadow .25s ease; }

/* ===== 卡片悬浮抬升(带 .lift 类的元素) ===== */
.lift { transition: transform .25s cubic-bezier(.22,1,.36,1), box-shadow .25s ease, border-color .25s ease; }
.lift:hover { transform: translateY(-3px); box-shadow: 0 12px 32px -12px rgb(6 182 212 / .25); border-color: rgb(6 182 212 / .3); }

/* ===== 列表交错入场(.stagger 容器) ===== */
.stagger > * { opacity: 0; animation: stagger-in .4s cubic-bezier(.22,1,.36,1) forwards; animation-delay: calc(var(--i, 0) * 45ms); }
@keyframes stagger-in { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: none; } }

/* ===== 成功脉冲(任务创建成功等场景) ===== */
@keyframes success-pop { 0% { transform: scale(.6); opacity: 0; } 60% { transform: scale(1.12); } 100% { transform: scale(1); opacity: 1; } }
.success-pop { animation: success-pop .45s cubic-bezier(.22,1,.36,1) both; }
@keyframes shake-x { 0%,100% { transform: translateX(0); } 25% { transform: translateX(-6px); } 75% { transform: translateX(6px); } }
.shake-x { animation: shake-x .35s ease; }

/* ===== 滚动条美化(全局, 细条+主题色) ===== */
::-webkit-scrollbar { width: 6px; height: 6px; }
::-webkit-scrollbar-track { background: transparent; }
::-webkit-scrollbar-thumb { background: rgba(128,140,160,.28); border-radius: 99px; }
::-webkit-scrollbar-thumb:hover { background: rgba(128,140,160,.5); }
* { scrollbar-width: thin; scrollbar-color: rgba(128,140,160,.28) transparent; }

/* 隐藏横向滚动条但保留滑动(操作栏类) */
.scrollbar-hide { scrollbar-width: none; -ms-overflow-style: none; }
.scrollbar-hide::-webkit-scrollbar { display: none; }

/* ===== 焦点环(键盘可访问性, 全局统一青色) ===== */
:focus-visible { outline: 2px solid rgb(6 182 212 / .6); outline-offset: 2px; border-radius: 4px; }

/* ===== 列表行悬停微反馈 ===== */
.row-hover { transition: background-color .18s ease; }
.row-hover:hover { background-color: rgb(128 140 160 / .07); }

/* ===== 骨架屏流光(加载态高级感) ===== */
.skeleton-shimmer { position: relative; overflow: hidden; background: rgb(128 140 160 / .08); }
.skeleton-shimmer::after {
  content: ''; position: absolute; inset: 0;
  background: linear-gradient(90deg, transparent, rgb(255 255 255 / .06), transparent);
  transform: translateX(-100%); animation: shimmer-slide 1.6s infinite;
}
@keyframes shimmer-slide { to { transform: translateX(100%); } }

/* ===== 输入框聚焦光晕 ===== */
.t-input__inner, .t-textarea__inner { transition: box-shadow .2s ease, border-color .2s ease; }
.t-is-focused .t-input__inner, .t-input__inner:focus { box-shadow: 0 0 0 3px rgb(6 182 212 / .15); border-color: rgb(6 182 212 / .5); }
</style>
