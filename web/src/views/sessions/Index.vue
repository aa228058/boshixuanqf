<template>
  <div class="space-y-4 p-3 md:p-6 max-w-6xl mx-auto">
    <!-- 头部 -->
    <div class="flex flex-wrap items-center gap-2">
      <h1 class="text-lg md:text-xl font-bold flex items-center gap-2">
        <KeyRound class="w-5 h-5" /> 协议号管理
      </h1>
      <span class="text-xs text-text-secondary hidden md:inline">导入外部会话直接可用，无需短信验证</span>
      <div class="flex-1"></div>
      <t-button v-if="selected.size" size="small" theme="danger" variant="outline" class="whitespace-nowrap shrink-0" @click="batchDelete">删除({{ selected.size }})</t-button>
      <t-button size="small" theme="primary" @click="showImport = true"> 导入协议号
      </t-button>
      <t-button size="small" variant="outline" @click="refresh">刷新</t-button>
    </div>

    <!-- 状态筛选 -->
    <div class="flex gap-2 flex-wrap">
      <button v-for="t in tabs" :key="t.key" @click="filterTab = t.key"
        class="px-3 py-1 rounded-full text-xs transition-colors"
        :class="filterTab === t.key ? 'bg-cyan text-white' : 'text-text-secondary'"
        :style="filterTab === t.key ? '' : 'background: var(--t-surface-2);'">
        {{ t.label }} · {{ t.count }}
      </button>
    </div>

    <!-- 统计条 -->
    <div class="grid grid-cols-3 gap-2 md:gap-4">
      <div class="rounded-[var(--t-r-md)] bg-surface/80 p-3 text-center">
        <div class="text-xl font-bold">{{ total }}</div>
        <div class="text-xs text-text-secondary">总数</div>
      </div>
      <div class="rounded-[var(--t-r-md)] bg-surface/80 p-3 text-center">
        <div class="text-xl font-bold text-success">{{ onlineCount }}</div>
        <div class="text-xs text-text-secondary">在线</div>
      </div>
      <div class="rounded-[var(--t-r-md)] bg-surface/80 p-3 text-center">
        <div class="text-xl font-bold text-danger">{{ restrictedCount }}</div>
        <div class="text-xs text-text-secondary">异常</div>
      </div>
    </div>

    <!-- 空态 -->
    <EmptyState v-if="!loading && accounts.length === 0" title="还没有协议号" description="点右上角「导入协议号」开始" />

    <!-- 桌面表格 / 移动卡片 -->
    <div v-else class="rounded-[var(--t-r-md)] bg-surface/80 overflow-hidden">
      <!-- 桌面端 -->
      <table class="w-full text-sm hidden md:table">
        <thead>
          <tr class="text-left text-xs text-text-secondary border-b border-border/50">
            <th class="p-3 w-6"></th>
            <th class="p-3">账号</th>
            <th class="p-3">用户名</th>
            <th class="p-3">姓名</th>
            <th class="p-3">备注</th>
            <th class="p-3">状态</th>
            <th class="p-3 text-right">操作</th>
          </tr>
        </thead>
        <tbody>
          <!-- P0-08: 桌面表格原绑 accounts 原数组,状态 Tab 在桌面端不生效(移动端已用 filteredAccounts) -->
          <tr v-for="acc in filteredAccounts" :key="acc.account_id" class="border-b border-border/30 last:border-0 hover:bg-milk-01">
            <td class="p-3">
              <div class="flex items-center gap-2">
                <img v-if="avatarOf(acc)" :src="avatarOf(acc)!" class="w-8 h-8 rounded-full object-cover" />
                <div v-else class="w-8 h-8 rounded-full bg-cyan/20 flex items-center justify-center text-xs font-bold">
                  {{ (acc.first_name || acc.phone || '?').slice(0, 1).toUpperCase() }}
                </div>
                <div>
                  <div class="font-medium">{{ acc.phone || acc.session_name || '未命名' }}</div>
                  <div class="text-xs text-text-secondary">ID: {{ shortId(acc.account_id) }}</div>
                </div>
              </div>
            </td>
            <td class="p-3">@{{ acc.username || '—' }}</td>
            <td class="p-3">{{ acc.first_name || '—' }}</td>
            <td class="p-3">{{ acc.remark || '—' }}</td>
            <td class="p-3"><StatusBadge :status="acc.status" /></td>
            <td class="p-3 text-right whitespace-nowrap">
              <t-button size="small" variant="text" @click="openEdit(acc)">编辑资料</t-button>
              <t-button size="small" variant="text" @click="pickAvatar(acc)">头像</t-button>
            </td>
          </tr>
        </tbody>
      </table>

      <!-- 移动端卡片 -->
      <div class="md:hidden divide-y divide-border/30">
        <div v-for="acc in filteredAccounts" :key="acc.account_id" class="p-3 flex items-center gap-3">
          <input type="checkbox" :checked="selected.has(acc.account_id)" @change="toggleSelect(acc.account_id)" class="accent-primary shrink-0" />
          <img v-if="avatarOf(acc)" :src="avatarOf(acc)!" class="w-10 h-10 rounded-full object-cover shrink-0" />
          <div v-else class="w-10 h-10 rounded-full bg-cyan/20 flex items-center justify-center text-sm font-bold shrink-0">
            {{ (acc.first_name || acc.phone || '?').slice(0, 1).toUpperCase() }}
          </div>
          <div class="flex-1 min-w-0">
            <div class="font-medium truncate">{{ acc.first_name || acc.phone || '未命名' }}
              <span class="text-xs text-text-secondary">@{{ acc.username || '无用户名' }}</span>
            </div>
            <div class="text-xs text-text-secondary truncate">{{ acc.remark || shortId(acc.account_id) }}</div>
            <StatusBadge :status="acc.status" class="mt-1 inline-block" />
          </div>
          <div class="flex flex-col gap-1 shrink-0">
            <t-button size="extra-small" variant="outline" @click="openEdit(acc)">编辑</t-button>
            <t-button size="extra-small" variant="outline" @click="pickAvatar(acc)">头像</t-button>
          </div>
        </div>
      </div>
    </div>

    <!-- 导入弹窗 -->
    <t-dialog attach="body" v-model:visible="showImport" header="导入协议号" :width="isMobile ? '95%' : '560px'"
      :confirm-btn="{ content: '开始导入', loading: importing }" @confirm="doImport">
      <div class="space-y-3 max-h-[55vh] overflow-y-auto">
        <div class="text-xs text-text-secondary leading-relaxed">
          两种方式任选：<b>上传 .session 文件</b>（卡密商交付格式，推荐）或<b>粘贴 session 字符串</b>，单次最多 50 个。
          导入后系统自动连接验证（每个约 5 秒），失败的在下方提示。
        </div>
        <!-- 方式一：上传 .session 文件（卡密商交付的格式） -->
        <div
          class="rounded-lg border-2 border-dashed border-border/60 p-4 text-center cursor-pointer transition-colors hover:border-cyan/50"
          @click="sessionFileInput?.click()">
          <div class="text-sm text-text-secondary">
            <template v-if="sessionFiles.length === 0">
              点击选择 <b>.session</b> 文件（可多选，单次最多 50 个）
            </template>
            <template v-else>
              已选 <b class="text-primary">{{ sessionFiles.length }}</b> 个文件：
              {{ sessionFiles.map(f => f.name).slice(0, 3).join('、') }}{{ sessionFiles.length > 3 ? '…' : '' }}
            </template>
          </div>
        </div>
        <input ref="sessionFileInput" type="file" accept=".session" multiple class="hidden"
          @change="onSessionFilesPicked" />

        <div class="flex items-center gap-3 text-xs text-text-secondary">
          <div class="flex-1 h-px bg-border/50"></div>或粘贴 session 字符串<div class="flex-1 h-px bg-border/50"></div>
        </div>

        <t-textarea v-model="importText" placeholder="Telethon / Pyrogram 格式，每行一个…"
          :autosize="{ minRows: 4, maxRows: 10 }" />
        <t-input v-model="importRemark" placeholder="统一备注（可选，如：A组-3月采购）" />
        <div v-if="importResult" class="rounded-lg bg-surface p-3 text-xs space-y-1 max-h-40 overflow-y-auto">
          <div>成功 <b class="text-success">{{ importResult.created }}</b> 个，失败
            <b class="text-danger">{{ importResult.failed }}</b> 个</div>
          <div v-for="(e, i) in importResult.errors" :key="i" class="text-danger">{{ e }}</div>
        </div>
      </div>
    </t-dialog>

    <!-- 编辑资料弹窗 -->
    <t-dialog attach="body" v-model:visible="showEdit" header="编辑资料" :width="isMobile ? '95%' : '480px'"
      :confirm-btn="{ content: '保存', loading: saving }" @confirm="doSave">
      <div class="space-y-3 max-h-[42vh] overflow-y-auto" v-if="editForm">
        <div class="flex flex-col sm:flex-row gap-3">
          <t-input v-model="editForm.first_name" label="名字" placeholder="名字" class="flex-1" />
          <t-input v-model="editForm.last_name" label="姓氏" placeholder="姓氏" class="flex-1" />
        </div>
        <t-input v-model="editForm.username" label="用户名" placeholder="用户名（不带 @，留空=清除）" />
        <t-textarea v-model="editForm.about" label="签名" placeholder="个人签名/bio" :autosize="{ minRows: 2 }" />
        <t-input v-model="editForm.remark" label="本地备注" placeholder="仅自己可见的备注" />
        <t-input v-model="editForm.twofa" label="2FA密码" placeholder="二步验证密码（卖家2fa.txt，掉线重连用；留空=保持不变）" />
      </div>
    </t-dialog>


    <!-- 隐藏的文件选择 -->
    <input ref="fileInput" type="file" accept="image/jpeg,image/png,image/webp" class="hidden" @change="onAvatarPicked" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, h, defineComponent } from 'vue'
