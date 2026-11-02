<template>
  <div class="p-4 sm:p-6 max-w-4xl mx-auto">
    <h1 class="text-title font-extrabold tracking-tight text-text-primary truncate">消息监控</h1>
    <p class="text-sm text-text-muted mt-1 mb-6">
      <!-- R109: 账号下拉(源群组选择/规则展示跟随), 不依赖顶栏切换器 -->
      <span>账号:
        <select v-model="monAccId" class="input-box h-8 px-2 text-xs rounded-lg ml-1 align-middle">
          <option v-for="a in accountStore.accounts" :key="a.account_id" :value="a.account_id">{{ accName(a) }}</option>
        </select>
      </span>
    </p>

    <div class="glass rounded-[var(--t-r-md)] p-4 sm:p-6 space-y-4">
      <!-- 监控状态(自动模式: 规则即开关) -->
      <div class="p-4 rounded-lg border border-border bg-bg-card">
        <div class="flex items-center gap-3">
          <div class="w-3 h-3 rounded-full shrink-0" :class="anyRuleActive ? 'bg-emerald animate-pulse' : 'bg-text-muted'" />
          <span class="text-sm font-medium whitespace-nowrap">{{ anyRuleActive ? '监控运行中' : '监控已停止' }}</span>
        </div>
        <p class="text-xs text-text-muted mt-1.5 pl-6">启用任意规则即自动开始，全部停用自动停止</p>
      </div>

      <!-- 规则列表 -->
      <div>
        <div class="flex items-center justify-between mb-4">
          <h3 class="text-sm font-medium">监控规则</h3>
          <!-- P0-28: 走 openAddRule 先清空 editingRule, 防编辑残留变覆盖 -->
          <button @click="openAddRule" class="glow-btn px-3 py-1.5 rounded-lg text-xs">添加规则</button>
        </div>
        <div class="space-y-2">
          <EmptyState v-if="rules.length === 0" icon="Eye" title="暂无监控规则" description="添加关键词规则，命中消息的群会被自动监控" class="py-8" />
          <div v-for="rule in rules" :key="rule.rule_id"
               class="p-4 rounded-[18px] flex items-center justify-between"
               style="background: var(--t-surface-1); border: 1px solid var(--t-line);">
            <div class="min-w-0">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="text-sm font-extrabold">{{ rule.name }}</span>
                <!-- R126 示例2 badge 语言: 圆点+文字胶囊 -->
                <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-bold"
                      :class="rule.is_active ? 'bg-emerald/10 text-emerald' : 'bg-text-muted/15 text-text-muted'">
                  <i class="w-1.5 h-1.5 rounded-full bg-current"></i>{{ rule.is_active ? '启用' : '停用' }}
                </span>
                <!-- 2026-09-22 隔离改造: 显示规则归属账号 -->
                <span v-if="rule.account_id" class="px-1.5 py-0.5 rounded-full text-[10px] font-semibold" style="background: var(--t-brand-dim); color: var(--t-brand);">
                  账号: {{ accName(accountStore.accounts.find(a => a.account_id === rule.account_id)) }}
                </span>
                <span v-else class="px-1.5 py-0.5 rounded-full text-[10px] font-semibold bg-text-muted/10 text-text-muted">全部账号</span>
              </div>
              <div class="text-xs mt-1" style="color: var(--t-text-3);">
                匹配: {{ matchTypeLabel(rule.match_type) }} | 动作: {{ actionLabel(rule.type) }}
                <span v-if="rule.keywords"> | 关键词: {{ rule.keywords }}</span>
              </div>
            </div>
            <div class="flex items-center gap-1">
              <t-switch :model-value="rule.is_active" @update:model-value="(v: boolean) => toggleRule(rule.rule_id, v)" />
              <button @click="editRule(rule)" class="p-1.5 rounded hover:bg-bg-secondary transition-colors" title="编辑">
                <Edit3 class="w-4 h-4 text-text-muted" />
              </button>
              <button @click="deleteRule(rule.rule_id)" class="p-1.5 rounded hover:bg-rose/10 transition-colors" title="删除">
                <Trash2 class="w-4 h-4 text-rose" />
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 添加/编辑规则弹窗: 统一全站 Modal(移动端底部抽屉) -->
    <Modal v-model="showAddRule" :title="editingRule ? '编辑规则' : '添加规则'" class="max-w-lg">
      <div class="space-y-4">
          <!-- R112 表单分组① -->
          <div class="flex items-center gap-2 -mt-1"><span class="text-[10.5px] font-semibold text-text-muted uppercase tracking-wider">基础信息</span><span class="flex-1 h-px bg-border/60"></span></div>
          <div>
            <label class="block text-sm text-text-secondary mb-1">规则名称</label>
            <input v-model="ruleForm.name" class="w-full px-3 py-2 rounded-lg glow-input" />
          </div>
          <div>
            <label class="block text-sm text-text-secondary mb-1">源群组（监控来源）</label>
            <div @click="showSourceSelector = true"
                 class="flex items-center gap-2 p-3 rounded-lg border border-border cursor-pointer hover:border-cyan/30 transition-colors">
              <Users class="w-4 h-4 text-text-muted" />
              <span v-if="!selectedSourceGroups.length" class="text-text-muted text-sm">点击选择源群组</span>
              <span v-else class="text-sm">已选 {{ selectedSourceGroups.length }} 个群组</span>
            </div>
            <div v-if="selectedSourceGroups.length" class="flex flex-wrap gap-1.5 mt-2">
              <span v-for="g in selectedSourceGroups" :key="g.group_id"
                    class="px-2 py-0.5 rounded-full text-xs bg-cyan-glow/30 text-cyan border border-cyan/20">{{ g.title }}</span>
            </div>
          </div>
          <!-- R112 表单分组② -->
          <div class="flex items-center gap-2 pt-1"><span class="text-[10.5px] font-semibold text-text-muted uppercase tracking-wider">匹配条件与动作</span><span class="flex-1 h-px bg-border/60"></span></div>
          <div>
            <label class="block text-sm text-text-secondary mb-1">匹配类型</label>
            <select v-model="ruleForm.match_type" class="w-full px-3 py-2 rounded-lg glow-input">
              <option value="contains">包含关键词</option>
              <option value="exact">完全匹配</option>
              <option value="regex">正则匹配</option>
              <option value="from_user">来自指定用户</option>
            </select>
          </div>
          <div>
            <label class="block text-sm text-text-secondary mb-1">关键词（每行一个）</label>
            <textarea v-model="ruleForm.keywordsInput" rows="3" class="w-full px-3 py-2 rounded-lg glow-input resize-none" />
          </div>
          <div>
            <label class="block text-sm text-text-secondary mb-1">动作</label>
            <select v-model="ruleForm.type" class="w-full px-3 py-2 rounded-lg glow-input">
              <option value="reply">自动回复</option>
              <option value="forward">转发消息</option>
            </select>
          </div>
          <div v-if="ruleForm.type === 'reply'">
            <label class="block text-sm text-text-secondary mb-1">回复内容</label>
            <textarea v-model="ruleForm.reply_content" rows="2" class="w-full px-3 py-2 rounded-lg glow-input resize-none" />
          </div>
          <div v-if="ruleForm.type === 'forward'">
            <label class="block text-sm text-text-secondary mb-1">目标群组（转发目的地）</label>
            <div @click="showTargetSelector = true"
                 class="flex items-center gap-2 p-3 rounded-lg border border-border cursor-pointer hover:border-cyan/30 transition-colors">
              <Users class="w-4 h-4 text-text-muted" />
              <span v-if="!selectedTargetGroups.length" class="text-text-muted text-sm">点击选择目标群组</span>
              <span v-else class="text-sm">已选 {{ selectedTargetGroups.length }} 个群组</span>
            </div>
            <div v-if="selectedTargetGroups.length" class="flex flex-wrap gap-1.5 mt-2">
              <span v-for="g in selectedTargetGroups" :key="g.group_id"
                    class="px-2 py-0.5 rounded-full text-xs bg-emerald-glow/30 text-emerald border border-emerald/20">{{ g.title }}</span>
            </div>
          </div>
        </div>
        <div class="flex justify-end gap-3 mt-6">
          <button @click="showAddRule = false" class="px-4 py-2 rounded-lg text-sm text-text-secondary">取消</button>
          <button @click="saveRule" class="px-4 py-2 rounded-lg text-sm font-medium glow-btn">保存</button>
        </div>
    </Modal>

    <!-- 群组选择器 -->
    <GroupSelector
      v-model="showSourceSelector"
      :groups="groups"
      :account-id="monAccId"
      mode="multiple"
      @select="(gs: Group[]) => selectedSourceGroups = gs"
      @refresh="fetchGroups"
    />
    <GroupSelector
      v-model="showTargetSelector"
      :groups="groups"
      :account-id="monAccId"
      mode="multiple"
      @select="(gs: Group[]) => selectedTargetGroups = gs"
      @refresh="fetchGroups"
    />
  </div>
  <LiveTaskFeed v-if="monAccId" :account-id="monAccId" task-type="broadcast" class="mt-6" />

