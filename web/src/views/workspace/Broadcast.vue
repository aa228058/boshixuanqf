<template>
  <div class="p-4 sm:p-6 max-w-3xl mx-auto">
    <div class="flex items-center justify-between gap-3 mb-4">
      <div class="min-w-0">
        <h1 class="text-title font-extrabold tracking-tight text-text-primary truncate">群发消息</h1>
        <!-- R109: 移除"当前账号"标签(群发已多账号池化, 单一账号标签误导) -->
        <p class="text-sm text-text-muted mt-1">执行账号在创建任务时选择, 可多选</p>
      </div>
      <button @click="showCreate = true" class="px-4 py-2 rounded-lg text-sm font-medium glow-btn flex items-center gap-2 shrink-0"> 新建群发任务
      </button>
    </div>

    <!-- 页面 tab: 任务列表 / 发送日历(发送日历合并自原独立页) -->
    <div class="flex items-center gap-1 border-b border-border mb-4">
      <button v-for="t in pageTabs" :key="t.key"
              @click="activeTab = t.key"
              class="px-4 py-2 text-sm font-medium transition-colors relative"
              :class="activeTab === t.key ? 'text-cyan' : 'text-text-secondary hover:text-text-primary'">
        {{ t.label }}
        <span v-if="activeTab === t.key" class="tab-underline absolute bottom-0 left-0 right-0 h-0.5 bg-cyan rounded-full" />
      </button>
    </div>

    <!-- 发送日历 tab: 每日成功发送量月历热力图 -->
    <SendCalendar v-if="activeTab === 'calendar'" />

    <!-- 任务列表 tab(默认): 原有全部内容 -->
    <div v-show="activeTab === 'tasks'">
    <!-- 实时进度面板: 创建任务后在此显示, 无需跳转 -->
    <!-- R110: 协议号协同任务记录块已删(isProtocol 恒 false 死代码; 统一走多号池任务) -->
    <!-- 进行中的任务在下方列表已显示成功/失败/等待进度, 不再重复展示实时卡片 -->

    <!-- 本账号的群发任务列表 -->
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
      <EmptyState v-if="tasksError" icon="MessageSquare" :title="tasksError" description="点击重试">
        <button @click="fetchTasks" class="mt-3 px-4 py-1.5 rounded-lg text-xs border border-border text-text-secondary hover:text-cyan hover:border-cyan/40 transition-colors">重新加载</button>
      </EmptyState>
      <EmptyState v-else-if="!tasks.length" icon="MessageSquare" title="暂无群发任务" description="点击右上角「新建群发任务」开始发送" />
      <div v-else class="space-y-3 mb-4">
        <pull-down-refresh v-if="tasks.length" v-model="pullRefreshing" @refresh="onPullRefresh">
        <div v-for="(t, ti) in tasks" :key="t.task_id" :class="['glass rounded-[var(--t-r-md)] p-4 card-lift card-enter hover:border-cyan/20 mb-3', t.status === 'running' ? 'card-running' : '']" :style="{ '--i': ti }">
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-2 min-w-0">
              <span class="px-2 py-0.5 rounded text-xs shrink-0 inline-flex items-center gap-1.5" :class="statusClass(t.status)">
                <span v-if="t.status === 'running'" class="w-1.5 h-1.5 rounded-full bg-current dot-pulse" />
                {{ statusLabel(t.status) }}
              </span>
              <span class="text-xs text-text-muted truncate">目标 {{ (t.target_ids || []).length || progOf(t).total || '-' }} 个</span>
            </div>
            <span class="text-xs text-text-muted shrink-0">{{ formatTime(t.created_at) }}</span>
          </div>
          <div class="mt-3">
            <div class="flex items-center justify-between text-xs mb-1.5">
              <span class="text-text-secondary tabular-nums">
                {{ progOf(t).done + (progOf(t).failed || 0) }} / {{ progOf(t).total }} 已处理
                <span class="text-emerald ml-1.5">{{ progOf(t).done }} 成功</span>
                <span v-if="progOf(t).failed" class="text-rose ml-1.5">{{ progOf(t).failed }} 失败</span>
              </span>
              </div>
            <!-- 进度环(交互示例28): 绿=成功段 青=其余进度, 44px 紧凑 -->
            <div class="flex items-center gap-2 mt-2">
              <ProgressRing :percent="percentOf(t)" :success="progOf(t).total ? Math.round((progOf(t).done / progOf(t).total) * 100) : 0" :size="44" :stroke="4" />
              <div class="flex-1">
                <!-- 分段进度条: 绿=成功 红=失败 灰底=剩余, 进行中/已完成都有 -->
                <div class="h-2 rounded-full bg-bg-hover overflow-hidden flex">
              <div class="h-full bg-emerald transition-all duration-500"
                   :style="{ width: (progOf(t).total ? (progOf(t).done / progOf(t).total) * 100 : 0) + '%' }" />
              <div v-if="progOf(t).failed" class="h-full bg-rose transition-all duration-500"
                   :style="{ width: (progOf(t).total ? (progOf(t).failed / progOf(t).total) * 100 : 0) + '%' }" />
                </div>
              </div>
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
    </div><!-- /v-show tasks tab 容器 -->

    <!-- 新建模态框: 表单全部内容, 统一全站弹窗组件(留在 tab 容器外, 任何 tab 可开) -->
    <Modal v-model="showCreate" title="新建群发任务">
    <div class="space-y-4">
      <!-- R110: 协议号协同死分支已删(isProtocol 恒 false)。统一多号池流程见下 -->
      <!-- 目标选择 -->
      <div>
        <!-- R98 表单分组① -->
      <div class="flex items-center gap-2 mb-3 -mt-1"><span class="w-[18px] h-[18px] rounded-full grid place-items-center text-[10px] font-black text-white shrink-0" style="background: var(--t-brand);">1</span><span class="text-[11.5px] font-extrabold uppercase tracking-wider" style="color: var(--t-text-2);">发送目标</span><span class="flex-1 h-px" style="background: var(--t-line);"></span></div>
      <label class="block text-sm font-medium mb-2">发送目标</label>
        <div @click="showTargetSelector = true" class="p-3 rounded-lg border border-border cursor-pointer hover:border-cyan/30 transition-colors">
          <div class="flex items-center gap-2">
            <MessageSquare class="w-5 h-5 text-text-muted shrink-0" />
            <span v-if="!selectedTargets.length" class="text-text-muted">点击选择发送目标</span>
            <!-- R118: 显示群名而不只是数量 —— 前3个群名 + 剩余折叠 -->
            <template v-else>
              <span class="text-sm">已选 {{ selectedTargets.length }} 个目标</span>
              <button type="button" class="ml-auto text-[11px] text-cyan hover:underline shrink-0" @click.stop="showTargetNames = !showTargetNames">{{ showTargetNames ? '收起' : '查看群名' }}</button>
            </template>
          </div>
          <!-- 群名 chips: 全部列出, 小字, 可滚动 -->
          <div v-if="selectedTargets.length && showTargetNames" class="flex flex-wrap gap-1 mt-2 max-h-24 overflow-y-auto">
            <span v-for="tid in selectedTargets" :key="tid" class="px-1.5 py-0.5 rounded text-[11px] bg-bg-secondary text-text-secondary border border-border/60">{{ targetTitle(tid) }}</span>
          </div>
        </div>
        <TargetSelector v-model="showTargetSelector" :groups="groups" :accountId="currentAccount?.account_id" @select="selectedTargets = $event" @refresh="onSelectorRefresh" />
        <!-- R101 P1: 执行账号多选(后端笛卡尔模式: 每账号×全目标) -->
        <label class="block text-sm font-medium mb-2 mt-4">执行账号（可多选）</label>
        <div class="flex flex-wrap gap-1.5">
          <button v-for="acc in onlineAccounts" :key="acc.account_id" type="button" @click="toggleExec(acc.account_id)"
                  class="px-2.5 py-1.5 rounded-lg text-xs border transition-colors whitespace-nowrap"
                  :class="execAccounts.includes(acc.account_id) ? 'border-cyan/50 bg-cyan/10 text-cyan font-medium' : 'border-border text-text-secondary hover:border-border-hover'">
            {{ acc.first_name || acc.phone }}
          </button>
        </div>
        <p v-if="selectedTargets.length && execAccounts.length" class="text-[10.5px] text-text-muted mt-1.5">
          预计发送 {{ selectedTargets.length * execAccounts.length }} 条 = {{ selectedTargets.length }} 目标 × {{ execAccounts.length }} 账号
          · 约需 {{ estMinutes }} 分钟
        </p>
        <!-- R101 P1-第二刀: 覆盖预览 -->
        <div v-if="selectedTargets.length && execAccounts.length && coverage.ok + coverage.fail > 0"
             class="mt-3 rounded-lg border p-3 text-xs"
             :class="(coverage.fail > 0 || coverage.unreachable.length) ? 'border-amber/30 bg-amber/5' : 'border-emerald/30 bg-emerald/5'">
          <div>
            <span class="text-emerald">✅ 可发 {{ coverage.ok }} 条</span>
            <span v-if="coverage.fail" class="text-amber ml-2">⚠️ 预计失败 {{ coverage.fail }} 条（账号未加入该群）</span>
            <span v-if="coverage.unreachable.length" class="text-rose ml-2">⛔ {{ coverage.unreachable.length }} 个群无账号加入</span>
          </div>
          <div v-if="coverage.unreachable.length" class="text-[10.5px] text-amber/90 mt-1.5 leading-relaxed">
            未覆盖：{{ coverage.unreachable.slice(0, 3).join('、') }}{{ coverage.unreachable.length > 3 ? ` 等 ${coverage.unreachable.length} 个` : '' }}——这些群发送会失败，建议先把账号拉进群
          </div>
        </div>
      </div>

      <div class="mt-2">
        <button type="button" @click="showImport = !showImport" class="text-xs text-cyan hover:underline">批量导入目标（粘贴列表/上传文件）</button>
        <div v-if="showImport" class="mt-2 space-y-2">
          <textarea v-model="importText" rows="4" class="w-full bg-bg-card border border-border rounded-lg px-3 py-2 text-sm"
            placeholder="每行一个目标, 支持:&#10;@用户名&#10;https://t.me/群名&#10;-100xxxxxxxxxx(群ID)&#10;也可粘贴CSV内容"></textarea>
          <div class="flex gap-2">
            <t-button theme="primary" size="sm" variant="outline" @click="applyImportText">解析添加</t-button>
            <label class="px-3 py-1.5 rounded-lg border border-border text-xs cursor-pointer hover:border-cyan/40 transition-colors">
              选择文件<input type="file" accept=".txt,.csv" class="hidden" @change="onImportFile">
            </label>
          </div>
        </div>
      </div>

      <!-- R98 表单分组② -->
      <div class="flex items-center gap-2 mb-3 mt-5"><span class="w-[18px] h-[18px] rounded-full grid place-items-center text-[10px] font-black text-white shrink-0" style="background: var(--t-brand);">2</span><span class="text-[11.5px] font-extrabold uppercase tracking-wider" style="color: var(--t-text-2);">消息内容</span><span class="flex-1 h-px" style="background: var(--t-line);"></span></div>
      <!-- 消息内容 -->
      <div>
      <label class="block text-sm font-medium mb-2">消息内容</label>
        <!-- 变量插入(交互示例10标签输入的轻量版): 点变量插入到光标处 -->
        <div class="flex flex-wrap gap-1.5 mb-2">
          <button v-for="v in msgVars" :key="v" @click="insertVar(v)"
                  class="px-2 py-0.5 rounded-md bg-bg-primary border border-border text-text-secondary text-xs font-mono hover:border-cyan hover:text-cyan transition-colors">{{ v }}</button>
          <span v-if="draftSaved" class="text-xs text-emerald self-center ml-auto">草稿已自动保存</span>
        </div>
        <t-textarea ref="msgRef" v-model="message" :autosize="{ minRows: 4, maxRows: 10 }"
                    placeholder="输入要发送的消息... 变量: {{first_name}}, {{group_name}}, {{user_id}}, {{now}}； 随机语法: {你好|Hi|Hello}" />
        <div class="flex items-center justify-between mt-2">
          <span class="text-xs text-text-muted">{{ message.length }} 字符</span>
          <button @click="showPreview = true" class="text-xs text-cyan hover:text-cyan-bright transition-colors">预览变量效果</button>
        </div>
      </div>

      <!-- 消息混淆(两种模式共用) -->
      <label class="flex items-center gap-2 text-xs cursor-pointer select-none mb-3">
        <input type="checkbox" v-model="obfuscate" class="accent-cyan w-4 h-4" />
        <span class="font-semibold">消息混淆</span>
        <span class="text-text-muted">每条自动加随机表情+隐形字符, 内容不重复防风控</span>
      </label>
      <!-- 每条消息自动变化: 一键让措辞/标点各不相同, 不用写任何变量 -->
      <label class="flex items-center gap-2 text-xs cursor-pointer select-none mb-3">
        <input type="checkbox" v-model="autoVary" class="accent-cyan w-4 h-4" />
        <span class="font-semibold">每条消息自动变化</span>
        <span class="text-text-muted">自动换同义词、抖动标点, 千条消息千种写法</span>
      </label>
      <!-- 富媒体(可选) -->
      <div>
        <label class="block text-sm font-medium mb-2">附带媒体 <span class="text-text-muted font-normal">(可选)</span></label>
        <t-select v-model="mediaType" class="mb-2" :options="[
          { label: '无', value: '' },
          { label: '图片', value: 'photo' },
          { label: '视频', value: 'video' },
        ]" />
        <div v-if="mediaType" class="space-y-2">
          <input v-model="mediaUrl" placeholder="媒体 URL (https://...)，或上传本地文件"
                 class="w-full px-4 py-2 rounded-[var(--t-r-md)] glow-input text-sm" />
          <div class="flex items-center gap-2">
            <label class="px-3 py-1.5 rounded-lg border border-border text-xs cursor-pointer hover:border-cyan/40 transition-colors">
              上传文件<input type="file" accept=".jpg,.jpeg,.png,.webp,.gif,.mp4,.mov" class="hidden" @change="onMediaFile">
            </label>
            <span v-if="mediaPath" class="text-xs text-cyan">已上传: {{ mediaPath }}</span>
            <span v-if="uploadingMedia" class="text-xs text-text-muted">上传中…</span>
            <button v-if="mediaPath || mediaUrl" @click="clearMedia" class="text-xs text-rose hover:underline">清除</button>
          </div>
        </div>
      </div>

      <!-- R98 表单分组③ -->
      <div class="flex items-center gap-2 mb-3 mt-5"><span class="w-[18px] h-[18px] rounded-full grid place-items-center text-[10px] font-black text-white shrink-0" style="background: var(--t-brand);">3</span><span class="text-[11.5px] font-extrabold uppercase tracking-wider" style="color: var(--t-text-2);">发送设置</span><span class="flex-1 h-px" style="background: var(--t-line);"></span></div>
      <!-- 发送间隔设置 -->
      <div>
        <label class="block text-sm font-medium mb-2">发送间隔（秒）</label>
        <!-- TDesign 骨架: 数字输入组件(品牌紫双主题) -->
        <div class="flex gap-3">
          <t-input-number v-model="intervalMin" :min="1" :max="300" theme="normal" class="flex-1" />
          <t-input-number v-model="intervalMax" :min="1" :max="300" theme="normal" class="flex-1" />
        </div>
        <p class="text-xs text-text-muted mt-1">系统会在最小和最大间隔之间随机取值，模拟真人发送</p>
      </div>

      <!-- 定时启动(可选) -->
      <div>
        <label class="block text-sm font-medium mb-2">定时启动 <span class="text-text-muted font-normal">(可选, 留空立即执行)</span></label>
        <t-date-picker v-model="scheduledAtTs" type="datetime" value-type="time-stamp" clearable
                       placeholder="选择定时启动时间（留空立即执行）" class="w-full" />
        <p class="text-xs text-text-muted mt-1">到设定时间后任务自动进入队列执行</p>
      </div>

      <!-- 发送模式(2026-10-20 双 tab): 只发一次 / 循环群发(套餐 feature 控制) -->
      <div>
        <label class="block text-sm font-medium mb-2">发送模式</label>
        <div class="flex rounded-lg bg-ink-01 p-1 gap-1 w-fit">
          <button type="button" class="px-4 py-1.5 rounded-md text-sm transition-colors"
                  :class="!isLoop ? 'bg-bg-card text-text-primary font-medium shadow-sm' : 'text-text-secondary'"
                  @click="isLoop = false">只发一次</button>
          <button type="button" class="px-4 py-1.5 rounded-md text-sm transition-colors relative"
                  :class="[isLoop ? 'bg-bg-card text-text-primary font-medium shadow-sm' : 'text-text-secondary', !loopFeature && 'opacity-60']"
                  @click="loopFeature ? (isLoop = true) : toast({ type: 'warning', message: '当前套餐未开通循环群发, 请联系管理员升级' })">
            循环群发
            <span v-if="!loopFeature" class="absolute -top-1 -right-1 text-[9px] px-1 rounded bg-amber/20 text-amber border border-amber/40">锁</span>
          </button>
        </div>
        <div v-if="isLoop" class="flex items-center gap-2 mt-2">
          <span class="text-xs text-text-secondary">每轮间隔</span>
          <t-input v-model="loopIntervalMin" type="number" :min="1" :max="10080" class="w-28" />
          <span class="text-xs text-text-muted">分钟</span>
        </div>
        <p v-if="isLoop" class="text-xs text-text-muted mt-1">混淆文案每轮重新随机, 目标列表去重不重复轰炸</p>
      </div>

      <!-- 操作按钮 -->
      <div class="flex items-center justify-end gap-3">
        <t-button variant="outline" theme="default" @click="showCreate = false">取消</t-button>
        <t-button theme="primary" :loading="submitting" :disabled="!canSubmit" @click="submit">创建任务</t-button>
      </div>
    </div>
    </Modal>

    <!-- 变量预览弹窗(统一走 Modal 封装) -->
    <Modal v-model="showPreview" title="变量预览">
      <div class="space-y-3 text-sm">
        <div class="p-3 rounded-lg bg-bg-card">
          <div class="text-text-muted mb-1">原始内容:</div>
          <div class="whitespace-pre-wrap">{{ message }}</div>
        </div>
        <div class="p-3 rounded-lg bg-cyan/5 border border-cyan/20">
          <div class="text-cyan mb-1">替换后效果:</div>
          <div class="whitespace-pre-wrap">{{ previewMessage }}</div>
        </div>
      </div>
      <template #footer>
        <button class="modal-btn-primary" @click="showPreview = false">关闭</button>
      </template>
    </Modal>
  </div>
