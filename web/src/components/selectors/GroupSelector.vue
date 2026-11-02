<template>
  <Modal v-model="visible" title="选择群组">
    <div class="flex flex-col max-h-[min(520px,calc(85vh-180px))]">
      <!-- 固定头部：标签 + 操作 + 搜索 -->
      <div class="shrink-0 space-y-3 pb-3">
        <div class="flex items-center justify-between gap-2">
          <div class="flex gap-2">
            <button
              v-for="tab in tabs"
              :key="tab.value"
              @click="activeTab = tab.value"
              class="px-3 py-1.5 rounded-lg text-xs font-medium transition-colors shrink-0 whitespace-nowrap"
              :class="activeTab === tab.value ? 'bg-cyan-glow/50 text-cyan border border-cyan/30' : 'text-text-secondary hover:text-text-primary'"
            >
              {{ tab.label }}
            </button>
          </div>
          <div class="flex items-center gap-2">
            <button
              v-if="props.mode === 'multiple' && filtered.length > 0"
              @click="selectAll"
              class="px-2.5 py-1.5 rounded-lg text-xs border border-border hover:border-cyan/30 hover:text-cyan transition-colors"
            >
              {{ allSelected ? '取消全选' : '全选' }}
            </button>
            <button
              @click="onRefresh"
              :disabled="loading"
              class="px-2.5 py-1.5 rounded-lg text-xs border border-border hover:border-cyan/30 hover:text-cyan transition-colors flex items-center gap-1 disabled:opacity-50"
              title="从Telegram同步并刷新"
             aria-label="刷新">
              <RefreshCw :class="['w-3.5 h-3.5', loading && 'animate-spin']" />
              {{ loading ? '同步中...' : '同步刷新' }}
            </button>
          </div>
        </div>
        <t-input v-model="search" placeholder="搜索群组名称或用户名..." clearable class="w-full">
          <template #prefix-icon>
            <Search class="w-4 h-4" />
          </template>
        </t-input>
      </div>

      <!-- 滚动列表 -->
      <div class="flex-1 overflow-y-auto space-y-2 min-h-0">
        <!-- 加载中 -->
        <div v-if="loading" class="py-12 text-center">
          <div class="animate-spin w-6 h-6 border-2 border-cyan border-t-transparent rounded-full mx-auto mb-3" />
          <p class="text-sm text-text-muted max-w-[65ch]">
            {{ syncStatus === 'syncing' ? '正在同步 Telegram 群组...' : '加载中...' }}
          </p>
          <p v-if="syncStatus === 'syncing'" class="text-xs text-text-secondary mt-1">
            首次同步约需 15-30 秒，请稍候
          </p>
        </div>
        <!-- 群组 tab: 普通群 + 超级群(频道已排除) -->
        <div
          v-else-if="activeTab !== 'dm'"
          v-for="g in filtered"
          :key="g.group_id"
          @click="toggle(g)"
          class="flex items-center gap-3 p-3 rounded-lg border border-border cursor-pointer transition-[transform,opacity,border-color,background-color]"
          :class="isSelected(g) ? 'border-cyan/50 bg-cyan/5' : 'hover:bg-bg-hover'"
         tabindex="0" role="button">
          <t-checkbox
            :model-value="isSelected(g)"
            @click.stop
            @update:model-value="toggle(g)"
          />
          <div class="w-10 h-10 rounded-lg bg-bg-card flex items-center justify-center shrink-0">
            <Users v-if="g.type === 'group'" class="w-5 h-5 text-cyan" />
            <Megaphone v-else-if="g.type === 'channel'" class="w-5 h-5 text-cyan" />
            <UsersRound v-else class="w-5 h-5 text-amber" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="font-medium text-sm truncate max-w-[65ch]">{{ g.title }}</div>
            <div class="flex items-center gap-2 mt-0.5">
              <span class="text-xs text-text-muted">{{ g.member_count }} 成员</span>
              <t-tag v-if="g.is_joined" theme="success" variant="light">已加入</t-tag>
            </div>
          </div>
        </div>
        <!-- 私聊 tab: 采集过的成员 -->
        <div
          v-else
          v-for="u in dmFiltered"
          :key="u.group_id"
          @click="toggle(u)"
          class="flex items-center gap-3 p-3 rounded-lg border border-border cursor-pointer transition-[transform,opacity,border-color,background-color]"
          :class="isSelected(u) ? 'border-cyan/50 bg-cyan/5' : 'hover:bg-bg-hover'"
         tabindex="0" role="button">
          <t-checkbox
            :model-value="isSelected(u)"
            @click.stop
            @update:model-value="toggle(u)"
          />
          <div class="w-10 h-10 rounded-lg bg-bg-card flex items-center justify-center shrink-0">
            <UsersRound class="w-5 h-5 text-amber" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="font-medium text-sm truncate max-w-[65ch]">{{ u.title }}</div>
            <div class="flex items-center gap-2 mt-0.5">
              <span class="text-xs text-text-muted">{{ u.username ? '@' + u.username : 'TG 用户 ' + u.tg_chat_id }}</span>
              <t-tag theme="warning" variant="light">私聊</t-tag>
            </div>
          </div>
        </div>
        <div v-if="!loading && (activeTab === 'dm' ? !dmFiltered.length : !filtered.length)" class="py-8 text-center">
          <div class="w-16 h-16 rounded-[var(--t-r-lg)] bg-bg-card flex items-center justify-center mx-auto mb-3">
            <Search class="w-8 h-8 text-text-muted" />
          </div>
          <template v-if="activeTab === 'dm'">
            <p class="text-sm text-text-secondary mb-1 max-w-[65ch]">暂无私聊对象</p>
            <p class="text-xs text-text-secondary">先在「成员采集」里采集任意群的成员，成员会自动成为私聊目标</p>
          </template>
          <template v-else>
            <p class="text-sm text-text-secondary mb-1 max-w-[65ch]">
              {{ syncStatus === 'timeout' ? '同步超时，请重试' : '没有找到群组' }}
            </p>
            <p v-if="syncStatus === 'timeout'" class="text-xs text-text-secondary">
              该账号可能未加入任何群组，或 Telegram 连接异常
            </p>
            <p v-else class="text-xs text-text-secondary">该账号尚未加入任何群组</p>
          </template>
        </div>
      </div>
    </div>
    <template #footer>
      <span class="text-xs text-text-secondary">已选 {{ selected.length }} 个</span>
      <div class="flex gap-3">
        <t-button variant="outline" theme="default" @click="visible = false">取消</t-button>
        <t-button theme="primary" @click="confirm">确认</t-button>
      </div>
    </template>
  </Modal>
