<template>
  <div class="p-4 sm:p-6 max-w-4xl mx-auto">
    <div class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 mb-4">
      <div>
        <h1 class="text-title font-extrabold tracking-tight text-text-primary truncate">成员采集</h1>
        <!-- R119: 账号选择已挪进创建弹窗(胶囊选择), 页头不再放下拉 -->
      </div>
      <div class="flex items-center gap-2 shrink-0 ml-auto">
        <button v-if="tasks.length" @click="clearLogs"
                class="px-3 py-2 rounded-lg text-sm text-rose/70 border border-rose/20 hover:bg-rose/10 hover:text-rose transition-colors flex items-center gap-2">
          <Trash2 class="w-4 h-4" />
          清空记录
        </button>
        <button @click="showCreate = true"
                class="px-4 py-2 rounded-lg text-sm font-medium glow-btn flex items-center gap-2"> 开始采集
        </button>
      </div>
    </div>

    <!-- 新建模态框: 统一全站弹窗组件 -->
    <Modal v-model="showCreate" title="新建采集任务">
      <!-- R119: 执行账号选择挪进创建弹窗 —— 与群发/加群同构, 页头不再单独占一行 -->
      <div>
        <label class="block text-sm font-medium mb-2">执行账号</label>
        <div class="flex flex-wrap gap-1.5">
          <button v-for="a in accountStore.accounts" :key="a.account_id" type="button"
                  @click="scrapeAccId = a.account_id"
                  class="px-3 py-1.5 rounded-full border text-xs transition-colors"
                  :class="scrapeAccId === a.account_id ? 'border-cyan text-cyan bg-cyan/10' : 'border-border text-text-secondary hover:border-cyan/30'">
            {{ accName(a) }}
          </button>
        </div>
      </div>
      <div>
        <label class="block text-sm font-medium mb-2">选择要采集的群组</label>
        <div @click="showGroupSelector = true"
             class="flex items-center gap-2 p-3 rounded-lg border border-border cursor-pointer hover:border-cyan/30 transition-colors">
          <MessageSquare class="w-5 h-5 text-text-muted" />
          <span v-if="!selectedGroup" class="text-text-muted">点击选择群组</span>
          <span v-else class="text-sm">{{ selectedGroup.title }} ({{ selectedGroup.member_count }} 成员)</span>
        </div>
      </div>
      <!-- 采集模式: 隐私群(成员列表不可见)改用历史消息活跃用户 -->
      <div class="pt-1">
        <div class="flex gap-2 mb-1.5">
          <button type="button" @click="scrapeMode = 'members'"
            class="flex-1 px-3 py-2 rounded-lg border text-sm transition-colors"
            :class="scrapeMode === 'members' ? 'border-cyan text-cyan bg-cyan/5' : 'border-border text-text-secondary'">
            普通成员列表
          </button>
          <button type="button" @click="scrapeMode = 'history'"
            class="flex-1 px-3 py-2 rounded-lg border text-sm transition-colors"
            :class="scrapeMode === 'history' ? 'border-cyan text-cyan bg-cyan/5' : 'border-border text-text-secondary'">
            历史消息活跃用户
          </button>
        </div>
        <p class="text-xs text-text-muted">
          {{ scrapeMode === 'history'
            ? '隐私群用此模式: 翻群历史消息提取所有发过言的用户, 结果可直接私聊'
            : '常规模式: 拉取群成员列表, 可按在线状态/头像筛选' }}
        </p>
        <div v-if="scrapeMode === 'history'" class="mt-2 flex items-center gap-2">
          <span class="text-xs text-text-secondary shrink-0">扫描消息数上限</span>
          <input v-model.number="maxMessages" type="number" min="100" max="10000" step="100"
            class="w-28 px-3 py-1.5 rounded-lg bg-bg-card border border-border text-sm" />
        </div>
      </div>
      <!-- 采集过滤: 死号/无头像不入库(仅普通模式) -->
      <div v-if="scrapeMode === 'members'" class="space-y-2 pt-1">
        <label class="flex items-center gap-2 text-sm cursor-pointer">
          <input type="checkbox" v-model="filterRecent" class="accent-cyan" />
          仅最近上线
          <!-- 天数选择: 原生 select 移动端自动唤起系统滚轮, 零依赖零报错 -->
          <select v-if="filterRecent" v-model.number="filterRecentDays"
                  class="bg-bg-card border border-border rounded px-2 py-0.5 text-xs text-cyan appearance-none cursor-pointer">
            <option :value="3">3 天内</option>
            <option :value="7">7 天内</option>
            <option :value="30">30 天内</option>
          </select>
          <span class="text-xs text-text-muted">滤掉长期离线的死号</span>
        </label>
        <label class="flex items-center gap-2 text-sm cursor-pointer">
          <input type="checkbox" v-model="filterPhoto" class="accent-cyan" />
          仅带头像用户
          <span class="text-xs text-text-muted">无头像多为小号/机器号</span>
        </label>
      </div>
      <div class="flex items-center justify-end gap-3 pt-2">
        <button @click="showCreate = false" class="px-4 py-2 rounded-lg text-sm text-text-secondary hover:bg-bg-card transition-colors">取消</button>
        <button @click="submit" :disabled="(!selectedGroup || !scrapeAccId) || submitting"
                class="px-6 py-2 rounded-lg text-sm font-medium glow-btn disabled:opacity-50 flex items-center gap-2">
          <Loader2 v-if="submitting" class="w-4 h-4 animate-spin" />
          {{ submitting ? '创建中...' : '开始采集' }}
        </button>
      </div>
    </Modal>

    <!-- 采集任务列表 -->
    <div class="space-y-3">
      <div v-if="taskLoading" class="py-12 text-center">
        <div class="animate-spin w-6 h-6 border-2 border-cyan border-t-transparent rounded-full mx-auto mb-3" />
        <p class="text-sm text-text-muted">加载中...</p>
      </div>
      <EmptyState v-else-if="!tasks.length" icon="Download" title="暂无采集任务" description="点击上方「开始采集」选择群组，可筛选活号/有头像用户" />
      <pull-down-refresh v-if="tasks.length" v-model="pullRefreshing" @refresh="onPullRefresh">
      <div v-for="(task, ti) in tasks" :key="task.task_id"
           class="glass rounded-[var(--t-r-md)] p-4 border border-border card-lift card-enter hover:border-cyan/30" :style="{ '--i': ti }">
        <div class="flex items-center justify-between gap-3 cursor-pointer" @click="toggleExpand(task.task_id)">
          <div class="flex items-center gap-3 min-w-0">
            <div class="w-10 h-10 rounded-[var(--t-r-md)] bg-amber-glow/30 flex items-center justify-center shrink-0">
              <Users class="w-5 h-5 text-amber" />
            </div>
            <div class="min-w-0">
              <div class="text-sm font-medium truncate">{{ task.config?.group_title || '成员采集' }} #{{ task.task_id?.slice(-6) }}</div>
              <div class="text-xs text-text-muted flex items-center gap-2 mt-0.5">
                <span>{{ formatDate(task.created_at) }}</span>
                <span class="w-1 h-1 rounded-full bg-text-muted" />
                <span v-if="task.status === 'running'">采集中...</span>
                <span v-else-if="task.status === 'completed' && (task.result_count || task.config?.member_count)">{{ task.result_count || task.config?.member_count }} 成员</span>
                <span v-else-if="task.status === 'completed'">点击展开查看结果</span>
                <!-- R112: 导出前置 —— 完成后无需展开即可导出名单 -->
                <template v-if="task.status === 'completed'">
                  <button @click.stop="exportCSV(task.task_id)" class="text-[10.5px] px-2 py-0.5 rounded bg-emerald/10 text-emerald border border-emerald/25 hover:bg-emerald/20 transition-colors">导出 CSV</button>
                  <button @click.stop="exportExcel(task.task_id)" class="text-[10.5px] px-2 py-0.5 rounded bg-cyan-glow/30 text-cyan border border-cyan/25 hover:bg-cyan-glow/50 transition-colors">Excel</button>
                </template>
                <span v-else>{{ task.result_count || task.config?.member_count || 0 }} 成员</span>
              </div>
            </div>
          </div>
          <div class="flex items-center gap-2 shrink-0">
            <span class="px-2 py-0.5 rounded text-xs font-medium"
                  :class="task.status === 'completed' ? 'bg-emerald/10 text-emerald' : task.status === 'running' ? 'bg-cyan/10 text-cyan' : task.status === 'failed' ? 'bg-rose/10 text-rose' : 'bg-text-muted/10 text-text-muted'">
              {{ statusText(task.status) }}
            </span>
            <ChevronDown class="w-4 h-4 text-text-muted transition-transform" :class="expanded[task.task_id] ? 'rotate-180' : ''" />
          </div>
        </div>
        <div v-if="task.status === 'running' && task.progress" class="mt-2">
          <div class="flex items-center justify-between text-xs mb-1">
            <span class="text-text-secondary">进度</span>
            <span class="text-amber">{{ progressOf(task).done }} / {{ progressOf(task).total }}</span>
          </div>
          <div class="h-1.5 bg-bg-card rounded-full overflow-hidden">
            <div class="h-full bg-amber rounded-full transition-all" :style="{ width: progressPct(task) + '%' }" />
          </div>
        </div>

        <!-- 展开: 成员结果列表 -->
        <div v-if="expanded[task.task_id]" class="mt-3 pt-3 border-t border-border">
          <div v-if="memberLoading[task.task_id]" class="py-4 text-center text-sm text-text-muted">加载成员中...</div>
          <div v-else-if="!members[task.task_id]?.length" class="py-4 text-center text-sm text-text-muted">该任务暂无采集到成员</div>
          <template v-else>
            <div class="flex items-center justify-between text-xs text-text-muted px-2 pb-2">
              <span>共 {{ members[task.task_id].length }} 名成员<span v-if="skippedBots[task.task_id]"> · 已自动跳过 {{ skippedBots[task.task_id] }} 个机器人</span></span>
            </div>
            <div class="max-h-56 overflow-y-auto space-y-1">
              <div v-for="(m, idx) in members[task.task_id]" :key="idx" class="flex items-center gap-3 px-2 py-1.5 rounded hover:bg-bg-hover/50 text-sm">
                <span class="text-cyan font-mono text-xs w-32 shrink-0">{{ m.tg_user_id || m.user_id }}</span>
                <span class="truncate">{{ m.username || m.first_name || '未设置用户名' }}</span>
                <span class="ml-auto flex items-center gap-2 shrink-0 text-xs text-text-muted">
                  <span v-if="m.has_photo" title="有头像">👤</span>
                  <span v-if="m.last_seen">{{ relTime(m.last_seen) }}</span>
                </span>
              </div>
            </div>
            <div class="flex justify-end gap-2 mt-3">
              <button @click="exportCSV(task.task_id)" class="text-xs px-3 py-1.5 rounded-lg border border-border hover:border-cyan/40 transition-colors">导出 CSV</button>
              <button @click="exportExcel(task.task_id)" class="text-xs px-3 py-1.5 rounded-lg border border-border hover:border-cyan/40 transition-colors">导出 Excel</button>
            </div>
          </template>
        </div>
    </div>

      </pull-down-refresh>
    <!-- 群组选择弹窗(复用群发同款组件: 搜索/标签页/同步刷新) -->
    <GroupSelector v-model="showGroupSelector" :groups="groups" :account-id="scrapeAccId" mode="single" @select="onSelectGroup" />
    </div>
  </div>
  <LiveTaskFeed v-if="scrapeAccId" :account-id="scrapeAccId" task-type="scrape_members" class="mt-6" />