</template>

<script setup lang="ts">
import { parseTargets } from '@/utils/importTargets'
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAccountStore } from '@/stores/account'
import { groupApi } from '@/api/group'
// R115: Group 死类型导入已删(protoGroupList 删除后零使用)
// R111 已删 ProtocolPicker / campaignApi / CAMPAIGN_STATUS 导入(协议协同死代码)
import { useGroupStore } from '@/stores/group'
import { taskApi } from '@/api/task'
import { useAuthStore } from '@/stores/auth'
import SendCalendar from '@/components/SendCalendar.vue'
import { mediaApi } from '@/api/media'
import { Loader2, MessageSquare, Plus } from 'lucide-vue-next'
import TargetSelector from '@/components/selectors/TargetSelector.vue'
import { toast } from '@/utils/toast'
// LiveTaskFeed 实时卡片已移除(与列表重复), 保留组件文件供其他页面使用
import Modal from '@/components/ui/Modal.vue'
import ProgressRing from '@/components/ui/ProgressRing.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Skeleton from '@/components/ui/Skeleton.vue'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import { useTaskEvents } from '@/composables/useTaskEvents'

const route = useRoute()
const router = useRouter()
const accountStore = useAccountStore()
const currentAccount = computed(() => accountStore.currentAccount)
const accName = (a: any) => a?.first_name || a?.remark || a?.phone || (a?.account_id || '').slice(0, 8)

