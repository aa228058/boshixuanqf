import { useAuthStore } from '@/stores/auth'
import { useAdminStore } from '@/stores/admin'
import router from '@/router'
import { ref } from 'vue'
import { emitMutation } from './mutationBus'

const BASE_URL = import.meta.env.VITE_API_BASE_URL || '/api/v1'
export const ADMIN_BASE_URL = ''

// 全局请求loading状态
export const globalLoading = ref(false)
let loadingTimer: ReturnType<typeof setTimeout> | null = null
let loadingCount = 0

// Token 刷新竞态保护：同一时刻只有一个刷新请求
let isRefreshing = false
let refreshPromise: Promise<string> | null = null

function setLoading(loading: boolean) {
  if (loading) {
    loadingCount++
    if (loadingTimer) clearTimeout(loadingTimer)
    loadingTimer = setTimeout(() => {
      globalLoading.value = true
    }, 200)
  } else {
    loadingCount = Math.max(0, loadingCount - 1)
    if (loadingCount === 0) {
      if (loadingTimer) clearTimeout(loadingTimer)
      globalLoading.value = false
    }
  }
}

export interface RequestOptions {
  method?: string
  headers?: Record<string, string>
  body?: Record<string, unknown> | unknown
  timeout?: number
  // 查询参数(2026-10-20): doFetch 拼 query string, 此前被静默忽略
  params?: Record<string, string | number | boolean | undefined>
  // silent(2026-10-22 审计修复): 后台静默请求——轮询/心跳类调用传 true,
  // 不触发顶部全局进度条(用户在详情页盯着进度条时, 后台轮询不该让顶条一直跑)
  silent?: boolean
}

