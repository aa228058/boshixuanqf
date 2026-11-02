import { api } from '@/utils/request'
import type { MonitorRule, MonitorMessage, ListResponse } from '@/types'

export const monitorApi = {
  listRules: () => api.get<ListResponse<MonitorRule>>('/monitor/rules'),
  getRule: (id: string) => api.get<MonitorRule>(`/monitor/rules/${id}`),
  createRule: (data: Partial<MonitorRule>) => api.post<MonitorRule>('/monitor/rules', data),
  updateRule: (id: string, data: Partial<MonitorRule>) => api.put<MonitorRule>(`/monitor/rules/${id}`, data),
  toggleRule: (id: string, data?: { is_active: boolean }) => api.post<void>(`/monitor/rules/${id}/toggle`, data || {}),
  deleteRule: (id: string) => api.delete<void>(`/monitor/rules/${id}`),
  // 修正：此前 limit 参数收了但没拼进 query（分页/限量实际不生效）
  getMessages: (params?: { rule_id?: string; limit?: number }) => {
    const sp = new URLSearchParams()
    if (params?.rule_id) sp.set('rule_id', params.rule_id)
    if (params?.limit) sp.set('limit', String(params.limit))
    const qs = sp.toString()
    return api.get<ListResponse<MonitorMessage>>(`/monitor/messages${qs ? '?' + qs : ''}`)
  },
  startMonitoring: (accountId: string) => api.post<void>(`/monitor/start/${accountId}`),
  stopMonitoring: (accountId: string) => api.post<void>(`/monitor/stop/${accountId}`),
}