// ===== tab 与发送模式状态(2026-10-20: 此前多轮编辑事故把这块定义整体冲丢, 模板引用 undefined
// 导致 tab 栏/循环开关静默不显示。此处集中补回, 以后动这个文件先看这里) =====
const activeTab = ref<'tasks' | 'calendar'>('tasks')
const pageTabs = [{ key: 'tasks', label: '任务列表' }, { key: 'calendar', label: '发送日历' }]
const isLoop = ref(false)
const loopIntervalMin = ref(60)
const authStore = useAuthStore()
const loopFeature = computed(() => {
  const pf: any = (authStore.tenant as any)?.plan_features
  return pf?.loop_broadcast === true
})

const selectedTargets = ref<string[]>([])
// R118: 群名显示 —— 目标 ID → 群标题映射; 缓存缺失时回退 ID 尾号
const showTargetNames = ref(false)
const targetTitle = (id: string) => {
  const g = (groups.value || []).find((x: any) => String(x.tg_chat_id || x.id) === String(id))
  if (!g) return '…' + String(id).slice(-6)
  return g.title || ('群 ' + String(g.tg_chat_id || g.id).slice(-6))
}
// R101 P1-切片1: 多号执行 —— 后端广播任务原生支持 account_ids 数组(笛卡尔模式:
// 每个账号对全部目标各发一遍)。默认勾选当前账号, 可勾选多个在线账号。
const execAccounts = ref<string[]>([])