// ===== 后端英文错误消息 → 中文翻译（完整映射表）=====
const errorMessageMap: Record<string, string> = {
  // 认证相关
  'invalid username or password': '用户名或密码错误',
  'username already taken': '用户名已被占用',
  'invalid credentials': '登录凭证无效',
  'invalid token': '登录凭证已失效',
  'invalid token format': '登录凭证格式错误',
  'invalid or expired refresh token': '刷新令牌已过期，请重新登录',
  'refresh_token required': '缺少刷新令牌',
  'token mismatch': '令牌不匹配',
  'unauthorized': '未授权，请重新登录',
  'forbidden': '无权访问',
  'tenant is not active': '账号已被停用，请联系客服',

  // 请求参数
  'invalid request': '请求参数错误',
  'invalid request body': '请求格式不正确',
  'bad request': '请求格式错误',
  'not found': '请求的资源不存在',
  'invalid tenant id': '租户ID无效',
  'invalid account id': '账号ID无效',
  'invalid task id': '任务ID无效',
  'invalid group id': '群组ID无效',
  'invalid rule id': '规则ID无效',
  'invalid template id': '模板ID无效',
  'invalid code id': '卡密ID无效',
  'invalid announcement id': '公告ID无效',
  'invalid page parameter': '分页参数错误',
  'invalid page_size parameter': '每页数量参数错误',

  // 字段必填
  'username and password required': '请输入用户名和密码',
  'username, password and name are required': '请填写完整注册信息',
  'phone required': '请输入手机号',
  'code required': '请输入验证码',
  'password required': '请输入密码',
  'identifier required': '请输入目标标识',

  // 操作失败
  'failed to create account': '创建账号失败',
  'failed to delete account': '删除账号失败',
  'failed to connect account': '连接账号失败',
  'failed to disconnect account': '断开账号失败',
  'failed to verify code': '验证码校验失败',
  'failed to submit 2fa': '两步验证提交失败',
  'failed to resolve peer': '查找目标失败',
  'failed to create broadcast task': '创建群发任务失败',
  'failed to create join groups task': '创建加群任务失败',
  'failed to create scrape members task': '创建成员采集任务失败',
  'failed to create scrape links task': '创建链接采集任务失败',
  'failed to create monitor rule': '创建监控规则失败',
  'failed to update monitor rule': '更新监控规则失败',
  'failed to delete rule': '删除规则失败',
  'failed to toggle rule': '切换规则状态失败',
  'failed to list monitor rules': '获取规则列表失败',
  'failed to start monitoring': '启动监控失败',
  'failed to pause task': '暂停任务失败',
  'failed to resume task': '恢复任务失败',
  'failed to cancel task': '取消任务失败',
  'failed to retry task': '重试任务失败',
  'failed to delete task': '删除任务失败',
  'failed to sync groups': '同步群组失败',
  'failed to create group': '创建群组失败',
  'failed to delete group': '删除群组失败',

  // 状态限制
  'cannot cancel task with status': '当前任务状态不允许取消',
  'cannot delete task with status': '请先停止或取消任务后再删除',
  'cannot pause task with status': '当前任务状态不允许暂停',
  'cannot resume task with status': '当前任务状态不允许恢复',
  'cannot retry task with status': '当前任务状态不允许重试',
  'account not connected, status:': '账号未连接，无法执行操作',

  // 资源不存在
  'account not found': '账号不存在',
  'account not found or access denied': '账号不存在或无权限',
  'group not found': '群组不存在',
  'task not found': '任务不存在',
  'rule not found': '规则不存在',
  'announcement not found': '公告不存在',
  'plan not found': '套餐不存在',
  'sync not available': '同步功能暂不可用',

  // 卡密/套餐
  'invalid code': '卡密无效',
  'code already used or expired': '卡密已被使用或已过期',
  'code expired': '卡密已过期',
  'activation code not found': '卡密不存在',

  // 限流
  'rate limit exceeded': '操作太频繁，请稍后再试',
  'too many login attempts, please try again later': '登录尝试次数过多，请稍后再试',
  'too many registration attempts, please try again later': '注册尝试次数过多，请稍后再试',
  'please wait a moment before trying again': '请稍等片刻再试',

  // 数量限制
  'account limit reached': '账号数量已达上限',
  'max concurrent tasks reached for tenant': '并发任务数已达上限',
  'mappings exceeds limit': '映射数量超出限制',

  // 监控规则
  'keywords cannot be empty for match_type': '该匹配类型需要填写关键词',
  'keywords cannot contain empty strings': '关键词不能为空字符串',
  'target_chat_ids cannot be empty for forward rules': '转发规则需要填写目标群组',

  // 通用
  'internal server error': '服务器开小差了，请稍后重试',
  'internal error': '服务器内部错误',
  'registration failed': '注册失败',
  'network error': '网络连接失败，请检查网络',
  'failed to fetch': '网络连接失败，请检查网络',
}

function translateError(msg: string): string {
  if (!msg) return '操作失败，请稍后重试'
  const lower = msg.toLowerCase().trim()
  // 先精确匹配
  if (errorMessageMap[lower]) return errorMessageMap[lower]
  // 再前缀匹配（处理带动态内容的错误）
  for (const [key, val] of Object.entries(errorMessageMap)) {
    if (lower.startsWith(key)) return val
  }
  // R87-M3 兜底: 数据库/系统级错误绝不直接弹给用户(之前 SQLSTATE 报错直接糊在弹窗上)
  if (/ERROR:|SQLSTATE|violates|duplicate key|pq:|syntax error|deadlock|connection reset|constraint/i.test(msg)) {
    return '服务器开小差了，请稍后重试'
  }
  return msg
}

