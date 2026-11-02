import { ref } from 'vue'

// ===== 复制反馈(交互层: 原地变勾, 不弹 Toast 打扰) =====
// 用法: const { copied, copy } = useCopy()
//   <button @click="copy(apiKey)">{{ copied ? '已复制 ✓' : '复制' }}</button>
export function useCopy(resetMs = 1600) {
  const copied = ref(false)
  let timer: ReturnType<typeof setTimeout> | null = null

  async function copy(text: string): Promise<boolean> {
    try {
      await navigator.clipboard.writeText(text)
      copied.value = true
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => { copied.value = false }, resetMs)
      return true
    } catch {
      // 剪贴板 API 不可用(非安全上下文)时退回 execCommand
      try {
        const ta = document.createElement('textarea')
        ta.value = text
        ta.style.position = 'fixed'
        ta.style.opacity = '0'
        document.body.appendChild(ta)
        ta.select()
        document.execCommand('copy')
        document.body.removeChild(ta)
        copied.value = true
        if (timer) clearTimeout(timer)
        timer = setTimeout(() => { copied.value = false }, resetMs)
        return true
      } catch {
        return false
      }
    }
  }

  return { copied, copy }
}
