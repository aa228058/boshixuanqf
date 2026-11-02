<template>
  <main class="space-y-4">
    <Breadcrumb :items="[{ path: '/admin/dashboard', label: '仪表盘' }, { path: '', label: '卡密管理' }]" />
    <div class="flex items-center gap-2 overflow-x-auto pb-1">
      <t-button theme="primary" size="sm" class="shrink-0" @click="showGenerate = true"> 生成卡密
      </t-button>
      <t-select v-model="statusFilter" :options="[
        { label: '全部状态', value: '' },
        { label: '未使用', value: 'unused' },
        { label: '已使用', value: 'used' }
      ]" class="w-24 shrink-0" />
      <t-button theme="primary" size="sm" variant="outline" class="shrink-0" @click="exportCodes">
        导出
      </t-button>
      <t-button v-if="selectedIds.length" size="sm" variant="outline" class="shrink-0" @click="copySelected">
        复制选中({{ selectedIds.length }})
      </t-button>
    </div>

    <!-- 桌面端表格 -->
    <div class="hidden sm:block glass rounded-[var(--t-r-md)] overflow-hidden">
      <t-table
        row-key="code_id"
        :columns="tdColumns"
        :data="codes"
        hover
        :pagination="{ current: page, pageSize: 20, total: total, onChange: (p: any) => page = p.current }"
      >
        <template #select="{ row }">
          <input type="checkbox" :value="row.code_id" v-model="selectedIds" class="accent-cyan-500" @click.stop />
        </template>
        <template #used_by="{ row }">
          <span v-if="row.used_by_tenant_name" class="text-sm">{{ row.used_by_tenant_name }}</span>
          <span v-else-if="row.used_by" class="text-sm text-text-secondary">{{ row.used_by.slice(0, 8) }}...</span>
          <span v-else class="text-text-secondary">-</span>
        </template>
        <template #code="{ row }">
          <span class="font-mono text-sm">{{ row.code }}</span>
        </template>
        <template #status="{ row }">
          <span class="text-xs px-2 py-0.5 rounded-full" :class="codeStatusClass(row.status as string)">
            {{ row.status === 'unused' ? '未使用' : row.status === 'used' ? '已使用' : row.status === 'expired' ? '已过期' : '已作废' }}
          </span>
        </template>
        <template #actions="{ row }">
          <button @click="copyCode(row.code as string)" class="text-xs text-cyan hover:underline px-2 py-1 rounded hover:bg-cyan/5">复制</button>
          <button @click="viewCode(row as any)" class="text-xs text-cyan hover:underline px-2 py-1 rounded hover:bg-cyan/5">查看</button>
          <button v-if="row.status === 'unused'" @click="revokeCode(row as any)" class="text-xs text-orange-400 hover:underline px-2 py-1 rounded hover:bg-orange-400/5">作废</button>
          <button @click="delCode(row as any)" class="text-xs text-rose hover:underline px-2 py-1 rounded hover:bg-rose/5">删除</button>
        </template>
      </t-table>
    </div>
    <!-- 移动端卡片 -->
    <div class="sm:hidden space-y-3">
      <div
        v-for="c in codes"
        :key="c.code_id"
        class="glass rounded-[var(--t-r-md)] p-4 space-y-2 lift"
      >
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div class="flex items-center gap-2 min-w-0">
            <input type="checkbox" :value="c.code_id" v-model="selectedIds" class="accent-cyan-500 shrink-0" />
            <span class="font-mono text-sm truncate">{{ c.code }}</span>
          </div>
          <span class="text-xs px-2 py-0.5 rounded-full shrink-0" :class="codeStatusClass(c.status)">
            {{ c.status === 'unused' ? '未使用' : c.status === 'used' ? '已使用' : c.status === 'expired' ? '已过期' : '已作废' }}
          </span>
        </div>
        <div class="flex items-center gap-3 text-xs text-text-secondary">
          <span>{{ planLabel(c.plan_id) }}</span>
          <span v-if="c.used_by_tenant_name">使用租户: {{ c.used_by_tenant_name }}</span>
          <span v-else-if="c.used_by">使用者: {{ c.used_by.slice(0, 8) }}...</span>
        </div>
        <div class="flex items-center justify-between text-xs text-text-secondary">
          <span>{{ new Date(c.created_at).toLocaleDateString('zh-CN') }}</span>
          <div>
            <button @click="viewCode(c)" class="text-cyan hover:underline px-2 py-1">查看</button>
            <button v-if="c.status === 'unused'" @click="revokeCode(c)" class="text-orange-400 hover:underline px-2 py-1">作废</button>
            <button @click="delCode(c)" class="text-rose hover:underline px-2 py-1">删除</button>
          </div>
        </div>
      </div>
      <EmptyState v-if="!codes.length" icon="Key" title="暂无卡密" description="点击上方按钮生成卡密" />
    </div>

    <Modal v-model="showGenerate" title="生成卡密">
      <div class="space-y-4">
        <div>
          <label for="code-plan" class="block text-sm text-text-secondary mb-1.5">套餐</label>
          <t-select v-model="generateForm.plan_id" :options="planOptions" />
        </div>
        <div>
          <label for="code-timing" class="block text-sm text-text-secondary mb-1.5">生效方式</label>
          <t-select v-model="generateForm.timing_type" :options="[
            { label: '立即生效（生成后开始计时）', value: 'immediate' },
            { label: '使用时生效（激活后开始计时）', value: 'on_use' }
          ]" />
        </div>
        <div>
          <label for="code-count" class="block text-sm text-text-secondary mb-1.5">数量</label>
          <input v-model.number="generateForm.count" type="number" min="1" max="100" class="w-full px-2.5 py-1.5 rounded-lg bg-bg-card border border-border focus:border-cyan focus:outline-none text-sm" aria-label="数量" />
        </div>
        <div>
          <label for="code-days" class="block text-sm text-text-secondary mb-1.5">有效期（天，可选）</label>
          <input v-model.number="generateForm.expires_days" type="number" min="1" class="w-full px-2.5 py-1.5 rounded-lg bg-bg-card border border-border focus:border-cyan focus:outline-none text-sm" aria-label="数量" />
        </div>
      </div>
      <template #footer>
        <button @click="showGenerate = false" class="px-4 py-2 rounded-lg text-sm text-text-secondary">取消</button>
        <t-button theme="primary" @click="doGenerate">生成</t-button>
      </template>
    </Modal>

    <Modal v-model="showView" title="卡密详情">
      <div v-if="viewingCode" class="space-y-3 text-sm">
        <div class="flex justify-between gap-4"><span class="text-text-secondary shrink-0">卡密</span><span class="font-mono break-all text-right">{{ viewingCode.code }}</span></div>
        <div class="flex justify-between"><span class="text-text-secondary">套餐</span><span>{{ planLabel(viewingCode.plan_id) }}</span></div>
        <div class="flex justify-between"><span class="text-text-secondary">状态</span><span>{{ viewingCode.status === 'unused' ? '未使用' : viewingCode.status === 'used' ? '已使用' : viewingCode.status === 'expired' ? '已过期' : '已作废' }}</span></div>
        <div class="flex justify-between"><span class="text-text-secondary">使用租户</span><span>{{ viewingCode.used_by_tenant_name || '-' }}</span></div>
        <div class="flex justify-between"><span class="text-text-secondary">使用时间</span><span>{{ viewingCode.used_at ? new Date(viewingCode.used_at).toLocaleString('zh-CN') : '-' }}</span></div>
        <div class="flex justify-between"><span class="text-text-secondary">过期时间</span><span>{{ viewingCode.expires_at ? new Date(viewingCode.expires_at).toLocaleString('zh-CN') : '-' }}</span></div>
        <div class="flex justify-between"><span class="text-text-secondary">生效方式</span><span>{{ viewingCode.timing_type === 'immediate' ? '立即生效' : '使用时生效' }}</span></div>
        <div v-if="viewingCode.notes" class="flex justify-between gap-4"><span class="text-text-secondary shrink-0">备注</span><span class="text-right">{{ viewingCode.notes }}</span></div>
      </div>
      <template #footer>
        <button @click="showView = false" class="px-4 py-2 rounded-lg text-sm text-text-secondary">关闭</button>
      </template>
    </Modal>
  </main>

  <!-- 批量操作吸底栏：选中卡密时弹出 -->
  <Teleport to="body">
    <Transition name="batch-bar">
      <div v-if="selectedIds.length" class="fixed bottom-5 left-1/2 -translate-x-1/2 z-50 flex items-center gap-2 pl-4 pr-2 py-1.5 rounded-full glass border border-border elev-pop">
        <span class="text-xs text-text-muted whitespace-nowrap">已选 {{ selectedIds.length }} 张</span>
        <button @click="batchRevoke" class="px-2.5 py-1 rounded-full text-xs bg-orange-400/15 text-orange-400 whitespace-nowrap hover:bg-orange-400/25 transition-colors">批量作废</button>
        <button @click="batchDelete" class="px-2.5 py-1 rounded-full text-xs bg-rose/15 text-rose whitespace-nowrap hover:bg-rose/25 transition-colors">批量删除</button>
        <button @click="selectedIds = []" class="w-6 h-6 rounded-full text-text-secondary hover:text-text-primary hover:bg-milk-01 transition-colors text-base leading-none">×</button>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { Plus, Key } from 'lucide-vue-next'
