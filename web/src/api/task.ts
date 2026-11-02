import { api } from '@/utils/request'
import type { Task, TaskLog, ListResponse, PaginationParams } from '@/types'

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

// 链接采集结果(与后端 /scrape-links 接口对应)
export interface ScrapedLink {
  link_id: string
  tenant_id: string
  account_id: string
  link_type: string
  link_hash: string
  full_url: string
  source_chat_id: number
  source_chat_title: string
  resolved_chat_id?: number
  resolved_title?: string
  member_count?: number
  status: string
  created_at: string
  resolved_at?: string
}

// (注: 此处原有第二份完全相同的 ScrapedLink 定义, 2026-10-22 审计已删)

// R128: 第二份重复的 ScrapedLink 定义已删(审计注释与实际不符——两份都在, 删下方这份, 上方 17 行处保留)
export const taskApi = {
  list: (params?: { status?: string; account_id?: string; type?: string } & PaginationParams) => {
    return api.get<ListResponse<Task>>(`/tasks${toQueryString(params as Record<string, unknown>)}`)
  },
  deleteTaskLogs: (taskId: string) => api.delete(`/tasks/${taskId}/logs`),
  // R116: cleanupLogs 封装已删(零页面调用; 对应后端 /logs/cleanup 同步移除)
  listScrapedLinks: (params: { account_id: string; since?: string; page?: number; page_size?: number }) =>
    api.get<{ items: ScrapedLink[]; total: number }>(`/scrape-links${toQueryString(params as Record<string, unknown>)}`),
  getById: (id: string) => api.get<Task>(`/tasks/${id}`),
  create: (data: Record<string, unknown>) => api.post<Task>('/tasks', data),
  // 重试: scope=failed 仅重发失败目标(克隆新任务, 续传防重保证不重复), 否则整任务重跑
  retry: (id: string, scope?: 'failed') => api.post<void>(`/tasks/${id}/retry${scope ? `?scope=${scope}` : ''}`),
  pause: (id: string) => api.post<void>(`/tasks/${id}/pause`),
  resume: (id: string) => api.post<void>(`/tasks/${id}/resume`),
  cancel: (id: string) => api.post<void>(`/tasks/${id}/cancel`),
  // 发送前目标预检: 批量校验目标有效性, 返回有效与无效(附原因)目标
  validateTargets: (targetIds: string[]) => api.post<{ total: number; valid: string[]; invalid: Array<{ target: string; reason: string }> }>('/tasks/validate-targets', { target_ids: targetIds }),
  delete: (id: string) => api.delete<void>(`/tasks/${id}`),
  batchCancel: (taskIds: string[]) => api.post<{ cancelled: number }>('/tasks/batch/cancel', { task_ids: taskIds }),
  batchDelete: (taskIds: string[]) => api.post<{ deleted: number }>('/tasks/batch/delete', { task_ids: taskIds }),
  getLogs: (id: string) => api.get<TaskLog[]>(`/tasks/${id}/logs`),
  getAccountLogs: (accountId: string, page = 1, pageSize = 50) =>
    api.get<ListResponse<TaskLog>>(`/tasks/logs${toQueryString({ account_id: accountId, page, page_size: pageSize })}`),
  getProgress: (id: string) => api.get<{ progress: number; status: string; total: number; completed: number }>(`/tasks/${id}/progress`),
  update: (id: string, data: Record<string, unknown>) => api.put<Task>(`/tasks/${id}`, data),
  // R128 清空全部抽奖参与记录
  clearRaffleResults: () => api.delete<{ message: string }>('/raffle/results'),
}
