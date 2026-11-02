<template>
  <div class="p-4 sm:p-6 max-w-3xl mx-auto">
    <div class="flex items-center justify-between gap-3 mb-4">
      <div class="min-w-0">
        <h1 class="text-title font-extrabold tracking-tight text-text-primary truncate">进群邀请</h1>
        <!-- R109: 移除"当前账号"标签(执行账号在创建弹窗里下拉选择) -->
        <p class="text-sm text-text-muted mt-1">执行账号在创建任务时选择</p>
      </div>
      <button @click="showCreate = true" class="px-4 py-2 rounded-lg text-sm font-medium glow-btn flex items-center gap-2 shrink-0"> 新建加群任务
      </button>
    </div>

    <!-- 实时进度面板: 创建后在此显示, 无需跳转 -->
    <LiveTaskFeed v-if="currentAccount" :account-id="currentAccount.account_id" task-type="join_groups" class="mb-4" />

    <!-- 本账号的加群任务列表 -->
    <div v-if="tasksLoading" class="glass rounded-[var(--t-r-md)] p-4 space-y-3 mb-4">
      <div v-for="i in 3" :key="i" class="space-y-2.5">
        <div class="flex items-center justify-between">
          <Skeleton height="1.25rem" width="40%" rounded="rounded-md" />
          <Skeleton height="0.875rem" width="25%" rounded="rounded-md" />
        </div>
        <Skeleton height="0.5rem" rounded="rounded-full" />
      </div>
    </div>
    <template v-else>
      <EmptyState v-if="tasksError" icon="Users" :title="tasksError" description="点击重试">
        <button @click="fetchTasks" class="mt-3 px-4 py-1.5 rounded-lg text-xs border border-border text-text-secondary hover:text-cyan hover:border-cyan/40 transition-colors">重新加载</button>
      </EmptyState>
      <EmptyState v-else-if="!tasks.length" icon="Users" title="暂无加群任务" description="点击右上角「新建加群任务」批量加入群组" />
      <div v-else class="space-y-3 mb-4">
        <pull-down-refresh v-if="tasks.length" v-model="pullRefreshing" @refresh="onPullRefresh">
        <div v-for="(t, ti) in tasks" :key="t.task_id" :class="['glass rounded-[var(--t-r-md)] p-4 card-lift card-enter hover:border-cyan/20 mb-3', t.status === 'running' ? 'card-running' : '']" :style="{ '--i': ti }">
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-2 min-w-0">
              <span class="px-2 py-0.5 rounded text-xs shrink-0 inline-flex items-center gap-1.5" :class="statusClass(t.status)">
                <span v-if="t.status === 'running'" class="w-1.5 h-1.5 rounded-full bg-current dot-pulse" />
                {{ statusLabel(t.status) }}
              </span>
              <span class="text-xs text-text-muted truncate">目标 {{ (t.target_ids || []).length || progOf(t).total || '-' }} 个群</span>
            </div>
            <span class="text-xs text-text-muted shrink-0">{{ formatTime(t.created_at) }}</span>
          </div>
          <div class="mt-3">
            <div class="flex items-center justify-between text-xs mb-1.5">
              <span class="text-text-secondary">{{ progOf(t).done }} / {{ progOf(t).total }}</span>
              <span class="text-cyan font-medium">{{ percentOf(t) }}%</span>
            </div>
            <div class="h-2 rounded-full bg-bg-hover overflow-hidden">
              <div class="h-full rounded-full transition-all duration-500 bg-cyan" :style="{ width: percentOf(t) + '%' }" />
            </div>
          </div>
          <div class="flex items-center justify-end gap-2 mt-3">
            <button v-if="['failed','cancelled','completed'].includes(t.status) && progOf(t).failed > 0" @click.stop="retryFailed(t.task_id)" class="px-2 py-1 rounded text-xs bg-amber/10 text-amber hover:bg-amber/20">重试失败 ({{ progOf(t).failed }})</button>
            <button v-if="['running','queued','pending'].includes(t.status)" @click.stop="stopTask(t.task_id)" class="px-2 py-1 rounded text-xs bg-rose/10 text-rose hover:bg-rose/20">停止</button>
            <button @click.stop="$router.push(`/workspace/${route.params.accountId}/tasks/${t.task_id}`)" class="px-2 py-1 rounded text-xs border border-border text-text-secondary hover:border-cyan/40 hover:text-cyan transition-colors">查看详情</button>
          </div>
        </div>
        </pull-down-refresh>
      </div>
    </template>

    <!-- 新建模态框 -->
    <Modal v-model="showCreate" title="新建加群任务">
    <div class="space-y-4">
      <!-- R108: 执行账号选择(普通号模式) —— 不再依赖顶栏切换器 -->
      <div>
        <label class="block text-sm font-medium mb-2">执行账号</label>
        <select v-model="phoneAccId" class="input-box w-full h-10 px-3 text-sm rounded-lg">
          <option v-for="a in onlineAccounts" :key="a.account_id" :value="a.account_id">{{ a.first_name || a.phone }}</option>
        </select>
      </div>
      <!-- R127 令牌化: seg 容器深浅主题自适应(原写死深底在浅色下违和) -->
      <div class="flex rounded-[var(--t-r-lg)] p-1 gap-1 mb-4" style="background: color-mix(in srgb, var(--t-text-1) 8%, transparent);">
        <button class="flex-1 py-2.5 rounded-[14px] text-sm font-bold transition-all active:scale-[0.98]"
          :style="joinMode === 'link'
            ? 'background: var(--t-surface-2); color: var(--t-text-1); box-shadow: 0 2px 8px rgba(0,0,0,0.2);'
            : 'color: var(--t-text-3);'"
          @click="joinMode = 'link'">链接加群</button>
        <button class="flex-1 py-2.5 rounded-[14px] text-sm font-bold transition-all active:scale-[0.98]"
          :style="joinMode === 'invite'
            ? 'background: var(--t-surface-2); color: var(--t-text-1); box-shadow: 0 2px 8px rgba(0,0,0,0.2);'
            : 'color: var(--t-text-3);'"
          @click="joinMode = 'invite'">邀请成员入群</button>
      </div>

      <!-- 邀请成员模式: 目标群(我的群) + 来源群(有采集成员的群) -->
      <div v-if="joinMode === 'invite'" class="space-y-3">
        <div>
          <label class="block text-xs font-semibold mb-1">我的群(邀请进这个群)</label>
          <select v-model="inviteTargetId" class="w-full px-3 py-2.5 rounded-lg glow-input text-sm bg-bg-card">
            <option value="">选择目标群</option>
            <option v-for="g in joinedGroups" :key="g.group_id" :value="g.group_id">{{ g.title || g.username || ('群' + g.tg_chat_id) }}</option>
          </select>
        </div>
        <div>
          <label class="block text-xs font-semibold mb-1">来源群(邀请这个群的成员)</label>
          <select v-model="inviteSourceId" class="w-full px-3 py-2.5 rounded-lg glow-input text-sm bg-bg-card">
            <option value="">选择来源群</option>
            <option v-for="g in inviteGroups" :key="g.group_id" :value="g.group_id">{{ g.title || g.username || ('群' + g.tg_chat_id) }} ({{ g.member_count || 0 }}人)</option>
          </select>
        </div>
        <p class="text-xs text-text-muted">需要先在「成员采集」里采集来源群成员；邀请间隔 1.5~3.5 秒, 被限流会自动暂停该账号。</p>
      </div>
      <!-- 协议号协同模式 -->
      <!-- R111: 协议协同死分支已删(view 恒 'phone')。群链接输入两模式本共用 -->
      <div>
        <label class="block text-sm font-medium mb-2">群链接 / 邀请链接</label>
        <textarea v-model="linksInput" rows="6" placeholder="每行一个链接，如:&#10;https://t.me/groupname&#10;https://t.me/+AbCdEfGhIjK"
                  class="w-full px-4 py-3 rounded-[var(--t-r-md)] glow-input resize-none font-mono text-sm" />
        <div class="flex items-center justify-between mt-2">
          <label class="px-3 py-1.5 rounded-lg border border-border text-xs cursor-pointer hover:border-cyan/40 transition-colors">
            导入文件<input type="file" accept=".txt,.csv" class="hidden" @change="onImportFile">
          </label>
          <span class="text-xs text-text-muted">已识别 {{ validLinks.length }} 个有效链接</span>
          <!-- R128 无效链接可展开查看: 原只报数量, 用户不知哪行错了 -->
          <button v-if="invalidLinks.length > 0" type="button"
                  class="text-xs text-rose underline decoration-dotted underline-offset-2"
                  @click="showInvalidLinks = !showInvalidLinks">{{ invalidLinks.length }} 个格式无效{{ showInvalidLinks ? '▲' : '▼' }}</button>
        </div>
        <div v-if="showInvalidLinks && invalidLinks.length" class="mt-2 rounded-lg p-2.5 space-y-1 max-h-28 overflow-y-auto"
             style="background: color-mix(in srgb, var(--t-err) 8%, transparent); border: 1px solid color-mix(in srgb, var(--t-err) 25%, transparent);">
          <div v-for="(bad, i) in invalidLinks" :key="i" class="text-[11px] font-mono truncate" style="color: var(--t-err);">{{ bad }}</div>
        </div>
        <!-- 加群间隔(租户自定义): 最小/最大分钟, 每次随机取区间内 -->
        <div class="grid grid-cols-2 gap-2">
          <label class="block text-xs text-text-secondary">最小间隔(分钟)
            <input v-model.number="joinIntervalMin" type="number" min="0" step="0.5"
              class="mt-0.5 w-full px-2.5 py-1.5 rounded-lg glow-input text-sm bg-bg-card" />
          </label>
          <label class="block text-xs text-text-secondary">最大间隔(分钟)
            <input v-model.number="joinIntervalMax" type="number" min="0" step="0.5"
              class="mt-0.5 w-full px-2.5 py-1.5 rounded-lg glow-input text-sm bg-bg-card" />
          </label>
        </div>
      </div>

      <!-- 操作按钮 -->
      <div class="flex items-center justify-end gap-3">
        <button @click="showCreate = false"
                class="px-4 py-2 rounded-lg text-sm text-text-secondary hover:bg-bg-card transition-colors">取消</button>
        <button @click="submit" :disabled="validLinks.length === 0 || submitting"
                class="px-6 py-2 rounded-lg text-sm font-medium glow-btn disabled:opacity-50 flex items-center gap-2">
          <Loader2 v-if="submitting" class="w-4 h-4 animate-spin" />
          {{ submitting ? '创建中...' : `创建加群任务 (${validLinks.length})` }}
        </button>
      </div>
    </div>
    </Modal>
  </div>