// ===== 带重试的请求核心 =====
// P0-05: 导出为公开接口——卡密导出等纯文本接口需要绕过 response.json() 直读 text
export async function doFetchWithRetry(
  baseUrl: string,
  endpoint: string,
  options: RequestOptions,
  token: string,
  retries = 3
): Promise<Response> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...options.headers,
  }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  // (2026-10-20 根修) params 拼 query string: 此前 doFetch 完全忽略 options.params,
  // 导致所有带查询参数的 GET(群库搜索 keyword/翻页 page/筛选) 参数丢失——全站级缺陷一处修复
  let finalEndpoint = endpoint
  if (options.params && Object.keys(options.params).length > 0) {
    const qs = new URLSearchParams()
    for (const [k, v] of Object.entries(options.params)) {
      if (v === undefined || v === null || v === '') continue
      qs.set(k, String(v))
    }
    const q = qs.toString()
    if (q) finalEndpoint = endpoint + (endpoint.includes('?') ? '&' : '?') + q
  }

  let lastError: Error | null = null
  for (let attempt = 0; attempt < retries; attempt++) {
    let fetchPromise: Promise<Response>
    try {
      const isForm = typeof FormData !== 'undefined' && options.body instanceof FormData
      if (isForm) {
        // FormData 必须带浏览器自动生成的 multipart boundary, 不能手动设 Content-Type
        delete (headers as Record<string, string>)['Content-Type']
      }
      fetchPromise = fetch(`${baseUrl}${finalEndpoint}`, {
        method: options.method || 'GET',
        headers,
        body: isForm ? (options.body as FormData) : (options.body ? JSON.stringify(options.body) : undefined),
      })
    } catch {
      throw new Error('网络连接失败，请检查网络')
    }

    // 幂等方法白名单：POST 等写操作 5xx 不重试——网关 502 但后端已入库时，
    // 盲目重试会创建重复任务（如群发任务发两遍）。
    const method = (options.method || 'GET').toUpperCase()
    const idempotent = method === 'GET' || method === 'HEAD' || method === 'OPTIONS'
    let timeoutId: ReturnType<typeof setTimeout> | undefined
    try {
      const timeoutPromise = new Promise<Response>((_, reject) => {
        timeoutId = setTimeout(() => reject(new Error('请求超时，请稍后重试')), options.timeout || 120000)
      })
      const response = await Promise.race([fetchPromise, timeoutPromise])
      // 只对幂等方法的 5xx 重试
      if (!response.ok && response.status >= 500 && idempotent && attempt < retries - 1) {
        await new Promise(r => setTimeout(r, 1000 * (attempt + 1)))
        continue
      }
      return response
    } catch (e: unknown) {
      lastError = e instanceof Error ? e : new Error(String(e))
      if (attempt < retries - 1) {
        await new Promise(r => setTimeout(r, 1000 * (attempt + 1)))
      }
    } finally {
      // 修复计时器泄漏：fetch 胜出后必须清掉超时定时器，
      // 否则每个请求都留下一个 120s 的悬挂 timer，长会话持续堆积。
      clearTimeout(timeoutId)
    }
  }
  throw lastError || new Error('请求失败，请稍后重试')
}

function parseResponse<T>(data: unknown): T {
  if (data && typeof data === 'object' && 'code' in data && 'data' in data) {
    const d = data as Record<string, unknown>
    if (d.code !== 0 && d.code !== 200 && d.code !== 201 && d.code !== '0' && d.code !== '200' && d.code !== '201') {
      throw new Error(translateError(String(d.message)) || '请求失败')
    }
    // 后端返回格式统一归一化(P0-35/P1-46 审计修复: 原契约"带 total 才包装"
    // 导致调用方 res.list 在纯数组响应下全是 undefined, 24 处列表点静默变空):
    // 1. 根级 total: {code,data:[...],total,...} → {list:data,total,...}
    // 2. 嵌套 total: {code,data:{list|items|data,total,...}} → 同上(items 兜底)
    // 3. data 是纯数组 → 也包装 {list:data,total:length} —— 全项目已核实无
    //    调用点把返回值当数组直用, 包装后 res.list 永远有效
    // 4. data 是对象 → 原样返回(详情接口不受影响)
    if ('total' in d) {
      const result = {
        list: d.data,
        total: d.total,
        page: d.page,
        page_size: d.page_size,
      } as T
      return result
    }
    // 嵌套 ListResponse: {code, data: {list|items|data, total, page, page_size}}
    const inner = d.data as Record<string, unknown> | null
    if (inner && typeof inner === 'object' && 'total' in inner) {
      const result = {
        list: ('list' in inner ? inner.list : ('items' in inner ? inner.items : ('data' in inner ? inner.data : []))),
        total: inner.total,
        page: inner.page,
        page_size: inner.page_size,
      } as T
      return result
    }
    // 纯数组响应(无 total)也包装成 ListResponse 形态
    if (Array.isArray(d.data)) {
      const result = {
        list: d.data,
        total: d.data.length,
      } as T
      return result
    }
    // 处理 data 为 null/undefined 的情况（如空日志列表）
    if (d.data === null || d.data === undefined) {
      return [] as T
    }
    return d.data as T
  }
  return data as T
}

