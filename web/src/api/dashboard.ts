import { api } from '@/utils/request'
import type { DashboardStats, RiskEvent, AuditLog, ListResponse } from '@/types'

function toQueryString(params?: Record<string, unknown>): string {
  if (!params) return ''
  const filtered: Record<string, string> = {}
  for (const [key, val] of Object.entries(params)) {
    if (val !== undefined && val !== null && val !== '') {
      filtered[key] = String(val)
    }
  }
  const qs = new URLSearchParams(filtered).toString()
  return qs ? '?' + qs : ''
}

export const dashboardApi = {
  getStats: (accountId?: string) => api.get<DashboardStats>(`/dashboard/stats${accountId ? `?account_id=${accountId}` : ''}`),
  getRiskEvents: (params?: { start_time?: string; end_time?: string; limit?: number; offset?: number }) => {
    return api.get<ListResponse<RiskEvent>>(`/dashboard/risk-events${toQueryString(params as Record<string, unknown>)}`)
  },
  getAuditLogs: (params?: { start_time?: string; end_time?: string; limit?: number; offset?: number }) => {
    return api.get<ListResponse<AuditLog>>(`/dashboard/activities${toQueryString(params as Record<string, unknown>)}`)
  },
}
