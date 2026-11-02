import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

interface LocalNotification {
  id: string
  type: string
  title?: string
  message?: string
  read: boolean
  created_at: string
}

export const useNotificationStore = defineStore('notification', () => {
  const notifications = ref<LocalNotification[]>([])
  const unreadCount = computed(() => notifications.value.filter(n => !n.read).length)

  function addNotification(n: Omit<LocalNotification, 'id' | 'created_at' | 'read'> & { read?: boolean }) {
    notifications.value.unshift({
      ...n,
      id: Math.random().toString(36).slice(2) + Date.now().toString(36),
      read: n.read ?? false,
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