export async function request<T>(endpoint: string, options: RequestOptions = {}): Promise<T> {
  const authStore = useAuthStore()
  const url = `${BASE_URL}${endpoint}`
  // silent 请求不进 loading 计数——顶部进度条只在用户主动操作时出现
  if (!options.silent) setLoading(true)
  try {
    const response = await doFetchWithRetry(BASE_URL, endpoint, options, authStore.token)

    // 401 处理：区分登录失败 vs token 过期
    if (response.status === 401) {
      const errData = await response.json().catch(() => ({}))
      const errMsg = translateError(errData.message || '')

      // 登录/注册接口的 401
      if (endpoint === '/auth/login' || endpoint === '/auth/register' || endpoint === '/admin/auth/login') {
        throw new Error(errMsg || '用户名或密码错误')
      }

      // R168: refresh 接口自身的 401 = refresh token 也过期。原逻辑会走"等待刷新"分支,
      // await 的是同一个进行中的 promise → 自等待死锁 → 全站 API 挂起, 页面永久空白。
      // 这里直接登出并踢回登录页。
      if (endpoint === '/auth/refresh') {
        authStore.logout()
        const publicPaths = ['/login', '/register', '/admin/login']
        if (!publicPaths.includes(window.location.pathname)) {
          router.push('/login')
        }
        throw new Error(errMsg || '登录已过期，请重新登录')
      }
      // 其他接口的 401 尝试刷新 token
      if (!isRefreshing) {
        isRefreshing = true
        refreshPromise = authStore.refresh()
          .then((newToken) => {
            isRefreshing = false
            return newToken
          })
          .catch((err) => {
            isRefreshing = false
            refreshPromise = null
            authStore.logout()
            const publicPaths = ['/login', '/register', '/admin/login']
            if (!publicPaths.includes(window.location.pathname)) {
              router.push('/login')
            }
            throw err
          })
      }

      // 等待刷新完成，然后用新 token 重试原请求
      try {
        await refreshPromise
        const retryResponse = await doFetchWithRetry(BASE_URL, endpoint, options, authStore.token)
        if (!retryResponse.ok) {
          const retryData = await retryResponse.json().catch(() => ({}))
          throw new Error(translateError(retryData.message) || `请求失败: ${retryResponse.status}`)
        }
        const retryData = await retryResponse.json().catch(() => ({}))
        emitMutation(options.method || 'GET', endpoint)
        return parseResponse<T>(retryData)
      } catch (refreshErr: unknown) {
        const msg = refreshErr instanceof Error ? refreshErr.message : '登录已过期，请重新登录'
        throw new Error(msg)
      }
    }

    // 403 处理：区分"账号被停用/套餐过期"（登出）和"套餐功能未开通"（仅提示）
    if (response.status === 403) {
      const errData = await response.json().catch(() => ({}))
      const errMsg = translateError(errData.message || '')
      const raw = String(errData.message || '').toLowerCase()
      const isAccountLevel = raw.includes('suspended') || raw.includes('expired') || raw.includes('not active')
      if (isAccountLevel) {
        authStore.logout()
        const publicPaths = ['/login', '/register', '/admin/login']
        if (!publicPaths.includes(window.location.pathname)) {
          router.push('/login')
        }
      }
      throw new Error(errMsg || '无权访问')
    }

    // R167/R172: 二进制响应(blob/arrayBuffer)直通 —— 原实现一律 JSON 解析, 头像等二进制接口永远抛错。
    // 注意必须在错误检查之后: 404 等错误响应不能被当作 blob 吞掉(R172 修复: 错误 JSON 被当成图片缓存, 头像永久破损)。
    if (options.responseType === 'blob') {
      if (!response.ok) {
        const errData = await response.json().catch(() => ({}))
        const err: any = new Error(translateError(errData.message) || `请求失败: ${response.status}`)
        err.status = response.status
        throw err
      }
      emitMutation((options.method || 'GET').toUpperCase(), endpoint)
      return (await response.blob()) as T
    }
    if (options.responseType === 'arrayBuffer') {
      if (!response.ok) {
        const errData = await response.json().catch(() => ({}))
        const err: any = new Error(translateError(errData.message) || `请求失败: ${response.status}`)
        err.status = response.status
        throw err
      }
      emitMutation((options.method || 'GET').toUpperCase(), endpoint)
      return (await response.arrayBuffer()) as T
    }

    const data = await response.json().catch(() => ({}))

    if (!response.ok) {
      const err: any = new Error(translateError(data.message) || `请求失败: ${response.status}`)
      err.status = response.status
      throw err
    }

    const parsed = parseResponse<T>(data)
    emitMutation(options.method || 'GET', endpoint)
    return parsed
  } finally {
    // 2026-10-22: 与入口的 silent 判断配对——silent 请求不进计数, 避免顶部进度条计数错乱
    if (!options.silent) setLoading(false)
  }
}

