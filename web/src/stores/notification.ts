import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { Notification } from '@/types'

export const useNotificationStore = defineStore('notification', () => {
  const notifications = ref<Notification[]>([])
  const unreadCount = computed(() => notifications.value.filter(n => !n.read).length)

  function addNotification(n: Omit<Notification, 'id' | 'created_at'>) {
    notifications.value.unshift({
      ...n,
      id: Math.random().toString(36).slice(2) + Date.now().toString(36),
      created_at: new Date().toISOString(),
    })
    if (notifications.value.length > 100) notifications.value.pop()
  }

  function markRead(id: string) {
    const n = notifications.value.find(x => x.id === id)
    if (n) n.read = true
  }

  function markAllRead() {
    notifications.value.forEach(n => n.read = true)
  }

  function remove(id: string) {
    notifications.value = notifications.value.filter(n => n.id !== id)
  }

  return { notifications, unreadCount, addNotification, markRead, markAllRead, remove }
})