import { KeyRound } from 'lucide-vue-next'
import { useAccountStore } from '@/stores/account'
import { useAccountAvatar } from '@/composables/useAccountAvatar'
import { toast } from '@/utils/toast'
// R115: 恢复全量导入 —— 复核脚本误报(函数名含 import 的行被脚本当 import 语句过滤),
// doImport 实际依赖 importSessions/importSessionFiles/ImportResult
import { importSessions, importSessionFiles, updateProfile, uploadAvatar, type ImportResult } from '@/api/account'
import EmptyState from '@/components/ui/EmptyState.vue'

const accountStore = useAccountStore()
const { avatarOf } = useAccountAvatar()
// 世界隔离：协议号管理页只显示 source='protocol' 的账号，
// 避免正常号混入列表、批量删除误删正常号
const accounts = computed(() => accountStore.protocolAccounts as any[])
const loading = ref(false)
const isMobile = ref(window.innerWidth < 768)
// 精读修复: resize 监听挂到 onMounted 并在卸载时移除, 防重复进页面累积泄漏
const onResize = () => { isMobile.value = window.innerWidth < 768 }

const total = computed(() => accounts.value.length)
const onlineCount = computed(() => accounts.value.filter((a: any) => a.status === 'online').length)
const restrictedCount = computed(() => accounts.value.filter((a: any) => a.status === 'restricted' || a.status === 'banned').length)

