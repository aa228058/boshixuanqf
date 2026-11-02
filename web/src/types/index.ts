// ============ 基础类型 ============
// 2026-09-24 让 TS 认识 import.meta.env（Vite 环境变量），不改动任何逻辑
/// <reference types="vite/client" />

// 2026-09-26 后端 AuditLog 结构
export interface AuditLog {
  log_id: string
  tenant_id: string
  user_id?: string
  action: string
  target?: string
  detail?: string
  ip?: string
  created_at: string
}

export interface Admin {
  admin_id: string
  username: string
  role: string
  status: string
  last_login_at?: string
  created_at: string
}

export type AccountStatus = Account['status']

export interface PaginationParams {
  page?: number
  page_size?: number
}

export interface ActivationCode {
  code_id: string
  code: string
  plan_id: string
  status: 'unused' | 'used' | 'expired' | 'revoked'
  used_by?: string
  used_at?: string
  expires_at?: string
  timing_type: 'immediate' | 'on_use'
  created_by?: string
  created_at: string
  notes?: string
  used_by_tenant_name?: string
}

export interface MonitorMessage {
  message_id?: number
  rule_id?: string
  account_id?: string
  source_chat_id?: number
  sender_id?: number
  message?: string
  message_preview?: string
  created_at?: string
}

export interface Account {
  account_id: string
  tenant_id: string
  phone: string
  username?: string
  first_name?: string
  last_name?: string
  avatar?: string
  status: 'online' | 'offline' | 'pending_auth' | 'restricted' | 'banned' | 'authenticated' | 'session_revoked'
  session_name?: string
  device_profile?: Record<string, any>
  last_connected_at?: string
  remark?: string
  created_at: string
  updated_at?: string
  source?: string
}

export interface TelegramGroup {
  group_id: string
  tenant_id: string
  account_id: string
  tg_id: number
  title: string
  username?: string
  member_count?: number
  type?: string
  is_admin?: boolean
  last_message_at?: string
  created_at: string
}

// 群组 API 返回的后端 domain.Group 结构；tg_chat_id 也兼容私聊选择器的字符串 ID。
export interface Group {
  group_id: string
  tenant_id: string
  tg_chat_id: number | string
  title: string
  username?: string
  type?: string
  member_count?: number
  is_joined?: boolean
  joined_at?: string
  last_scraped_at?: string
  access_hash?: number
  metadata?: Record<string, unknown>
  _joinedAccounts?: string[]
}

export interface Member {
  member_id: string
  tenant_id: string
  account_id: string
  group_id: string
  tg_id: number
  username?: string
  first_name?: string
  last_name?: string
  phone?: string
  bio?: string
  last_seen?: string
  created_at: string
}

export interface TaskProgress {
  total: number
  done: number
  failed: number
  current_target?: string
  account_progress?: Array<{ account_id: string; done: number; failed: number; total: number }>
}

export interface Task {
  task_id: string
  tenant_id: string
  account_id?: string
  account_ids: string[]
  target_ids: string[]
  type: string
  status: 'pending' | 'scheduled' | 'queued' | 'running' | 'paused' | 'completed' | 'failed' | 'cancelled'
  priority?: number
  payload: Record<string, any>
  config?: Record<string, any>
  progress: TaskProgress
  result?: Record<string, any>
  error?: string
  retry_count?: number
  scheduled_at?: string
  created_at: string
  started_at?: string
  completed_at?: string
  is_loop?: boolean
  loop_interval_minutes?: number
  min_interval_seconds?: number
  max_interval_seconds?: number
  next_run_at?: string
}

export interface TaskLog {
  log_id: string
  task_id: string
  tenant_id: string
  account_id: string
  target_id: string
  target_name?: string
  action: string
  status: string
  tg_response?: string
  error_code?: string
  message?: string
  tg_message_id?: number
  executed_at: string
}

export interface Tenant {
  tenant_id: string
  name: string
  username: string
  max_accounts: number
  max_concurrent_tasks: number
  plan_id: string
  status: string
  expires_at?: string
  created_at: string
  notify_email?: string
  notify_settings?: Record<string, boolean>
  plan_features?: Record<string, boolean>
}

export interface MessageTemplate {
  template_id: string
  tenant_id: string
  name: string
  content: string
  variables: string[]
  created_at: string
  updated_at?: string
}

export interface AccountGroup {
  group_id: string
  tenant_id: string
  name: string
  color?: string
  account_ids?: string[]
  created_at: string
}

export interface Contact {
  contact_id: string
  tenant_id: string
  account_id: string
  tg_id: number
  username?: string
  first_name?: string
  last_name?: string
  phone?: string
  created_at: string
}

