<template>
  <div class="flex h-[100dvh] bg-bg-secondary">
    <!-- 移动端遮罩 -->
    <div
      v-if="mobileMenuOpen"
      class="fixed inset-0 bg-ink-03 z-40 lg:hidden"
      @click="mobileMenuOpen = false"
      tabindex="0" role="button"
    />

    <!-- 侧边栏 -->
    <aside
      class="fixed lg:static inset-y-0 left-0 z-50 w-64 shrink-0 border-r border-border bg-bg-card backdrop-blur-xl flex flex-col transition-transform duration-300"
      :class="mobileMenuOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'"
    >
      <div class="h-16 flex items-center px-6 border-b border-border">
        <div class="w-8 h-8 rounded-lg bg-cyan/10 flex items-center justify-center mr-3">
          <Shield class="w-5 h-5 text-cyan" />
        </div>
        <span class="font-bold text-lg tracking-tight">管理后台</span>
      </div>

      <nav class="flex-1 overflow-y-auto py-4 px-3 space-y-1">
        <router-link
          v-for="item in menuItems"
          :key="item.path"
          :to="item.path"
          class="flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm transition-[transform,opacity,border-color,background-color]"
          :class="$route.path.startsWith(item.path) ? 'bg-cyan/10 text-cyan border border-cyan/20' : 'text-text-secondary hover:text-text-primary hover:bg-bg-hover'"
          @click="mobileMenuOpen = false"
        >
          <component :is="item.icon" class="w-5 h-5" />
          {{ item.label }}
        </router-link>
      </nav>

      <div class="p-4 border-t border-border">
        <button
          @click="logout"
          class="w-full flex items-center justify-center gap-2 px-3 py-2 rounded-lg text-sm text-text-secondary hover:text-rose hover:bg-rose/5 transition-colors"
        >
          <LogOut class="w-4 h-4" />
          退出登录
        </button>
      </div>
    </aside>

    <div class="flex-1 flex flex-col min-w-0">
      <header class="h-16 border-b border-border flex items-center justify-between px-4 lg:px-6 bg-ink-01 backdrop-blur-xl">
        <div class="flex items-center gap-2">
          <button
            class="lg:hidden p-2 rounded-lg hover:bg-bg-hover"
            @click="mobileMenuOpen = !mobileMenuOpen"
            aria-label="切换菜单"
          >
            <Menu class="w-5 h-5" />
          </button>
          <h1 class="text-lg font-semibold">{{ $route.meta.title }}</h1>
        </div>
        <div class="flex items-center gap-2">
          <span class="text-xs text-text-secondary hidden sm:inline">管理员</span>
          <div class="w-8 h-8 rounded-full bg-cyan/10 flex items-center justify-center">
            <User class="w-4 h-4 text-cyan" />
          </div>
        </div>
      </header>
      <main class="flex-1 overflow-y-auto p-4 lg:p-6">
        <router-view />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Shield, LayoutDashboard, Users, Ticket, Headphones,
  Megaphone, Activity, FileText, LogOut, User, Menu,
  SlidersHorizontal
} from 'lucide-vue-next'
import { useAdminStore } from '@/stores/admin'

const router = useRouter()
const adminStore = useAdminStore()
const mobileMenuOpen = ref(false)

const menuItems = [
  { path: '/admin/dashboard', label: '仪表盘', icon: LayoutDashboard },
  { path: '/admin/plans', label: '套餐管理', icon: SlidersHorizontal },
  { path: '/admin/tenants', label: '租户管理', icon: Users },
  { path: '/admin/codes', label: '卡密管理', icon: Ticket },
  { path: '/admin/support', label: '客服配置', icon: Headphones },
  { path: '/admin/probe', label: '风控探测', icon: Shield },
  { path: '/admin/announcements', label: '公告管理', icon: Megaphone },
  { path: '/admin/system', label: '系统监控', icon: Activity },
  { path: '/admin/audit', label: '审计日志', icon: FileText },
]

function logout() {
  adminStore.logout()
  router.push('/admin/login')
}
</script>