</template>

<script setup lang="ts">

import { ref, computed, watch, nextTick } from 'vue'
import { Users, Megaphone, UsersRound, Search, RefreshCw } from 'lucide-vue-next'
import { groupApi } from '@/api/group'
// R115: needsSync 死导入已删(正文零使用)
import { syncLock, markSyncing } from '@/utils/syncLock'
import { useGroupStore } from '@/stores/group'
import Modal from '@/components/ui/Modal.vue'
import type { Group } from '@/types'

const props = defineProps<{
  modelValue: boolean
  groups: Group[]
  mode: 'single' | 'multiple'
  accountId?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'select', v: Group[]): void
  (e: 'refresh'): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: v => emit('update:modelValue', v)
})

const tabs = [
  { label: '群组', value: 'group' },
]

const activeTab = ref('group')
const search = ref('')
const selected = ref<Group[]>([])
const loading = ref(false)
const hasAutoRefreshed = ref(false)
const syncFailCount = ref(0)
const syncStatus = ref<'idle' | 'syncing' | 'done' | 'timeout' | 'error'>('idle')
const localGroups = ref<Group[]>([])
const groupStore = useGroupStore()

// props.groups 变化时同步到本地
watch(() => props.groups, (v) => {
  localGroups.value = v && v.length > 0 ? [...v] : localGroups.value
}, { deep: true, immediate: true })

const filtered = computed(() => {
  // 群组 tab = 普通群 + 超级群, 明确排除频道(channel)
  let list = localGroups.value.filter(g => g.type === 'group' || g.type === 'supergroup')
  if (!search.value) return list
  const s = search.value.toLowerCase()
  return list.filter(g =>
    g.title.toLowerCase().includes(s) ||
    (g.username && g.username.toLowerCase().includes(s))
  )
})

