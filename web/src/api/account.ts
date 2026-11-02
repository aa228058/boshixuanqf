import { api } from '@/utils/request'
// R115: PaginationParams 死类型导入已删(全项目零引用)
import type { Account, ListResponse } from '@/types'

export const accountApi = {
  // silent: 后台轮询传 true, 不触发顶部全局进度条(2026-10-22 审计修复)
  list: (silent = false) => api.get<ListResponse<Account>>('/accounts', { silent }),
  getById: (id: string) => api.get<Account>(`/accounts/${id}`),
  create: (phone: string, remark?: string) => api.post<Account>('/accounts', { phone, remark }),
  delete: (id: string) => api.delete<void>(`/accounts/${id}`),
  connect: (id: string) => api.post<void>(`/accounts/${id}/connect`),
  disconnect: (id: string) => api.post<void>(`/accounts/${id}/disconnect`),
  submitCode: (id: string, code: string) => api.post<void>(`/accounts/${id}/code`, { code }),
  setRaffle: (id: string, enabled: boolean) => api.put<{ enabled: boolean }>(`/accounts/${id}/raffle`, { enabled }),
  reconnect: (id: string) => api.post<{ reconnecting: boolean }>(`/accounts/${id}/reconnect`),
  setPool: (id: string, status: 'active' | 'standby') => api.put<{ status: string }>(`/accounts/${id}/pool`, { status }),
  // 资料运营(2026-10-21)
  // R119: 账号能力探测
  runProbe: (id: string) => api.post<any>(`/accounts/${id}/probe`),
  getProbe: (id: string) => api.get<any>(`/accounts/${id}/probe`),
  updateProfile: (id: string, p: { first_name?: string; last_name?: string; username?: string; about?: string }) =>
    api.put<{ ok: boolean }>(`/accounts/${id}/profile`, p),
  cloneProfile: (source: string, targetIds: string[]) =>
    api.post<{ ok: boolean; applied: number; failed: number }>('/accounts/clone-profile', { source, target_ids: targetIds }),
  stats: () => api.get<Record<string, { success: number; failed: number; skipped: number }>>('/accounts/stats'),
  leaveAllGroups: (id: string) => api.post<{ left: number; skipped: number; failed: number; errors?: string[] }>(`/accounts/${id}/leave-all-groups`),
  // R181 群转移: 源号导出邀请链接 → 目标号创建 join_groups 任务(间隔租户可调, 分钟)
  transferGroups: (id: string, targetAccountId: string, intervalMinMinutes = 2, intervalMaxMinutes = 3) =>
    api.post<{
      task: { task_id: string; status: string } | null
      links_total: number
      skipped: Array<{ title: string; reason: string }>
      message?: string
    }>(`/accounts/${id}/transfer-groups`, {
      target_account_id: targetAccountId,
      interval_min_minutes: intervalMinMinutes,
      interval_max_minutes: intervalMaxMinutes,
    }),
  refreshAvatar: (id: string) => api.post<{ refreshing: boolean }>(`/accounts/${id}/avatar/refresh`),
  update2FA: (id: string, current: string, next: string) => api.put<{ updated: boolean }>(`/accounts/${id}/2fa`, { current, next }),
  // 2026-10-21: 恢复调用既有接口 /accounts/profile-batch(平铺 body)——
  // 此前误判 404 是我搜错名字; 后端+路由一直完好, 我的 /batch-profile 重复版已删
  batchUpdateProfile: (ids: string[], data: { first_name?: string; last_name?: string; about?: string; username?: string; remark?: string; random_username?: boolean }) =>
    api.post<{ success: number; failed: number; errors?: string[] }>('/accounts/profile-batch', { account_ids: ids, ...data }),
  batchSetPool: (ids: string[], status: 'active' | 'standby') =>
    api.post<{ success: number; failed: number; errors: string[] }>('/accounts/pool-batch', { account_ids: ids, status }),
  getAuthState: (id: string) => api.get<{ phase: string; hint?: string; code_retries?: number; pwd_retries?: number }>(`/accounts/${id}/auth-state`),
  submit2FA: (id: string, password: string) => api.post<void>(`/accounts/${id}/password`, { password }),
  update: (id: string, data: Partial<Account>) => api.put<Account>(`/accounts/${id}`, data),
  getQuotas: (id: string) => api.get<{ level: string; broadcast_daily: number; join_daily: number; scrape_daily: number; min_interval: number }>(`/accounts/${id}/quotas`),
  getDialogs: (id: string) => api.get<Array<{ id: string; title: string; type: string }>>(`/accounts/${id}/telegram-dialogs`),
  getContacts: (id: string) => api.get<Array<{ id: string; first_name: string; last_name?: string; username?: string }>>(`/accounts/${id}/telegram-contacts`),
}

// ============ 协议号管理 ============

export interface ImportResult {
  created: number
  failed: number
  errors: string[]
  account_ids: string[]
}

/** 批量导入协议号（每行一个 session 字符串） */
export const importSessions = (sessions: string[], remark: string) =>
  api.post<ImportResult>('/accounts/import', { sessions, remark })

/** 修改账号资料：TG 名字/签名/用户名 + 本地备注 */
export const updateProfile = (accountId: string, data: {
  first_name?: string
  last_name?: string
  about?: string
  username?: string
  twofa?: string
  remark?: string
}) => api.put<{ ok: boolean }>(`/accounts/${accountId}/profile`, data)

/** 上传账号头像（multipart，字段名 photo） */
export const uploadAvatar = (accountId: string, file: File) => {
  const fd = new FormData()
  fd.append('photo', file)
  return api.post<{ ok: boolean }>(`/accounts/${accountId}/avatar`, fd as any)
}

/** 上传 .session 文件批量导入（Telethon SQLite 格式，可多选） */
export const importSessionFiles = (files: File[], remark: string) => {
  const fd = new FormData()
  files.forEach(f => fd.append('files', f))
  fd.append('remark', remark)
  return api.post<ImportResult>('/accounts/import-files', fd as any)
}

/** 能力体检(实测安全项 + 风控历史推断发送项) */
export const checkupAccount = (accountId: string): Promise<Record<string, any>> =>
  api.post(`/accounts/${accountId}/checkup`)

/** 向官方 @SpamBot 提交申诉 */
export const appealAccount = (accountId: string): Promise<{ ok: boolean }> =>
  api.post(`/accounts/${accountId}/appeal`)

/** 设置账号代理(socks5://user:pass@host:port, 空串=直连) */
export const setAccountProxy = (accountId: string, proxyUrl: string): Promise<{ ok: boolean }> =>
  api.put(`/accounts/${accountId}/proxy`, { proxy_url: proxyUrl })

/** 代理连通性检测 */
export const testAccountProxy = (accountId: string): Promise<{ ok: boolean; latency_ms?: number; proxy?: string; error?: string }> =>
  api.post(`/accounts/${accountId}/proxy/test`)