// R151: 自动同步群组 —— 执行账号就绪即 ensure(有缓存秒显; 无缓存/过期后台自动同步)
watch(execAccounts, (list) => {
  for (const id of list || []) groupStore.ensure(id)
}, { immediate: true })

// 选择器同步完成后回填(覆盖 mutation 总线擦除的空窗)
function onSelectorRefresh() {
  for (const id of execAccounts.value) groupStore.ensure(id)
}
watch(currentAccount, (acc: any) => {
  if (acc?.account_id && !execAccounts.value.includes(acc.account_id)) {
    execAccounts.value = [acc.account_id, ...execAccounts.value.filter(id => id !== acc.account_id)]
  }
}, { immediate: true })
function toggleExec(id: string) {
  const i = execAccounts.value.indexOf(id)
  if (i >= 0) execAccounts.value.splice(i, 1)
  else execAccounts.value.push(id)
}
const onlineAccounts = computed(() => (accountStore.accounts || []).filter((a: any) => a.status === 'online'))

// R101 P1-第二刀: 覆盖预览 —— 拉全租户群(带 account_id), 计算"每个目标有哪些账号已加入",
// 提交前摊开: 可发/预计失败/完全无账号加入的群。
const _allGroups = ref<any[]>([])
async function loadAllGroups() {
  // R182.4: request.ts 已把列表响应统一包装为 {list,total} 对象(P0-35 审计修复), 不能当数组直用;
  // 直接 for...of 会抛 "not iterable" 导致群发页整页爆红。兼容数组/包装两种形态。
  try {
    const res: any = await groupApi.list()
    _allGroups.value = Array.isArray(res) ? res : (res?.list ?? [])
  } catch { _allGroups.value = [] }
}
function groupNameOf(t: string): string {
  const g: any = _allGroups.value.find((x: any) =>
    String(x.group_id) === t || String(x.tg_chat_id) === t || x.username === t)
  return g ? (g.title || g.username || t) : t
}
const coverage = computed(() => {
  const joinedBy = new Map<string, Set<string>>()
  for (const g of _allGroups.value as any[]) {
    const keys = [g.group_id, g.tg_chat_id, g.username, g.tg_link].filter(Boolean).map(String)
    for (const k of keys) {
      if (!joinedBy.has(k)) joinedBy.set(k, new Set())
      joinedBy.get(k)!.add(g.account_id)
    }
  }
  const selected = new Set(execAccounts.value)
  let ok = 0, fail = 0
  const unreachable: string[] = []
  for (const t of selectedTargets.value) {
    const joined = joinedBy.get(String(t))
    if (!joined || joined.size === 0) { unreachable.push(groupNameOf(t)); continue }
    const cand = [...joined].filter(id => selected.has(id)).length
    ok += cand
    fail += execAccounts.value.length - cand
  }
  return { ok, fail, unreachable }
})
// R102: 预计耗时 = 总条数 × 平均间隔(秒) / 60, 向上取整
const estMinutes = computed(() => {
  const total = selectedTargets.value.length * execAccounts.value.length
  if (!total) return 0
  const avgSec = (intervalMin.value + intervalMax.value) / 2
  return Math.max(1, Math.ceil((total * avgSec) / 60))
})
const showImport = ref(false)
const importText = ref('')