</template>

<script setup lang="ts">
// 进度对象后端可能是 JSON 字符串或对象,统一解析
function progressOf(task: any): { total: number; done: number; failed: number } {
  const p = task?.progress
  if (!p) return { total: 0, done: 0, failed: 0 }
  if (typeof p === 'string') {
    try { return { total: 0, done: 0, failed: 0, ...JSON.parse(p) } } catch { return { total: 0, done: 0, failed: 0 } }
  }
  return p
}
function progressPct(task: any): number {
  const p = progressOf(task)
  return p.total > 0 ? Math.round(p.done / p.total * 100) : 0
}

import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAccountStore } from '@/stores/account'
// R111 已删 ProtocolPicker/campaignApi/CAMPAIGN_STATUS 导入(协议协同死代码)
import { useAuthStore } from '@/stores/auth'
import { groupApi } from '@/api/group'
import { taskApi } from '@/api/task'
import { Loader2, MessageSquare, Plus, X, Users, ChevronDown, Download, Trash2 } from 'lucide-vue-next'
import LiveTaskFeed from '@/components/LiveTaskFeed.vue'
import GroupSelector from '@/components/selectors/GroupSelector.vue'
import { useGroupStore } from '@/stores/group'
import { toast } from '@/utils/toast'
import EmptyState from '@/components/ui/EmptyState.vue'
import Modal from '@/components/ui/Modal.vue'
import type { Group } from '@/types'
import { ask } from '@/utils/confirm'

