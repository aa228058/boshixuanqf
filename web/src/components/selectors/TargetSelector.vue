<template>
  <Modal v-model="visible" title="选择发送目标">
    <div class="space-y-4">
      <!-- R151: 同步状态可见: 群数量 + 上次同步时间 -->
      <div v-if="activeTab === 'group'" class="flex items-center gap-2 text-caption px-1" style="color: var(--t-text-3);">
        <span>{{ props.groups.length }} 个群</span>
        <span>· 上次同步 {{ lastSyncText }}</span>
        <span class="ml-auto">每 30 分钟自动同步</span>
      </div>
      <div class="flex items-center justify-between gap-2">
        <div class="flex items-center gap-2 overflow-x-auto pb-0.5">
          <button
            v-for="tab in tabs"
            :key="tab.value"
            @click="switchTab(tab.value)"
            class="px-3 py-1.5 rounded-lg text-xs font-medium transition-colors"
            :class="(activeTab === tab.value ? 'bg-cyan-glow/50 text-cyan border border-cyan/30' : 'text-text-secondary hover:text-text-primary') + ' shrink-0 whitespace-nowrap'"
          >
            {{ tab.label }}
          </button>
        </div>
        <button
          v-if="activeTab !== 'manual'"
          @click="onRefresh"
          :disabled="loading"
          class="px-2.5 py-1.5 rounded-lg text-xs border border-border hover:border-cyan/30 hover:text-cyan transition-colors flex items-center gap-1 shrink-0 disabled:opacity-50"
          title="点击刷新：从 Telegram 同步群组/私聊并更新缓存"
         aria-label="刷新">
          <RefreshCw :class="['w-3.5 h-3.5', loading && 'animate-spin']" />
          {{ loading ? '刷新中...' : '刷新' }}
        </button>
      </div>

      <!-- 私聊 -->
      <div v-if="activeTab === 'user'" class="flex flex-col max-h-[min(400px,calc(85vh-180px))]">
        <div class="flex items-center gap-2 shrink-0 pb-2">
          <input
            v-model="search"
            placeholder="搜索私聊联系人..."
            class="flex-1 px-3 py-2 rounded-lg glow-input text-sm"
          />
          <button
            v-if="filteredDialogs.length > 0"
            @click="selectAllDialogs"
            class="px-2.5 py-2 rounded-lg text-xs border border-border hover:border-cyan/30 hover:text-cyan transition-colors shrink-0"
          >
            {{ allDialogsSelected ? '取消全选' : '全选' }}
          </button>
        </div>
        <div class="overflow-y-auto space-y-2">
        <div v-if="loading" class="py-8 text-center text-sm text-text-muted">
          <div class="animate-spin w-5 h-5 border-2 border-cyan border-t-transparent rounded-full mx-auto mb-2" />
          <p>正在加载...</p>
          <p class="text-xs mt-1">首次同步约需 15-30 秒, 请稍候</p>
        </div>
        <div v-else-if="!filteredDialogs.length" class="py-8 text-center text-sm text-text-muted">
          暂无私聊联系人
        </div>
        <div
          v-for="d in displayDialogs"
          :key="d.id"
          @click="toggleTarget(d.id)"
          class="flex items-center gap-3 p-3 rounded-lg border border-border cursor-pointer transition-[transform,opacity,border-color,background-color]"
          :class="isTargetSelected(d.id) ? 'border-cyan/50 bg-cyan/5' : 'hover:bg-bg-hover'"
         tabindex="0" role="button">
          <t-checkbox
            :model-value="isTargetSelected(d.id)"
            @click.stop
            @update:model-value="toggleTarget(d.id)"
          />
          <div class="w-8 h-8 rounded-full bg-bg-card flex items-center justify-center shrink-0">
            <Users class="w-4 h-4 text-cyan" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="text-sm font-medium truncate max-w-[65ch]">{{ d.title }}</div>
            <div class="text-xs text-text-muted">{{ d.typeLabel }}</div>
          </div>
        </div>
        </div>
      </div>

      <!-- 超级群组 / 群组 -->
      <div v-else-if="['supergroup','group'].includes(activeTab)" class="flex flex-col max-h-[min(400px,calc(85vh-180px))]">
        <div class="flex items-center gap-2 shrink-0 pb-2">
          <t-input
            v-model="groupSearch"
            :placeholder="'搜索' + tabLabelMap[activeTab] + '...'"
            clearable
            aria-label="搜索"
            class="flex-1"
          />
          <button
            v-if="filteredGroups.length > 0"
            @click="selectAllGroups"
            class="px-2.5 py-2 rounded-lg text-xs border border-border hover:border-cyan/30 hover:text-cyan transition-colors shrink-0"
          >
            {{ allGroupsSelected ? '取消全选' : '全选' }}
          </button>
        </div>
        <div class="overflow-y-auto space-y-2">
        <div v-if="loading" class="py-8 text-center text-sm text-text-muted">
          <div class="animate-spin w-5 h-5 border-2 border-cyan border-t-transparent rounded-full mx-auto mb-2" />
          <p>正在加载...</p>
          <p class="text-xs mt-1">首次同步约需 15-30 秒, 请稍候</p>
          <p v-if="syncStatus === 'syncing'" class="text-xs text-text-secondary mt-1">首次同步约需 15-30 秒，请稍候</p>

          <p class="text-[10px] text-text-muted mt-0.5">{{ syncHint }}</p>
        </div>
        <div v-else-if="!filteredGroups.length" class="py-8 text-center text-sm text-text-muted">
          <p v-if="syncStatus === 'timeout'">同步超时，请重试</p>
          <p v-else>暂无{{ tabLabelMap[activeTab] }}</p>
        </div>
        <div
          v-for="g in filteredGroups"
          :key="g.group_id"
          @click="toggleTarget(String(g.tg_chat_id))"
          class="flex items-center gap-3 p-3 rounded-lg border border-border cursor-pointer transition-[transform,opacity,border-color,background-color]"
          :class="isTargetSelected(String(g.tg_chat_id)) ? 'border-cyan/50 bg-cyan/5' : 'hover:bg-bg-hover'"
         tabindex="0" role="button">
          <input
            type="checkbox"
            :checked="isTargetSelected(String(g.tg_chat_id))"
            class="rounded border-border bg-bg-card text-cyan shrink-0"
            @click.stop
            @change="toggleTarget(String(g.tg_chat_id))" aria-label="选择" />
          <div class="w-8 h-8 rounded-lg bg-bg-card flex items-center justify-center shrink-0">
            <UsersRound v-if="g.type === 'group'" class="w-4 h-4 text-cyan" />
            <Users v-else-if="g.type === 'supergroup'" class="w-4 h-4 text-amber" />
            <MessageCircle v-else class="w-4 h-4 text-text-muted" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="text-sm font-medium truncate max-w-[65ch]">{{ g.title }}</div>
            <!-- R108: 并集模式下标注已加入的账号数(数据来自 Broadcast 的 _joinedAccounts) -->
            <div v-if="g._joinedAccounts?.length" class="text-[10px] text-cyan shrink-0">{{ g._joinedAccounts.length }} 个已选号已加入</div>
            <div class="flex items-center gap-2 text-xs">
              <span class="text-text-muted">{{ g.member_count }} 成员</span>
              <span class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full bg-emerald/10 text-emerald">
                <span class="w-1.5 h-1.5 rounded-full bg-emerald" />已加入
              </span>
            </div>
          </div>
        </div>
        </div>
      </div>

      <!-- 手动输入 -->
      <div v-else-if="activeTab === 'manual'" class="space-y-3">
        <t-textarea
          v-model="manualInput"
          label="手动输入目标"
          placeholder="每行输入一个目标&#10;支持格式：&#10;@username&#10;-1001234567890&#10;https://t.me/groupname"
          :rows="6"
        />
        <div class="flex justify-between text-xs">
          <span class="text-text-muted">已识别 {{ manualTargets.length }} 个目标</span>
          <span v-if="manualInvalid.length" class="text-rose">{{ manualInvalid.length }} 个格式无效</span>
        </div>
        <div class="flex items-center gap-2">
          <label class="px-3 py-1.5 rounded-lg border border-border text-xs cursor-pointer hover:border-cyan/40 transition-colors">
            导入文件<input type="file" accept=".txt,.csv" class="hidden" @change="onManualImportFile">
          </label>
          <span class="text-xs text-text-muted">支持 .txt / .csv, 与采集导出格式互通</span>
        </div>
      </div>

      <!-- 已选目标 -->
      <div class="border-t border-border pt-3">
        <div class="text-xs text-text-secondary mb-2">已选目标 ({{ allTargets.length }})</div>
        <div class="flex flex-wrap gap-2">
          <span
            v-for="t in allTargets"
            :key="t"
            class="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs bg-cyan-glow/50 text-cyan border border-cyan/20 max-w-[200px]"
            :title="t"
          >
            <span class="truncate">{{ getTargetTitle(t) }}</span>
            <button @click="removeTarget(t)" class="hover:text-rose" aria-label="删除">
              <X class="w-3 h-3" />
            </button>
          </span>
        </div>
      </div>
    </div>
    <template #footer>
      <div class="flex gap-3">
        <!-- R109 修复: 原样式在深色下被弹窗底色衬成白块(无背景色+继承样式), 显式令牌化 -->
        <button @click="visible = false"
                class="px-4 py-2 rounded-lg text-sm bg-bg-hover text-text-secondary hover:text-text-primary transition-colors">
          取消
        </button>
        <t-button theme="primary" @click="confirm" >确认 ({{ allTargets.length }})</t-button>
      </div>
    </template>
  </Modal>