async function refresh() {
  loading.value = true
  try { await accountStore.fetchAccounts() }
  catch { toast({ type: 'error', title: '加载失败' }) }
  finally { loading.value = false }
}

const shortId = (id: string) => (id || '').slice(0, 8)

// ============ 状态筛选 + 批量删除 ============
const filterTab = ref('all')
const tabs = computed(() => [
  { key: 'all', label: '全部', count: accounts.value.length },
  { key: 'online', label: '在线', count: accounts.value.filter((a: any) => a.status === 'online').length },
  { key: 'standby', label: '待机', count: accounts.value.filter((a: any) => a.status !== 'online' && a.status !== 'restricted' && a.status !== 'banned').length },
  { key: 'error', label: '异常', count: accounts.value.filter((a: any) => a.status === 'restricted' || a.status === 'banned').length },
])
const filteredAccounts = computed(() => {
  if (filterTab.value === 'online') return accounts.value.filter((a: any) => a.status === 'online')
  if (filterTab.value === 'standby') return accounts.value.filter((a: any) => a.status !== 'online' && a.status !== 'restricted' && a.status !== 'banned')
  if (filterTab.value === 'error') return accounts.value.filter((a: any) => a.status === 'restricted' || a.status === 'banned')
  return accounts.value
})
const selected = ref<Set<string>>(new Set())
function toggleSelect(id: string) {
  const s = new Set(selected.value)
  if (s.has(id)) { s.delete(id) } else { s.add(id) }
  selected.value = s
}
// P1-31 审计修复: 原 for+await 串行删除(每个约 5s, 10 个 = 50s 无进度), 失败仅
// console.error 用户无从感知。改并行 + 汇总反馈。
async function batchDelete() {
  if (!selected.value.size) return
  if (!window.confirm(`确定删除选中的 ${selected.value.size} 个协议号？`)) return
  const ids = Array.from(selected.value)
  const results = await Promise.allSettled(ids.map((id) => accountStore.deleteAccount(id)))
  const failed = results.filter((r) => r.status === 'rejected').length
  selected.value = new Set()
  await refresh()
  if (failed > 0) {
    toast({ type: 'warning', title: `删除完成, ${failed} 个失败`, message: '失败的号可能正在执行任务, 请稍后重试' })
  }
}

