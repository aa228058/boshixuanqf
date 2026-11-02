<template>
  <main class="space-y-4">
    <div class="flex items-center justify-between">
      <t-button theme="primary" size="sm" class="shrink-0" @click="showCreate = true"> 发布公告
      </t-button>
    </div>

    <div v-if="!announcements.length" class="py-12">
      <EmptyState icon="Megaphone" title="暂无公告" description="点击上方按钮，向所有租户发布一条公告~" />
    </div>
    <div v-else class="space-y-3">
      <div
        v-for="a in announcements"
        :key="a.announcement_id"
        class="glass rounded-[var(--t-r-md)] p-5 lift"
        :class="a.priority === 'urgent' ? 'border-rose/30' : 'border-border'"
      >
        <div class="flex items-start justify-between">
          <div class="flex-1">
            <div class="flex items-center gap-2">
              <span v-if="a.priority === 'urgent'" class="text-xs px-2 py-0.5 rounded bg-rose/10 text-rose font-medium">紧急</span>
              <span class="font-medium">{{ a.title }}</span>
            </div>
            <p class="text-sm text-text-secondary mt-2 whitespace-pre-wrap max-w-[65ch]">{{ a.content }}</p>
            <div class="flex items-center gap-4 mt-3 text-xs text-text-secondary">
              <span>目标: {{ a.target_type === 'all' ? '全部租户' : a.target_plans?.join(', ') }}</span>
              <span>开始: {{ formatDate(a.start_at) }}</span>
              <span v-if="a.end_at">结束: {{ formatDate(a.end_at) }}</span>
            </div>
          </div>
          <div class="flex items-center gap-1 ml-4">
            <button @click="editAnnouncement(a)" class="p-1.5 rounded hover:bg-bg-hover" aria-label="编辑">
              <Pencil class="w-4 h-4 text-text-muted" />
            </button>
            <button @click="deleteAnnouncement(a)" class="p-1.5 rounded hover:bg-rose/10">
              <Trash2 class="w-4 h-4 text-text-muted hover:text-rose" />
            </button>
          </div>
        </div>
      </div>
    </div>

    <ConfirmDialog
      ref="deleteConfirm"
      title="删除公告"
      :message="`确定删除公告「${deletingAnnouncement?.title || ''}」吗？`"
      confirm-text="确认删除"
      danger
      @confirm="doDelete"
    />

    <Modal v-model="showCreate" :title="editing ? '编辑公告' : '发布公告'">
      <div class="space-y-4">
        <div>
          <label for="ann-title" class="block text-sm text-text-secondary mb-1.5">标题</label>
          <input id="ann-title" v-model="form.title" class="w-full px-3 py-2 rounded-lg bg-bg-card border border-border focus:border-cyan focus:outline-none text-sm" aria-label="输入" />
        </div>
        <div>
          <t-textarea v-model="form.content" label="内容" :rows="4" />
        </div>
        <div>
          <label for="ann-priority" class="block text-sm text-text-secondary mb-1.5">优先级</label>
          <div class="flex gap-2">
            <button
              @click="form.priority = 'normal'"
              class="flex-1 py-2 rounded-lg text-sm border transition-colors"
              :class="form.priority === 'normal' ? 'border-cyan/30 bg-cyan/5 text-cyan' : 'border-border text-text-secondary'"
            >
              普通
            </button>
            <button
              @click="form.priority = 'urgent'"
              class="flex-1 py-2 rounded-lg text-sm border transition-colors"
              :class="form.priority === 'urgent' ? 'border-rose/30 bg-rose/5 text-rose' : 'border-border text-text-secondary'"
            >
              紧急
            </button>
          </div>
        </div>
        <div>
          <label for="ann-target" class="block text-sm text-text-secondary mb-1.5">目标租户</label>
          <select id="ann-target" v-model="form.target_type" class="w-full px-3 py-2 rounded-lg bg-bg-card border border-border focus:border-cyan focus:outline-none text-sm">
            <option value="all">全部租户</option>
            <option value="specific_plan">指定套餐</option>
          </select>
        </div>
        <div v-if="form.target_type === 'specific_plan'">
          <label for="ann-plans" class="block text-sm text-text-secondary mb-1.5">选择套餐</label>
          <div class="flex flex-wrap gap-2">
            <label v-for="p in plans" :key="p.plan_id" class="flex items-center gap-1 text-sm">
              <input type="checkbox" :value="p.plan_id" v-model="form.target_plans" class="rounded border-border bg-bg-card text-cyan" aria-label="选择" />
              {{ p.name }}
            </label>
          </div>
        </div>
      </div>
      <template #footer>
        <button @click="showCreate = false" class="px-4 py-2 rounded-lg text-sm text-text-secondary">取消</button>
        <t-button theme="primary" @click="saveAnnouncement">
          {{ editing ? '保存' : '发布' }}
        </t-button>
      </template>
    </Modal>
  </main>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { Plus, Pencil, Trash2, Megaphone } from 'lucide-vue-next'
import { adminApiMod } from '@/api/admin'
import { formatDate } from '@/utils/format'
import { toast } from '@/utils/toast'
import Modal from '@/components/ui/Modal.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue'
import type { Announcement, Plan } from '@/types'

const announcements = ref<Announcement[]>([])
const plans = ref<Plan[]>([])
const showCreate = ref(false)
const editing = ref(false)
const editId = ref('')
const deleteConfirm = ref()
const deletingAnnouncement = ref<Announcement | null>(null)

const form = ref<Partial<Announcement>>({
  title: '', content: '', priority: 'normal', target_type: 'all', target_plans: [],
})

async function fetchAnnouncements() {
  try {
    const res = await adminApiMod.listAnnouncements()
    announcements.value = res.list || []
  } catch (e: unknown) {
    toast({ type: 'error', title: '获取公告失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

function editAnnouncement(a: Announcement) {
  editing.value = true
  editId.value = a.announcement_id
  // P1-26: 原裸 spread——老数据缺 priority/target_type 时编辑保存会丢字段。
  // 默认值先铺底再覆盖。
  form.value = { title: '', content: '', priority: 'normal', target_type: 'all', target_plans: [], ...a }
  showCreate.value = true
}

async function saveAnnouncement() {
  try {
    if (editing.value) {
      await adminApiMod.updateAnnouncement(editId.value, form.value)
      toast({ type: 'success', title: '公告已更新' })
    } else {
      await adminApiMod.createAnnouncement(form.value)
      toast({ type: 'success', title: '公告已发布' })
    }
    showCreate.value = false
    editing.value = false
    form.value = { title: '', content: '', priority: 'normal', target_type: 'all', target_plans: [] }
    await fetchAnnouncements()
  } catch (e: unknown) {
    toast({ type: 'error', title: '保存失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

function deleteAnnouncement(a: Announcement) {
  deletingAnnouncement.value = a
  deleteConfirm.value?.open()
}

async function doDelete() {
  if (!deletingAnnouncement.value) return
  try {
    await adminApiMod.deleteAnnouncement(deletingAnnouncement.value.announcement_id)
    await fetchAnnouncements()
    toast({ type: 'success', title: '公告已删除' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '删除失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

onMounted(async () => {
  await fetchAnnouncements()
  try {
    const res = await adminApiMod.listPlans()
    plans.value = res.list || []
  } catch (e: unknown) {
    toast({ type: 'error', title: '获取套餐失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
})
</script>