export async function adminRequest<T>(endpoint: string, options: RequestOptions = {}): Promise<T> {
  const adminStore = useAdminStore()
  setLoading(true)
  try {
    const response = await doFetchWithRetry(ADMIN_BASE_URL, endpoint, options, adminStore.token)

    if (response.status === 401) {
      const errData = await response.json().catch(() => ({}))
      if (endpoint === '/admin/auth/login') {
        throw new Error(translateError(errData.message) || '用户名或密码错误')
      }
      adminStore.logout()
      const publicPaths = ['/login', '/register', '/admin/login']
      if (!publicPaths.includes(window.location.pathname)) {
        router.push('/admin/login')
      }
      if (publicPaths.includes(window.location.pathname)) {
        return {} as T
      }
      throw new Error('登录已过期，请重新登录')
    }

    // 403 处理: 权限不足/功能未开通——留在原页提示, 绝不登出
    // (bug 修复: 非 super 管理员保存套餐被 RequireRole 403, 原逻辑误判为会话失效踢回登录页)
    if (response.status === 403) {
      const errData = await response.json().catch(() => ({}))
      const errMsg = translateError(errData.message || '')
      throw new Error(errMsg || '权限不足, 该操作仅限超级管理员')
    }

    const data = await response.json().catch(() => ({}))

    if (!response.ok) {
      throw new Error(translateError(data.message) || `请求失败: ${response.status}`)
    }

    emitMutation((options.method || 'GET').toUpperCase(), endpoint)
    return parseResponse<T>(data)
  } finally {
    setLoading(false)
  }
}

export const api = {
  // options 整体透传: 调用方按 RequestOptions 传({ params } / { responseType } 均可), 不再丢弃
  get: <T>(endpoint: string, options?: RequestOptions) => request<T>(endpoint, options),
  post: <T>(endpoint: string, body?: Record<string, unknown> | unknown, extra?: Record<string, unknown>) => request<T>(endpoint, { method: 'POST', body, ...extra }),
  put: <T>(endpoint: string, body?: Record<string, unknown> | unknown) => request<T>(endpoint, { method: 'PUT', body }),
  delete: <T>(endpoint: string) => request<T>(endpoint, { method: 'DELETE' }),
}

export const adminApi = {
  get: <T>(endpoint: string, options?: RequestOptions) => adminRequest<T>(endpoint, options),
  post: <T>(endpoint: string, body?: Record<string, unknown> | unknown) => adminRequest<T>(endpoint, { method: 'POST', body }),
  put: <T>(endpoint: string, body?: Record<string, unknown> | unknown) => adminRequest<T>(endpoint, { method: 'PUT', body }),
  delete: <T>(endpoint: string) => adminRequest<T>(endpoint, { method: 'DELETE' }),
}
