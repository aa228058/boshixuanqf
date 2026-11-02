// Toast 工具: 基于 vue-sonner 的精美提示, 保留原 API 全站零改动
// 原 toasts 数组保留为空(兼容旧引用), 实际渲染由 App.vue 的 <Toaster /> 完成
import { ref } from 'vue'
import { toast as sonner } from 'vue-sonner'

export interface Toast {
  id: string
  type: 'success' | 'error' | 'info' | 'warning'
  title: string
  message?: string
  duration?: number
  // 撤销按钮(删除类操作 5 秒撤销窗)
  action?: { label: string; onClick: () => void }
}

// 兼容导出(不再驱动 UI)
export const toasts = ref<Toast[]>([])

// 重复抑制: 2 秒内相同内容只弹一次(保留原有行为)
const recentAdds = new Map<string, number>()
const DEDUPE_WINDOW_MS = 2000

const typeMap: Record<string, 'success' | 'error' | 'info' | 'warning'> = {
  success: 'success',
  error: 'error',
  info: 'info',
  warning: 'warning',
}

export function useToast() {
  function add(t: Omit<Toast, 'id'>) {
    const key = `${t.type}|${t.title}|${t.message || ''}`
    const now = Date.now()
    const last = recentAdds.get(key)
    if (last && now - last < DEDUPE_WINDOW_MS) return
    recentAdds.set(key, now)
    if (recentAdds.size > 50) {
      for (const [k, ts] of recentAdds) {
        if (now - ts > DEDUPE_WINDOW_MS) recentAdds.delete(k)
      }
    }
    const fn = sonner[typeMap[t.type] || 'info']
    fn(t.title, {
      description: t.message,
      duration: t.duration || 4000,
      action: t.action,
    })
  }

  function remove(_id: string) {
    sonner.dismiss()
  }

  return { add, remove }
}

// R117: removeToast/toastWithUndo 已删(零外部引用; 底部 toast 具名导出保留——
// ToastContainer 时代 removeToast 曾以 as remove 引用, 组件 r93 已删, 引用随之消失)
export function toast(t: Omit<Toast, 'id'>) {
  useToast().add(t)
}
