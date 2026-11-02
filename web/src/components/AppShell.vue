<template>
  <!-- R124 新外壳(推倒重做): 移动端=顶栏+底部五键Tab(无抽屉); 桌面端=分组侧栏+顶栏。
       页面头统一示例语言: eyebrow 小标签 + 大标题 + 灰副文案。 -->
  <div class="min-h-screen" style="background: var(--t-bg); color: var(--t-text-1);">

    <!-- ── 桌面侧栏(示例1 语言) ── -->
    <aside class="hidden lg:flex flex-col fixed inset-y-0 left-0 w-[248px] px-3.5 py-5 gap-6 overflow-y-auto"
           style="background: var(--t-surface-1); border-right: 1px solid var(--t-line);">
      <div class="flex items-center gap-2.5 px-2">
        <div class="w-10 h-10 rounded-[var(--t-r-lg)] grid place-items-center text-white text-lg font-black"
             style="background: var(--t-brand); box-shadow: 0 8px 20px color-mix(in srgb, var(--t-brand) 40%, transparent);">➤</div>
        <div>
          <div class="text-body font-extrabold tracking-tight">博士轩群发器</div>
          <div class="text-[11px]" style="color: var(--t-text-3);">运营控制中心</div>
        </div>
      </div>
      <nav v-for="g in navGroups" :key="g.label" class="grid gap-1">
        <div class="text-[10px] font-extrabold tracking-[0.12em] px-3 pb-1.5" style="color: var(--t-text-3);">{{ g.label }}</div>
        <router-link v-for="item in g.items" :key="item.path" :to="item.path"
                     class="flex items-center gap-2.5 px-3 py-2.5 rounded-[var(--t-r-md)] text-small font-semibold transition-colors"
                     :class="isActive(item.path) ? 'font-extrabold' : ''"
                     :style="isActive(item.path)
                       ? 'background: var(--t-brand-dim); color: var(--t-brand);'
                       : 'color: var(--t-text-2);'">
          <component :is="item.icon" class="w-[18px] h-[18px]" :stroke-width="isActive(item.path) ? 2.2 : 1.9" />
          {{ item.label }}
        </router-link>
      </nav>
      <div class="mt-auto pt-4 grid gap-2" style="border-top: 1px solid var(--t-line);">
        <div class="flex items-center gap-2 px-2 text-[11px]" style="color: var(--t-ok);">
          <span class="w-1.5 h-1.5 rounded-full" style="background: var(--t-ok);" />实时连接正常
        </div>
        <div class="flex items-center gap-2.5 px-2 py-1.5">
          <div class="w-8 h-8 rounded-full grid place-items-center text-white text-xs font-extrabold" style="background: var(--t-brand);">D</div>
          <div class="min-w-0">
            <div class="text-caption font-bold truncate">{{ authStore.tenant?.name || '租户' }}</div>
            <div class="text-[10.5px]" style="color: var(--t-text-3);">{{ authStore.tenant?.plan_id || '' }} · 到期 {{ (authStore.tenant?.expires_at || '').slice(0, 10) }}</div>
          </div>
        </div>
        <button class="mx-2 py-2 rounded-[var(--t-r-md)] text-caption font-bold transition-opacity hover:opacity-80"
                style="border: 1px solid var(--t-line); color: var(--t-text-2);" @click="logout">退出登录</button>
      </div>
    </aside>

    <!-- ── 主区 ── -->
    <div class="lg:pl-[248px] flex flex-col min-h-screen">
      <!-- 顶栏 -->
      <header class="sticky top-0 z-30 flex items-center justify-between gap-3 px-4 sm:px-6 h-16"
              style="background: color-mix(in srgb, var(--t-bg) 85%, transparent); backdrop-filter: blur(14px); border-bottom: 1px solid var(--t-line);">
        <div class="min-w-0">
          <div class="text-[10.5px] font-extrabold tracking-[0.13em] uppercase" style="color: var(--t-text-3);">{{ eyebrow }}</div>
          <h1 class="text-title font-extrabold tracking-tight leading-tight truncate">{{ title }}</h1>
        </div>
        <div class="flex items-center gap-2 shrink-0">
          <slot name="actions" />
        </div>
      </header>

      <!-- 内容 -->
      <main class="flex-1 px-4 sm:px-6 py-5 pb-24 lg:pb-8 max-w-[1200px] w-full mx-auto">
        <slot />
      </main>
    </div>

    <!-- ── 移动端底部五键 Tab(示例3 语言, 无抽屉) ── -->
    <nav class="lg:hidden fixed bottom-0 inset-x-0 z-40 px-2 pt-2"
         style="background: color-mix(in srgb, var(--t-surface-1) 92%, transparent); backdrop-filter: blur(18px); border-top: 1px solid var(--t-line); padding-bottom: calc(8px + env(safe-area-inset-bottom));">
      <div class="grid grid-cols-5 max-w-md mx-auto">
        <router-link v-for="t in tabs" :key="t.path" :to="t.path"
                     class="flex flex-col items-center gap-0.5 py-1 text-[10.5px] font-semibold"
                     :style="isActive(t.path) ? 'color: var(--t-brand);' : 'color: var(--t-text-3);'">
          <span v-if="t.center"
                class="w-11 h-11 -mt-6 rounded-[var(--t-r-lg)] grid place-items-center text-white active:scale-95 transition-transform"
                style="background: var(--t-brand); box-shadow: 0 8px 20px color-mix(in srgb, var(--t-brand) 45%, transparent);">
            <Plus class="w-5 h-5" stroke-width="2.4" />
          </span>
          <component v-else :is="t.icon" class="w-[22px] h-[22px]" :stroke-width="isActive(t.path) ? 2.3 : 1.8" />
          <span v-if="!t.center">{{ t.label }}</span>
        </router-link>
      </div>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Home, Users, Plus, ListChecks, Settings,
  Send, UserPlus, Compass, Download, Link2, Eye, Gift, ShieldCheck,
} from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useAccountStore } from '@/stores/account'