</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAccountStore } from '@/stores/account'
import { useAuthStore } from '@/stores/auth'
import { monitorApi } from '@/api/monitor'
import { groupApi } from '@/api/group'
import EmptyState from '@/components/ui/EmptyState.vue'
import Modal from '@/components/ui/Modal.vue'
import { toast } from '@/utils/toast'
import { Eye, Power, Edit3, Trash2, Users } from 'lucide-vue-next'
import GroupSelector from '@/components/selectors/GroupSelector.vue'
import type { Group } from '@/types'
import LiveTaskFeed from '@/components/LiveTaskFeed.vue'
import { ask } from '@/utils/confirm'

const route = useRoute()
const router = useRouter()
const accountStore = useAccountStore()
const authStore = useAuthStore()
const currentAccount = computed(() => accountStore.currentAccount)
const accName = (a: any) => a?.first_name || a?.remark || a?.phone || (a?.account_id || '').slice(0, 8)
// R109: 监控按账号管理, 下拉选择管理哪个号
const monAccId = ref('')
watch(currentAccount, (acc: any) => { if (acc?.account_id && !monAccId.value) monAccId.value = acc.account_id }, { immediate: true })
watch(monAccId, () => { loadGroups(); fetchRules() })
const groups = ref<Group[]>([])
const showSourceSelector = ref(false)
const showTargetSelector = ref(false)
const selectedSourceGroups = ref<Group[]>([])
const selectedTargetGroups = ref<Group[]>([])

