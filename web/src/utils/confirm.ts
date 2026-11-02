// 全局 promise 化确认弹窗: 替代原生 confirm(移动端 WebView 里原生 confirm 可能不渲染但 JS 已阻塞)
//
// 实现说明(重要):
// 早期版本用 createVNode + render() 手动挂载到 body —— 这样挂载的组件
// 脱离 Vue app 上下文(没有 vfm 插件/provide 链), vue-final-modal 内部
// 状态机失效, 弹窗显示出来但点击无响应("无法点击" bug 的根因)。
//
// 现在改为: App 树内常驻一个 ConfirmHost 组件(见 components/ui/ConfirmHost.vue),
// ask() 只往这里写入响应式状态, 由 ConfirmHost 渲染弹窗 —— 全链路都在
// app 上下文里, vfm 正常工作。
import { reactive } from 'vue'

export interface ConfirmRequest {
  visible: boolean
  title: string
  message: string
  detail?: string
  danger: boolean
  confirmText: string
  cancelText: string
  resolve?: (v: boolean) => void
}

// 模块级响应式状态: ConfirmHost 直接消费
export const confirmState = reactive<ConfirmRequest>({
  visible: false,
  title: '请确认',
  message: '',
  detail: undefined,
  danger: true,
  confirmText: '确认',
  cancelText: '取消',
  resolve: undefined,
})

export function ask(
  message: string,
  opts?: { title?: string; detail?: string; danger?: boolean; confirmText?: string; cancelText?: string }
): Promise<boolean> {
  // 上一个弹窗还没关(极端连点), 先把旧的取消掉, 避免 resolve 泄漏
  confirmState.resolve?.(false)

  return new Promise<boolean>((resolve) => {
    confirmState.title = opts?.title ?? '请确认'
    confirmState.message = message
    confirmState.detail = opts?.detail
    confirmState.danger = opts?.danger ?? true
    confirmState.confirmText = opts?.confirmText ?? '确认'
    confirmState.cancelText = opts?.cancelText ?? '取消'
    confirmState.resolve = resolve
    confirmState.visible = true
  })
}

// 供 ConfirmHost 调用: 关闭并回传结果
export function settle(result: boolean) {
  confirmState.visible = false
  const r = confirmState.resolve
  confirmState.resolve = undefined
  r?.(result)
}