export interface TelegramDialog {
  dialog_id: string
  tenant_id: string
  account_id: string
  peer_tg_id: number
  type?: string
  name?: string
  last_message?: string
  last_message_date?: string
  unread_count?: number
  created_at: string
}

export interface ChannelAccount {
  channel_account_id: string
  channel_id: string
  account_id: string
  status: string
  assigned_at?: string
  last_used_at?: string
  account?: Account
}

export interface Channel {
  channel_id: string
  tenant_id: string
  account_id?: string
  name?: string
  channel_name?: string
  type?: string
  link?: string
  invite_link?: string
  member_count?: number
  accounts?: ChannelAccount[]
  account_count?: number
  account_ids?: string[]
  round_robin_index?: number
  status?: string
  created_at: string
}

export interface MonitorRule {
  rule_id: string
  tenant_id: string
  account_id?: string
  name?: string
  type?: 'reply' | 'forward'
  source_chat_ids?: string[]
  target_chat_ids?: string[]
  keywords: string[]
  match_type: 'contains' | 'regex' | 'exact' | 'from_user'
  reply_content?: string
  is_active?: boolean
  forward_enabled?: boolean
  forward_target_account_id?: string
  forward_target_group_id?: string
  forward_mode?: 'new_message' | 'edit' | 'forward'
  auto_reply_enabled?: boolean
  auto_reply_content?: string
  enabled?: boolean
  created_at: string
  updated_at?: string
}

export interface Campaign {
  campaign_id: string
  tenant_id: string
  name: string
  message?: string
  status: 'draft' | 'running' | 'paused' | 'completed' | 'failed'
  total_members?: number
  sent_count?: number
  fail_count?: number
  started_at?: string
  completed_at?: string
  created_at: string
}

// ============ API 响应类型 ============
export interface TaskListResponse {
  list: Task[]
  total: number
}

export interface MemberListResponse {
  list: Member[]
  total: number
}

export interface GroupListResponse {
  list: TelegramGroup[]
  total: number
}

export interface HealthMetrics {
  task_success_rate?: number
  weekly_messages?: number
  ban_rate_24h?: number
  flood_events_24h?: number
}

// ============ 仪表盘类型 ============
export interface RecentActivity {
  id: string
  type: string
  status: string
  target?: string
  created_at: string
}

export interface ScheduledBroadcast {
  schedule_id: string
  tenant_id: string
  account_id: string
  name: string
  message?: string
  cron_expression: string
  target_type: 'contacts' | 'group_members'
  target_group_id?: string
  round_robin_enabled?: boolean
  enabled: boolean
  status?: string
  created_at: string
  updated_at?: string
}

export interface MonitorEvent {
  event_id: string
  tenant_id: string
  account_id: string
  rule_id?: string
  event_type: string
  source_chat_id?: number
  source_chat_name?: string
  sender_id?: number
  sender_name?: string
  message_preview?: string
  created_at: string
}

export interface RiskEvent {
  risk_id: string
  tenant_id: string
  account_id?: string
  risk_type: string
  severity: string
  detail?: string
  created_at: string
}

export interface MonitorKeywordStat {
  keyword: string
  count: number
  last_seen?: string
}

export interface KeywordMonitorStats {
  top_keywords: MonitorKeywordStat[]
  total_events: number
}

export interface Announcement {
  announcement_id: string
  title: string
  content: string
  priority?: 'normal' | 'urgent'
  target_type?: 'all' | 'specific_plan'
  target_plans: string[]
  pinned?: boolean
  is_active?: boolean
  start_at?: string
  end_at?: string
  created_at: string
}

export interface AnnouncementRead {
  announcement_id: string
  read_at: string
}

// R119: 账号能力探测配置(管理端全局, 单行)
export interface ProbeConfig {
  id?: number
  test_username: string    // 主动私聊探测目标(真实小号 username, 不用 bot)
  private_invite: string   // 私有群永久邀请链接(https://t.me/+xxx)
  public_link: string      // 公开群链接/username(https://t.me/xxx)
  updated_at?: string
}

export interface Material {
  material_id: string
  tenant_id: string
  account_id: string
  name?: string
  type: 'text' | 'image' | 'video' | 'document' | 'audio'
  file_name?: string
  file_url: string
  file_size?: number
  mime_type?: string
  caption?: string
  used_count?: number
  created_at: string
}

export interface Notification {
  notification_id: string
  tenant_id: string
  type?: string
  title?: string
  message?: string
  is_read: boolean
  created_at: string
}

// P0-04: 原类型字段(telegram_link/wechat_id/qq_group/email)与后端真实结构
// (007 迁移 support_config 表 + repository)完全脱节——页面不可用。以磁盘为准重写。
export interface SupportConfig {
  id: number
  contact_type: 'wechat' | 'telegram' | 'qq' | 'email' | 'custom'
  contact_value: string
  display_name: string
  qr_code_url?: string
  is_active: boolean
  updated_at?: string
  // 本地预览用的文件对象(不提交)
  qr_code_file?: File
}

