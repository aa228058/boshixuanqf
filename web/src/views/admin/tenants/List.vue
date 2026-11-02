<template>
  <main class="space-y-4">
    <Breadcrumb :items="[{ path: '/admin/dashboard', label: '仪表盘' }, { path: '', label: '租户管理' }]" />
    <div class="flex items-center gap-2 overflow-x-auto pb-1">
      <t-button theme="primary" size="sm" class="shrink-0" @click="showCreate = true"> 创建租户
      </t-button>
      <t-select v-model="statusFilter" :options="[
        { label: '全部状态', value: '' },
        { label: '正常', value: 'active' },
        { label: '未激活', value: 'inactive' },
        { label: '已停用', value: 'suspended' }
      ]" class="w-24 shrink-0" />
      <input v-model="search" placeholder="搜索租户..." class="px-2.5 py-1.5 rounded-lg bg-bg-card border border-border focus:border-cyan focus:outline-none text-sm flex-1 min-w-[100px]" aria-label="租户..." />
    </div>

    <!-- 桌面端表格 -->
    <div class="hidden sm:block glass rounded-[var(--t-r-md)] overflow-hidden">
      <t-table
        row-key="tenant_id"
        :columns="tdColumns"
        :data="filteredTenants"
        hover
        :pagination="{ current: page, pageSize: pageSize, total: total, onChange: (p: any) => page = p.current }"
      >
        <template #status="{ row }">
          <span class="inline-flex items-center gap-1.5 text-xs px-2 py-0.5 rounded-full font-bold whitespace-nowrap" :class="tenantStatusClass(row.status as string)">
            <i class="w-1.5 h-1.5 rounded-full bg-current opacity-80"></i>{{ row.status === 'active' ? '正常' : row.status === 'suspended' ? '已停用' : '未激活' }}
          </span>
        </template>
        <template #plan_id="{ row }">
          <span class="text-xs">{{ planLabel(row.plan_id as string) }}</span>
        </template>
        <template #actions="{ row }">
          <div class="flex items-center gap-1">
            <t-tooltip content="详情" position="top">
              <button @click="viewDetail(row as any)" class="p-1.5 rounded hover:bg-bg-hover" aria-label="详情">
                <Eye class="w-4 h-4 text-text-muted" />
              </button>
            </t-tooltip>
            <t-tooltip content="重置Key" position="top">
              <button @click="resetKey(row as any)" class="p-1.5 rounded hover:bg-bg-hover" aria-label="重置Key">
                <KeyRound class="w-4 h-4 text-text-muted" />
              </button>
            </t-tooltip>
            <t-tooltip content="删除" position="top">
              <button @click="confirmDelete(row as any)" class="p-1.5 rounded hover:bg-rose/10" aria-label="删除">
                <Trash2 class="w-4 h-4 text-text-muted hover:text-rose" />
              </button>
            </t-tooltip>
          </div>
        </template>
      </t-table>
    </div>
    <!-- 移动端卡片 -->
    <div class="sm:hidden space-y-3">
      <div
        v-for="t in filteredTenants"
        :key="t.tenant_id"
        class="glass rounded-[var(--t-r-md)] p-4 space-y-2 lift"
      >
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="font-medium text-sm truncate">{{ t.name }}</span>
          <span class="text-xs px-2 py-0.5 rounded-full shrink-0" :class="tenantStatusClass(t.status)">
            {{ t.status === 'active' ? '正常' : t.status === 'suspended' ? '已停用' : '未激活' }}
          </span>
        </div>
        <div class="flex items-center gap-3 text-xs text-text-secondary">
          <span>{{ planLabel(t.plan_id) }}</span>
          <span>{{ t.max_accounts }} 账号上限</span>
        </div>
        <div class="flex items-center justify-between text-xs text-text-secondary">
          <span>{{ new Date(t.created_at).toLocaleDateString('zh-CN') }}</span>
          <div class="flex items-center gap-1">
            <t-tooltip content="详情" position="top">
              <button @click="viewDetail(t)" class="p-2 rounded-lg hover:bg-bg-hover min-w-[36px] min-h-[36px] flex items-center justify-center" aria-label="详情">
                <Eye class="w-4 h-4 text-text-muted" />
              </button>
            </t-tooltip>
            <t-tooltip content="重置Key" position="top">
              <button @click="resetKey(t)" class="p-2 rounded-lg hover:bg-bg-hover min-w-[36px] min-h-[36px] flex items-center justify-center" aria-label="重置Key">
                <KeyRound class="w-4 h-4 text-text-muted" />
              </button>
            </t-tooltip>
            <t-tooltip content="删除" position="top">
              <button @click="confirmDelete(t)" class="p-2 rounded-lg hover:bg-rose/10 min-w-[36px] min-h-[36px] flex items-center justify-center" aria-label="删除">
                <Trash2 class="w-4 h-4 text-text-muted hover:text-rose" />
              </button>
            </t-tooltip>
          </div>
        </div>
      </div>
      <EmptyState v-if="!filteredTenants.length" icon="Users" title="暂无租户" description="点击上方按钮创建租户" />
    </div>

    <!-- 创建租户弹窗 -->
    <Modal v-model="showCreate" title="创建租户">
      <div class="space-y-4">
        <div>
          <label class="block text-sm text-text-secondary mb-1.5">租户名称</label>
          <input v-model="createForm.name" placeholder="请输入租户名称" class="w-full px-2.5 py-1.5 rounded-lg bg-bg-card border border-border focus:border-cyan focus:outline-none text-sm" aria-label="请输入租户名称" />
        </div>
        <div>
          <label class="block text-sm text-text-secondary mb-1.5">登录用户名</label>
          <input v-model="createForm.username" placeholder="租户登录用的用户名" class="w-full px-2.5 py-1.5 rounded-lg bg-bg-card border border-border focus:border-cyan focus:outline-none text-sm" aria-label="租户登录用的用户名" />
        </div>
        <div>
          <label class="block text-sm text-text-secondary mb-1.5">登录密码</label>
          <input v-model="createForm.password" type="password" placeholder="租户登录用的密码" class="w-full px-2.5 py-1.5 rounded-lg bg-bg-card border border-border focus:border-cyan focus:outline-none text-sm" aria-label="租户登录用的密码" />
        </div>
        <div>
          <label class="block text-sm text-text-secondary mb-1.5">套餐</label>
          <t-select v-model="createForm.plan_id" :options="planOptions" />
        </div>
      </div>
      <template #footer>
        <button @click="showCreate = false" class="px-4 py-2 rounded-lg text-sm text-text-secondary">取消</button>
        <t-button theme="primary" @click="doCreate">创建</t-button>
      </template>
    </Modal>

    <ConfirmDialog ref="deleteConfirm" title="删除租户" message="确定要删除这个租户吗？" detail="删除后所有数据将不可恢复。" confirm-text="确认删除" danger @confirm="doDelete" />
  </main>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import { useRouter } from 'vue-router'