const route = useRoute()
const router = useRouter()
const accountStore = useAccountStore()
const authStore = useAuthStore()
const currentAccount = computed(() => accountStore.currentAccount)
const accName = (a: any) => a?.first_name || a?.remark || a?.phone || (a?.account_id || '').slice(0, 8)
// R108: 执行账号下拉 —— 群列表/任务列表/提交全部跟随所选账号, 不依赖顶栏切换器
const scrapeAccId = ref('')
watch(currentAccount, (acc: any) => { if (acc?.account_id && !scrapeAccId.value) scrapeAccId.value = acc.account_id }, { immediate: true })
watch(scrapeAccId, () => { fetchGroups(); fetchTasks() })

const selectedGroup = ref<any>(null)
// 采集过滤: 死号/无头像不入库
// 采集模式: members=普通成员列表(Participants) history=历史消息活跃用户(隐私群 Participants 只给管理员时用)
const scrapeMode = ref<'members' | 'history'>('members')
const maxMessages = ref(1000)
const filterRecent = ref(false)
const filterRecentDays = ref(7)
const filterPhoto = ref(false)
const showGroupSelector = ref(false)
const showCreate = ref(false)
// 响应式绑定 groupStore（与 Broadcast.vue 同款修复，2026-09-22）：
// 后台同步完成后写缓存即自动刷新选择器，不再依赖整页刷新
const groupStore = useGroupStore()
const groups = computed<any[]>(() => {
  return scrapeAccId.value ? (groupStore.get(scrapeAccId.value) || []) : []
})
// ===== R111: 协议号协同变量已全部删除(view 死常量恒 'phone') =====
const submitting = ref(false)

