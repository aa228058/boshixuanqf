// 全局变更总线：任何非 GET 请求成功后广播，相关 store 自动失效重刷。
// 解决"每个页面自己记得刷新"的漏刷问题——新增功能不用手动接刷新。

export type InvalidateFn = () => void

const registry: Array<{ pattern: RegExp; fn: InvalidateFn }> = []

/** 注册一个 URL 模式与对应的失效回调 */
export function onMutation(pattern: RegExp, fn: InvalidateFn): void {
  registry.push({ pattern, fn })
}

/** 广播变更事件（request.ts 内部调用） */
export function emitMutation(method: string, url: string): void {
  if (method.toUpperCase() === 'GET') return
  for (const { pattern, fn } of registry) {
    if (pattern.test(url)) {
      try { fn() } catch { /* 单个订阅者失败不影响其他 */ }
    }
  }
}
