import { api } from '@/utils/request'
import type { Group, Member, ListResponse, PaginationParams } from '@/types'

export const groupApi = {
  list: (accountId?: string, params?: PaginationParams) => {
    const query = new URLSearchParams()
    if (accountId) query.append('account_id', accountId)
    if (params?.page) query.append('page', String(params.page))
    if (params?.page_size) query.append('page_size', String(params.page_size))
    const qs = query.toString()
    return api.get<ListResponse<Group>>(`/groups${qs ? '?' + qs : ''}`)
  },
  getById: (id: string) => api.get<Group>(`/groups/${id}`),
  create: (data: Partial<Group>) => api.post<Group>('/groups', data),
  update: (id: string, data: Partial<Group>) => api.put<Group>(`/groups/${id}`, data),
  delete: (id: string) => api.delete<void>(`/groups/${id}`),
  sync: (accountId: string) => api.post<void>('/groups/sync', { account_id: accountId }),
  // 批量同步: 后台串行同步全部在线账号(后端3秒间隔防风控)
  syncAll: () => api.post<void>('/groups/sync-all'),
  getMembers: (id: string, params?: PaginationParams) => api.get<ListResponse<Member>>(`/groups/${id}/members${params?.page ? `?page=${params.page}&page_size=${params.page_size || 20}` : ''}`),
  // 全局联系人列表(私聊选择器): 后端 GET /members?account_id=, 读 contacts 表缓存
  listMembersGlobal: (accountId: string) => api.get<Member[]>(`/members?account_id=${accountId}`),
  scrapeMembers: (id: string, accountId: string) => api.post<{ task_id: string }>(`/groups/${id}/scrape`, { account_id: accountId }),
}
