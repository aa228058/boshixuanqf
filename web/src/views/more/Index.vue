<template>
  <!-- R125 "更多"功能导航页(示例2 语言): 用户卡 + 功能列表(图标+标题+描述+chevron) + 管理端分组 -->
  <main class="flex-1 p-4 sm:p-6 overflow-y-auto pb-24 lg:pb-6 max-w-[720px] mx-auto w-full">
    <h1 class="text-display font-extrabold tracking-tight mt-2 mb-1" style="color: var(--t-text-1);">更多</h1>
    <p class="text-caption mb-5" style="color: var(--t-text-3);">{{ authStore.tenant?.name || '运营' }} · 全部功能入口</p>

    <!-- 用户卡 -->
    <div class="flex items-center gap-3 p-4 rounded-[20px] mb-5" style="background: var(--t-surface-1); border: 1px solid var(--t-line);">
      <div class="w-11 h-11 rounded-[var(--t-r-lg)] grid place-items-center text-white text-base font-extrabold shrink-0"
           style="background: linear-gradient(135deg, var(--t-brand-active), var(--t-brand)); box-shadow: 0 8px 20px rgba(107,91,255,0.4);">{{ (authStore.tenant?.name || '运')[0] }}</div>
      <div class="flex-1 min-w-0">
        <div class="text-small font-bold truncate">{{ authStore.tenant?.name || '我的' }}</div>
        <div class="text-[11.5px] mt-0.5" style="color: var(--t-text-3);">{{ authStore.tenant?.plan_id || '' }} · 到期 {{ (authStore.tenant?.expires_at || '').slice(0, 10) }}</div>
      </div>
      <button class="px-3 py-1.5 rounded-[var(--t-r-md)] text-caption font-bold" style="border: 1px solid var(--t-line); color: var(--t-text-2);" @click="logout">退出</button>
    </div>

    <!-- 功能分组 -->
    <div v-for="g in groups" :key="g.label" class="mb-5">
      <div class="text-[11px] font-extrabold tracking-[0.1em] mb-2 px-1" style="color: var(--t-text-3);">{{ g.label }}</div>
      <div class="rounded-[20px] overflow-hidden" style="background: var(--t-surface-1); border: 1px solid var(--t-line);">
        <router-link v-for="(item, i) in g.items" :key="item.path" :to="item.path"
                     class="flex items-center gap-3 px-4 py-3.5 active:opacity-70 transition-opacity"
                     :style="i < g.items.length - 1 ? 'border-bottom: 1px solid var(--t-line-light);' : ''">
          <span class="w-9 h-9 rounded-[var(--t-r-md)] grid place-items-center shrink-0" style="background: var(--t-brand-dim); color: var(--t-brand);">
            <component :is="item.icon" class="w-[18px] h-[18px]" stroke-width="2" />
          </span>
          <span class="flex-1 min-w-0">
            <span class="block text-small font-bold truncate" style="color: var(--t-text-1);">{{ item.label }}</span>
            <span class="block text-[11.5px] mt-0.5 truncate" style="color: var(--t-text-3);">{{ item.desc }}</span>
          </span>
          <ChevronRight class="w-4 h-4 shrink-0" style="color: var(--t-text-3);" />
        </router-link>
      </div>
    </div>
  </main>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { ChevronRight, Send, UserPlus, Compass, Download, Link2, Eye, Gift, ListChecks, ShieldCheck, Settings, HelpCircle, Megaphone, RefreshCw, KeyRound } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useAccountStore } from '@/stores/account'

const router = useRouter()
const authStore = useAuthStore()
const accountStore = useAccountStore()

const accId = computed(() => accountStore.currentAccount?.account_id || 'none')

const groups = computed(() => {
  const base = [
    { label: '运营', items: [
      { path: `/workspace/${accId.value}/join`, label: '进群邀请', desc: '批量加入目标群组', icon: UserPlus },
      { path: '/groups/discover', label: '群组发现', desc: '浏览与加入公开群组', icon: Compass },
      { path: `/workspace/${accId.value}/scrape`, label: '成员采集', desc: '整理目标成员数据', icon: Download },
    ]},
    { label: '工具', items: [
      { path: `/workspace/${accId.value}/scrape-links`, label: '链接采集', desc: '从文本提取群链接', icon: Link2 },
      { path: `/workspace/${accId.value}/monitor`, label: '消息监控', desc: '关键词命中实时提醒', icon: Eye },
      { path: '/raffle', label: '抽奖助手', desc: '自动参与群抽奖', icon: Gift },
      { path: `/workspace/${accId.value}/tasks`, label: '任务列表', desc: '全部任务与进度', icon: ListChecks },
    ]},
    { label: '系统', items: [
      { path: '/verifier/logs', label: '验证记录', desc: '入群验证处理历史', icon: ShieldCheck },
      { path: '/settings', label: '设置', desc: '套餐、通知与账号', icon: Settings },
      { path: '/help', label: '帮助中心', desc: '常见问题与指引', icon: HelpCircle },
    ]},
  ]
  // 管理员追加管理端分组
  if (authStore.isAdmin || String(authStore.tenant?.role) === 'admin') {
    base.push({ label: '管理后台', items: [
      { path: '/admin/dashboard', label: '运营总览', desc: '平台数据一览', icon: Megaphone },
      { path: '/admin/tenants', label: '租户管理', desc: '租户与套餐', icon: KeyRound },
    ]})
  }
  return base
})

function logout() {
  authStore.logout?.()
  router.push('/login')
}
</script>