// 解析导入文本: 每行一个目标, 支持 @用户名 / t.me链接 / 纯ID
function applyImportText() {
  const items = parseTargets(importText.value)
  if (!items.length) { toast({ type: 'error', title: '没有可识别的目标' }); return }
  selectedTargets.value = [...new Set([...selectedTargets.value, ...items])]
  toast({ type: 'success', title: `已添加 ${items.length} 个目标` })
  importText.value = ''
  showImport.value = false
}

function onImportFile(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (!f) return
  const r = new FileReader()
  r.onload = () => { importText.value = String(r.result || ''); applyImportText() }
  r.readAsText(f)
}
const message = ref('')

// ===== 变量插入 + 草稿自动保存(交互示例 10/09/116, 2026-10-21) =====
// P0-02 审计修复: 原单花括号与后端 dictVarRe 的双花括号约定不一致——
// 变量从未生效, 且单花括号会被后端当 spintax 随机改写消息。统一双花括号。
const msgVars = ['{{first_name}}', '{{group_name}}', '{{user_id}}', '{{now}}', '{{username}}', '{{phone}}']
const msgRef = ref<any>(null)
const draftSaved = ref(false)

function insertVar(v: string) {
  // t-textarea 内部是 textarea 元素; 优先插到光标处, 拿不到光标就追加
  const el = msgRef.value?.$el?.querySelector?.('textarea') || msgRef.value?.$el
  if (el && typeof el.selectionStart === 'number') {
    const s = el.selectionStart, e = el.selectionEnd
    message.value = message.value.slice(0, s) + v + message.value.slice(e)
    nextTick(() => { el.focus(); el.selectionStart = el.selectionEnd = s + v.length })
  } else {
    message.value += v
  }
}

