<template>
  <Modal v-model="visible" title="公告通知">
    <div class="space-y-3 max-h-[500px] overflow-y-auto">
      <div
        v-for="a in announcements"
        :key="a.announcement_id"
        class="p-4 rounded-lg border"
        :class="a.priority === 'urgent' ? 'border-rose/30 bg-rose/5' : 'border-border bg-bg-card'"
      >
        <div class="flex items-center gap-2 mb-2">
          <span v-if="a.priority === 'urgent'" class="text-xs px-1.5 py-0.5 rounded bg-rose/10 text-rose font-medium">紧急</span>
          <span class="font-medium text-sm">{{ a.title }}</span>
          <span class="text-xs text-text-secondary ml-auto">{{ formatDate(a.created_at) }}</span>
        </div>
        <p class="text-sm text-text-secondary whitespace-pre-wrap max-w-[65ch] mx-auto text-center">{{ a.content }}</p>
      </div>
      <EmptyState
        v-if="!announcements.length"
        icon="Bell"
        title="暂无公告"
        description="管理员还没有发布公告"
      />
    </div>
  </Modal>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import Modal from './Modal.vue'
import EmptyState from './EmptyState.vue'
import { settingsApi } from '@/api/settings'
import { formatDate } from '@/utils/format'
import type { Announcement } from '@/types'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: boolean): void }>()

const visible = computed({ get: () => props.modelValue, set: v => emit('update:modelValue', v) })
const announcements = ref<Announcement[]>([])

onMounted(async () => {
  try {
    const res = await settingsApi.getAnnouncements()
    announcements.value = Array.isArray(res) ? res : []
  } catch (e: unknown) {
    announcements.value = []
  }
})
</script>