// ============ 状态徽标（本地组件） ============
const statusMap: Record<string, { text: string; cls: string }> = {
  online: { text: '在线', cls: 'bg-success/15 text-success' },
  offline: { text: '待机', cls: 'bg-milk-01 text-text-secondary' },
  restricted: { text: '受限', cls: 'bg-warning/15 text-warning' },
  banned: { text: '封号', cls: 'bg-danger/15 text-danger' },
  pending_auth: { text: '待验证', cls: 'bg-cyan/15 text-primary' },
  // 认证成功待上线/按需待机的过渡态：后端 status=authenticated（此前无映射显示原文）
  authenticated: { text: '已认证', cls: 'bg-cyan/15 text-primary' },
  // P2-62: 补 pending_verification——raffle 页有此映射此处缺, 同状态两页显示不一致(一个中文一个英文)
  pending_verification: { text: '待验证', cls: 'bg-cyan/15 text-primary' },
}
const StatusBadge = defineComponent({
  props: { status: { type: String, default: '' } },
  setup(props) {
    return () => {
      const s = statusMap[props.status] || { text: props.status || '?', cls: 'bg-milk-01' }
      return h('span', { class: `text-xs px-2 py-0.5 rounded-full ${s.cls}` }, s.text)
    }
  },
})

// ============ 导入 ============
const showImport = ref(false)
const importText = ref('')
const importRemark = ref('')
const importing = ref(false)
const importResult = ref<ImportResult | null>(null)

const importLines = computed(() => importText.value.split('\n').map(s => s.trim()).filter(Boolean))

const sessionFileInput = ref<HTMLInputElement>()
const sessionFiles = ref<File[]>([])

function onSessionFilesPicked(e: Event) {
  const input = e.target as HTMLInputElement
  if (input.files) sessionFiles.value = Array.from(input.files)
  input.value = ''
}

