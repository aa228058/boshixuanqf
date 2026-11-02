import { adminApi, doFetchWithRetry, ADMIN_BASE_URL } from '@/utils/request'
import { useAdminStore } from '@/stores/admin'
import type { Tenant, Plan, ActivationCode, SupportConfig, ProbeConfig, Announcement, AdminStats, SystemMetrics, ListResponse } from '@/types'


function toQueryString(params?: Record<string, unknown>): string {
  if (!params) return ''
  const usp = new URLSearchParams()
  for (const [key, val] of Object.entries(params)) {
    if (val !== undefined && val !== null && val !== '') usp.append(key, String(val))
  }
  const qs = usp.toString()
  return qs ? `?${qs}` : ''
}

export const adminApiMod = {
  // 认证
  login: (username: string, password: string) => adminApi.post<{ token: string }>('/admin/auth/login', { username, password }),
  logout: () => adminApi.post<{ revoked: boolean }>('/admin/auth/logout'),

  // 仪表盘
  getStats: () => adminApi.get<AdminStats>('/admin/stats'),
  getSystemMetrics: () => adminApi.get<SystemMetrics>('/admin/system'),

  // 租户管理
  listTenants: (params?: Record<string, unknown>) => adminApi.get<ListResponse<Tenant>>(`/admin/tenants${toQueryString(params)}`),
  getTenant: (id: string) => adminApi.get<Tenant>(`/admin/tenants/${id}`),
  createTenant: (data: Record<string, unknown>) => adminApi.post<Tenant>('/admin/tenants', data),
  updateTenant: (id: string, data: Record<string, unknown>) => adminApi.put<Tenant>(`/admin/tenants/${id}`, data),
  deleteTenant: (id: string) => adminApi.delete<void>(`/admin/tenants/${id}`),
  resetApiKey: (id: string) => adminApi.post<{ api_key: string }>(`/admin/tenants/${id}/reset-key`),

  // 卡密管理
  listCodes: (params?: Record<string, unknown>) => adminApi.get<ListResponse<ActivationCode>>(`/admin/codes${toQueryString(params)}`),
  generateCodes: (data: { plan_id: string; count: number; timing_type: string; expires_days?: number }) => adminApi.post<ActivationCode[]>('/admin/codes', data),
  revokeCode: (id: string) => adminApi.delete<void>(`/admin/codes/${id}`),
  // P0-05: 导出接口返回纯文本(CSV), 通用 adminApi.get 内部一律 response.json(),
  // 纯文本解析失败被吞成 {} —— 下载内容是 [object Object]。改走底层 fetch 读原始文本。
  exportCodes: async () => {
    const adminStore = useAdminStore()
    const res = await doFetchWithRetry(ADMIN_BASE_URL, '/admin/codes/export', {}, adminStore.token)
    if (!res.ok) throw new Error(`导出失败 (${res.status})`)
    return res.text()
  },
  getCode: (id: string) => adminApi.get<ActivationCode>(`/admin/codes/${id}`),
  batchRevokeCodes: (data: { ids: string[] }) => adminApi.post<void>('/admin/codes/batch-revoke', data),
  batchDeleteCodes: (data: { ids: string[] }) => adminApi.post<void>('/admin/codes/batch-delete', data),

  // 客服配置
  getSupport: () => adminApi.get<SupportConfig>('/admin/support'),
  updateSupport: (data: Partial<SupportConfig>) => adminApi.put<SupportConfig>('/admin/support', data),

  // R119: 账号能力探测配置(全局)
  getProbe: () => adminApi.get<ProbeConfig>('/admin/probe'),
  updateProbe: (data: Partial<ProbeConfig>) => adminApi.put<ProbeConfig>('/admin/probe', data),

  // 公告管理
  listAnnouncements: () => adminApi.get<ListResponse<Announcement>>('/admin/announcements'),
  createAnnouncement: (data: Partial<Announcement>) => adminApi.post<Announcement>('/admin/announcements', data),
  updateAnnouncement: (id: string, data: Partial<Announcement>) => adminApi.put<Announcement>(`/admin/announcements/${id}`, data),
  deleteAnnouncement: (id: string) => adminApi.delete<void>(`/admin/announcements/${id}`),

  // 套餐配置
  listPlans: () => adminApi.get<ListResponse<Plan>>('/admin/plans'),
  updatePlan: (id: string, data: Partial<Plan>) => adminApi.put<Plan>(`/admin/plans/${id}`, data),

  // 审计日志
  getAuditLogs: (params?: Record<string, unknown>) => adminApi.get<ListResponse<Record<string, unknown>>>(`/admin/audit-logs${toQueryString(params)}`),
}