// 模板 149 行的相对时间调用此前无定义（渲染即崩）。
function relTime(iso: string): string {
  const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 60) return `${s}秒前`
  if (s < 3600) return `${Math.floor(s / 60)}分钟前`
  if (s < 86400) return `${Math.floor(s / 3600)}小时前`
  return `${Math.floor(s / 86400)}天前`
}

const tasks = ref<any[]>([])
const taskLoading = ref(false)
const expanded = ref<Record<string, boolean>>({})
const members = ref<Record<string, any[]>>({})
const memberLoading = ref<Record<string, boolean>>({})
const skippedBots = ref<Record<string, number>>({})

// 判断是否为机器人账号(is_bot 字段或 username 以 bot 结尾)
function isBot(m: any): boolean {
  return !!m.is_bot || (m.username || '').toLowerCase().endsWith('bot')
}

// 清空当前账号的全部采集任务记录(进行中的任务不会删除)
const clearing = ref(false)
async function clearLogs() {
  const finished = tasks.value.filter(t => t.status !== 'running' && t.status !== 'queued')
  if (!finished.length) return
  if (!await ask(`确定清空 ${finished.length} 条采集记录？此操作不可恢复`)) return
  clearing.value = true
  try {
    await taskApi.batchDelete(finished.map(t => t.task_id))
    expanded.value = {}
    members.value = {}
    skippedBots.value = {}
    await fetchTasks()
  } catch (e: any) {
    toast({ type: 'error', title: '清空失败', message: e?.message || '请稍后重试' })
  } finally {
    clearing.value = false
  }
}