export interface LinkScrapeSource {
  source_id: string
  tenant_id: string
  url: string
  name?: string
  enabled: boolean
  scrape_interval: number
  last_scraped_at?: string
  created_at: string
}

export interface ScrapedLink {
  link_id: string
  tenant_id: string
  source_id?: string
  url: string
  title?: string
  chat_type?: string
  member_count?: number
  discovered_at: string
}

export interface Plan {
  plan_id: string
  name: string
  price_monthly: number
  max_accounts: number
  max_concurrent: number
  can_scrape_members: boolean
  can_join_groups: boolean
  can_broadcast: boolean
  can_monitor: boolean
  can_export: boolean
  can_scrape_links: boolean
  can_use_templates: boolean
  can_protocol?: boolean
  // 051 套餐体系: 功能key清单 + 日限额(后台套餐管理页可编辑, 后端 admin/plans 返回)
  features?: string[]
  daily_search_limit?: number
  max_daily_broadcasts?: number
  description?: string
  is_active?: boolean
}

// ============ 定时任务（调度中心） ============
export interface ScheduledTask {
  id: string
  name: string
  type: 'broadcast' | 'scrape'
  cron: string
  config: Record<string, any>
  status: 'active' | 'paused'
  enabled: boolean
  next_run_at?: string
  last_run_at?: string
  last_run_status?: string
  last_run_message?: string
  created_at: string
  updated_at?: string
}

// ============ 账号风控配额（账号级并行度配额） ============
export interface AccountQuota {
  account_id: string
  per_account_limit?: number
  burst_percent?: number
  burst_window_sec?: number
}

// ============ 会话导入 ============
export interface SessionImportResult {
  account_id: string
  phone?: string
  success: boolean
  error?: string
}

export interface BulkImportRequest {
  sessions: Array<{
    phone?: string
    session_data: string
    remark?: string
    group_id?: string
  }>
  default_remark?: string
}

// ============ 批量操作 ============
export interface BulkAccountOperation {
  account_ids: string[]
  operation: 'connect' | 'disconnect' | 'delete' | 'export' | 'group'
  params?: Record<string, any>
}

export interface BulkOperationResult {
  success_count: number
  failed_count: number
  results: Array<{
    account_id: string
    success: boolean
    error?: string
  }>
}

// ============ 通知设置 ============
export interface NotificationSetting {
  setting_id: string
  tenant_id: string
  event_type: string
  channel: string
  enabled: boolean
  created_at?: string
}

// 通知渠道
export type NotificationChannel = 'browser' | 'email' | 'webhook'

// 通知事件类型
export type NotificationEventType =
  | 'broadcast_complete'
  | 'broadcast_failed'
  | 'account_banned'
  | 'account_restricted'
  | 'monitor_triggered'
  | 'flood_wait_detected'

// ============ 通知偏好设置 ============
export interface NotificationPreferences {
  browser_enabled: boolean
  email_enabled: boolean
  email_address?: string
  webhook_enabled: boolean
  webhook_url?: string
  webhook_secret?: string
  quiet_hours_enabled: boolean
  quiet_hours_start?: string
  quiet_hours_end?: string
}

// ============ 仪表盘最近活动类型 ============
export type ActivityType =
  | 'login'
  | 'broadcast'
  | 'join'
  | 'scrape'
  | 'add_account'
  | 'create_task'
  | 'export'
  | 'settings'

// ============ 系统公告 ============
export interface SystemAnnouncement {
  announcement_id: string
  title: string
  content: string
  target_plans: string[]
  start_at?: string
  end_at?: string
  is_pinned: boolean
  created_at: string
  updated_at?: string
}

// ============ 渠道账号分配 ============
export interface ChannelAccountAssignment {
  channel_account_id: string
  channel_id: string
  account_id: string
  status: string
  assigned_at?: string
  last_used_at?: string
}

// ============ 渠道统计 ============
export interface ChannelStats {
  total_channels: number
  total_accounts: number
  active_accounts: number
  messages_sent_24h: number
}

// ============ 账号同步 ============
export interface SyncAccountRequest {
  account_ids: string[]
  sync_groups: boolean
  sync_contacts: boolean
  sync_dialogs: boolean
}

export interface SyncAccountResult {
  account_id: string
  success: boolean
  groups_synced?: number
  contacts_synced?: number
  dialogs_synced?: number
  error?: string
}

// ============ 消息预览 ============
export interface MessagePreview {
  message_id: string
  from_id?: string
  from_name?: string
  to_id?: string
  to_name?: string
  message: string
  date: string
  is_outgoing?: boolean
}

