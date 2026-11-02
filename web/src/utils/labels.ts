// 中英标签映射（2026-09-22）：仪表盘/任务详情的类型、状态、风险事件统一显示中文。
// 映射缺失时回退原始值，保证新类型出现时不会显示空白。
export function riskTypeLabel(t: string): string {
  const m: Record<string, string> = {
    flood_wait: '频繁限制',
    phone_banned: '手机号封禁',
    restriction: '账号限制',
    rate_limit: '发送频率限制',
    peer_flood: '对方频繁',
    account_banned: '账号封禁',
    session_revoked: '会话被注销',
    broadcast_flood_wait: '群发频繁限制',
    broadcast_fail: '群发失败',
    account_restricted: '账号受限',
    session_restore_failed: '会话恢复失败',
    broadcast_group_restricted: '群组发言受限',
    join_fail: '加群失败',
    invite_flood: '邀请频繁限制',
    appeal_approved: '申诉通过',
  }
  return m[t] || t
}

export function severityLabel(s: string): string {
  const m: Record<string, string> = { low: '低', medium: '中', high: '高', critical: '严重' }
  return m[s] || s
}

export function auditActionLabel(a: string): string {
  const m: Record<string, string> = {
    task_create: '创建任务',
    task_delete: '删除任务',
    task_cancel: '取消任务',
    task_pause: '暂停任务',
    task_resume: '恢复任务',
    task_retry: '重试任务',
    login: '登录系统',
    login_failed: '登录失败',
    logout: '退出登录',
    register: '注册账号',
    renew: '套餐续费',
    activate_code: '卡密激活',
    sync_groups: '同步群组',
    account_add: '添加账号',
    account_delete: '删除账号',
    account_reconnect: '重连账号',
    // 审计服务实际写入的码(修复: 缺失导致仪表盘显示英文)
    tenant_login: '租户登录',
    tenant_register: '租户注册',
    account_import: '导入账号',
    account_import_file: '导入账号文件',
    code_redeem: '卡密激活',
    admin_login: '管理员登录',
  }
  return m[a] || a
}

const TASK_TYPE_TOKENS: Record<string, string> = {
  broadcast: '群发消息',
  join_groups: '加群任务',
  scrape_members: '成员采集',
  scrape_links: '链接采集',
  scheduled_broadcast: '循环群发',
}

// 活动 target 字段是后端拼的英文串（如 "scheduled_broadcast accounts=1 targets=1"），整体替换为中文
export function targetLabel(s: string): string {
  if (!s) return s
  let out = String(s)
  for (const [k, v] of Object.entries(TASK_TYPE_TOKENS)) out = out.split(k).join(v)
  return out.split('accounts=').join('账号数=').split('targets=').join('目标数=')
}

export function statusLabel(s: string): string {
  const m: Record<string, string> = {
    success: '成功',
    failed: '失败',
    info: '提示',
    pending: '排队中',
    running: '执行中',
    paused: '已暂停',
    cancelled: '已取消',
    join_failed: '加入失败',
    invite_expired: '邀请链接已失效',
    invite_invalid: '邀请链接无效',
    username_invalid: '群组地址无效',
    peer_flood: '对方操作频繁',
    too_many_groups: '加入群数量达上限',
    group_private: '私有群，无访问权限',
    not_invited: '未被邀请，需管理员通过',
    banned_from_group: '已被该群封禁',
    join_flood_wait: '加群操作频繁受限',
    already_member: '已是群成员',
    flood_wait: '触发频繁限制',
    send_success: '发送成功',
    send_failed: '发送失败',
    // ===== TG 原始错误码中文映射(2026-10-21): 日志显示"已被该群封禁", 不再裸奔英文码 =====
    USER_BANNED_IN_CHANNEL: '已被该群封禁',
    CHAT_WRITE_FORBIDDEN: '无发言权限(可能被禁言)',
    CHAT_ADMIN_REQUIRED: '需要管理员权限',
    CHANNEL_PRIVATE: '群不存在或已解散',
    PEER_FLOOD: '发送太频繁,账号被限流',
    USER_RESTRICTED: '账号被官方限制',
    CHAT_RESTRICTED: '该群已被限制',
    INPUT_USER_DEACTIVATED: '账号已注销',
    AUTH_KEY_UNREGISTERED: '登录已失效,需重新登录',
    MSG_ID_INVALID: '消息ID无效',
    EXTERNAL_URL_INVALID: '外部链接无效',
    MEDIA_CAPTION_TOO_LONG: '文案超过长度限制',
    TIMEOUT: '网络超时',
    RPC_CALL_FAIL: 'TG服务器错误',
    API_CALL_ERROR: '接口调用失败',
  }
  return m[s] || s
}