// 草稿: 停止输入 800ms 后存 localStorage, 重新打开页面自动恢复(示例116)
let draftTimer: any = null
const DRAFT_KEY = 'broadcast_msg_draft'
watch(message, (v) => {
  draftSaved.value = false
  clearTimeout(draftTimer)
  draftTimer = setTimeout(() => {
    try {
      if (v.trim()) { localStorage.setItem(DRAFT_KEY, v); draftSaved.value = true }
      else localStorage.removeItem(DRAFT_KEY)
    } catch {}
  }, 800)
})
// 恢复草稿(挂载时, 仅当当前内容为空)
onMounted(() => {
  try {
    const d = localStorage.getItem(DRAFT_KEY)
    if (d && d.trim() && !message.value.trim()) {
      message.value = d
      toast({ type: 'info', message: '已恢复上次未发送的草稿' })
    }
  } catch {}
})
// 默认 [5,12] 与后端强制下限及协议群发页一致；原先 [4,300] 会让协同任务
// 平均 2.5 分钟才发一条，看起来像"群发没反应、账号全待机"
const intervalMin = ref(5)
const intervalMax = ref(12)

// 定时启动(可选): datetime-local 值原样提交, 后端支持 "2006-01-02T15:04" 格式
const scheduledAtTs = ref<number | null>(null)
// ===== 协议号协同模式: 勾选多个号, 走协同引擎(分工/限流换人/阵亡替补) =====
// R111 已删 isProtocol 计算属性(accountStore.view 为死常量, 恒 'phone')
// R111: protocolIds 等协议协同变量已删(死代码, view 恒 'phone')
// R111 已删 protoGroups
// R111 已删 protoGroupList / toggleProtoGroup / protocolTargets / protocolKind(协议协同死代码)
const obfuscate = ref(false) // 消息混淆: 自动加随机emoji+隐形字符, 不用写变量语法
const autoVary = ref(true) // 每条消息自动变化: 同义词+标点抖动, 一键开启无需语法
// R111 已删 campaigns / loadCampaigns(协议协同死代码)
const submitting = ref(false)
// 任务优先级: 5=普通 8=优先插队(队列按 priority DESC 领取, 紧急任务可插队)
const priority = ref(5)