</template>

<script setup lang="ts">
import { parseTargets } from '@/utils/importTargets'
import { toast } from '@/utils/toast'
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAccountStore } from '@/stores/account'
import { useGroupStore } from '@/stores/group'
import type { Group } from '@/types'
// R111 已删 ProtocolPicker/campaignApi/CAMPAIGN_STATUS 导入(死代码)
import { useAuthStore } from '@/stores/auth'
import { taskApi } from '@/api/task'
import { Loader2, Plus, Users } from 'lucide-vue-next'
import LiveTaskFeed from '@/components/LiveTaskFeed.vue'
import Modal from '@/components/ui/Modal.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import { useTaskEvents } from '@/composables/useTaskEvents'

const route = useRoute()
const router = useRouter()
const accountStore = useAccountStore()
const groupStore = useGroupStore()
const authStore = useAuthStore()
const currentAccount = computed(() => accountStore.currentAccount)
const accName = (a: any) => a?.first_name || a?.remark || a?.phone || (a?.account_id || '').slice(0, 8)

// ===== 模态框化: 本页任务列表 + 新建弹窗 =====
const showCreate = ref(false)
// R108: 手机号模式的执行账号(默认当前账号, 可下拉改选, 不必切换顶栏)
const phoneAccId = ref('')
watch(currentAccount, (acc: any) => { if (acc?.account_id && !phoneAccId.value) phoneAccId.value = acc.account_id }, { immediate: true })
const onlineAccounts = computed(() => (accountStore.accounts || []).filter((a: any) => a.status === 'online'))
const tasks = ref<any[]>([])
const tasksLoading = ref(true)
const tasksError = ref('')

