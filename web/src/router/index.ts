import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useAdminStore } from '@/stores/admin'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    // 租户端
    {
      path: '/login',
      name: 'Login',
      component: () => import('@/views/auth/Login.vue'),
      meta: { public: true, title: '登录' }
    },
    {
      path: '/register',
      name: 'Register',
      component: () => import('@/views/auth/Register.vue'),
      meta: { public: true, title: '注册' }
    },
    {
      path: '/',
      component: () => import('@/views/Layout.vue'),
      meta: { requiresAuth: true },
      children: [
        // 首页 - 账号选择/仪表盘
        { path: '', redirect: '/dashboard' },
        { path: 'dashboard', name: 'Dashboard', component: () => import('@/views/dashboard/Index.vue'), meta: { title: '仪表盘' } },
        // R125 示例2 "更多"功能导航页
        { path: 'more', name: 'More', component: () => import('@/views/more/Index.vue'), meta: { title: '更多' } },
        // 功能页均为全屏模态(overlay), 子弹层不受影响; 仪表盘保留传统内嵌页
        { path: 'accounts', name: 'Accounts', component: () => import('@/views/accounts/List.vue'), meta: { title: '账号管理', overlay: true } },
        { path: 'settings', name: 'Settings', component: () => import('@/views/settings/Index.vue'), meta: { title: '设置', overlay: true } },
        { path: 'groups/discover', name: 'GroupDiscover', component: () => import('@/views/groups/Discover.vue'), meta: { title: '发现群组', overlay: true } },
        { path: 'verifier/logs', name: 'VerifierLogs', component: () => import('@/views/verifier/Logs.vue'), meta: { title: '验证记录', overlay: true } },
        { path: 'updates', name: 'Updates', component: () => import('@/views/updates/Index.vue'), meta: { title: '更新日志', overlay: true } },
        { path: 'help', name: 'HelpCenter', component: () => import('@/views/help/Index.vue'), meta: { title: '帮助中心', overlay: true } },
        { path: 'raffle', name: 'Raffle', component: () => import('@/views/raffle/Index.vue'), meta: { title: '抽奖助手', overlay: true } },
        // R113: 旧协议协同系统(protocol/* 三页)已整建制删除 —— 与主系统功能完全重复
        // (无入口的活死人路由), 统一由 workspace/ 下各页承载所有账号类型。
        { path: 'sessions', name: 'Sessions', component: () => import('@/views/sessions/Index.vue'), meta: { title: '会话管理', overlay: true } },

        // 账号工作区 - 选中账号后的功能页面（数据隔离）
        {
          path: 'workspace/:accountId',
          component: () => import('@/views/workspace/WorkspaceLayout.vue'),
          meta: { title: '工作区' },
          children: [
            { path: '', redirect: (to: any) => `/workspace/${to.params.accountId}/broadcast` },
            { path: 'broadcast', name: 'WorkspaceBroadcast', component: () => import('@/views/workspace/Broadcast.vue'), meta: { title: '群发消息', overlay: true } },
            { path: 'join', name: 'WorkspaceJoin', component: () => import('@/views/workspace/Join.vue'), meta: { title: '加群任务', overlay: true } },
            { path: 'scrape', name: 'WorkspaceScrape', component: () => import('@/views/workspace/Scrape.vue'), meta: { title: '成员采集', overlay: true } },
            { path: 'scrape-links', name: 'WorkspaceScrapeLinks', component: () => import('@/views/scrape/Links.vue'), meta: { title: '链接采集', overlay: true } },
            { path: 'monitor', name: 'WorkspaceMonitor', component: () => import('@/views/workspace/Monitor.vue'), meta: { title: '消息监控', overlay: true } },
            { path: 'tasks', name: 'WorkspaceTasks', component: () => import('@/views/workspace/Tasks.vue'), meta: { title: '任务列表', overlay: true } },
            { path: 'tasks/:id', name: 'WorkspaceTaskDetail', component: () => import('@/views/tasks/Detail.vue'), meta: { title: '任务详情', overlay: true } },
            { path: 'scheduled-tasks', redirect: (to: any) => `/workspace/${to.params.accountId}/broadcast` },  // 循环群发已并入群发页
          ]
        },

        // 旧路由重定向到 workspace 真页面(孤儿页已清理)
        { path: 'tasks', redirect: () => ({ path: '/workspace' }) },
        { path: 'tasks/:id', redirect: (to) => ({ path: `/workspace/${(to.params as any).accountId || ''}/tasks/${(to.params as any).id}` }) },
        { path: 'scrape', redirect: () => ({ path: '/workspace' }) },
        { path: 'scrape/links', redirect: (to) => ({ path: `/workspace/${(to.params as any).accountId || ''}/scrape-links` }) },
        // 旧 /monitor 孤儿路由已清理, 监控功能统一走 workspace/:accountId/monitor
        { path: 'scheduled-tasks', redirect: '/broadcast' },  // 循环群发已并入群发页 tab
      ]
    },
    // 管理员后台
    {
      path: '/admin/login',
      name: 'AdminLogin',
      component: () => import('@/views/admin/Login.vue'),
      meta: { public: true, title: '管理员登录' }
    },
    {
      path: '/admin',
      component: () => import('@/views/admin/Layout.vue'),
      meta: { requiresAdmin: true },
      children: [
        { path: '', redirect: '/admin/dashboard' },
        { path: 'dashboard', name: 'AdminDashboard', component: () => import('@/views/admin/dashboard/Index.vue'), meta: { title: '管理仪表盘' } },
        { path: 'plans', name: 'AdminPlans', component: () => import('@/views/admin/plans/Index.vue'), meta: { title: '套餐管理' } },
        { path: 'tenants', name: 'AdminTenants', component: () => import('@/views/admin/tenants/List.vue'), meta: { title: '租户管理' } },
        { path: 'tenants/:id', name: 'AdminTenantDetail', component: () => import('@/views/admin/tenants/Detail.vue'), meta: { title: '租户详情' } },
        { path: 'codes', name: 'AdminCodes', component: () => import('@/views/admin/codes/List.vue'), meta: { title: '卡密管理' } },
        { path: 'support', name: 'AdminSupport', component: () => import('@/views/admin/support/Config.vue'), meta: { title: '客服配置' } },
        { path: 'probe', name: 'AdminProbe', component: () => import('@/views/admin/probe/Config.vue'), meta: { title: '风控探测配置' } },
        { path: 'announcements', name: 'AdminAnnouncements', component: () => import('@/views/admin/announcements/List.vue'), meta: { title: '公告管理' } },
        { path: 'system', name: 'AdminSystem', component: () => import('@/views/admin/system/Monitor.vue'), meta: { title: '系统监控' } },
        { path: 'audit', name: 'AdminAudit', component: () => import('@/views/admin/audit/Logs.vue'), meta: { title: '审计日志' } },
      ]
    },
    { path: '/:pathMatch(.*)*', redirect: '/login' }
  ]
})