</template>

<script setup lang="ts">
import { parseTargets } from '@/utils/importTargets'

import { ref, computed, watch } from 'vue'
import { X, RefreshCw, Users, MessageCircle, UsersRound } from 'lucide-vue-next'
import Modal from '@/components/ui/Modal.vue'
// R115: accountApi 死导入已删(正文零使用)
import { groupApi } from '@/api/group'
import { contactApi } from '@/api/contact'
import { syncLock } from '@/utils/syncLock'
import { toast } from '@/utils/toast'
import { useGroupStore } from '@/stores/group'

const groupStore = useGroupStore()
import type { Group } from '@/types'

interface DialogItem {
  id: string
  title: string
  type: string
  typeLabel: string
}

// P0-13: 原 props 里的 loading 与下方 const loading = ref(false) 同名——script setup
// 中顶层变量遮蔽 props, 父组件传入的加载态被静默忽略; 且全项目调用方无一传 :loading,
// 该 props 是纯死代码。删除 props 声明, 内部 ref 独占 loading 名。
const props = withDefaults(defineProps<{
  modelValue: boolean
  groups: Group[]
  accountId?: string
  /** 群组同步状态: 'idle' | 'syncing' | 'timeout' */
  syncStatus?: string
}>(), {
  syncStatus: 'idle',
})

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'select', v: string[]): void
  (e: 'refresh'): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: v => emit('update:modelValue', v)
})