// ============ 消息发送结果 ============
export interface MessageSendResult {
  message_id?: string
  success: boolean
  error?: string
  date?: string
}

// ============ 定时任务执行历史 ============
export interface ScheduleRunHistory {
  run_id: string
  schedule_id: string
  started_at: string
  finished_at?: string
  status: 'running' | 'completed' | 'failed'
  triggered_by: string
  result?: string
  error?: string
  stats?: Record<string, any>
}

// ============ 两步验证 ============
export interface TwoFactorAuth {
  enabled: boolean
  secret?: string
  qr_code?: string
  backup_codes?: string[]
}

// ============ API 密钥 ============
export interface ApiKey {
  key_id: string
  name: string
  key: string
  scopes: string[]
  last_used_at?: string
  created_at: string
  expires_at?: string
}

// ============ Webhook ============
export interface Webhook {
  webhook_id: string
  url: string
  events: string[]
  secret?: string
  enabled: boolean
  created_at: string
  updated_at?: string
  last_triggered_at?: string
}

// ============ 仪表盘统计数据 ============
export interface DashboardStats {
  total_accounts: number
  online_accounts: number
  total_tasks: number
  running_tasks: number
  total_messages_sent: number
  messages_sent_today: number
  success_rate: number
  recent_tasks?: Task[]
  recent_activities?: RecentActivity[]
  completed_tasks_today?: number
  total_groups?: number
  risk_events_24h?: number
  task_trend?: number[]
  account_distribution?: Array<{ value: number; name: string }>
  trends?: Record<string, string>
  system_health?: {
    cpu_usage?: number
    memory_usage?: number
    disk_usage?: number
    uptime?: number
  }
}

// ============ 群发统计 ============
export interface BroadcastStats {
  total_sent: number
  total_failed: number
  success_rate: number
  avg_delivery_time?: number
  accounts_used?: number
  last_broadcast_at?: string
}

// ============ 账号池统计 ============
export interface AccountPoolStats {
  total: number
  available: number
  in_use: number
  resting: number
  average_health_score?: number
}

// ============ 任务执行详情 ============
export interface TaskExecution {
  execution_id: string
  task_id: string
  account_id: string
  status: string
  started_at: string
  finished_at?: string
  error?: string
  retry_count?: number
}

// ============ 系统设置 ============
export interface SystemSettings {
  system_name?: string
  system_version?: string
  maintenance_mode?: boolean
  registration_enabled?: boolean
  max_accounts_per_tenant?: number
  max_tasks_per_tenant?: number
  default_broadcast_delay?: number
  max_broadcast_delay?: number
}

// ============ 通知历史 ============
export interface NotificationHistory {
  history_id: string
  notification_id: string
  channel: string
  status: string
  sent_at?: string
  delivered_at?: string
  read_at?: string
  error?: string
}

// ============ 计划任务（系统级） ============
export interface SystemCronJob {
  job_id: string
  name: string
  description?: string
  schedule: string
  command: string
  enabled: boolean
  last_run_at?: string
  last_run_status?: string
  next_run_at?: string
  created_at: string
  updated_at?: string
}

// ============ 缓存统计 ============
export interface CacheStats {
  hit_rate: number
  total_keys: number
  memory_usage: number
  evictions: number
}

// ============ 数据库统计 ============
export interface DatabaseStats {
  total_connections: number
  active_connections: number
  idle_connections: number
  max_connections: number
  database_size?: string
  table_stats?: Array<{
    table_name: string
    row_count: number
    size: string
  }>
}

// ===== P0-34/P0-27 审计修复: 以下类型被 api 层 import 但从未定义(项目从未跑过
// vue-tsc, 类型缺失一直静默)。字段以后端实际返回为准, 允许扩展键兜底。 =====

/** 列表响应统一形态——request.ts parseResponse 归一后所有列表接口都长这样 */
export interface ListResponse<T = any> {
  list: T[]
  total: number
  page?: number
  page_size?: number
}

/** 系统监控指标(后端 /admin/metrics, map 键值; 与 system/Monitor.vue 消费对齐) */
export interface SystemMetrics {
  cpu: number
  memory: number
  memoryUsed: number
  memoryTotal: number
  disk: number
  diskUsed: number
  diskTotal: number
  onlineConnections: number
  completedToday?: number
  [k: string]: any
}

/** 管理端仪表盘统计(/admin/stats) */
export interface AdminStats {
  total_tenants?: number
  active_tenants?: number
  total_accounts?: number
  total_codes?: number
  unused_codes?: number
  today_revenue?: number
  month_revenue?: number
  [k: string]: any
}

/** 管理员资料 */
export interface AdminProfile {
  username?: string
  nickname?: string
  avatar?: string
  [k: string]: any
}
