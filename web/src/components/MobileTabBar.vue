<template>
  <!-- R125 示例2 语言: 底部悬浮玻璃 Tab —— 首页/群发/账号/任务/更多, 等分五键 -->
  <nav class="lg:hidden fixed left-3 right-3 bottom-3 z-40 h-[68px] px-1.5 flex justify-around items-center"
       style="background: color-mix(in srgb, var(--t-surface-1) 88%, transparent); border: 1px solid var(--t-line); border-radius: 26px; backdrop-filter: blur(20px); -webkit-backdrop-filter: blur(20px); padding-bottom: env(safe-area-inset-bottom);">
    <router-link v-for="t in tabs" :key="t.path" :to="t.path"
                 class="flex flex-col items-center gap-[3px] min-w-[56px] py-1.5 px-2.5"
                 :style="{ color: isActive(t) ? 'var(--t-brand)' : 'var(--t-text-3)' }">
      <component :is="t.icon" class="w-[21px] h-[21px]" :stroke-width="isActive(t) ? 2.3 : 1.8" />
      <span class="text-[11px] font-semibold" :style="{ color: isActive(t) ? 'var(--t-text-1)' : 'var(--t-text-3)' }">{{ t.label }}</span>
    </router-link>
  </nav>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { Home, Send, Users, ListChecks, MoreHorizontal } from 'lucide-vue-next'
import { useAccountStore } from '@/stores/account'

const route = useRoute()
const accountStore = useAccountStore()

// workspace 路由带 :accountId 死参数(账号池化遗留, 页面内自选号), 与 Layout 同款约定
const accId = computed(() => accountStore.currentAccount?.account_id || 'none')

// R125 示例2 Tab 顺序: 首页/群发/账号/任务/更多("更多"是功能导航页)
const tabs = computed(() => [
  { path: '/dashboard', label: '首页', icon: Home },
  { path: `/workspace/${accId.value}/broadcast`, label: '群发', icon: Send },
  { path: '/accounts', label: '账号', icon: Users },
  { path: `/workspace/${accId.value}/tasks`, label: '任务', icon: ListChecks },
  { path: '/more', label: '更多', icon: MoreHorizontal },
])

function isActive(t: any) {
  return String(route.path).startsWith(t.path)
}
</script>
