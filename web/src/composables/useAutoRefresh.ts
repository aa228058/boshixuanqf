import { onMounted, onUnmounted } from 'vue'

// 页面可见时自动轮询刷新数据; 切到后台自动暂停, 回来自动续上
// 用法: useAutoRefresh(fetchTasks, 5000)
export function useAutoRefresh(fn: () => void | Promise<void>, intervalMs = 5000) {
  let timer: ReturnType<typeof setInterval> | null = null
  const tick = () => {
    if (document.visibilityState === 'visible') fn()
  }
  const start = () => {
    if (!timer) timer = setInterval(tick, intervalMs)
  }
  const stop = () => {
    if (timer) { clearInterval(timer); timer = null }
  }
  const onVis = () => {
    if (document.visibilityState === 'visible') fn() // 回到前台立即刷一次
  }
  onMounted(() => {
    document.addEventListener('visibilitychange', onVis)
    start()
  })
  onUnmounted(() => {
    document.removeEventListener('visibilitychange', onVis)
    stop()
  })
}