function statusText(s: string) {
  const map: Record<string, string> = { pending: '待执行', running: '执行中', completed: '已完成', failed: '失败', cancelled: '已取消' }
  return map[s] || s
}
function formatDate(d: string) {
  if (!d) return '-'
  return new Date(d).toLocaleString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function selectGroup(g: any) {
  selectedGroup.value = g
}

// 单选模式: 取第一个选中项
function onSelectGroup(selected: Group[]) {
  if (selected.length) selectGroup(selected[0])
}

function toggleExpand(taskId: string) {
  expanded.value[taskId] = !expanded.value[taskId]
  if (expanded.value[taskId] && !members.value[taskId]) {
    loadMembers(taskId)
  }
}

async function loadMembers(taskId: string) {
  memberLoading.value[taskId] = true
  try {
    const task = tasks.value.find(t => t.task_id === taskId)
    const groupId = (task?.payload as any)?.group_id || task?.target_ids?.[0]
    if (!groupId) {
      members.value[taskId] = []
      return
    }
    const res = await groupApi.getMembers(groupId)
    const raw = (res as any).members || res.list || res || []
    // 自动跳过机器人结果(源头已在采集时过滤, 这里兜底处理历史数据)
    skippedBots.value[taskId] = raw.filter(isBot).length
    members.value[taskId] = raw.filter((m: any) => !isBot(m))
  } catch {
    members.value[taskId] = []
  } finally {
    memberLoading.value[taskId] = false
  }
}

function exportCSV(taskId: string) {
  const list = members.value[taskId] || []
  if (!list.length) return
  const headers = ['user_id', 'username', 'first_name', 'last_name', 'last_seen', 'has_photo', 'is_bot']
  // 精读修复: 统一过 sanitizeCell(防 Excel 公式注入 =cmd|...)
  const rows = list.map((m: any) => [m.user_id, m.username || '', m.first_name || '', m.last_name || '', m.last_seen || '', m.has_photo ? '1' : '0', m.is_bot ? '1' : '0'])
  // P0-15 审计修复: 原路径先过 sanitizeCell(HTML 转义, " 已变 &quot;) 再做 CSV
  // 引号翻倍——HTML 实体进了 CSV 文件, 引号双转义失效。CSV 路径只做公式注入
  // 防护 + CSV 标准引号翻倍, 不做 HTML 转义。
  const csv = [headers.join(','), ...rows.map((r: any[]) => r.map(csvCell).join(','))].join('\n')
  const blob = new Blob(['\uFEFF' + csv], { type: 'text/csv;charset=utf-8;' })
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = `members_${taskId.slice(-6)}_${new Date().toISOString().slice(0,10)}.csv`
  link.click()
}

// 导出单元格消毒：数据来自 TG 用户名/昵称等外部输入，恶意用户可设 =cmd|...!A1
// 这类用户名，受害者导出后在 Excel 打开即执行（公式注入）；HTML 导出还需防
// </td><script> 类标签注入。处理：公式前缀(= + - @)加 tab 强制按文本 + HTML 转义。
// P0-15: CSV 单元格专用——公式前缀加 tab + CSV 引号翻倍(标准), 不做 HTML 转义
function csvCell(v: any): string {
  let s = String(v ?? '')
  if (/^[=+\-@]/.test(s)) s = '\t' + s
  return `"${s.replace(/"/g, '""')}"`
}

function sanitizeCell(v: any): string {
  let s = String(v ?? '')
  if (/^[=+\-@]/.test(s)) s = '\t' + s
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

function exportExcel(taskId: string) {
  const list = members.value[taskId] || []
  if (!list.length) return
  const html = `<html xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel">
    <head><meta charset="UTF-8"><style>td,th{border:1px solid #ccc;padding:6px}</style></head>
    <body><table>
      <tr><th>User ID</th><th>Username</th><th>First Name</th><th>Last Name</th><th>Last Seen</th><th>Has Photo</th><th>Is Bot</th></tr>
      ${list.map((m: any) => `<tr><td>${m.user_id}</td><td>${m.username || ''}</td><td>${m.first_name || ''}</td><td>${m.last_name || ''}</td><td>${m.last_seen || ''}</td><td>${m.has_photo ? 'Yes' : 'No'}</td><td>${m.is_bot ? 'Yes' : 'No'}</td></tr>`).join('')}
    </table></body></html>`
  const blob = new Blob([html], { type: 'application/vnd.ms-excel' })
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = `members_${taskId.slice(-6)}_${new Date().toISOString().slice(0,10)}.xls`
  link.click()
}

// 群组走缓存 store: 有缓存零请求, 只有 GroupSelector 里点"同步"才调接口
// groupStore 已在文件前部声明（groups computed 上方），此处复用
async function fetchGroups() {
  if (!scrapeAccId.value) return
  try {
    await groupStore.ensure(scrapeAccId.value)
  } catch {}
}

const pullRefreshing = ref(false)
async function onPullRefresh() {
  await fetchTasks()
  pullRefreshing.value = false
}

async function fetchTasks() {
  if (!scrapeAccId.value) return
  taskLoading.value = true
  try {
    const res = await taskApi.list({ type: 'scrape_members', account_id: scrapeAccId.value })
    tasks.value = res.list || []
  } catch {
    tasks.value = []
  } finally {
    taskLoading.value = false
  }
}

async function submit() {
  // R111: 协议协同死分支已删
  // R109: 走下拉所选账号(原绑全局切换器)
  if (!scrapeAccId.value || !selectedGroup.value) return
  submitting.value = true
  try {
    await taskApi.create({
      type: 'scrape_members',
      account_id: scrapeAccId.value,
      target_ids: [selectedGroup.value.group_id],
      config: {
        group_id: selectedGroup.value.group_id,
        group_title: selectedGroup.value.title,
      },
      payload: {
        mode: scrapeMode.value,
        ...(scrapeMode.value === 'history' ? { max_messages: maxMessages.value } : {}),
        ...(scrapeMode.value === 'members' ? {
          filter: {
            ...(filterRecent.value ? { recent_days: filterRecentDays.value } : {}),
            ...(filterPhoto.value ? { has_photo: true } : {}),
          },
        } : {}),
      },
    })
    showCreate.value = false
    selectedGroup.value = null
    await fetchTasks()
    toast({ type: 'success', title: '任务创建成功', message: '成员采集已开始，进度见任务列表' })
  } catch (e: any) {
    toast({ type: 'error', title: '创建失败', message: e?.message || '请稍后重试' })
  } finally {
    submitting.value = false
  }
}

watch(() => route.params.accountId, () => {
  fetchGroups()
  fetchTasks()
}, { immediate: true })

onMounted(() => {
  if (!authStore.tenant?.plan_features?.scrape_members) {
    toast({ type: 'error', title: '当前套餐不支持成员采集，请联系客服升级套餐' })
    router.push('/dashboard')
    return
  }
  fetchGroups()
  fetchTasks()
})
</script>
