import { api } from '@/utils/request'
import type { SystemGroup } from '@/types/models'

export interface GroupLibraryResult {
  list: SystemGroup[]
  total: number
  page: number
  page_size: number
}

export interface GroupLibraryQuery {
  keyword?: string
  gtype?: string
  status?: string
  source?: string
  page?: number
  page_size?: number
}

// 平台级群组库(找群): 所有租户采集汇总, 登录即可见
export function getGroupLibrary(params: GroupLibraryQuery): Promise<GroupLibraryResult> {
  return api.get('/group-library', { params: { ...params } })
}

// 勾选批量加入: 对每个账号创建一个加群任务
export function joinLibraryGroups(accountIds: string[], sgIds: string[]): Promise<{ created: number; links: number; failed_err: string }> {
  return api.post('/group-library/join', { account_ids: accountIds, sg_ids: sgIds })
}

// 关键词搜群: 借本租户在线账号做 TG 全局搜索, 结果直接入库并返回
export function searchLibraryGroups(keyword: string, accountId?: string): Promise<{ list: SystemGroup[]; count: number }> {
  return api.post('/group-library/search', { keyword, account_id: accountId || undefined })
}