// P2-56: parseJWTPayload 已删——全项目零调用, 是"以为会用"留下的死代码;
// token 过期判断由后端 401 + request.ts 的 refresh 流程兜底。

// Phase 8 异常页: 404 catchAll——不存在的路径原来渲染空白
router.addRoute({ path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('@/views/NotFound.vue') })

router.afterEach((to) => {
  hideRouteLoading()
  const title = to.meta.title as string
  document.title = title ? `${title} - 博士轩群发器` : '博士轩群发器 - Telegram 运营平台'
})

// ===== 全局路由加载遮罩(R146 已删除): 全屏"加载中"动画改为按钮级反馈 =====

// R146: 空实现 —— 路由守卫仍调用 showRouteLoading() 并执行返回的 done(), 均为 no-op
function showRouteLoading(): () => void {
  return () => {}
}
function hideRouteLoading() {}

router.beforeEach(async (to, from, next) => {
  showRouteLoading()
  const authStore = useAuthStore()
  const adminStore = useAdminStore()

  // F-20: token 仅存内存。硬刷新后凭登录标志位 + HttpOnly refresh Cookie
  // 静默恢复会话，恢复失败才踢回登录页。
  if (to.meta.requiresAuth && !authStore.isLoggedIn) {
    const restored = await authStore.restoreSession()
    if (!restored) {
      next('/login')
      return
    }
  }

  // Check token expiration for tenant routes
  if (to.meta.requiresAuth) {
    // 预校验改为后台静默执行, 不阻塞导航:
    // 之前 await refreshTenant() 每次切页面都卡在网络往返上,
    // 弹窗长请求占满连接时直接表现为"页面卡死"
    authStore.refreshTenant().catch(() => {
      authStore.logout()
      if (router.currentRoute.value.meta.requiresAuth) router.replace('/login')
    })
    // 检查租户是否被停用（status 缺失时不拦截，由后端 403 兜底）
    if (authStore.tenant?.status && authStore.tenant.status !== 'active') {
      authStore.logout()
      next('/login')
      return
    }
  }

  // Check admin token expiration
  if (to.meta.requiresAdmin) {
    if (!adminStore.isLoggedIn) {
      next('/admin/login')
      return
    }
    // Admin tokens are validated by the API on each request
    // Optional: add admin-specific pre-validation here
  }

  if (to.path === '/login' && authStore.isLoggedIn) {
    next('/dashboard')
  } else if (to.path === '/admin/login' && adminStore.isLoggedIn) {
    next('/admin/dashboard')
  } else {
    next()
  }
})

// 懒加载 chunk 加载失败(常见于部署后旧缓存引用失效/移动 WebView 内存回收后恢复):
// 自动刷新拉取新资源, 但限频——60 秒内最多一次, 防止"顶条一直在动/页面反复刷新"的循环
router.onError((err) => {
  const msg = String(err)
  if (/dynamically imported module|Importing a module script failed|Failed to fetch dynamically/i.test(msg)) {
    // R99: 限频 60s→300s, 页面在后台时不刷新 —— 避免租户看到"网页过一会自己刷一下"
    if (document.visibilityState !== 'visible') return
    const now = Date.now()
    const last = Number(sessionStorage.getItem('chunk-reload-ts') || 0)
    if (now - last < 300000) return
    sessionStorage.setItem('chunk-reload-ts', String(now))
    window.location.reload()
  }
})

export default router
