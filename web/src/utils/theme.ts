// 主题三态(R126): normal 正常(示例3 靛蓝) / eye 护眼(示例1 陶土) / dark 深色(示例2 玻璃紫)
// 结构统一示例2 语言, 三套只是配色。html class: 无 / theme-eye / theme-dark

export type ThemeMode = 'normal' | 'eye' | 'dark'

const THEME_KEY = 'tg_theme_mode'

const htmlEl = () => document.documentElement

function readStored(): ThemeMode {
  try {
    const v = localStorage.getItem(THEME_KEY)
    if (v === 'eye' || v === 'dark') return v
    // 兼容旧的 dark 标记
    if (localStorage.getItem('theme') === 'dark' || localStorage.getItem('tg_theme_dark') === '1') return 'dark'
  } catch { /* SSR/隐私模式 */ }
  return 'normal'
}

export function apply(mode: ThemeMode) {
  const el = htmlEl()
  el.classList.toggle('theme-eye', mode === 'eye')
  el.classList.toggle('theme-stripe', mode === 'eye')   // 兼容旧样式文件里的 theme-stripe 选择器
  el.classList.toggle('theme-dark', mode === 'dark')
  // 浅色背景不用光晕; 深色光晕由 --t-glow 接管(body 的 background-image 已 var 化)
  try { localStorage.setItem(THEME_KEY, mode) } catch { /* 忽略 */ }
  // 通知(部分组件监听 html class 自行响应, 无需额外事件)
}

export function currentTheme(): ThemeMode {
  const el = htmlEl()
  if (el.classList.contains('theme-dark')) return 'dark'
  if (el.classList.contains('theme-eye') || el.classList.contains('theme-stripe')) return 'eye'
  return 'normal'
}

// 三态循环: normal → eye → dark → normal
export function toggleTheme() {
  const order: ThemeMode[] = ['normal', 'eye', 'dark']
  const next = order[(order.indexOf(currentTheme()) + 1) % order.length]
  apply(next)
  return next
}

// 兼容旧 API(旧代码调 isDark/toggleDarkMode)
export function isDark(): boolean {
  return currentTheme() === 'dark'
}

export function initTheme() {
  apply(readStored())
}
