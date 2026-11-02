import type { Component } from 'vue'
import {
  Send, Users, MessageSquare, Link2, Shield, Eye,
  CheckCircle, Clock, PauseCircle, PlayCircle, XCircle, AlertTriangle,
  Radar, UserPlus,
} from 'lucide-vue-next'

// ==================== Account Status ====================
export const accountStatusClass = (s: string): string => {
  const map: Record<string, string> = {
    online: 'bg-emerald/10 text-emerald',
    offline: 'bg-text-muted/10 text-text-muted',
    pending_auth: 'bg-amber/10 text-amber',
    authenticated: 'bg-cyan/10 text-cyan',
    restricted: 'bg-rose/10 text-rose',
    banned: 'bg-rose/10 text-rose',
  }
  return map[s] || 'bg-text-muted/10 text-text-muted'
}

export const accountStatusLabel = (s?: string): string => {
  // R124: 入参判空 —— 列表混入空项时返回占位而非渲染 undefined
  if (!s) return '—'
  const map: Record<string, string> = {
    online: '在线',
    offline: '离线',
    pending_auth: '待认证',
    authenticated: '已认证',
    restricted: '受限',
    banned: '已封禁',
    session_revoked: '需重新登录',
  }
  return map[s] || s
}

// ==================== Task Status ====================
export const taskStatusClass = (s: string): string => {
  const map: Record<string, string> = {
    pending: 'bg-amber/10 text-amber',
    queued: 'bg-cyan/10 text-cyan',
    running: 'bg-emerald/10 text-emerald',
    paused: 'bg-amber/10 text-amber',
    completed: 'bg-emerald/10 text-emerald',
    failed: 'bg-rose/10 text-rose',
    cancelled: 'bg-text-muted/10 text-text-muted',
  }
  return map[s] || 'bg-text-muted/10 text-text-muted'
}

export const taskStatusLabel = (s: string): string => {
  const map: Record<string, string> = {
    pending: '待执行',
    queued: '队列中',
    running: '执行中',
    paused: '已暂停',
    completed: '已完成',
    failed: '失败',
    cancelled: '已取消',
  }
  return map[s] || s
}

// ==================== Task Type ====================
export const taskTypeLabel = (t: string): string => {
  const map: Record<string, string> = {
    broadcast: '群发消息',
    scrape_members: '成员采集',
    scrape_links: '链接采集',
  scheduled_broadcast: '定时广播',
    join_groups: '加群任务',
    monitor_forward: '监控转发',
    monitor_reply: '监控回复',
  }
  return map[t] || '未知任务'
}

export const taskTypeIcon = (t: string): Component => {
  const map: Record<string, Component> = {
    broadcast: Send,
    scrape_members: Users,
    scrape_links: Link2,
    join_groups: Users,
    monitor_forward: Eye,
    monitor_reply: MessageSquare,
    // P2-17: 补上缺失的三个类型——原来落到 AlertTriangle(警告三角)误导用户
    scheduled_broadcast: Clock,
    monitor_scrape: Radar,
    invite_members: UserPlus,
  }
  return map[t] || AlertTriangle
}

// ==================== Monitor Rule ====================
export const matchTypeLabel = (t: string): string => {
  const map: Record<string, string> = {
    contains: '包含',
    regex: '正则',
    exact: '精确匹配',
    from_user: '来自用户',
  }
  return map[t] || t
}

// ==================== Plan / Tenant ====================
export const planLabel = (p: string): string => {
  const map: Record<string, string> = {
    free: '免费版',
    basic: '基础版',
    pro: '专业版',
    enterprise: '企业版',
  }
  return map[p] || p
}

export const tenantStatusClass = (s: string): string => {
  const map: Record<string, string> = {
    active: 'bg-emerald/10 text-emerald',
    inactive: 'bg-text-muted/10 text-text-muted',
    suspended: 'bg-rose/10 text-rose',
  }
  return map[s] || ''
}

export const tenantStatusLabel = (s: string): string => {
  return s === 'active' ? '正常' : s === 'suspended' ? '已停用' : '未激活'
}

// ==================== Code Status ====================
export const codeStatusClass = (s: string): string => {
  const map: Record<string, string> = {
    unused: 'bg-emerald/10 text-emerald',
    used: 'bg-text-muted/10 text-text-muted',
    expired: 'bg-amber/10 text-amber',
    revoked: 'bg-rose/10 text-rose',
  }
  return map[s] || ''
}

// ==================== Date Format ====================
export const formatDate = (v: string): string => {
  if (!v) return '-'
  const d = new Date(v)
  if (isNaN(d.getTime())) return 'Invalid Date'
  return d.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' })
}

export const formatDateTime = (v: string): string => {
  if (!v) return '-'
  const d = new Date(v)
  if (isNaN(d.getTime())) return 'Invalid Date'
  return d.toLocaleString('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
    hour12: false
  })
}

export const formatRelativeTime = (v: string): string => {
  if (!v) return '-'
  const d = new Date(v)
  if (isNaN(d.getTime())) return 'Invalid Date'
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHour = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHour / 24)

  if (diffSec < 60) return '刚刚'
  if (diffMin < 60) return `${diffMin}分钟前`
  if (diffHour < 24) return `${diffHour}小时前`
  if (diffDay < 7) return `${diffDay}天前`
  return d.toLocaleDateString('zh-CN')
}

export const formatPrice = (v: number): string => {
  if (v === undefined || v === null) return '-'
  return '¥' + v.toFixed(2)
}

export const formatNumber = (v: number): string => {
  if (v === undefined || v === null) return '-'
  return v.toLocaleString('zh-CN')
}