const tabs = [
  { label: '私聊', value: 'user' },
  { label: '群组', value: 'group' },
  { label: '手动输入', value: 'manual' },
]

const tabLabelMap: Record<string, string> = {
  user: '私聊联系人',
  group: '群组'}

const activeTab = ref('group')
const selectedTargets = ref<Set<string>>(new Set())
const manualInput = ref('')

// 手动输入 tab: 文件导入(与采集导出格式互通)
function onManualImportFile(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (!f) return
  const r = new FileReader()
  r.onload = () => {
    manualInput.value = [...new Set([...manualInput.value.split('\n').filter(Boolean), ...parseTargets(String(r.result || ''))])].join('\n')
  }
  r.readAsText(f)
  ;(e.target as HTMLInputElement).value = ''
}
const dialogs = ref<DialogItem[]>([])
const loading = ref(false)
const syncAllLoading = ref(false)

// 批量同步: 后台逐个同步全部在线账号的群组(后端串行3秒间隔), 30秒防重复点击
async function syncAllAccounts() {
  if (syncAllLoading.value) return
  syncAllLoading.value = true
  try {
    await groupApi.syncAll()
    toast({ type: 'success', title: '批量同步已启动', message: '后台逐个同步在线账号, 约3秒/账号; 同步完成后再打开选择器即可看到最新群组' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '批量同步启动失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  } finally {
    setTimeout(() => { syncAllLoading.value = false }, 30000)
  }
}
const syncStatus = ref<'idle' | 'syncing' | 'done' | 'timeout' | 'error'>('idle')
const fromCache = ref(false)
const syncHint = ref('')

// 长列表性能: 最多渲染 200 条, 超出用搜索过滤(搜索框已是主要查找手段)
const displayDialogs = computed(() => filteredDialogs.value.slice(0, 200))
const search = ref('')
const groupSearch = ref('')

const filteredDialogs = computed(() => {
  const list = dialogs.value.filter(d => d.type === 'user')
  if (!search.value) return list
  const q = search.value.toLowerCase()
  return list.filter(d => d.title.toLowerCase().includes(q))
})

const filteredGroups = computed(() => {
  if (activeTab.value === 'user') return []
  let list = props.groups.filter(g => activeTab.value === 'group' ? (g.type === 'group' || g.type === 'supergroup') : g.type === activeTab.value)
  if (groupSearch.value) {
    const q = groupSearch.value.toLowerCase()
    list = list.filter(g => g.title.toLowerCase().includes(q))
  }
  return list
})

const allDialogsSelected = computed(() => {
  const ids = filteredDialogs.value.map(d => d.id)
  return ids.length > 0 && ids.every(id => selectedTargets.value.has(id))
})

const allGroupsSelected = computed(() => {
  // 统一存数字 chat_id(配合库里的 AccessHash 直发); username 解析失败率极高(FloodWait/改名/未加入)
  const ids = filteredGroups.value.map(g => String(g.tg_chat_id))
  return ids.length > 0 && ids.every(id => selectedTargets.value.has(id))
})

const manualTargets = computed(() => {
  const lines = manualInput.value.split(/\n/).map(l => l.trim()).filter(Boolean)
  const valid: string[] = []
  for (const line of lines) {
    // Strict validation: @username (5-32 chars), -100ID (pure digits, >=10), t.me/name (5-32 chars)
    if (
      /^@[a-zA-Z0-9_]{5,32}$/.test(line) ||
      /^-100\d{10}$/.test(line) ||
      /^https?:\/\/t\.me\/[a-zA-Z0-9_]{5,32}$/.test(line)
    ) {
      valid.push(line)
    }
  }
  return valid
})

const manualInvalid = computed(() => {
  const lines = manualInput.value.split(/\n/).map(l => l.trim()).filter(Boolean)
  return lines.filter(l => {
    return !(
      /^@[a-zA-Z0-9_]{5,32}$/.test(l) ||
      /^-100\d{10}$/.test(l) ||
      /^https?:\/\/t\.me\/[a-zA-Z0-9_]{5,32}$/.test(l)
    )
  })
})

const allTargets = computed(() => Array.from(selectedTargets.value))

function getTargetTitle(id: string): string {
  const g = props.groups.find(g => String(g.tg_chat_id) === id || g.username === id.replace(/^@/, ''))
  if (g) return g.title
  const d = dialogs.value.find(d => d.id === id)
  if (d) return d.title
  return id
}

function isTargetSelected(t: string) {
  return selectedTargets.value.has(t)
}

function toggleTarget(t: string) {
  if (selectedTargets.value.has(t)) selectedTargets.value.delete(t)
  else selectedTargets.value.add(t)
}

function selectAllDialogs() {
  const ids = filteredDialogs.value.map(d => d.id)
  if (allDialogsSelected.value) {
    ids.forEach(id => selectedTargets.value.delete(id))
  } else {
    ids.forEach(id => selectedTargets.value.add(id))
  }
}

function selectAllGroups() {
  // 统一存 TG chat ID(数字字符串): 用户名解析 FloodWait 高发且易失效,
  // chat_id 是同步时落库的永久 ID, 配合库中 AccessHash 可直发零解析
  const ids = filteredGroups.value.map(g => String(g.tg_chat_id))
  if (allGroupsSelected.value) {
    ids.forEach(id => selectedTargets.value.delete(id))
  } else {
    ids.forEach(id => selectedTargets.value.add(id))
  }
}

function removeTarget(t: string) {
  selectedTargets.value.delete(t)
}

async function fetchDialogs() {
  if (!props.accountId) return
  loading.value = true
  try {
    // 读 contacts 表缓存(同步群组时写入), 秒开; 不再实时拉 TG(慢且每次重复)
    const res: any = await contactApi.list(props.accountId)
    const data = res.data || res || []
    dialogs.value = data.map((d: any) => ({
      id: String(d.tg_user_id),
      title: [d.first_name, d.last_name].filter(Boolean).join(' ') || d.username || String(d.tg_user_id),
      type: 'user',
      typeLabel: '私聊',
    })).filter((d: DialogItem) => d.id && d.id !== 'undefined' && d.id !== 'null')
  } catch {
    dialogs.value = []
  } finally {
    loading.value = false
  }
}

async function switchTab(tab: string) {
  activeTab.value = tab
  // 不自动同步: 打开时的 openSelector 已按"整库无缓存才同步一次"统一决策,
  // 私聊无缓存时展示空态提示, 由用户点"同步"按钮获取(遵守: 不点同步不调接口)
}

async function onRefresh() {
  if (loading.value) return

  if (activeTab.value === 'user') {
    await fetchDialogs()
    return
  }

  // 手动"同步刷新"按钮: 永远全量同步(带转圈动画), 只有打开选择器时才是缓存优先
  try {
    const cached: any = await groupApi.list(props.accountId)
    const cachedList = cached.list || cached.data || (Array.isArray(cached) ? cached : [])
    if (false && cachedList.length > 0) {
      await groupStore.refresh(props.accountId!); syncStatus.value = 'done'
      fromCache.value = true
      emit('refresh')
      // 后台静默同步, 完成后失效缓存并通知父组件刷新——不用手动重开
      groupApi.sync(props.accountId || '')
        .then(async () => {
          groupStore.invalidate(props.accountId || '')
          try {
            const fresh: any = await groupApi.list(props.accountId)
            const freshList = fresh.list || fresh.data || (Array.isArray(fresh) ? fresh : [])
            if (freshList.length) emit('refresh')
          } catch { /* 下次打开再拉 */ }
        })
        .catch(() => {})
      return
    }
  } catch (e) {
    console.error('[TargetSelector] cache-first list:', e)
  }

  // 全局同步锁：防止跨组件重复同步同一账号
  if (props.accountId && !syncLock.acquire(props.accountId)) {
    loading.value = true
    syncStatus.value = 'syncing'
    await waitForGlobalSync()
    // 同步完成后刷新列表
    try {
      const raw = await groupApi.list(props.accountId)
      const list = raw.list || []
      if (list.length > 0) {
        emit('refresh')
      }
    } catch (e) {
      console.error('[TargetSelector] list after global sync:', e)
    }
    loading.value = false
    return
  }

  if (!props.accountId) {
    syncLock.release()
    emit('refresh')
    return
  }

  loading.value = true
  syncStatus.value = 'syncing'
  try {
    await groupApi.sync(props.accountId || '')
    // 轮询等待同步结果
    let delay = 1000
    const start = Date.now()
    // R99: 同步是后台长任务(后端最长10分钟), 30s 太短必超时 —— 放宽到 120s
    while (Date.now() - start < 120000) {
      // 弹窗关闭则停止轮询，避免资源浪费
      if (!visible.value) {
        break
      }
      try {
        const raw = await groupApi.list(props.accountId)
        const list = raw.list || []
        if (list.length > 0) {
          await groupStore.refresh(props.accountId!); syncStatus.value = 'done'
          emit('refresh')
          break
        }
      } catch (e) {
        console.error('[TargetSelector] poll error:', e)
      }
      await new Promise(r => setTimeout(r, delay))
      delay = Math.min(delay * 2, 8000)
    }
    if (syncStatus.value !== 'done' && visible.value) {
      syncStatus.value = 'timeout'
      // R99: 超时不等于失败(后台仍在跑), 提示租户稍后重开或等自动出现
      toast({ type: 'info', message: '同步仍在后台进行，完成后群组会自动出现，也可稍后重开选择器' })
    }
  } catch (e: unknown) {
    console.error('[TargetSelector] sync failed:', e)
    syncStatus.value = 'error'
  } finally {
    loading.value = false
    syncLock.release()
  }
}

// Wait for another component to finish syncing, then return
async function waitForGlobalSync(): Promise<void> {
  const checkInterval = 500
  const maxWait = 30000
  const start = Date.now()
  while (Date.now() - start < maxWait) {
    if (!syncLock.isSyncing()) {
      return
    }
    await new Promise(r => setTimeout(r, checkInterval))
  }
}

function confirm() {
  manualTargets.value.forEach(t => selectedTargets.value.add(t))
  emit('select', allTargets.value)
  visible.value = false
}

// R151: 状态栏 —— 上次同步时间文案
const lastSyncText = computed(() => {
  const ts = props.accountId ? groupStore.lastSyncAt(props.accountId) : 0
  if (!ts) return '未同步过'
  const m = Math.floor((Date.now() - ts) / 60000)
  return m < 1 ? '刚刚' : m < 60 ? `${m} 分钟前` : `${Math.floor(m / 60)} 小时前`
})

// 打开时缓存优先: 库里有数据先展示(秒开), 后台静默同步; 无缓存才走全量同步
async function openSelector() {
  if (!props.accountId) { syncHint.value = '错误: 缺少 accountId'; return }
  try {
    const cached: any = await groupApi.list(props.accountId)
    const cl = cached.list || cached.data || (Array.isArray(cached) ? cached : [])
    // P1-10: 调试文案仅开发环境可见——生产用户看到"缓存检查: 库里 N 个群"
    // 这类内部话术会困惑
    if (import.meta.env.DEV) syncHint.value = `缓存检查: 库里 ${cl.length} 个群`
    if (cl.length) {
      fromCache.value = true
      await groupStore.refresh(props.accountId!); syncStatus.value = 'done'
      emit('refresh')
      // 私聊联系人从数据库缓存填充(同步时才入库), 不点"同步"不调接口
      try {
        const cr: any = await contactApi.list(props.accountId)
        // P0-21: parseResponse 归一后列表接口统一 {list,total}, 但纯数组直返时
        // cr.list 是 undefined——私聊列表曾因此恒空。数组兜底。
        const rows = Array.isArray(cr) ? cr : (cr.list || cr.data || [])
        dialogs.value = rows.map((r: any) => ({
          id: String(r.tg_user_id),
          title: r.username ? '@' + r.username : (r.first_name || String(r.tg_user_id)),
          type: 'user',
        }))
        syncHint.value += `, 私聊缓存 ${rows.length} 人`
      } catch { /* 私聊无缓存保持空, 切 tab 时才同步 */ }
      return
    }
    syncHint.value += ' → 无缓存, 全量同步'
  } catch (e: any) {
    syncHint.value = '缓存查询失败: ' + (e?.message || e)
  }
  fetchDialogs()
}

watch(() => props.modelValue, v => {
  if (v) {
    selectedTargets.value.clear()
    manualInput.value = ''
    activeTab.value = 'user'
    openSelector()
  }
})
</script>
