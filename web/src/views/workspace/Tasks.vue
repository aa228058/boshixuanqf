<template>
  <main class="p-6">
    <div class="flex items-center justify-between mb-6 flex-wrap gap-3">
      <div>
        <h1 class="text-title font-extrabold tracking-tight text-text-primary truncate">任务列表</h1>
        <!-- R109: 任务列表为全租户视角(各账号任务汇总), 移除误导性的"当前账号"标签 -->
        <p class="text-sm text-text-muted mt-1 max-w-[65ch]">全部账号的任务汇总, 点卡片查看详情</p>
      </div>
      <!-- 类型 Tab: 点一次直接过滤 -->
      <div class="flex gap-1.5 overflow-x-auto pb-1 -mx-1 px-1">
        <button v-for="t in typeTabs" :key="t.value" @click="typeFilter = t.value"
                class="px-3 py-1.5 rounded-lg text-sm whitespace-nowrap transition-colors"
                :class="typeFilter === t.value ? 'bg-cyan-glow/50 text-cyan font-medium' : 'text-text-secondary hover:bg-bg-hover'">
          {{ t.label }}
        </button>
      </div>
      <div class="flex gap-2 mb-3">
  <button v-for="r in [['today','今日'],['7d','近7日'],['all','全部']]" :key="r[0]"
    @click="range = r[0]; page = 1; fetchTasks()"
    class="px-3 py-1 rounded-full text-xs transition-colors"
    :class="range === r[0] ? 'bg-cyan text-white font-semibold shadow-sm' : 'bg-bg border border-border text-text-secondary hover:text-text-primary'">
    {{ r[1] }}
  </button>