import { adminApiMod } from '@/api/admin'
import { toast } from '@/utils/toast'
import Modal from '@/components/ui/Modal.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Breadcrumb from '@/components/ui/Breadcrumb.vue'
import type { ActivationCode, Plan } from '@/types'
// R115: 恢复 codeStatusClass(复核脚本误判, 正文实际有使用)
import { codeStatusClass, planLabel } from '@/utils/format'
import { ask } from '@/utils/confirm'

const codes = ref<ActivationCode[]>([])

// 模板 17/48 行的"复制"按钮此前调用不存在的 copySelected/copyCode（点击无反应）。
function copyCode(code: string) {
  navigator.clipboard?.writeText(code).then(
    () => toast({ type: 'success', title: '已复制', message: code }),
    () => toast({ type: 'error', title: '复制失败' })
  )
}
function copySelected() {
  const text = codes.value.filter(c => selectedIds.value.includes(c.code_id)).map(c => c.code).join('\n')
  if (!text) return
  navigator.clipboard?.writeText(text).then(
    () => toast({ type: 'success', title: '已复制', message: `${selectedIds.value.length} 条卡密` }),
    () => toast({ type: 'error', title: '复制失败' })
  )
}
const plans = ref<Plan[]>([])
const statusFilter = ref('')
const page = ref(1)
const total = ref(0)
const showGenerate = ref(false)
const generateForm = ref({ plan_id: 'basic', count: 10, timing_type: 'immediate', expires_days: undefined as number | undefined })