// ===== 私聊 tab: contacts 表缓存(同步群组时已写入), 按 tg_user_id 去重, 伪装 Group 结构供父组件取 tg_chat_id =====
const dmUsers = ref<Group[]>([])
const dmLoaded = ref(false)

async function loadDmUsers() {
  if (dmLoaded.value) return
  if (!props.accountId) return
  try {
    const raw: any = await groupApi.listMembersGlobal(props.accountId)
    const arr: any[] = Array.isArray(raw) ? raw : (raw?.list || raw?.data || [])
    const seen = new Set<number>()
    dmUsers.value = arr.filter((m: any) => {
      if (seen.has(m.tg_user_id)) return false
      seen.add(m.tg_user_id)
      return true
    }).map((m: any) => ({
      group_id: 'dm_' + m.tg_user_id,
      title: [m.first_name, m.last_name].filter(Boolean).join(' ') || m.username || String(m.tg_user_id),
      tg_chat_id: String(m.tg_user_id),
      username: m.username || '',
      member_count: 0,
      is_joined: true,
      type: 'dm',
      access_hash: m.access_hash || 0,
    })) as unknown as Group[]
  } catch (e) {
    console.error('[GroupSelector] loadDmUsers:', e)
    dmUsers.value = []
  }
  dmLoaded.value = true
}

const dmFiltered = computed(() => {
  if (!search.value) return dmUsers.value
  const s = search.value.toLowerCase()
  return dmUsers.value.filter(u => u.title.toLowerCase().includes(s) || (u.username && u.username.toLowerCase().includes(s)))
})

watch(activeTab, t => { if (t === 'dm') loadDmUsers() })

const allSelected = computed(() => {
  return filtered.value.length > 0 && filtered.value.every(g => isSelected(g))
})

function selectAll() {
  if (allSelected.value) {
    // 取消全选：只取消当前 filtered 中的
    const ids = new Set(filtered.value.map(g => g.group_id))
    selected.value = selected.value.filter(s => !ids.has(s.group_id))
  } else {
    // 全选：把当前 filtered 中未选的加入
    const existing = new Set(selected.value.map(s => s.group_id))
    for (const g of filtered.value) {
      if (!existing.has(g.group_id)) {
        selected.value.push(g)
      }
    }
  }
}

function isSelected(g: Group) {
  return selected.value.some(s => s.group_id === g.group_id)
}

function toggle(g: Group) {
  if (props.mode === 'single') {
    selected.value = [g]
  } else {
    if (isSelected(g)) {
      selected.value = selected.value.filter(s => s.group_id !== g.group_id)
    } else {
      selected.value.push(g)
    }
  }
}

function confirm() {
  emit('select', [...selected.value])
  visible.value = false
}