async function fetchGroups() {
  try {
    const res = await groupApi.list(monAccId.value)
    const raw = res as any
    groups.value = Array.isArray(raw.list) ? raw.list : []
  } catch (e) { /* ignore */ }
}

const rules = ref<any[]>([])
const isMonitoring = ref(false) // 精读注: 预留字段, 当前由 rules.length+isAccountOnline 推导运行态
const anyRuleActive = computed(() => rules.value.some(r => r.is_active))
const showAddRule = ref(false)
const editingRule = ref<any>(null)
const ruleForm = ref({
  name: '',
  match_type: 'contains',
  keywordsInput: '',
  type: 'reply',
  reply_content: '',
})

async function fetchRules() {
  try {
    const res = await monitorApi.listRules()
    rules.value = res.list || []
  } catch (e: any) {
    toast({ type: 'error', title: '加载规则失败', message: e?.message || '请稍后重试' })
  }
}


async function toggleRule(id: string, val: boolean) {
  try {
    await monitorApi.toggleRule(id, { is_active: val })
    fetchRules()
  } catch {
    toast({ type: 'error', title: '操作失败', message: '规则状态更新失败，请重试' })
    fetchRules() // 回滚本地乐观变更
  }
}

async function deleteRule(id: string) {
  if (!await ask('确定删除此规则吗？')) return
  try {
    await monitorApi.deleteRule(id)
    fetchRules()
  } catch {
    toast({ type: 'error', title: '删除失败', message: '请稍后重试' })
  }
}

