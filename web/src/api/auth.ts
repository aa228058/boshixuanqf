import { api, adminApi } from '@/utils/request'
import type { Tenant } from '@/types'

export const authApi = {
  me: () => api.get<Tenant>('/auth/me'),
  register: (data: { username: string; name: string; password: string; code: string }) => api.post<{ tenant_id: string; username: string; name: string; api_key: string; message: string }>('/auth/register', data),
  login: (username: string, password: string) => api.post<{ token: string; refresh_token: string; tenant_id: string; tenant_name: string; plan_id: string; plan_features: Record<string, boolean>; max_accounts: number; max_concurrent_tasks: number }>('/auth/login', { username, password }),
  logout: () => api.post<{ message: string }>('/auth/logout'),
  /** 卡密续费：账号+密码+卡密，延长订阅并同步套餐 */
  renew: (username: string, password: string, code: string) =>
    api.post<{ message: string; expires_at: string; plan_id: string }>('/auth/renew', { username, password, code }),
  refresh: () => api.post<{ token: string }>('/auth/refresh', {}, { credentials: 'include' }),
  // Admin login uses adminApi with empty baseURL to match backend /admin/* routes
  adminLogin: (username: string, password: string) => adminApi.post<{ token: string }>('/admin/auth/login', { username, password }),
}