async function onRefresh() {
  // 防止重复点击
  if (loading.value) {
    return
  }
  // 全局同步锁：防止跨组件重复同步同一账号
  if (props.accountId && !syncLock.acquire(props.accountId)) {
    // 如果其他组件正在同步同一账号，直接等待结果
    loading.value = true
    syncStatus.value = 'syncing'
    await waitForGlobalSync()
    // 同步完成后刷新本地数据
    try {
      const raw: any = await groupApi.list(props.accountId)
      const list = raw.list || raw.data || (Array.isArray(raw) ? raw : [])
      if (list.length > 0) {
        localGroups.value = list; if (props.accountId) groupStore.setGroups(props.accountId, list)
        emit('refresh')
      }
    } catch (e) {
      console.error('[GroupSelector] list after global sync:', e)
    }
    loading.value = false
    return
  }
  // 手动刷新时重置失败计数
  syncFailCount.value = 0
  if (!props.accountId) {
    syncLock.release()
    emit('refresh')
    return
  }
  loading.value = true
  syncStatus.value = 'syncing'
  try {
    await groupApi.sync(props.accountId)
    // 指数退避轮询：直接调用API检查同步结果，不依赖props响应式更新
    let delay = 1000
    const start = Date.now()
    let pollCount = 0
    while (Date.now() - start < 30000) {
      // 如果弹窗已关闭，停止轮询避免资源浪费
      if (!visible.value) {
        break
      }
      pollCount++
      try {
        const raw: any = await groupApi.list(props.accountId)
        const list = raw.list || raw.data || (Array.isArray(raw) ? raw : [])
        if (list.length > 0) {
          syncStatus.value = 'done'
          // P2-07: 原此行写了两次 setGroups(复制粘贴残留)——幂等但无意义, 去重
          localGroups.value = list; if (props.accountId) groupStore.setGroups(props.accountId, list)
          if (props.accountId) markSyncing(props.accountId)
          emit('refresh')
          await nextTick()
          break
        }
      } catch (e) {
        console.error('[GroupSelector] poll error:', e)
      }
      await new Promise(r => setTimeout(r, delay))
      delay = Math.min(delay * 2, 8000)
    }
    if (syncStatus.value !== 'done' && visible.value) {
      syncStatus.value = 'timeout'
    }
  } catch (e: unknown) {
    console.error('[GroupSelector] sync failed:', e)
    syncStatus.value = 'error'
  } finally {
    await nextTick()
    loading.value = false
    syncLock.release()
  }
}

watch(() => props.modelValue, async (v) => {
  if (v) {
    selected.value = []
    search.value = ''
    activeTab.value = 'all'
    // 不再自动同步，仅手动点击"同步刷新"按钮时同步。
    // 打开时优先读群组缓存(零请求); 无缓存且父组件未传入数据时才查库。
    if (!localGroups.value.length && props.accountId) {
      const cached = groupStore.get(props.accountId)
      if (cached.length) {
        localGroups.value = cached
      } else {
        loading.value = true
        try {
          localGroups.value = await groupStore.ensure(props.accountId)
        } catch (e) {
          console.error('[GroupSelector] ensure error:', e)
        } finally {
          loading.value = false
        }
      }
    }
  }
})

async function autoSync() {
  // 防御性检查：必须有 accountId
  if (!props.accountId) {
    return
  }
  // 全局同步锁检查
  if (syncLock.isSyncing(props.accountId)) {
    loading.value = true
    syncStatus.value = 'syncing'
    await waitForGlobalSync()
    try {
      const raw: any = await groupApi.list(props.accountId)
      const list = raw.list || raw.data || (Array.isArray(raw) ? raw : [])
      if (list.length > 0) {
        localGroups.value = list; if (props.accountId) groupStore.setGroups(props.accountId, list)
        emit('refresh')
      }
    } catch (e) {
      console.error('[GroupSelector] autoSync list error:', e)
    }
    loading.value = false
    return
  }
  if (!syncLock.acquire(props.accountId)) {
    return
  }
  loading.value = true
  syncStatus.value = 'syncing'
  try {
    await groupApi.sync(props.accountId)
  } catch (e: unknown) {
    console.error('[GroupSelector] autoSync sync failed:', e)
    syncStatus.value = 'error'
    loading.value = false
    syncLock.release()
    return
  }
  // 指数退避轮询：直接调用API检查同步结果
  let delay = 1000
  const start = Date.now()
  let pollCount = 0
  while (Date.now() - start < 30000) {
    // 如果弹窗已关闭，停止轮询
    if (!visible.value) {
      break
    }
    pollCount++
    try {
      const raw: any = await groupApi.list(props.accountId)
      const list = raw.list || raw.data || (Array.isArray(raw) ? raw : [])
      if (list.length > 0) {
        syncStatus.value = 'done'
        localGroups.value = list; if (props.accountId) groupStore.setGroups(props.accountId, list)
        emit('refresh')
        await nextTick()
        break
      }
    } catch (e) {
      console.error('[GroupSelector] autoSync poll error:', e)
    }
    await new Promise(r => setTimeout(r, delay))
    delay = Math.min(delay * 2, 8000)
  }
  if (syncStatus.value !== 'done' && visible.value) {
    syncStatus.value = 'timeout'
  }
  await nextTick()
  loading.value = false
  syncLock.release()
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
</script>