function editRule(rule: any) {
  editingRule.value = rule
  ruleForm.value = {
    name: rule.name,
    match_type: rule.match_type,
    keywordsInput: (rule.keywords || []).join('\n'),
    type: rule.type || rule.action,
    reply_content: rule.reply_content || '',
  }
  // 恢复选中的源群组和目标群组
  selectedSourceGroups.value = (rule.source_chat_ids || []).map((id: string) => {
    const found = groups.value.find(g => g.group_id === id || String(g.tg_chat_id) === id)
    return found || { group_id: id, title: id, tg_chat_id: id }
  })
  selectedTargetGroups.value = (rule.target_chat_ids || []).map((id: string) => {
    const found = groups.value.find(g => g.group_id === id || String(g.tg_chat_id) === id)
    return found || { group_id: id, title: id, tg_chat_id: id }
  })
  showAddRule.value = true
}

// P0-28: 打开添加弹窗前必须清空 editingRule(见模板注释)
function openAddRule() {
  editingRule.value = null
  showAddRule.value = true
}

async function saveRule() {
  const data = {
    name: ruleForm.value.name,
    type: ruleForm.value.type as "reply" | "forward",
    match_type: ruleForm.value.match_type as "contains" | "exact" | "regex" | "from_user",
    keywords: ruleForm.value.keywordsInput.split('\n').map(k => k.trim()).filter(Boolean),
    reply_content: ruleForm.value.reply_content,
    source_chat_ids: selectedSourceGroups.value.map(g => String(g.tg_chat_id || g.group_id)),
    target_chat_ids: ruleForm.value.type === 'forward' ? selectedTargetGroups.value.map(g => String(g.tg_chat_id || g.group_id)) : [],
    enabled: true,
    // R109: 规则归属下拉所选账号(原绑 currentAccount=全局切换器, 已删除)
    account_id: monAccId.value || '',
  }
  try {
    if (editingRule.value) {
      await monitorApi.updateRule(editingRule.value.rule_id, data)
    } else {
      await monitorApi.createRule(data)
    }
    // 先记录是否编辑态(editingRule 随后会被清空), 保证提示文案准确
    const wasEditing = !!editingRule.value
    showAddRule.value = false
    editingRule.value = null
    ruleForm.value = { name: '', match_type: 'contains', keywordsInput: '', type: 'reply', reply_content: '' }
    selectedSourceGroups.value = []
    selectedTargetGroups.value = []
    toast({ type: 'success', title: wasEditing ? '规则已更新' : '监控规则已创建', message: '规则即时生效' })
    fetchRules()
  } catch (e: any) {
    toast({ type: 'error', title: '保存失败', message: e?.message || '请稍后重试' })
  }
}

function matchTypeLabel(t: string) {
  const map: Record<string, string> = { contains: '包含', exact: '完全匹配', regex: '正则', from_user: '指定用户' }
  return map[t] || t
}

function actionLabel(a: string) {
  const map: Record<string, string> = { reply: '自动回复', forward: '转发', alert: '告警' }
  return map[a] || a
}

onMounted(() => {
  if (!authStore.tenant?.plan_features?.monitor) {
    toast({ type: 'warning', title: '套餐限制', message: '当前套餐不支持消息监控，请联系客服升级' })
    router.push('/dashboard')
    return
  }
  fetchRules()
})
</script>
