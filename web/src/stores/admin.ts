import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { adminApiMod } from '@/api/admin'
import type { AdminProfile } from '@/types'

// F-32 安全修复: admin token 不持久化到 localStorage（8h 有效期，XSS 窃取即后台接管）。
// token 只活在内存；标志位用于界面状态判断，admin 平面无 refresh 端点，
// 硬刷新后需重新登录。旧版本残留 token 在此直接废弃清除。
const SESSION_KEY = 'tg_sender_admin_session'
const LEGACY_TOKEN_KEY = 'tg_sender_admin_token'

function safeSetItem(key: string, val: string): void {
  try { localStorage.setItem(key, val) } catch {}
}
function safeRemoveItem(key: string): void {
  try { localStorage.removeItem(key) } catch {}
}

export const useAdminStore = defineStore('admin', () => {
  const token = ref('') // 纯内存
  const admin = ref<AdminProfile | null>(null)
  const loading = ref(false)

  // 清除旧版本持久化的 admin token
  safeRemoveItem(LEGACY_TOKEN_KEY)

  const isLoggedIn = computed(() => !!token.value)

  async function login(username: string, password: string) {
    loading.value = true
    try {
      const res = await adminApiMod.login(username, password)
      token.value = res.token
      safeSetItem(SESSION_KEY, '1')
      return res
    } finally {
      loading.value = false
    }
  }

  async function logout() {
    // 先通知后端把当前 admin token 的 JTI 加入撤销黑名单（泄露时可立即作废），
    // 再清本地；请求失败也不阻塞本地登出。
    try {
      await adminApiMod.logout()
    } catch { /* 后端不可达时仅本地登出 */ }
    token.value = ''
    admin.value = null
    safeRemoveItem(SESSION_KEY)
  }

  return { token, admin, loading, isLoggedIn, login, logout }
})