const planOptions = computed(() => plans.value.map(p => ({ label: p.name, value: p.plan_id })))

const columns = [
  { key: 'select', title: '', class: 'w-10' },
  { key: 'code', title: '卡密' },
  { key: 'plan_id', title: '套餐', format: (v: any) => planLabel(v) },
  { key: 'status', title: '状态' },
  { key: 'used_by', title: '使用租户' },
  { key: 'created_at', title: '创建时间', format: (v: any) => new Date(v).toLocaleDateString('zh-CN') },
  { key: 'actions', title: '操作', class: 'w-36' },
]

// DataTable → TDesign 表格列映射(key 字段对齐插槽名)
const tdColumns = computed(() => columns.map(c => ({ colKey: c.key, title: c.title, width: c.class?.includes('w-10') ? 48 : (c.class?.includes('w-36') ? 160 : undefined) })))

// P0-22 审计修复: 原 statusFilter 无任何 watch——下拉切换不触发查询, 列表纹丝不动。
// 切换即重置到第 1 页并重新拉取(状态筛选走后端, 页码不重置会看不到结果)。
watch(statusFilter, () => { page.value = 1; fetchCodes() })

async function fetchCodes() {
  try {
    // 状态筛选走后端（此前前端只过滤当前页 20 条，"未使用"可能显示为空）
    const res = await adminApiMod.listCodes({ page: page.value, page_size: 20, ...(statusFilter.value ? { status: statusFilter.value } : {}) })
    codes.value = res.list || []
    total.value = res.total || 0
  } catch (e: unknown) {
    toast({ type: 'error', title: '获取卡密失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

async function doGenerate() {
  try {
    await adminApiMod.generateCodes(generateForm.value)
    showGenerate.value = false
    generateForm.value = { plan_id: 'basic', count: 10, timing_type: 'immediate', expires_days: undefined }
    await fetchCodes()
    toast({ type: 'success', title: '卡密已生成' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '生成失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

async function revokeCode(c: ActivationCode) {
  try {
    await adminApiMod.revokeCode(c.code_id)
    await fetchCodes()
    toast({ type: 'success', title: '卡密已作废' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '作废失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

// 导出全部未使用卡密：走后端 /admin/codes/export（此前只导出当前页过滤结果）
async function exportCodes() {
  try {
    const text = await adminApiMod.exportCodes()
    if (!text) {
      toast({ type: 'info', title: '没有可导出的卡密' })
      return
    }
    const blob = new Blob([text], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `codes_export_${new Date().toISOString().slice(0, 10)}.txt`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e: any) {
    toast({ type: 'error', title: '导出失败', message: e?.message || '请稍后重试' })
  }
}

const selectedIds = ref<string[]>([])

async function batchRevoke() {
  if (!await ask(`批量作废 ${selectedIds.value.length} 张卡密？`)) return
  try {
    await adminApiMod.batchRevokeCodes({ ids: selectedIds.value })
    selectedIds.value = []
    await fetchCodes()
    toast({ type: 'success', title: '已批量作废' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '批量作废失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

async function batchDelete() {
  if (!await ask(`批量删除 ${selectedIds.value.length} 张卡密？不可恢复！`)) return
  try {
    await adminApiMod.batchDeleteCodes({ ids: selectedIds.value })
    selectedIds.value = []
    await fetchCodes()
    toast({ type: 'success', title: '已批量删除' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '批量删除失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

async function delCode(c: ActivationCode) {
  if (!await ask(`确认删除卡密 ${c.code}？不可恢复！`)) return
  try {
    await adminApiMod.batchDeleteCodes({ ids: [c.code_id] })
    await fetchCodes()
    toast({ type: 'success', title: '卡密已删除' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '删除失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

const viewingCode = ref<ActivationCode | null>(null)
const showView = ref(false)
function viewCode(c: ActivationCode) {
  viewingCode.value = c
  showView.value = true
}

onMounted(async () => {
  await fetchCodes()
  try {
    const res = await adminApiMod.listPlans()
    plans.value = res.list || []
  } catch (e: unknown) {
    toast({ type: 'error', title: '获取套餐失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
})
</script>

<style scoped>
.batch-bar-enter-active,
.batch-bar-leave-active {
  transition: all 0.25s ease;
}
.batch-bar-enter-from,
.batch-bar-leave-to {
  opacity: 0;
  transform: translate(-50%, 12px);
}
</style>
