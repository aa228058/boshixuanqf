<template>
  <!-- 内部换 TDesign Empty 芯, 对外接口不变(icon/title/description/slot) -->
  <t-empty :description="title" class="py-10">
    <template #image>
      <div class="w-20 h-20 rounded-[var(--t-r-lg)] bg-bg-card flex items-center justify-center mx-auto">
        <component :is="resolvedIcon" class="w-10 h-10 text-text-secondary" stroke-width="1.5" />
      </div>
    </template>
    <p v-if="description" class="text-sm text-text-secondary max-w-[65ch] mx-auto">{{ description }}</p>
    <slot />
  </t-empty>
</template>

<script setup lang="ts">
import { computed, type Component } from 'vue'
import {
  Inbox, Search, Users, MessageSquare, ListTodo, Eye,
  Send, Shield, Bell, Megaphone, CheckCircle, AlertTriangle,
  Key, ClipboardList, Download, ShieldCheck, FileText, BarChart3,
  Settings, Trash2, FolderPlus, Plug, Unplug, KeyRound
} from 'lucide-vue-next'

const props = defineProps<{
  icon?: Component | string
  title: string
  description?: string
}>()

const iconMap: Record<string, Component> = {
  Inbox, Search, Users, MessageSquare, ListTodo, Eye,
  Send, Shield, Bell, Megaphone, CheckCircle, AlertTriangle,
  Key, ClipboardList, Download, ShieldCheck, FileText, BarChart3,
  Settings, Trash2, FolderPlus, Plug, Unplug, KeyRound
}

const resolvedIcon = computed(() => {
  if (!props.icon) return Inbox
  if (typeof props.icon === 'string') return iconMap[props.icon] || Inbox
  return props.icon
})
</script>