</div>
<div class="flex items-center gap-2 flex-wrap">
        <!-- R128 原生 select: t-select 的 tdesign 下拉面板在移动端定位滑到页面底部(用户反馈),
             原生 select 视觉已令牌化且系统自带可靠下拉 -->
        <select v-model="statusFilter"
                class="w-32 px-3 py-2 rounded-[var(--t-r-md)] text-xs font-medium outline-none cursor-pointer"
                style="background: var(--t-surface-2); border: 1px solid var(--t-line); color: var(--t-text-1);">
          <option value="">全部状态</option>
          <option value="running">运行中</option>
          <option value="queued">排队中</option>
          <option value="completed">已完成</option>
          <option value="failed">失败</option>
          <option value="cancelled">已取消</option>
        </select>
        <button @click="clearFinished" title="清空已完成任务"
                class="p-2 rounded-lg bg-rose/10 text-rose hover:bg-rose/20 transition-colors">
          <Trash2 class="w-4 h-4" />
        </button>
        <button @click="fetchTasks" class="p-2 rounded-lg bg-cyan-glow/50 text-cyan hover:bg-cyan/20 transition-colors">
          <RefreshCw class="w-4 h-4" />
        </button>
      </div>
    </div>

    <!-- 任务列表 -->
    <div class="space-y-3">
      <div v-if="loading" class="space-y-3 py-2">
        <div v-for="i in 5" :key="i" class="bg-bg-card rounded-[var(--t-r-md)] p-4 space-y-3">
          <div class="flex items-center gap-3">
            <div class="rounded-full bg-bg-hover/70 animate-pulse" style="width:2.5rem;height:2.5rem"></div>
            <div class="flex-1 space-y-2">
              <div class="rounded bg-bg-hover/70 animate-pulse" style="width:40%;height:0.875rem"></div>
              <div class="rounded bg-bg-hover/70 animate-pulse" style="width:25%;height:0.75rem"></div>
            </div>
            <div class="rounded bg-bg-hover/70 animate-pulse" style="width:4rem;height:1.25rem"></div>
          </div>
          <div class="rounded bg-bg-hover/70 animate-pulse" style="width:100%;height:0.5rem"></div>
        </div>
      </div>
      <div v-else-if="tasks.length === 0" class="py-6">
        <EmptyState title="暂无任务" description="创建群发/加群/采集任务后会显示在这里" />
      </div>
      <!-- 电脑端: TDesign 表格骨架 -->
      <div v-if="tasks.length" class="hidden md:block mt-2">
        <t-table row-key="task_id" :data="tasks" :columns="tableCols" hover style="min-width: 860px"
                 @row-click="(ctx: any) => $router.push(`/workspace/${route.params.accountId}/tasks/${ctx.row.task_id}`)">
          <template #status="{ row }">
            <span class="px-2 py-0.5 rounded text-xs font-medium" :class="statusClass(row.status)">{{ statusLabel(row.status) }}</span>
          </template>
          <template #type="{ row }">{{ taskTypeLabel(row.type) }}</template>
          <!-- 消息概览: 群发类任务预览文案前 24 字 -->
          <template #overview="{ row }">
            <span class="text-xs text-text-secondary max-w-[180px] truncate inline-block align-middle" :title="msgPreview(row)">{{ msgPreview(row) || '—' }}</span>
          </template>
          <!-- 目标数: 任务总目标量 -->
          <template #targets="{ row }">
            <span class="text-xs text-text-secondary">{{ progressOf(row.progress).total }}</span>
          </template>
          <!-- 累计下发: 成功/失败/等待 + 双色进度条 + 部分失败标签 -->
          <template #progress="{ row }">
            <div class="space-y-1">
              <div class="flex items-center gap-2">
                <div class="w-[120px] h-1.5 bg-bg-secondary rounded-full overflow-hidden shrink-0 flex">
                  <div class="h-full bg-cyan transition-all duration-500" :style="{ width: donePct(row) + '%' }" />
                  <div class="h-full bg-rose transition-all duration-500" :style="{ width: failedPct(row) + '%' }" />
                </div>
                <span v-if="progressOf(row.progress).failed > 0" class="px-1.5 py-0.5 rounded text-[10px] bg-amber/10 text-amber whitespace-nowrap">部分失败</span>
              </div>
              <div class="text-xs text-text-secondary whitespace-nowrap">
                成功 <span class="text-emerald font-medium">{{ progressOf(row.progress).done }}</span>
                / 失败 <span class="text-rose font-medium">{{ progressOf(row.progress).failed }}</span>
                / 等待 <span class="text-amber">{{ Math.max(0, progressOf(row.progress).total - progressOf(row.progress).done - progressOf(row.progress).failed) }}</span>
              </div>
              <!-- R118: 等待倒计时(仅运行中) -->
              <div v-if="row.status === 'running' && waitInfo[row.task_id]" class="text-[11px] text-amber whitespace-nowrap">
                <span class="tabular-nums font-medium">{{ waitInfo[row.task_id].remaining }}s</span>
                <span class="text-text-secondary"> · 账号 {{ accNameById(waitInfo[row.task_id].account_id) }} 等待中</span>
              </div>
            </div>
          </template>
          <template #created_at="{ row }">
            <span class="text-xs">{{ formatDate(row.created_at) }}</span>
          </template>
          <template #op="{ row }">
            <div class="flex items-center gap-1.5">
              <button v-if="row.status === 'running'" @click.stop="cancelTask(row.task_id)" class="px-2 py-1 rounded text-xs bg-rose/10 text-rose hover:bg-rose/20">停止</button>
              <button v-if="row.status === 'failed'" @click.stop="retryTask(row.task_id)" class="px-2 py-1 rounded text-xs bg-cyan-glow/50 text-cyan hover:bg-cyan/20">重试</button>
              <button v-if="['failed','cancelled','completed'].includes(row.status) && progressOf(row.progress).failed > 0"
                      @click.stop="retryTask(row.task_id, 'failed')" class="px-2 py-1 rounded text-xs bg-amber/10 text-amber hover:bg-amber/20">重试失败 ({{ progressOf(row.progress).failed }})</button>
              <!-- 每账号统计: 点击弹层查看该任务各账号成功/失败/跳过 -->
              <t-tooltip content="每账号统计" position="top">
                <button @click.stop="showAccStats(row)" class="px-2 py-1 rounded text-xs bg-ink-01 text-text-secondary hover:text-cyan hover:bg-cyan-glow/30">
                  <BarChart3 class="w-3.5 h-3.5" />
                </button>
              </t-tooltip>
            </div>
          </template>
        </t-table>
      </div>

      <!-- 手机端: 卡片列表(支持下拉刷新) -->
      <pull-down-refresh v-if="tasks.length" v-model="pullRefreshing" class="md:hidden" @refresh="onPullRefresh">
      <div v-for="(task, ti) in tasks" :key="task.task_id" :style="{ '--i': ti }"
           :class="['p-4 rounded-[var(--t-r-md)] border bg-bg-card card-lift transition-colors cursor-pointer', task.status === 'running' ? 'card-running' : 'border-border']"
           @click="$router.push(`/workspace/${route.params.accountId}/tasks/${task.task_id}`)" tabindex="0" role="button">
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-3">
            <span class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-bold whitespace-nowrap" :class="statusClass(task.status)">
              <i class="w-1.5 h-1.5 rounded-full bg-current opacity-80"></i>{{ statusLabel(task.status) }}
            </span>
            <span class="text-sm font-medium text-text-primary">{{ taskTypeLabel(task.type) }}</span>
            <!-- R112: 执行账号列 —— 全租户汇总后标识每条任务属于哪个号 -->
            <span v-if="accountNameOf(task)" class="text-[10.5px] px-1.5 py-0.5 rounded bg-cyan-glow/30 text-cyan whitespace-nowrap"
                  :title="'执行账号: ' + accountNameOf(task)">
              {{ accountNameOf(task) }}
            </span>
          </div>
          <span class="text-xs text-text-secondary">{{ formatDate(task.created_at) }}</span>
          <span v-if="task.status === 'queued'" class="text-xs text-amber">已等待 {{ elapsed(task.created_at) }}</span>
          <span v-else-if="task.status === 'running'" class="text-xs text-cyan">已运行 {{ elapsed(task.created_at) }}</span>
        </div>
        <div class="mt-2">
          <div class="flex items-center justify-between text-xs text-text-secondary mb-1">
            <span>成功 {{ progressOf(task.progress).done }} / 失败 {{ progressOf(task.progress).failed }} / 等待 {{ Math.max(0, progressOf(task.progress).total - progressOf(task.progress).done - progressOf(task.progress).failed) }}</span>
            <span v-if="progressOf(task.progress).failed > 0" class="px-1.5 py-0.5 rounded text-[10px] bg-amber/10 text-amber">部分失败</span>
          </div>
          <div class="w-full h-1.5 bg-bg-secondary rounded-full overflow-hidden flex">
            <div class="h-full bg-cyan transition-all duration-500" :class="task.status === 'running' ? 'bar-animated' : ''" :style="{ width: donePct(task.progress) + '%' }" />
            <div class="h-full bg-rose transition-all duration-500" :style="{ width: failedPct(task.progress) + '%' }" />
          </div>
          <!-- R118: 等待倒计时 —— 等待过程每秒可见 -->
          <div v-if="task.status === 'running' && waitInfo[task.task_id]" class="mt-1.5 flex items-center gap-1.5 text-[11px] text-amber">
            <span class="tabular-nums font-medium">{{ waitInfo[task.task_id].remaining }}s</span>
            <span class="text-text-secondary">· 账号 {{ accNameById(waitInfo[task.task_id].account_id) }} 等待中</span>
            <span v-if="waitInfo[task.task_id].total" class="text-text-muted">({{ waitInfo[task.task_id].next_index }}/{{ waitInfo[task.task_id].total }})</span>
          </div>
        </div>
        <div class="mt-2 flex items-center gap-2">
          <button v-if="task.status === 'running'" @click.stop="cancelTask(task.task_id)"
                  class="px-2 py-1 rounded text-xs bg-rose/10 text-rose hover:bg-rose/20">取消</button>
          <button v-if="task.status === 'failed'" @click.stop="retryTask(task.task_id)"
                  class="px-2 py-1 rounded text-xs bg-cyan-glow/50 text-cyan hover:bg-cyan/20">重试</button>
          <button v-if="['failed','cancelled','completed'].includes(task.status) && progressOf(task.progress).failed > 0"
                  @click.stop="retryTask(task.task_id, 'failed')"
                  class="px-2 py-1 rounded text-xs bg-amber/10 text-amber hover:bg-amber/20">重试失败 ({{ progressOf(task.progress).failed }})</button>
          <button @click.stop="showAccStats(task)" class="px-2 py-1 rounded text-xs bg-ink-01 text-text-secondary">统计</button>
        </div>
      </div>
      </pull-down-refresh>
    </div>

    <!-- 分页 -->
    <div v-if="total > pageSize" class="flex items-center justify-between mt-6">
      <span class="text-xs text-text-secondary">共 {{ total }} 条</span>
      <!-- P0-07: TDesign total 是总条数不是页数, 原传 totalPages 导致分页严重错误 -->
      <t-pagination :page="page" :total="total" :page-size="pageSize" @change="(p: any) => page = p.current" />
    </div>

    <!-- 每账号统计弹层: 任务内各账号成功/失败/跳过 -->
    <Modal v-model="accStatsVisible" :title="`每账号统计 — ${accStatsTaskName}`" width="480px">
      <div v-if="accStatsLoading" class="py-8 flex justify-center">
        <div class="skeleton-shimmer w-full h-8 rounded mb-2" v-for="i in 4" :key="i" />
      </div>
      <table v-else-if="accStatsData.length" class="w-full text-sm">
        <thead>
          <tr class="text-left text-xs text-text-muted border-b border-border">
            <th class="py-2 pr-2 font-medium">执行账号</th>
            <th class="py-2 pr-2 text-right font-medium text-emerald">成功</th>
            <th class="py-2 pr-2 text-right font-medium text-rose">失败</th>
            <th class="py-2 text-right font-medium">跳过</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in accStatsData" :key="row.phone" class="border-b border-border/50 row-hover">
            <td class="py-2.5 pr-2 text-text-primary">{{ row.phone }}</td>
            <td class="py-2.5 pr-2 text-right tabular-nums text-emerald">{{ row.success }}</td>
            <td class="py-2.5 pr-2 text-right tabular-nums text-rose">{{ row.failed }}</td>
            <td class="py-2.5 text-right tabular-nums text-text-secondary">{{ row.skipped }}</td>
          </tr>
        </tbody>
        <tfoot>
          <tr class="text-text-primary font-medium">
            <td class="py-2.5 pr-2">合计</td>
            <td class="py-2.5 pr-2 text-right tabular-nums text-emerald">
              {{ accStatsData.reduce((s, r) => s + r.success, 0) }}</td>
            <td class="py-2.5 pr-2 text-right tabular-nums text-rose">
              {{ accStatsData.reduce((s, r) => s + r.failed, 0) }}</td>
            <td class="py-2.5 text-right tabular-nums text-text-secondary">
              {{ accStatsData.reduce((s, r) => s + r.skipped, 0) }}</td>
          </tr>
        </tfoot>
      </table>
      <p v-else class="py-8 text-center text-sm text-text-muted">该任务暂无执行记录</p>
    </Modal>
  </main>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import { useRoute, useRouter } from 'vue-router'
