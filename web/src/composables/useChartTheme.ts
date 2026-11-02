// Phase 6 数据可视化: 图表主题统一取色。
// ECharts 是 canvas 渲染, 不读 CSS 变量——此前各图表硬编码颜色(text-cyan 类名/
// #10b981 hex), 切主题/深浅模式时图表不跟随。本 composable 从已应用的 CSS 变量
// 读出当前主题的实际色值, 图表构建 option 时统一从这里取。
import { ref, watch } from 'vue'

const PALETTE_VARS = [
  '--chart-1', '--chart-2', '--chart-3', '--chart-4', '--chart-5', '--chart-6',
] as const

function readVar(name: string, fallback: string): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

export function useChartTheme() {
  // 主题切换会改 documentElement 的 class(theme-stripe/theme-dark),
  // MutationObserver 监听 class 变化即刷新色板(响应式, 切主题图表自动跟随)。
  const palette = ref<string[]>([
    '#22d3ee', '#34d399', '#fbbf24', '#f87171', '#a78bfa', '#f472b6',
  ])
  const textPrimary = ref('#f1f5f9')
  const textMuted = ref('#94a3b8')
  const border = ref('#232a3a')

  function refresh() {
    palette.value = PALETTE_VARS.map((v, i) =>
      readVar(v, ['#22d3ee', '#34d399', '#fbbf24', '#f87171', '#a78bfa', '#f472b6'][i]))
    textPrimary.value = readVar('--text-primary', '#f1f5f9')
    textMuted.value = readVar('--text-muted', '#94a3b8')
    border.value = readVar('--border', '#232a3a')
  }

  let observer: MutationObserver | null = null
  function start() {
    refresh()
    observer = new MutationObserver(refresh)
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  }
  function stop() {
    observer?.disconnect()
    observer = null
  }

  return { palette, textPrimary, textMuted, border, start, stop }
}