async function fetchTasks() {
  if (!currentAccount.value) { tasks.value = []; return }
  tasksLoading.value = true
  try {
    const res: any = await taskApi.list({ type: 'join_groups', account_id: currentAccount.value.account_id, page: 1, page_size: 20, hide_loops: 1 } as any)
    tasks.value = res.list || []
    tasksError.value = ''
  } catch (e: any) {
    tasksError.value = e?.message || '加载失败, 点击重试'
  } finally {
    tasksLoading.value = false
  }
}
useAutoRefresh(fetchTasks, 15000)

// SSE 实时: 进度事件即时更新对应卡片, 状态事件刷新列表——亚秒级, 不再等轮询
useTaskEvents((evt: any) => {
  if (evt.type === 'task_status') { if (!evt.task_type || evt.task_type === 'join_groups') fetchTasks(); return }
  if (evt.progress && evt.task_id) {
    const t = tasks.value.find((x: any) => x.task_id === evt.task_id)
    if (t) t.progress = evt.progress
  }
})
// 邀请成员入群模式
const joinMode = ref<'link' | 'invite'>('link')
const inviteTargetId = ref('')
const inviteSourceId = ref('')
// 加群间隔(分钟, 0=用系统默认)
const joinIntervalMin = ref(0)
const joinIntervalMax = ref(0)
const inviteGroups = ref<Group[]>([])