import { useAccountStore } from '@/stores/account'
import { taskApi } from '@/api/task'
import { request } from '@/utils/request'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import { useTaskEvents } from '@/composables/useTaskEvents'
import { toast } from '@/utils/toast'
import { ask } from '@/utils/confirm'
import { ListTodo, RefreshCw, Trash2 } from 'lucide-vue-next'

const route = useRoute()
const router = useRouter()
const accountStore = useAccountStore()
const currentAccount = computed(() => accountStore.currentAccount)
// R112: 任务→执行账号名(account_ids 首号; 名称取 first_name/remark/phone)
const accountNameOf = (task: any) => {
  const id = task?.account_ids?.[0]
  if (!id) return ''
  const a = (accountStore.accounts || []).find((x: any) => x.account_id === id)
  return a ? (a.first_name || a.remark || a.phone || '') : ''
}
// R118: accName 死函数已删(模板无引用)

// 模板 12 行的类型过滤 Tab 此前引用未定义（Tab 栏整体不渲染，过滤功能失效）。
const typeTabs = [
  { value: '', label: '全部' },
  { value: 'broadcast', label: '群发' },
  { value: 'scrape_members', label: '采集' },
  { value: 'join_groups', label: '加群' },
  { value: 'monitor_scrape', label: '监控' },
  { value: 'scheduled_broadcast', label: '定时' },
]