import { Plus, Eye, KeyRound, Trash2 } from 'lucide-vue-next'
import { adminApiMod } from '@/api/admin'
import Breadcrumb from '@/components/ui/Breadcrumb.vue'
import Modal from '@/components/ui/Modal.vue'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import type { Tenant, Plan } from '@/types'
// R115: tenantStatusLabel 死导入已删(正文零使用, 状态色由 tenantStatusClass 承担)
import { planLabel, tenantStatusClass } from '@/utils/format'
import { toast } from '@/utils/toast'

const router = useRouter()
const tenants = ref<Tenant[]>([])
const plans = ref<Plan[]>([])
const search = ref('')
const statusFilter = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const showCreate = ref(false)
const createForm = ref({ name: '', username: '', password: '', plan_id: 'free' })

const planOptions = computed(() => plans.value.map(p => ({ label: p.name, value: p.plan_id })))

// DataTable → TDesign 表格列映射
const tdColumns = computed(() => columns.map(c => ({ colKey: c.key, title: c.title })))

const columns = [
  { key: 'name', title: '租户名称' },
  { key: 'plan_id', title: '套餐' },
  { key: 'max_accounts', title: '账号上限' },
  { key: 'status', title: '状态' },
  { key: 'created_at', title: '注册时间', format: (v: any) => new Date(v).toLocaleDateString('zh-CN') },
  { key: 'actions', title: '操作', class: 'w-32' },
]

const filteredTenants = computed(() => {
  let list = tenants.value
  if (statusFilter.value) list = list.filter(t => t.status === statusFilter.value)
  if (search.value) {
    const s = search.value.toLowerCase()
    list = list.filter(t => t.name?.toLowerCase().includes(s) || t.tenant_name?.toLowerCase().includes(s))
  }
  return list
})

// P2-50: 原 5 秒轮询对管理后台偏频繁——租户列表变化低频, 15 秒足够, 无谓的
// DB/带宽压力直接降三分之二。
useAutoRefresh(fetchTenants, 15000)

async function fetchTenants() {
  try {
    const res = await adminApiMod.listTenants({ page: page.value, page_size: pageSize.value })
    tenants.value = res.list || []
    total.value = res.total || 0
  } catch (e: unknown) {
    toast({ type: 'error', title: '获取租户失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

async function doCreate() {
  try {
    await adminApiMod.createTenant(createForm.value)
    showCreate.value = false
    createForm.value = { name: '', username: '', password: '', plan_id: 'free' }
    await fetchTenants()
    toast({ type: 'success', title: '租户创建成功' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '创建失败', message: (e instanceof Error ? e.message : '请检查输入后重试') })
  }
}

function viewDetail(t: Tenant) {
  router.push(`/admin/tenants/${t.tenant_id}`)
}

async function resetKey(t: Tenant) {
  try {
    await adminApiMod.resetApiKey(t.tenant_id)
    toast({ type: 'success', title: 'API Key 已重置' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '重置失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

const deleteConfirm = ref()
const deletingTenant = ref<Tenant | null>(null)

function confirmDelete(t: Tenant) {
  deletingTenant.value = t
  deleteConfirm.value?.open()
}

async function doDelete() {
  if (!deletingTenant.value) return
  try {
    await adminApiMod.deleteTenant(deletingTenant.value.tenant_id)
    await fetchTenants()
    toast({ type: 'success', title: '租户已删除' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '删除失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}


// planLabel 已从 @/utils/format 导入

onMounted(async () => {
  await fetchTenants()
  try {
    const res = await adminApiMod.listPlans()
    plans.value = res.list || []
  } catch (e: unknown) {  }
})
</script>
