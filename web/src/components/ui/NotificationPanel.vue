<template>
  <Modal v-model="visible" title="消息通知">
    <div class="space-y-1 max-h-[500px] overflow-y-auto">
      <div
        v-for="n in notifications"
        :key="n.id"
        @click="markRead(n.id)"
        class="p-3 rounded-lg cursor-pointer transition-colors"
        :class="n.read ? 'opacity-60' : 'bg-cyan/5 border border-cyan/10'"
       tabindex="0" role="button">
        <div class="flex items-start gap-3">
          <component :is="typeIcon(n.type)" class="w-5 h-5 shrink-0 mt-0.5" :class="typeColor(n.type)" />
          <div class="flex-1 min-w-0">
            <p class="text-sm font-medium">{{ n.title }}</p>
            <p class="text-xs text-text-secondary mt-0.5">{{ n.message }}</p>
            <p class="text-xs text-text-muted mt-1">{{ formatRelativeTime(n.created_at) }}</p>
          </div>
          <div v-if="!n.read" class="w-2 h-2 rounded-full bg-cyan shrink-0 mt-1.5" />
        </div>
      </div>
      <EmptyState
        v-if="!notifications.length"
        icon="Bell"
        title="暂无通知"
        description="有新消息时会在这里显示"
      />
    </div>
    <template #footer>
      <button v-if="unreadCount > 0" @click="markAllRead" class="text-sm text-cyan hover:text-cyan-bright transition-colors">
        全部标记已读
      </button>
    </template>
  </Modal>
</template>

<script setup lang="ts">
import { computed, type Component } from 'vue'
import Modal from './Modal.vue'
import EmptyState from './EmptyState.vue'
import { useNotificationStore } from '@/stores/notification'
import { formatRelativeTime } from '@/utils/format'
import { CheckCircle, AlertTriangle, XCircle, Info, Bell } from 'lucide-vue-next'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const visible = computed({ get: () => props.modelValue, set: v => emit('update:modelValue', v) })
const store = useNotificationStore()
const notifications = computed(() => store.notifications)
const unreadCount = computed(() => store.unreadCount)

function typeIcon(type: string): Component {
  const map: Record<string, unknown> = {
    task_completed: CheckCircle, task_failed: XCircle,
    account_risk: AlertTriangle, account_offline: XCircle,
    plan_expiring: Info, system: Info, scrape_completed: CheckCircle
  }
  return map[type] || Bell
}

function typeColor(type: string) {
  const map: Record<string, string> = {
    task_completed: 'text-emerald', task_failed: 'text-rose',
    account_risk: 'text-amber', account_offline: 'text-rose',
    plan_expiring: 'text-amber', system: 'text-cyan', scrape_completed: 'text-emerald'
  }
  return map[type] || 'text-text-secondary'
}

function markRead(id: string) { store.markRead(id) }
function markAllRead() { store.markAllRead() }
</script>