const tasks = ref<any[]>([])
const loading = ref(false)
const loadError = ref('')
// E-02: 分页/筛选状态与 URL 双向同步——刷新/分享/前进后退均不丢状态
const query = route.query as Record<string, string | undefined>
const page = ref(Math.max(1, parseInt(query.page || '1', 10) || 1))
const pageSize = 20
const total = ref(0)
const range = ref(query.range || 'today') // 今日(默认) | 7d | all
const statusFilter = ref(query.status || '')
const typeFilter = ref(query.type || '')

const totalPages = computed(() => Math.ceil(total.value / pageSize))

// 清空已结束任务: 复用现有 batch/delete 接口, 零后端改动
// 跨页拉全量(每状态最多 500 条), 避免只清当前页漏其他页
// R109: 任务列表全租户化 —— 移除 account_id 过滤(原绑全局切换器, 已删除;
// 页头文案已是"全部账号的任务汇总")
async function clearFinished() {
  const ids: string[] = []
  for (const status of ['completed', 'failed', 'cancelled']) {
    try {
      const res: any = await taskApi.list({
        status,
        page: 1,
        page_size: 500,
        hide_loops: 1,
        range: 'all',
      } as any)
      const list = Array.isArray(res) ? res : (res?.data ?? res?.list ?? [])
      for (const t of list) if (t?.task_id) ids.push(t.task_id)
    } catch { /* 单状态拉取失败不阻塞整体 */ }
  }
  if (!ids.length) {
    toast({ type: 'info', title: '没有已结束的任务' })
    return
  }
  const ok = await ask(`确定清空 ${ids.length} 条已结束任务？此操作不可恢复`,
    { title: '清空任务', danger: true, confirmText: '清空' })
  if (!ok) return
  try {
    await taskApi.batchDelete(ids)
    toast({ type: 'success', title: `已清空 ${ids.length} 条任务` })
    page.value = 1
    fetchTasks()
  } catch (e: any) {
    toast({ type: 'error', title: '清空失败', message: e?.message || '请稍后重试' })
  }
}