// P0-32 审计修复: 原过滤条件 g.is_joined 在 TelegramGroup 类型里根本不存在——
// 过滤恒 false, 邀请目标群下拉永远空白。数据源 groupStore.ensure() 拉的就是
// 该账号已加入的群(dialogs 语义), 过滤本就冗余, 直接透传。
const joinedGroups = computed(() => inviteGroups.value)

// R108: 群列表跟随"执行账号"下拉(原绑 currentAccount, 选号与选群错位必失败)
async function loadInviteGroups() {
  if (!phoneAccId.value) return
  inviteGroups.value = await groupStore.ensure(phoneAccId.value)
}
watch(phoneAccId, () => {
  inviteGroups.value = []
  inviteTargetId.value = ''
  inviteSourceId.value = ''
  loadInviteGroups()
})

watch(currentAccount, () => {
  fetchTasks()
  inviteTargetId.value = ''
  inviteSourceId.value = ''
  loadInviteGroups()
})

const statusLabelMap: Record<string, string> = {
  pending: '等待调度', queued: '排队中', running: '运行中',
  completed: '已完成', failed: '失败', cancelled: '已取消',
}
function statusLabel(s: string) { return statusLabelMap[s] || s }
function statusClass(s: string): string {
  return ({
    pending: 'bg-amber/10 text-amber', queued: 'bg-amber/10 text-amber',
    running: 'bg-cyan/10 text-cyan', completed: 'bg-emerald/10 text-emerald',
    failed: 'bg-rose/10 text-rose', cancelled: 'bg-text-muted/10 text-text-muted',
  } as any)[s] || 'bg-bg-hover text-text-muted'
}
function progOf(t: any) {
  let p = t.progress
  if (typeof p === 'string') { try { p = JSON.parse(p) } catch { p = {} } }
  return { total: p?.total || 0, done: p?.done || 0, failed: p?.failed || 0 }
}
function percentOf(t: any) {
  const pr = progOf(t)
  return pr.total ? Math.min(100, Math.round((pr.done / pr.total) * 100)) : 0
}
function formatTime(iso: string) { return iso ? new Date(iso).toLocaleString('zh-CN') : '' }
const pullRefreshing = ref(false)
async function onPullRefresh() {
  await fetchTasks()
  pullRefreshing.value = false
}

async function retryFailed(id: string) {
  try { await taskApi.retry(id, 'failed'); fetchTasks() }
  catch (e: any) { toast({ type: 'error', title: '重试失败', message: e?.message }) }
}
async function stopTask(id: string) {
  try { await taskApi.cancel(id); fetchTasks() }
  catch (e: any) { toast({ type: 'error', title: '停止失败', message: e?.message }) }
}

const linksInput = ref('')
// ===== 协议号协同模式 =====
// R111 已删 isProtocol/protocolIds(协议协同死代码, view 恒 'phone')
// R111 已删 protoGroups
// 当前账号的已同步TG群列表(群勾选数据源; Group类型: title/username/member_count/tg_chat_id)
// R111 已删 protoGroupList/toggleProtoGroup(协议协同死代码)
// R111 已删 campaigns/loadCampaigns(协议协同死代码)
const submitting = ref(false)

function onImportFile(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (!f) return
  const r = new FileReader()
  r.onload = () => {
    const items = parseTargets(String(r.result || ''), { keepTmeLinks: true })
    linksInput.value = [...new Set([...linksInput.value.split('\n').filter(Boolean), ...items])].join('\n')
  }
  r.readAsText(f)
  ;(e.target as HTMLInputElement).value = ''
}

