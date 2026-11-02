<template>
  <main class="max-w-3xl mx-auto space-y-4">
    <Breadcrumb :items="[{ path: `/workspace/${accountStore.currentAccount?.account_id || ''}`, label: '工作台' }, { path: '', label: '链接采集' }]" />
    <div class="flex items-center gap-3">
      <button @click="$router.back()" class="p-2 rounded-lg hover:bg-bg-hover transition-colors" aria-label="返回">
        <ArrowLeft class="w-5 h-5" />
      </button>
      <h2 class="text-lg font-semibold text-gradient">群链接采集</h2>
    </div>

    <!-- R127 令牌化: seg 容器深浅主题自适应 -->
    <div class="flex rounded-[var(--t-r-lg)] p-1 gap-1 max-w-xs" style="background: color-mix(in srgb, var(--t-text-1) 8%, transparent);">
      <button
        v-for="t in [{ key: 'create', label: '链接监听' }, { key: 'results', label: '采集结果' }]"
        :key="t.key"
        @click="switchTab(t.key as 'create' | 'results')"
        class="flex-1 py-2 rounded-[12px] text-caption font-bold transition-all"
        :style="activeTab === t.key
          ? 'background: var(--t-surface-2); color: var(--t-text-1); box-shadow: 0 2px 8px rgba(0,0,0,0.18);'
          : 'color: var(--t-text-3);'"
      >
        {{ t.label }}
      </button>
    </div>

    <!-- R111: 协议协同创建Tab死块已删(view 恒 'phone'), 统一走监听开关流程 -->
    <!-- ===== 链接监听开关 ===== -->
    <div v-if="activeTab === 'create'" class="glass rounded-[var(--t-r-md)] p-5 space-y-4">
      <div class="flex items-center justify-between gap-4">
        <div class="min-w-0">
          <div class="font-medium">链接监听</div>
          <p class="text-xs text-text-muted mt-1 leading-relaxed">开启后, 当前账号自动采集<b>所有群组</b>消息中的邀请链接(公开+私密), 无需再选群</p>
        </div>
        <button
          @click="toggleListening"
          class="relative w-12 h-7 rounded-full transition-colors shrink-0"
          :class="listening ? 'bg-cyan' : 'bg-bg-hover'"
          aria-label="链接监听开关"
        ><span class="absolute top-1 w-5 h-5 rounded-full bg-white transition-all" :class="listening ? 'left-6' : 'left-1'" /></button>
      </div>
      <p v-if="listening" class="text-xs font-bold" style="color: var(--t-ok);">● 监听中, 结果在"采集结果"Tab 查看与导出</p>
      <p v-else class="text-xs text-text-muted">
        账号:
        <select v-model="linksAccId" class="input-box h-8 px-2 text-xs rounded-lg ml-1 align-middle">
          <option v-for="a in accountStore.accounts" :key="a.account_id" :value="a.account_id">{{ a.first_name || a.phone }}</option>
        </select>
      </p>
    </div>


    <!-- ===== 采集结果 ===== -->
    <div v-else class="space-y-3">
      <div v-if="tasksLoading" class="flex justify-center py-8">
        <t-loading size="small" loading class="inline-flex" />
      </div>

      <template v-else>
      <!-- R87-M8: 空态与下方总开关卡的空提示二选一(原双重空态), 展开面板时不再显示大空态 -->
      <EmptyState v-if="!tasks.length && !allLinksLoading && !allLinks.length && !allLinksOpen" icon="Link" title="暂无数据" description="暂无历史任务，也没有采集到链接" class="mb-3" />

      <!-- 总开关模式: 全部采集链接 -->
      <div class="glass rounded-[var(--t-r-md)] overflow-hidden mb-3">
        <button @click="toggleAllLinks" class="w-full flex items-center justify-between gap-3 p-4 text-left hover:bg-bg-hover/50 transition-colors">
          <div class="flex items-center gap-3 min-w-0">
            <ChevronRight class="w-4 h-4 shrink-0 transition-transform" :class="{ 'rotate-90': allLinksOpen }" />
            <span class="text-sm font-medium">当前账号全部采集链接</span>
            <span v-if="allLinks.length" class="text-xs text-text-muted">{{ allLinks.length }} 条</span>
          </div>
          <span class="flex items-center gap-2 shrink-0">
            <span class="text-xs px-3 py-1 rounded-lg border border-cyan/30 text-cyan hover:bg-cyan/10 transition-colors" @click.stop="exportAllLinks('txt')">导出TXT</span>
            <span class="text-xs px-3 py-1 rounded-lg border border-cyan/30 text-cyan hover:bg-cyan-glow/20 transition-colors" @click.stop="exportAllLinks('csv')">导出CSV</span>
          </span>
        </button>
        <div v-if="allLinksOpen" class="border-t border-border">
          <div v-if="allLinksLoading" class="flex justify-center py-6 text-sm text-text-muted">加载中...</div>
          <div v-else-if="!allLinks.length" class="p-6 text-center text-text-muted text-sm">暂无链接, 先在"链接监听"Tab 开启监听, 收到含链接的群消息后会自动入库</div>
          <div v-else>
            <!-- 单行横向布局: 链接截断 + 类型徽标 + 复制, 手机上不再一链接占一整块 -->
            <div class="px-4 py-2 text-xs text-text-muted">
              共 {{ allLinks.length }} 条 · 私密 {{ allLinks.filter(l => l.link_type === 'private').length }} · 公开 {{ allLinks.filter(l => l.link_type !== 'private').length }}
            </div>
            <div class="divide-y divide-border/50">
              <pull-down-refresh v-model="pullRefreshing" @refresh="onPullRefresh">
              <div v-for="link in allLinks" :key="link.link_id" class="px-4 py-2.5 hover:bg-bg-hover/30">
                <!-- 上行: 时间 + 来源群 -->
                <div class="flex items-center gap-2 text-xs text-text-muted mb-1">
                  <span>{{ new Date(link.created_at).toLocaleString('zh-CN') }}</span>
                  <span v-if="link.source_chat_title" class="truncate max-w-[45%]">来自 {{ link.source_chat_title }}</span>
                  <span v-else-if="link.source_chat_id">来自 {{ link.source_chat_id }}</span>
                </div>
                <!-- 下行: 链接 -->
                <div class="flex items-center gap-2">
                  <!-- P2修复(前端审查): full_url 来自被抓取的TG消息(攻击者可控制内容),
                       Vue 不净化 :href 绑定, javascript: 协议可造成同源 XSS。
                       仅 http/https 渲染为链接, 其余降级为纯文本(与 safeUrl 同款思路)。 -->
                  <a v-if="isHttpUrl(link.full_url)" :href="link.full_url" target="_blank" rel="noopener" class="text-cyan text-sm truncate min-w-0 flex-1" :title="link.full_url">{{ link.full_url }}</a>
                  <span v-else class="text-cyan text-sm truncate min-w-0 flex-1" :title="link.full_url">{{ link.full_url }}</span>
                  <span class="shrink-0 text-xs px-1.5 py-0.5 rounded bg-bg-hover text-text-muted">{{ link.link_type === 'private' ? '私密' : '公开' }}</span>
                  <button @click="copyLink(link.full_url)" class="shrink-0 text-xs px-2 py-0.5 rounded border border-border text-text-muted hover:text-cyan hover:border-cyan/40 transition-colors">复制</button>
                </div>
              </div>
              </pull-down-refresh>
            </div>
          </div>
        </div>
      </div>

      <div v-if="tasks.length" class="space-y-2">
        <div
          v-for="task in tasks"
          :key="task.task_id"
          class="glass rounded-[var(--t-r-md)] overflow-hidden"
        >
          <!-- 任务行 -->
          <button
            @click="toggleTask(task)"
            class="w-full flex items-center justify-between gap-3 p-4 text-left hover:bg-bg-hover/50 transition-colors"
          >
            <div class="flex items-center gap-3 min-w-0">
              <ChevronRight class="w-4 h-4 shrink-0 transition-transform" :class="{ 'rotate-90': expandedTaskId === task.task_id }" />
              <span class="text-sm font-medium truncate">任务 #{{ task.task_id.slice(0, 8) }}</span>
              <span
                class="text-xs px-2 py-0.5 rounded-full shrink-0"
                :class="{
                  'bg-green/10 text-green': task.status === 'completed',
                  'bg-yellow/10 text-yellow': task.status === 'running',
                  'bg-red/10 text-red': task.status === 'failed',
                  'bg-bg-hover text-text-muted': !['completed', 'running', 'failed'].includes(task.status),
                }"
              >{{ statusLabel(task.status) }}</span>
            </div>
            <div class="flex items-center gap-3 shrink-0">
              <span class="text-xs text-text-muted">{{ new Date(task.created_at).toLocaleString() }}</span>
              <span class="flex items-center gap-2">
                <span
                  class="text-xs px-3 py-1 rounded-lg border border-cyan/30 text-cyan hover:bg-cyan/10 transition-colors"
                  @click.stop="exportTaskLinks(task, 'txt')"
                >导出TXT</span>
                <span
                  class="text-xs px-3 py-1 rounded-lg border border-cyan/30 text-cyan hover:bg-cyan-glow/20 transition-colors"
                  @click.stop="exportTaskLinks(task, 'csv')"
                >导出CSV</span>
              </span>
            </div>
          </button>

          <!-- 展开: 链接列表 -->
          <div v-if="expandedTaskId === task.task_id" class="border-t border-border">
            <div v-if="linksLoading[task.task_id]" class="flex justify-center py-6">
              <t-loading size="small" loading class="inline-flex" />
            </div>
            <div v-else-if="!taskLinks[task.task_id]?.length" class="p-6 text-center text-text-muted text-sm">
              该任务窗口内暂无采集到链接(链接可能被去重过滤, 或服务未监听到消息)
            </div>
            <div v-else class="overflow-x-auto">
              <table class="w-full text-sm">
                <thead>
                  <tr class="text-left text-text-muted border-b border-border">
                    <th class="px-4 py-2 font-normal">链接</th>
                    <th class="px-4 py-2 font-normal">类型</th>
                    <th class="px-4 py-2 font-normal">解析结果</th>
                    <th class="px-4 py-2 font-normal">成员数</th>
                    <th class="px-4 py-2 font-normal">来源群</th>
                    <th class="px-4 py-2 font-normal">采集时间</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="link in taskLinks[task.task_id]" :key="link.link_id" class="border-b border-border/50 hover:bg-bg-hover/30">
                    <td class="px-4 py-2">
                      <a v-if="isHttpUrl(link.full_url)" :href="link.full_url" target="_blank" rel="noopener" class="text-cyan hover:underline break-all">{{ link.full_url }}</a>
                      <span v-else class="text-cyan break-all">{{ link.full_url }}</span>
                    </td>
                    <td class="px-4 py-2">{{ link.link_type === 'private' ? '私密' : '公开' }}</td>
                    <td class="px-4 py-2">{{ link.resolved_title || '-' }}</td>
                    <td class="px-4 py-2">{{ link.member_count ?? '-' }}</td>
                    <td class="px-4 py-2">{{ link.source_chat_title || link.source_chat_id }}</td>
                    <td class="px-4 py-2 text-text-muted whitespace-nowrap">{{ new Date(link.created_at).toLocaleString() }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>
      </template>
    </div>
  </main>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import { useAuthStore } from '@/stores/auth'
import { useAccountStore } from '@/stores/account'
import { toast } from '@/utils/toast'
// R111 已删 ProtocolPicker/campaignApi/CAMPAIGN_STATUS 导入(协议协同死代码)
import { api } from '@/utils/request'

import { useRouter } from 'vue-router'
import { taskApi } from '@/api/task'
import type { ScrapedLink } from '@/api/task'

import Breadcrumb from '@/components/ui/Breadcrumb.vue'
// R115: Group/Account 死类型导入已删(正文零使用)

// 仅放行 http/https 协议, 拦截 javascript:/data: 等危险协议(同 safeUrl 思路)
const isHttpUrl = (u: string): boolean => /^https?:\/\//i.test(u || '')

const router = useRouter()
const authStore = useAuthStore()
const accountStore = useAccountStore()

// ===== Tab: create=新建采集 results=采集结果 =====
const activeTab = ref<'create' | 'results'>('create')

function switchTab(tab: 'create' | 'results') {
  activeTab.value = tab
  if (tab === 'results') {
    fetchTasks()
    // 预载链接总量, 避免 EmptyState 因 allLinks 未加载而误报"暂无数据"
    if (!allLinks.value.length && !allLinksLoading.value) loadAllLinks()
  }
}

// ===== 采集结果 Tab =====
interface ScrapeTask {
  task_id: string
  status: string
  account_ids: string[]
  created_at: string
}
// 总开关模式: 直接展示当前账号采集到的全部链接(不依赖任务)
const allLinks = ref<ScrapedLink[]>([])
const allLinksOpen = ref(false)
const allLinksLoading = ref(false)

const tasks = ref<ScrapeTask[]>([])
const tasksLoading = ref(false)
const expandedTaskId = ref<string | null>(null)
const taskLinks = ref<Record<string, ScrapedLink[]>>({})
const linksLoading = ref<Record<string, boolean>>({})

// R108: 视图级账号切换 —— 监听配置按账号独立, 下拉选择管理哪个号(不依赖顶栏切换器)
const linksAccId = ref('')
watch(accountStore.currentAccount, (acc: any) => { if (acc?.account_id && !linksAccId.value) linksAccId.value = acc.account_id }, { immediate: true })
watch(linksAccId, () => { fetchTasks(); loadAllLinks() })

async function fetchTasks() {
  const accId = linksAccId.value
  if (!accId) return
  tasksLoading.value = true
  try {
    const res = await taskApi.list({ type: 'scrape_links', account_id: accId, page: 1, page_size: 50 } as any)
    const raw = res as any
    tasks.value = raw.list || raw.items || []
  } catch (e: unknown) {
    toast({ type: 'error', title: '加载任务失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  } finally {
    tasksLoading.value = false
  }
}

const pullRefreshing = ref(false)
async function onPullRefresh() {
  // P1-28 审计修复: 原 Promise.all 任一请求失败, 后续赋值不执行——下拉转圈永不
  // 停止。finally 保证收尾, allSettled 允许部分成功。
  try {
    await Promise.allSettled([loadAllLinks(), fetchTasks()])
  } finally {
    pullRefreshing.value = false
  }
}

async function loadAllLinks() {
  const accId = linksAccId.value
  if (!accId) return
  allLinksLoading.value = true
  try {
    const res = await taskApi.listScrapedLinks({ account_id: accId, page: 1, page_size: 100 })
    const raw = res as any
    allLinks.value = raw.list || raw.items || []
  } catch (e: any) {
    toast({ type: 'error', title: '加载链接失败', message: e?.message || String(e) })
  } finally {
    allLinksLoading.value = false
  }
}

async function toggleAllLinks() {
  allLinksOpen.value = !allLinksOpen.value
  if (allLinksOpen.value && !allLinks.value.length) await loadAllLinks()
}

async function toggleTask(task: ScrapeTask) {
  if (expandedTaskId.value === task.task_id) {
    expandedTaskId.value = null
    return
  }
  expandedTaskId.value = task.task_id
  if (taskLinks.value[task.task_id]) return
  linksLoading.value = { ...linksLoading.value, [task.task_id]: true }
  try {
    // since=任务创建时间, 圈出该任务的采集窗口
    const res = await taskApi.listScrapedLinks({
      account_id: task.account_ids[0],
      since: task.created_at,
      page: 1,
      page_size: 100,
    })
    const raw = res as any
    taskLinks.value = { ...taskLinks.value, [task.task_id]: raw.list || raw.items || [] }
  } catch (e: unknown) {
    toast({ type: 'error', title: '加载链接失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  } finally {
    linksLoading.value = { ...linksLoading.value, [task.task_id]: false }
  }
}

// 复制链接到剪贴板(clipboard API 不可用时降级)
async function copyLink(url: string) {
  try {
    await navigator.clipboard.writeText(url)
    toast({ type: 'success', title: '已复制链接' })
  } catch {
    const ta = document.createElement('textarea')
    ta.value = url
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
    toast({ type: 'success', title: '已复制链接' })
  }
}

// 导出纯文本(每行一个链接), 与"自动加群-导入链接"的格式一致
async function exportTaskLinks(task: ScrapeTask, format = 'txt') {
  try {
    const base = import.meta.env.VITE_API_BASE_URL || '/api/v1'
    const params = new URLSearchParams({ account_id: task.account_ids[0], since: task.created_at, format })
    const resp = await fetch(`${base}/scrape-links/export?${params}`, {
      headers: { Authorization: `Bearer ${authStore.token}` },
    })
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
    const text = await resp.text()
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `scrape-links-${task.task_id.slice(0, 8)}.${format}`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e: unknown) {
    toast({ type: 'error', title: '导出失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

// 导出全部链接(不带时间窗口)
async function exportAllLinks(format: 'txt' | 'csv') {
  const acc = accountStore.accounts.find((a: any) => a.account_id === linksAccId.value)
  if (!acc) { toast({ type: 'error', title: '请先选择账号' }); return }
  try {
    const base = import.meta.env.VITE_API_BASE_URL || '/api/v1'
    const params = new URLSearchParams({ account_id: acc.account_id, format })
    const resp = await fetch(`${base}/scrape-links/export?${params}`, {
      headers: { Authorization: `Bearer ${authStore.token}` },
    })
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
    const text = await resp.text()
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `scrape-links-all.${format}`
    a.click()
    URL.revokeObjectURL(url)
  } catch {
    toast({ type: 'error', title: '导出失败' })
  }
}

function statusLabel(s: string) {
  const m: Record<string, string> = {
    queued: '排队中', running: '运行中', paused: '已暂停',
    completed: '已完成', failed: '失败', cancelled: '已取消',
  }
  return m[s] || s
}


onMounted(() => {
  if (!authStore.tenant?.plan_features?.scrape_links) {
    toast({ type: 'error', title: '当前套餐不支持链接采集，请联系客服升级套餐' })
    router.push('/dashboard')
  }
})

// ===== 链接监听总开关 =====
const listening = ref(false)

onMounted(async () => {
  const acc = accountStore.accounts.find((a: any) => a.account_id === linksAccId.value)
  if (!acc) return
  try {
    const res = await fetch(`${import.meta.env.VITE_API_BASE_URL || '/api/v1'}/scrape-links/status?account_id=${encodeURIComponent(acc.account_id)}`, {
      headers: { Authorization: `Bearer ${authStore.token}` },
    })
    if (res.ok) listening.value = (await res.json()).enabled
  } catch {}
})

// ===== R111: 协议协同脚本已全部删除(loadCampaigns/submitProtocol/controlCampaign/exportLinks) =====

async function toggleListening() {
  const acc = accountStore.accounts.find((a: any) => a.account_id === linksAccId.value)
  if (!acc) { toast({ type: 'error', title: '请先选择账号' }); return }
  const next = !listening.value
  try {
    const res = await fetch(`${import.meta.env.VITE_API_BASE_URL || '/api/v1'}/scrape-links/toggle`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${authStore.token}` },
      body: JSON.stringify({ account_id: acc.account_id, enabled: next }),
    })
    if (!res.ok) throw new Error()
    listening.value = next
    toast({ type: 'success', title: next ? '链接监听已开启' : '链接监听已关闭' })
    if (next) switchTab('results')
  } catch {
    toast({ type: 'error', title: '操作失败, 请重试' })
  }
}
</script>