async function fetchTasks() {
  loading.value = true
  try {
    const params: any = { page: page.value, page_size: pageSize, hide_loops: 1 }
    if (range.value !== 'all') params.range = range.value
    if (statusFilter.value) params.status = statusFilter.value
    if (typeFilter.value) params.type = typeFilter.value
    const res = await taskApi.list(params)
    const rawList = res.list || []
    // 2026-09-23 防御: 按 task_id 去重（用户实测"创建任务出现多条相同"——
    // 多为双击/网络重试导致创建了两条, 此处在渲染层兜底不重复显示）
    tasks.value = [...new Map(rawList.map(t => [t.task_id, t])).values()]
    total.value = res.total || 0
  } catch (e) {
    console.error('获取任务失败:', e)
  } finally {
    loading.value = false
  }
}

// 筛选变化时重置到第一页再拉取(避免停留在后面的页码导致筛选"看起来没反应")
watch([statusFilter, typeFilter], () => {
  if (page.value !== 1) page.value = 1
  else fetchTasks()
})

// E-02: 状态回写 URL（默认值不写，保持 URL 干净）
watch([page, range, statusFilter, typeFilter], () => {
  router.replace({
    query: {
      ...route.query,
      page: page.value > 1 ? String(page.value) : undefined,
      range: range.value !== 'today' ? range.value : undefined,
      status: statusFilter.value || undefined,
      type: typeFilter.value || undefined,
    },
  })
})