// 2026-09-22 宽松识别+归一化：缺协议头的 t.me/telegram.me、@开头 都接受，
// 统一归一为 https://t.me/... 再提交。此前严格 startsWith 会把 "t.me/xxx" 等常见写法
// 静默滤掉——用户明明填了链接，validLinks 却是 0，建出零目标任务（服务端日志实证）。
const validLinks = computed(() => {
  return linksInput.value.split('\n').map(l => l.trim()).filter(Boolean).map(l => {
    if (l.startsWith('@')) return 'https://t.me/' + l.slice(1)
    // 2026-09-22 补: 手机端从 TG 应用内分享复制的邀请链接是 tg://join?invite= 格式,
    // 此前会被归一化判空, 用户明明天天填了链接却建出零目标任务(实证: https://t.me/+llE9x5nvZpQ0Nzk9 案例)
    const tg = l.match(/^tg:\/\/join\?invite=([A-Za-z0-9_-]+)$/i)
    if (tg) return 'https://t.me/+' + tg[1]
    const m = l.match(/^(?:https?:\/\/)?(?:www\.)?(?:t\.me|telegram\.me)\/(.+)$/i)
    if (m) return 'https://t.me/' + m[1]
    return ''
  }).filter(Boolean)
})

const invalidLinks = computed(() => {
  const all = linksInput.value.split('\n').map(l => l.trim()).filter(Boolean)
  return all.filter(l => {
    if (l.startsWith('@')) return false
    if (/^tg:\/\/join\?invite=[A-Za-z0-9_-]+$/i.test(l)) return false
    return !/^(?:https?:\/\/)?(?:www\.)?(?:t\.me|telegram\.me)\//i.test(l)
  })
})
// R128 无效链接展开查看(模板引用, 编译必需)
const showInvalidLinks = ref(false)

async function submit() {
  // R111: 协议协同死分支已删
  // ===== 邀请成员入群模式 =====
  if (joinMode.value === 'invite') {
    if (!phoneAccId.value || !inviteTargetId.value || !inviteSourceId.value) {
      toast({ type: 'warning', title: '请选择执行账号、目标群和来源群' })
      return
    }
    submitting.value = true
    try {
      await taskApi.create({
        type: 'invite_members',
        account_id: phoneAccId.value,
        payload: {
          target_group_id: inviteTargetId.value,
          source_group_id: inviteSourceId.value,
        },
      })
      toast({ type: 'success', title: '邀请入群任务已创建', message: '成员邀请已排队执行' })
      inviteTargetId.value = ''
      inviteSourceId.value = ''
      joinMode.value = 'link'
      showCreate.value = false
      fetchTasks()
    } catch (e: any) {
      toast({ type: 'error', title: '创建失败', message: e?.message || '请稍后重试' })
    } finally {
      submitting.value = false
    }
    return
  }
  if (!phoneAccId.value) {
    toast({ type: 'warning', title: '请选择执行账号' })
    return
  }
  if (validLinks.value.length === 0) {
    toast({ type: 'warning', title: '请先添加有效的群链接' })
    return
  }
  submitting.value = true
  try {
    await taskApi.create({
      type: 'join_groups',
      account_id: phoneAccId.value,
      links: validLinks.value,
      interval_min_minutes: joinIntervalMin.value || undefined,
      interval_max_minutes: joinIntervalMax.value || undefined,
    })
    // 创建后留在本页: 关闭模态框, 列表与实时面板立即出现新任务
    linksInput.value = ''
    showCreate.value = false
    toast({ type: 'success', title: '加群任务已创建', message: validLinks.value.length + ' 个目标已排队' })
    fetchTasks()
  } catch (e: any) {
    toast({ type: 'error', title: '创建失败', message: e?.message || '请稍后重试' })
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  fetchTasks()
  loadInviteGroups()
  // 入口分流(2026-10-20): 侧边栏"邀请入群"项带 ?mode=invite 进入自动切模式
  const q = new URLSearchParams(window.location.search)
  if (q.get('mode') === 'invite' && authStore.tenant?.plan_features?.invite) {
    joinMode.value = 'invite'
  }
  // R118: 守卫改并集 —— 加群/邀请合并入口后, 任一权限可用即可留在页内
  // (joinMode 默认 'link'; 无加群权限但有邀请权限时自动落到 invite Tab)
  const pf = authStore.tenant?.plan_features
  if (!pf?.join_groups && !pf?.invite) {
    toast({ type: 'error', title: '当前套餐不支持进群邀请，请联系客服升级套餐' })
    router.push('/dashboard')
    return
  }
  if (!pf?.join_groups && pf?.invite && !q.get('mode')) {
    joinMode.value = 'invite'
  }
})
</script>
