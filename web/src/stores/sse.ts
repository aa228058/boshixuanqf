import { ref } from 'vue'

// SSE 全局连接状态(交互层完善: 租户随时可见实时连接是否健康)
// connected=绿色  reconnecting=黄色(第N次重试)  disconnected=红色
export const sseStatus = ref<'connected' | 'reconnecting' | 'disconnected'>('disconnected')
export const sseRetryCount = ref(0)

export function setSseStatus(s: 'connected' | 'reconnecting' | 'disconnected') {
  sseStatus.value = s
  if (s === 'connected') sseRetryCount.value = 0
}
export function bumpSseRetry() {
  sseRetryCount.value++
}
