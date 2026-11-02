import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { authApi } from '@/api/auth'
import type { Tenant } from '@/types'

// F-20 安全修复: access token 不再持久化到 localStorage（CSP 含 unsafe-inline，
// XSS 即可窃取长寿命令牌）。token 只活在内存；localStorage 仅保留一个不含密钥的
// "登录标志位"，供硬刷新后凭 HttpOnly refresh Cookie 静默恢复会话。
// 旧版本写入的 token 键在登录/登出/恢复时统一清理。
const SESSION_KEY = 'tg_sender_session'
const LEGACY_TOKEN_KEYS = ['tg_sender_token', 'tg_sender_refresh_token']

// 安全读写 localStorage（兼容隐私模式/禁用 localStorage 的浏览器）
function safeGetItem(key: string): string | null {
  try { return localStorage.getItem(key) } catch { return null }
}
function safeSetItem(key: string, val: string): void {
  try { localStorage.setItem(key, val) } catch {}
}
function safeRemoveItem(key: string): void {
  try { localStorage.removeItem(key) } catch {}
}

function clearLegacyTokenKeys() {
  LEGACY_TOKEN_KEYS.forEach(safeRemoveItem)
}

export const useAuthStore = defineStore('auth', () => {
  const token = ref('') // 纯内存，绝不落 localStorage
  const tenant = ref<Tenant | null>(null)
  const loading = ref(false)

  const isLoggedIn = computed(() => !!token.value)
  // 是否存在可恢复会话：标志位在即可用 refresh Cookie 续签
  const hasSession = computed(() => !!safeGetItem(SESSION_KEY))

  async function login(username: string, password: string) {
    loading.value = true
    try {
      const res = await authApi.login(username, password)
      token.value = res.token
      safeSetItem(SESSION_KEY, '1')
      clearLegacyTokenKeys()
      // 注意: 不在这里调 me() —— 登录后 Layout.onMounted 会统一调 refreshTenant(),
      // 避免同接口 60 秒内重复请求, 省一次串行 RTT
      return res
    } finally {
      loading.value = false
    }
  }

  async function logout() {
    // 本地状态同步先清: 路由守卫和界面立即感知, 不等待退出接口返回
    token.value = ''
    tenant.value = null
    safeRemoveItem(SESSION_KEY)
    clearLegacyTokenKeys()
    try {
      await authApi.logout()
    } catch (e) {
      console.error('logout api failed:', e)
    }
  }

  // 刷新走 HttpOnly Cookie(credentials), refresh_token 不出现在前端任何存储
  async function refresh(): Promise<string> {
    const res = await authApi.refresh()
    token.value = res.token
    safeSetItem(SESSION_KEY, '1')
    return res.token
  }

  // 硬刷新后恢复会话：登录标志位在且内存无 token 时，用 refresh Cookie 静默换发。
  // 失败（Cookie 过期/被撤销）则清除标志位，守卫据此踢回登录页。
  async function restoreSession(): Promise<boolean> {
    if (token.value) return true
    if (!hasSession.value) return false
    try {
      await refresh()
      return true
    } catch {
      safeRemoveItem(SESSION_KEY)
      return false
    }
  }

  async function refreshTenant() {
    if (!token.value) return
    try {
      const meRes = await authApi.me()
      tenant.value = meRes
    } catch (e) {
      console.error('刷新租户信息失败:', e)
    }
  }

  return { token, tenant, loading, isLoggedIn, hasSession, login, logout, refresh, refreshTenant, restoreSession }
})
