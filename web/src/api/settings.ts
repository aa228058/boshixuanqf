import { api } from '@/utils/request'
import type { Tenant, Plan, ActivationCode, SupportConfig, Announcement, MessageTemplate, AccountGroup, ListResponse } from '@/types'

export const settingsApi = {
  getProfile: () => api.get<Tenant>('/tenant/profile'),
  activateCode: (code: string) => api.post<Tenant>('/tenant/activate', { code }),
  getPlans: () => api.get<Plan[]>('/plans'),
  getSupport: () => api.get<SupportConfig>('/support'),
  getAnnouncements: () => api.get<Announcement[]>('/announcements'),
  markAnnouncementRead: (id: string) => api.post<void>(`/announcements/${id}/read`),

  // Message Templates
  getTemplates: () => api.get<ListResponse<MessageTemplate>>('/templates'),
  createTemplate: (data: { name: string; content: string }) => api.post<MessageTemplate>('/templates', data),
  updateTemplate: (id: string, data: { name: string; content: string }) => api.put<void>(`/templates/${id}`, data),
  deleteTemplate: (id: string) => api.delete<void>(`/templates/${id}`),

  // Account Groups
  getAccountGroups: () => api.get<ListResponse<AccountGroup>>('/account-groups'),
  createAccountGroup: (data: { name: string; color: string; account_ids: string[] }) => api.post<AccountGroup>('/account-groups', data),
  updateAccountGroup: (id: string, data: { name: string; color: string; account_ids: string[] }) => api.put<void>(`/account-groups/${id}`, data),
  deleteAccountGroup: (id: string) => api.delete<void>(`/account-groups/${id}`),

  // Export
  exportMembers: (taskId: string, format: 'csv' | 'json', groupId?: string) => {
    let url = `/scrape/${taskId}/export?format=${format}`
    if (groupId) url += `&group_id=${groupId}`
    return api.get(url)
  },

  // R119+R120: 通知中心(租户级 SMTP)
  getNotify: () => api.get<{ email: string; settings: Record<string, boolean>; events: Record<string, string>; smtp: Record<string, any>; smtp_configured: boolean }>('/tenant/notify'),
  updateNotify: (email: string, settings: Record<string, boolean>, smtp?: Record<string, any>) => api.put('/tenant/notify', { email, settings, smtp }),
  testNotify: () => api.post<{ message: string }>('/tenant/notify/test'),
}