// P1-35: 创建成功后重置整个表单(消息/目标/媒体/定时/循环全部归默认),
// 防止下次创建误带上次内容
function resetCreateForm() {
  message.value = ''
  intervalMin.value = 5
  intervalMax.value = 12
  priority.value = 5
  isLoop.value = false
  scheduledAtTs.value = null
  mediaType.value = ''
  mediaUrl.value = ''
  mediaPath.value = ''
  // R111: 协议协同字段重置已删(变量不存在)
}
const validating = ref(false)

// 发送前目标预检: 剔除无效目标(未同步/已退出的群、格式错误的用户名), 创建时失败率大降
async function precheckTargets() {
  if (!selectedTargets.value.length) return
  validating.value = true
  try {
    const res = await taskApi.validateTargets(selectedTargets.value)
    if (res.invalid?.length) {
      const invalidSet = new Set(res.invalid.map(i => i.target))
      selectedTargets.value = selectedTargets.value.filter(t => !invalidSet.has(t))
      toast({ type: 'warning', title: `已剔除 ${res.invalid.length} 个无效目标`, message: res.invalid.slice(0, 5).map(i => `${i.target}(${i.reason})`).join('；') + (res.invalid.length > 5 ? '…' : '') })
    } else {
      toast({ type: 'success', title: `全部 ${res.total} 个目标有效` })
    }
  } catch {
    toast({ type: 'error', title: '预检失败', message: '可忽略预检直接创建' })
  } finally { validating.value = false }
}
const showPreview = ref(false)

// ===== 模态框化: 本页任务列表 + 新建弹窗 =====
const showCreate = ref(false)
// R101 P1-第二刀: 打开创建弹窗即预拉全租户群(覆盖预览数据源)
watch(showCreate, (v) => { if (v) loadAllGroups() })
const tasks = ref<any[]>([])
const tasksLoading = ref(true)
const tasksError = ref('')