defineProps<{
  title: string
  eyebrow?: string
}>()

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const accountStore = useAccountStore()

// workspace 路由带 :accountId 死参数(账号池化遗留, 任意值可进), 与 Layout 同款约定
const accId = computed(() => accountStore.currentAccount?.account_id || 'none')

const tabs = computed(() => [
  { path: '/dashboard', label: '首页', icon: Home },
  { path: '/accounts', label: '账号', icon: Users },
  { path: `/workspace/${accId.value}/broadcast`, label: '', icon: Plus, center: true },
  { path: `/workspace/${accId.value}/tasks`, label: '任务', icon: ListChecks },
  { path: '/settings', label: '我的', icon: Settings },
])

const navGroups = computed(() => [
  { label: '总览', items: [
    { path: '/dashboard', label: '仪表盘', icon: Home },
    { path: '/accounts', label: '账号管理', icon: Users },
  ]},
  { label: '运营工作区', items: [
    { path: `/workspace/${accId.value}/broadcast`, label: '群发消息', icon: Send },
    { path: `/workspace/${accId.value}/join`, label: '进群邀请', icon: UserPlus },
    { path: '/groups/discover', label: '找群搜群', icon: Compass },
    { path: `/workspace/${accId.value}/scrape`, label: '成员采集', icon: Download },
    { path: `/workspace/${accId.value}/scrape-links`, label: '链接采集', icon: Link2 },
    { path: `/workspace/${accId.value}/monitor`, label: '消息监控', icon: Eye },
    { path: '/raffle', label: '抽奖助手', icon: Gift },
    { path: `/workspace/${accId.value}/tasks`, label: '任务列表', icon: ListChecks },
    { path: '/verifier/logs', label: '验证记录', icon: ShieldCheck },
  ]},
  { label: '系统', items: [
    { path: '/settings', label: '设置', icon: Settings },
    { path: '/help', label: '帮助中心', icon: ShieldCheck },
  ]},
])

function isActive(path: string) {
  return route.path.startsWith(path)
}

function logout() {
  authStore.logout?.()
  router.push('/login')
}
</script>