async function cancelTask(id: string) {
  try {
    await taskApi.cancel(id)
    fetchTasks()
    toast({ type: 'success', title: '任务已取消' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '取消失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

// 每账号统计弹层: 点任务行"统计"按钮, 懒加载该任务各账号成功/失败/跳过
const accStatsVisible = ref(false)
const accStatsLoading = ref(false)
const accStatsData = ref<{ phone: string; success: number; failed: number; skipped: number }[]>([])
const accStatsTaskName = ref('')

async function showAccStats(row: any) {
  accStatsVisible.value = true
  accStatsLoading.value = true
  accStatsTaskName.value = row.name || row.task_id?.slice(0, 8) || ''
  accStatsData.value = []
  try {
    const raw = await request.get<Record<string, { success: number; failed: number; skipped: number }>>(
      `/tasks/${row.task_id}/stats-by-account`)
    // account_id → 手机号映射(从任务 account_ids + 账号 store)
    const idSet = new Set<string>()
    const collect = (v: any) => { if (Array.isArray(v)) v.forEach((x: any) => idSet.add(String(x))) }
    collect(row.account_ids); collect(row.payload?.account_ids); collect(row.payload?.accounts)
    const phoneMap: Record<string, string> = {}
    for (const acc of accountStore.accounts) {
      if (idSet.has(String(acc.account_id))) phoneMap[String(acc.account_id)] = acc.phone
    }
    accStatsData.value = Object.entries(raw).map(([id, v]) => ({
      phone: phoneMap[id] || `账号 ${id.slice(0, 6)}…`,
      success: v.success, failed: v.failed, skipped: v.skipped,
    })).sort((a, b) => b.success - a.success)
  } catch { /* 请求层统一报错 */ }
  finally { accStatsLoading.value = false }
}

async function retryTask(id: string, scope?: 'failed') {
  try {
    await taskApi.retry(id, scope)
    fetchTasks()
    toast({ type: 'success', title: scope === 'failed' ? '已创建续发任务(仅失败目标)' : '任务已重试' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '重试失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

function statusClass(s: string) {
  const map: Record<string, string> = {
    running: 'bg-cyan-glow/50 text-cyan', queued: 'bg-amber/10 text-amber',
    completed: 'bg-emerald/10 text-emerald', failed: 'bg-rose/10 text-rose',
    cancelled: 'bg-text-muted/15 text-text-muted', paused: 'bg-text-muted/15 text-text-muted',
    pending: 'bg-amber/10 text-amber'
  }
  return map[s] || 'bg-text-muted/15 text-text-muted'
}

function statusLabel(s: string) {
  const map: Record<string, string> = {
    running: '运行中', queued: '排队中', completed: '已完成',
    failed: '失败', cancelled: '已取消', paused: '已暂停', pending: '待执行'
  }
  return map[s] || s
}

function taskTypeLabel(t: string) {
  const map: Record<string, string> = {
    broadcast: '群发消息', join_groups: '加群任务', join: '加群任务',
    scrape_members: '成员采集', scrape: '成员采集', scrape_links: '链接采集',
    scheduled_broadcast: '定时广播', monitor_scrape: '监控采集', invite_members: '邀请入群'
  }
  return map[t] || t
}

function progressBarClass(s: string) {
  return s === 'failed' ? 'bg-rose' : s === 'completed' ? 'bg-emerald' : 'bg-cyan'
}


// 电脑端表格列定义
const pullRefreshing = ref(false)
async function onPullRefresh() {
  await fetchTasks()
  pullRefreshing.value = false
}

const tableCols = [
  { colKey: 'status', title: '状态', width: 100 },
  { colKey: 'type', title: '类型', width: 120 },
  { colKey: 'overview', title: '消息内容', width: 200 },
  { colKey: 'targets', title: '目标数', width: 80 },
  { colKey: 'progress', title: '投放明细', width: 260 },
  { colKey: 'created_at', title: '创建时间', width: 170 },
  { colKey: 'op', title: '操作', width: 240 },
]

// 消息概览: 群发类任务取 payload.message 前 24 字
function msgPreview(t: any): string {
  const m = t?.payload?.message || t?.payload?.text || ''
  return typeof m === 'string' ? (m.length > 24 ? m.slice(0, 24) + '…' : m) : ''
}

function donePct(p: any) {
  const { total, done } = progressOf(p)
  return total ? Math.round((done / total) * 100) : 0
}

function failedPct(p: any) {
  const { total, failed } = progressOf(p)
  return total ? Math.round((failed / total) * 100) : 0
}

function progressOf(p: any) {
  if (typeof p === 'string') { try { p = JSON.parse(p) } catch { p = {} } }
  return { total: p?.total || 0, done: p?.done || 0, failed: p?.failed || 0 }
}

function progressPercent(p: any) {
  if (!p || !p.total) return 0
  return Math.round((p.done / p.total) * 100)
}

// R128 紧凑时间: 原 toLocaleString 全格式("2026/10/24 06:33:03")在任务卡占两行;
// 同年省年份, 一律秒级省略 → "10/24 06:33"
function formatDate(d: string) {
  const dt = new Date(d)
  const now = new Date()
  const sameYear = dt.getFullYear() === now.getFullYear()
  const mm = String(dt.getMonth() + 1).padStart(2, '0')
  const dd = String(dt.getDate()).padStart(2, '0')
  const hh = String(dt.getHours()).padStart(2, '0')
  const mi = String(dt.getMinutes()).padStart(2, '0')
  return `${sameYear ? '' : dt.getFullYear() + '/'}${mm}/${dd} ${hh}:${mi}`
}

// 等待/运行时长: 人性化显示 分/秒
function elapsed(iso: string): string {
  const s = Math.max(0, Math.floor((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 60) return `${s} 秒`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m} 分钟`
  return `${Math.floor(m / 60)} 小时 ${m % 60} 分`
}

watch(() => route.params.accountId, () => { page.value = 1; fetchTasks() }, { immediate: true })

// 15 秒轮询兜底 + SSE 亚秒级: 进度事件即时更新对应行, 状态事件即时刷新
// R118: 等待倒计时 —— task_wait 帧(每秒)驱动"当前动作"显示
const waitInfo = ref<Record<string, { account_id: string; remaining: number; next_index: number; total: number; _t: number }>>({})
function pruneWaitInfo() {
  const now = Date.now()
  for (const [id, info] of Object.entries(waitInfo.value)) {
    if (now - info._t > 5000) { const copy = { ...waitInfo.value }; delete copy[id]; waitInfo.value = copy }
  }
}
setInterval(pruneWaitInfo, 3000)
const accNameById = (id: string) => {
  const a = (accountStore.accounts || []).find((x: any) => String(x.account_id) === String(id))
  return a ? (a.first_name || a.remark || a.phone || id.slice(0, 6)) : id.slice(0, 6)
}

useAutoRefresh(fetchTasks, 15000)
useTaskEvents((evt: any) => {
  if (evt.type === 'task_wait') {
    waitInfo.value = { ...waitInfo.value, [String(evt.task_id)]: { account_id: String(evt.account_id || ''), remaining: Number(evt.remaining || 0), next_index: Number(evt.next_index || 0), total: Number(evt.total || 0), _t: Date.now() } }
    return
  }
  if (evt.type === 'task_status') {
    if (evt.status !== 'running' && waitInfo.value[evt.task_id]) { const copy = { ...waitInfo.value }; delete copy[evt.task_id]; waitInfo.value = copy }
    fetchTasks(); return
  }
  if (evt.progress && evt.task_id) {
    const t = tasks.value.find((x: any) => x.task_id === evt.task_id)
    if (t) t.progress = evt.progress
  }
})
watch(page, fetchTasks)
</script>