async function fetchTasks() {
  if (!currentAccount.value) { tasks.value = []; return }
  tasksLoading.value = true
  try {
    const res: any = await taskApi.list({ type: 'broadcast', account_id: currentAccount.value.account_id, page: 1, page_size: 20, hide_loops: 1 } as any)
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
  if (evt.type === 'task_status') { if (!evt.task_type || evt.task_type === 'broadcast') fetchTasks(); return }
  if (evt.progress && evt.task_id) {
    const t = tasks.value.find((x: any) => x.task_id === evt.task_id)
    if (t) t.progress = evt.progress
  }
})
watch(currentAccount, fetchTasks)

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
// 2026-10-21 修正: 进度按"已处理"(成功+失败)——失败也是处理结果,
// 旧口径 done/total 会让"已完成"任务停在 51%(48成功+47失败=95 全处理完却只显示一半)
function percentOf(t: any) {
  const pr = progOf(t)
  return pr.total ? Math.min(100, Math.round(((pr.done + (pr.failed || 0)) / pr.total) * 100)) : 0
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
const showTargetSelector = ref(false)
// 响应式绑定 groupStore：后台同步完成后写缓存即自动刷新选择器
// （原实现是快照赋值：页面加载后同步完成的群组永远不会出现，除非整页刷新）
// R108: 群并集 —— 取"所选执行账号"的群合并去重(按 group_id), 标注哪些号已加入。
// 顶栏切换器唯一剩余作用(给选群提供账号上下文)由此消除, 之后可删切换器。
const groups = computed<any[]>(() => {
  const byId = new Map<string, any>()
  for (const accId of execAccounts.value) {
    for (const g of (groupStore.get(accId) || []) as any[]) {
      const key = String(g.group_id || g.tg_chat_id || g.username || Math.random())
      if (byId.has(key)) {
        const ex = byId.get(key)
        if (!ex._joinedAccounts.includes(accId)) ex._joinedAccounts.push(accId)
      } else {
        byId.set(key, { ...g, _joinedAccounts: [accId] })
      }
    }
  }
  return [...byId.values()]
})
// 所选账号变化时预拉每个号的群缓存(并集的数据源)
watch(execAccounts, (ids) => { ids.forEach(id => groupStore.ensure(id)) }, { deep: true })

// 富媒体与链接按钮
const mediaType = ref('')
const mediaUrl = ref('')
const mediaPath = ref('')
const uploadingMedia = ref(false)

async function onMediaFile(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (!f) return
  uploadingMedia.value = true
  try {
    const res: any = await mediaApi.uploadMedia(f)
    mediaPath.value = res.path || res
    toast({ type: 'success', title: '媒体上传成功' })
  } catch (err: any) {
    toast({ type: 'error', title: '上传失败', message: err?.message })
  } finally {
    uploadingMedia.value = false
    ;(e.target as HTMLInputElement).value = ''
  }
}
function clearMedia() { mediaType.value = ''; mediaUrl.value = ''; mediaPath.value = '' }

// R110: 协议号协同死分支已删(view 为死常量, 该判断恒 false)。统一多号池校验。
const canSubmit = computed(() => {
  if (!message.value.trim().length) return false
  return selectedTargets.value.length > 0 && execAccounts.value.length > 0
})

const previewMessage = computed(() => {
  return message.value
    .replace(/\{\{first_name\}\}/g, '张三')
    .replace(/\{\{group_name\}\}/g, '测试群组')
    .replace(/\{\{user_id\}\}/g, '123456789')
    .replace(/\{\{now\}\}/g, new Date().toLocaleString('zh-CN'))
})

const groupsLoading = ref(false)
// 群组走缓存 store: 有缓存零请求, 只有选择器里点"同步"才调接口
const groupStore = useGroupStore()

async function fetchData() {
  if (!currentAccount.value) return
  groupsLoading.value = true
  try {
    // ensure 会把结果写入 groupStore 缓存，上方 computed 自动同步到选择器
    await groupStore.ensure(currentAccount.value.account_id)
  } catch {} finally {
    groupsLoading.value = false
  }
}

// 首次加载(空列表+加载中)视为正在同步群组, 与监控规则选择器的提示保持一致
const groupsSyncStatus = computed(() =>
  groupsLoading.value && groups.value.length === 0 ? 'syncing' : 'idle'
)

async function submit() {
  if (!canSubmit.value) return
  // R111: 协议号协同死分支已删(isProtocol 恒 false, 该段从未执行)
  submitting.value = true
  try {
    // R101 P1: 多号 —— 用勾选的账号池替代"仅当前账号"
    if (!execAccounts.value.length) {
      toast({ type: 'warning', title: '请至少勾选一个执行账号' })
      submitting.value = false
      return
    }
    const payload: any = {
      type: 'broadcast',
      account_ids: [...execAccounts.value],
      target_ids: selectedTargets.value,
      message: message.value,
      interval_min: intervalMin.value,
      interval_max: intervalMax.value,
      auto_vary: autoVary.value, // 每条消息自动变化(同义词+标点抖动)
    }
    // 富媒体 + 链接按钮(带校验, 防止"选了类型没传文件"被静默丢弃)
    const mediaSource = (mediaPath.value || mediaUrl.value.trim())
    // P2-27: 原只拦"有类型无源"——反向"有源无类型"(选了文件但没选媒体类型)会
    // 静默丢弃媒体, 用户以为发了图实际纯文本。双向都拦。
    if (!mediaType.value && mediaSource) {
      toast({ type: 'warning', title: '请选择媒体类型', message: '已提供媒体文件/链接, 但未选择类型(图片/视频/文档)' })
      return
    }
    if (mediaType.value && !mediaSource) {
      toast({ type: 'error', title: '已选择媒体类型但未提供图片/视频', message: '请填写媒体链接或上传文件, 或把类型改回「无」' })
      submitting.value = false
      return
    }
    if (mediaSource) payload.media = { type: mediaType.value, source: mediaSource }
    // 链接按钮功能已移除(实测 TG 收不到按钮消息), payload.buttons 后端兼容保留, 前端不再传
    // 转 ISO 带时区: datetime-local 是本地时间, 服务器按 UTC 解析, 必须显式换算否则晚 8 小时
    if (scheduledAtTs.value) payload.scheduled_at = new Date(scheduledAtTs.value).toISOString()
    // 循环发送(合并原"循环群发"独立页): 每轮结束按间隔自动续跑, 混淆每轮重随机
    if (isLoop.value) {
      payload.is_loop = true
      payload.loop_interval_minutes = Math.max(1, Number(loopIntervalMin.value) || 60)
    }
    await taskApi.create(payload)
    // 创建后留在本页: 关闭模态框, 列表与实时面板立即出现新任务
    showCreate.value = false
    fetchTasks()
    // P1-35: 重置整个表单——原实现不重置, 下次创建带着上次的媒体/定时/循环设置,
    // 极易把上次的消息/媒体误发到新任务。
    resetCreateForm()
    // UI升级: 成功反馈(脉冲面板交给 success-pop, 这里补确认提示)
    toast({ type: 'success', title: '任务创建成功', message: '实时进度见下方面板' })
  } catch (e: any) {
    toast({ type: 'error', title: '创建失败', message: e.message })
  } finally {
    submitting.value = false
  }
}

onMounted(() => { fetchData(); fetchTasks() })
// 侧边栏切换账号: 重新拉取群组缓存
watch(currentAccount, () => { fetchData() })
</script>