async function doImport() {
  if (importLines.value.length === 0 && sessionFiles.value.length === 0) {
    toast({ type: 'warning', title: '请先选择 .session 文件或粘贴 session 字符串' })
    return
  }
  importing.value = true
  try {
    const res = sessionFiles.value.length > 0
      ? await importSessionFiles(sessionFiles.value, importRemark.value)
      : await importSessions(importLines.value, importRemark.value)
    importResult.value = res
    if (res.created > 0) {
      toast({ type: 'success', title: `成功导入 ${res.created} 个`, message: '正在后台连接验证…' })
      accountStore.fetchAccounts(true) // 立即刷新——行已在DB，无需等8秒
      setTimeout(() => { refresh(); accountStore.fetchAccounts(true) }, 8000) // 8秒后二次刷新获取连接状态
    }
    if (res.failed === 0) {
      setTimeout(() => { showImport.value = false; importText.value = ''; sessionFiles.value = []; importResult.value = null }, 1500)
    }
  } catch (e: any) {
    toast({ type: 'error', title: '导入失败', message: e?.message })
  } finally {
    importing.value = false
  }
}

// ============ 编辑资料 ============
const showEdit = ref(false)
const saving = ref(false)
const editForm = ref<{
  account_id: string; first_name: string; last_name: string;
  about: string; username: string; remark: string; twofa: string
} | null>(null)

function openEdit(acc: any) {
  editForm.value = {
    account_id: acc.account_id,
    first_name: acc.first_name || '',
    last_name: acc.last_name || '',
    about: acc.about || '',
    username: acc.username || '',
    remark: acc.remark || '',
    twofa: '',
  }
  showEdit.value = true
}

async function doSave() {
  if (!editForm.value) return
  saving.value = true
  try {
    await updateProfile(editForm.value.account_id, {
      first_name: editForm.value.first_name,
      last_name: editForm.value.last_name,
      about: editForm.value.about,
      username: editForm.value.username.replace(/^@/, ''),
      twofa: editForm.value.twofa,
      remark: editForm.value.remark,
    })
    toast({ type: 'success', title: '资料已更新' })
    showEdit.value = false
    refresh()
  } catch (e: any) {
    toast({ type: 'error', title: '保存失败', message: e?.message })
  } finally {
    saving.value = false
  }
}

// ============ 头像 ============
const fileInput = ref<HTMLInputElement>()
const avatarFor = ref('')

function pickAvatar(acc: any) {
  avatarFor.value = acc.account_id
  fileInput.value?.click()
}

async function onAvatarPicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || !avatarFor.value) return
  if (file.size > 5 * 1024 * 1024) {
    toast({ type: 'warning', title: '图片不能超过 5MB' })
    return
  }
  try {
    await uploadAvatar(avatarFor.value, file)
    toast({ type: 'success', title: '头像已更新' })
    refresh()
  } catch (err: any) {
    toast({ type: 'error', title: '上传失败', message: err?.message })
  }
}

// 状态自动刷新：导入/认证为后台异步流程（TG 连接 5-15s），轮询让"在线"及时出现
let statusPoll: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  refresh()
  statusPoll = setInterval(refresh, 15000)
  window.addEventListener('resize', onResize)
})
onUnmounted(() => {
  if (statusPoll) clearInterval(statusPoll)
  window.removeEventListener('resize', onResize)
})
</script>

<style scoped>
/* 弹窗底部按钮: 右对齐 + 统一间距(修复确认/取消不对齐/窄屏被挤竖排) */
:deep(.t-dialog__footer) {
  display: flex !important;
  flex-direction: row !important;
  justify-content: flex-end;
  align-items: center;
  gap: 8px;
  flex-wrap: nowrap;
}
:deep(.t-dialog__footer .t-button) {
  flex: none;
  width: auto;
  min-width: 96px;
}
/* toast 浮在弹窗遮罩之上, 保存成功提示不被盖住 */
:deep(.t-message) {
  z-index: 3500;
}
</style>
