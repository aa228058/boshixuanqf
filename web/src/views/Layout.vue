<template>
  <div class="flex h-[100dvh] relative">
    <!-- 全局氛围层: 光晕 + 淡紫粒子网, 所有页面常显 -->
    <!-- P1-06 审计修复: 原四层背景叠加(星空+柔光+aurora 漂移+粒子)——移动端无
         降级且是持续 GPU 开销。按设计规范深色收敛为 2 层(星空+柔光), 删 aurora
         与 ParticleField。ParticleField 组件保留(auth/Login 页仍在用)。 -->
    <!-- Skip to content -->
    <a href="#main-content" class="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-[100] focus:px-4 focus:py-2 focus:rounded-lg focus:bg-cyan focus:text-white text-sm font-medium">
      跳转到主内容
    </a>
    <!-- 侧边栏 R124: 移动端去抽屉(纯底部Tab导航), 桌面=248px 分组侧栏 -->
    <aside
      class="hidden md:flex w-[248px] shrink-0 flex-col h-full px-3.5 py-4 gap-1 overflow-y-auto"
      style="background: var(--t-surface-1); border-right: 1px solid var(--t-line);"
     role="complementary" aria-label="导航">
      <!-- Logo(陶橙圆角块, 示例1 语言) -->
      <header class="flex items-center gap-2.5 px-2 pb-3" style="border-bottom: 1px solid var(--t-line-light);">
        <div class="w-10 h-10 rounded-[var(--t-r-lg)] grid place-items-center text-white text-lg font-black"
             style="background: var(--t-brand); box-shadow: 0 8px 20px color-mix(in srgb, var(--t-brand) 40%, transparent);">➤</div>
        <div>
          <div class="text-body font-extrabold tracking-tight text-text-primary">博士轩群发器</div>
          <div class="text-[10.5px] text-text-muted">运营控制中心</div>
        </div>
      </header>

    <!-- R113: 旧侧栏账号切换器段(103行 v-if=false 死块)已删 -->

      <!-- 导航 -->
      <nav class="flex-1 overflow-y-auto py-3 px-3 space-y-0.5">
        <!-- 全局菜单（分区可折叠；协议号系统按套餐过滤） -->
        <template v-for="sec in menuSections" :key="sec.label || 'main'">
          <div
            v-if="sec.label"
            class="flex items-center gap-3 px-3.5 pt-2 pb-1 text-[10.5px] font-semibold tracking-wider text-text-muted/60 uppercase"
          >
            <Folder class="w-[20px] h-[20px]" stroke-width="2" />
            {{ GROUP_LABELS[sec.label] || sec.label }}
          </div>
          <div>
            <template v-for="item in sec.items" :key="item.path">
              <router-link
                :to="item.path"
                class="relative flex items-center gap-3 px-3.5 py-2 rounded-[var(--t-r-md)] text-sm font-medium transition-[transform,opacity,border-color,background-color] duration-200"
                :class="[$route.path === item.path ? 'menu-on' : 'text-text-secondary hover:text-text-primary hover:bg-bg-hover', item.locked ? 'opacity-60' : '']"
                @click="onGlobalItemClick(item, $event)"
              >
                <!-- 激活指示条: 弹性滑入滑出 + 高度过渡(2026-10-21 侧边栏精修) -->
                <i
                  class="absolute -left-3 w-1 rounded-full bg-cyan transition-all duration-300 ease-[cubic-bezier(0.34,1.56,0.64,1)]"
                  :class="$route.path === item.path ? 'h-5 opacity-100' : 'h-0 opacity-0'"
                />
                <component :is="item.icon" class="w-[18px] h-[18px]" stroke-width="2" />
                {{ item.label }}
                <Lock v-if="item.locked" class="w-3.5 h-3.5 ml-auto text-amber/70" />
              </router-link>
            </template>
          </div>
        </template>

        <!-- 工作区菜单（按 消息/增长/数据 分组，没选账号或套餐不支持时禁用） -->
        <template v-for="(grp, gi) in workspaceGroups" :key="gi">
          <div v-if="grp.label" class="px-3.5 pt-2.5 pb-1 text-[10.5px] font-semibold tracking-wider text-text-muted/60 uppercase">
            {{ grp.label }}
          </div>
          <div class="stagger">
          <button
            v-for="(item, ii) in grp.items"
            :style="{ '--i': ii }"
            :key="item.path"
            @click="handleMenuClick(item)"
            class="w-full flex items-center gap-3 px-3.5 py-2 rounded-[var(--t-r-md)] text-sm font-medium transition-colors duration-200 text-left"
            :class="[
              item.disabled
                ? 'text-text-muted/40 cursor-not-allowed'
                : ($route.path.startsWith(item.path)
                  ? 'menu-on font-semibold'
                  : 'text-text-secondary hover:text-text-primary hover:bg-bg-hover'),
            ]"
            :disabled="item.disabled">
            <component :is="item.icon" class="w-[18px] h-[18px]" stroke-width="1.8" />
            {{ item.label }}
            <Lock v-if="item.disabled" class="w-3 h-3 ml-auto text-text-muted/30" />
          </button>
          </div>
        </template>

      </nav>

      <!-- 底部信息: R87-M2 手术① —— 套餐大卡已瘦身为一行小字(完整信息在设置页), 侧栏只留导航+状态 -->
      <footer class="px-4 py-3 border-t border-border/50">
        <div class="flex items-center justify-between text-[11.5px] text-text-muted leading-none">
          <span>{{ planName }} · 到期 {{ expiryText }}</span>
          <span :class="isExpiringSoon ? 'text-rose font-medium' : ''" v-if="isExpiringSoon">即将到期</span>
        </div>
        <div class="hidden"> <!-- 原套餐大卡片已移除: 当前套餐/到期时间/账号使用进度 → 设置页查看 -->
          <div class="flex items-center justify-between mb-2">
            <span class="text-xs text-text-secondary">当前套餐</span>
            <span class="text-xs font-semibold text-cyan">{{ planName }}</span>
          </div>
          <div class="flex items-center justify-between mb-2">
            <span class="text-xs text-text-secondary">到期时间</span>
            <span class="text-xs font-medium" :class="isExpiringSoon ? 'text-rose' : 'text-text-primary'">
              {{ expiryText }}
            </span>
          </div>
          <div class="flex items-center justify-between">
            <span class="text-xs text-text-secondary">账号使用</span>
            <span class="text-xs font-medium" :class="accountUsage >= 0.9 ? 'text-rose' : 'text-text-primary'">
              {{ accountCount }}/{{ maxAccounts }}
            </span>
          </div>
          <div class="w-full h-1.5 bg-ink-03 rounded-full mt-2.5 overflow-hidden shadow-inner">
            <div class="h-full bg-cyan rounded-full transition-[width,background-color] duration-300 glow-progress" :style="{ width: `${Math.min(accountUsage * 100, 100)}%` }" />
          </div>
        </div>
        <!-- 深浅主题切换: 即时生效无需刷新 -->
        <!-- P1-08: 原此处有第二个主题切换按钮(带文字), 与顶栏图标按钮重复——
             全站只保留顶栏一处入口, 删除此副本 -->
        <button
          @click="logout"
          class="w-full mt-1.5 flex items-center justify-center gap-2 px-3 py-2.5 rounded-[var(--t-r-md)] text-sm text-text-secondary hover:text-rose hover:bg-rose/10 transition-[transform,opacity,border-color,background-color] duration-200"
        >
          <LogOut class="w-4 h-4" stroke-width="2" />
          退出登录
        </button>
      </footer>

    <!-- R118: 实时连接状态 —— 绿=正常/黄=重连中/灰=断开(可点击立即重连)
         断开从红色警报降级为弱提示: 手机网络抖动是常态, 红字常驻制造焦虑 -->
    <div class="px-3.5 py-2.5 mt-1 border-t border-border/50 flex items-center gap-2 text-[11px]"
         :class="sseStatus === 'connected' ? 'text-text-muted' : sseStatus === 'reconnecting' ? 'text-amber' : 'text-text-muted/70'">
      <span class="relative flex w-2 h-2 shrink-0">
        <span v-if="sseStatus === 'connected'"
              class="absolute inline-flex w-full h-full rounded-full bg-emerald opacity-60 animate-ping" />
        <span class="relative inline-flex w-2 h-2 rounded-full shrink-0"
              :class="sseStatus === 'connected' ? 'bg-emerald' : sseStatus === 'reconnecting' ? 'bg-amber animate-pulse' : 'bg-text-muted/50'" />
      </span>
      <span v-if="sseStatus === 'connected'">实时连接正常</span>
      <span v-else-if="sseStatus === 'reconnecting'">重连中{{ sseRetryCount > 1 ? `(第${sseRetryCount}次)` : '' }}…</span>
      <button v-else class="flex items-center gap-1 hover:text-cyan transition-colors"
              title="点击立即重连" @click="reconnectNow">
        连接不可用 · 点击重连
      </button>
    </div>
    </aside>

    <!-- R124: 移动端遮罩与汉堡按钮已删 —— 移动端=纯底部五键Tab导航(无抽屉),
         桌面端侧栏常驻。 -->

    <!-- 主内容区 -->
    <div class="flex-1 flex flex-col min-w-0">
      <!-- 顶部导航：滚动时从透明变毛玻璃 + 阴影 -->
      <header
        ref="headerRef"
        class="h-14 shrink-0 flex items-center justify-between px-4 md:px-6 sticky top-0 z-20 transition-[transform,opacity,border-color,background-color] duration-300"
        :class="isScrolled
          ? 'bg-ink-01 backdrop-blur-md'
          : 'bg-bg-primary border-b border-border/50'"
      >
        <div class="flex items-center gap-2.5">
          <!-- R127 顶栏左 = 品牌(logo+博士轩TG助手): 页面功能名各页已有大标题, 顶栏不再重复显示功能名 -->
          <div class="w-8 h-8 rounded-[var(--t-r-md)] grid place-items-center text-white text-caption font-black shrink-0"
               style="background: var(--t-brand); box-shadow: 0 6px 14px color-mix(in srgb, var(--t-brand) 40%, transparent);">➤</div>
          <span class="text-caption font-extrabold tracking-tight whitespace-nowrap shrink-0 leading-none">博士轩 TG 助手</span>
          <!-- R130 版本徽章: 构建号一眼可见(部署验证), 令牌色跟主题 -->
          <span class="px-1.5 py-0.5 rounded-md text-[10px] font-bold shrink-0"
                style="background: var(--t-brand-dim); color: var(--t-brand);">{{ BUILD_TAG }}</span>
        </div>
        <div class="flex items-center gap-2">
          <!-- R126 三主题循环: 正常(靛蓝) → 护眼(陶土) → 深色(玻璃紫) -->
          <button
            @click="toggleDarkMode"
            :title="`当前:${themeLabel}模式 · 点击切换`"
            class="flex items-center gap-1.5 px-2.5 py-2 rounded-[var(--t-r-md)] hover:bg-bg-hover transition-colors"
          >
            <Sun v-if="themeMode === 'normal'" class="w-5 h-5 text-text-secondary" stroke-width="2" />
            <Leaf v-else-if="themeMode === 'eye'" class="w-5 h-5" style="color: var(--t-ok);" stroke-width="2" />
            <Moon v-else class="w-5 h-5" style="color: var(--t-brand);" stroke-width="2" />
            <span class="text-[11px] font-bold" style="color: var(--t-text-3);">{{ themeLabel }}</span>
          </button>
          <button
            @click="showAnnouncements = true"
            class="relative p-2.5 rounded-[var(--t-r-md)] hover:bg-bg-hover transition-colors"
          >
            <Megaphone class="w-5 h-5 text-text-secondary" stroke-width="2" />
            <span v-if="unreadAnnouncements" class="absolute top-1.5 right-1.5 w-2 h-2 rounded-full bg-rose" />
          </button>
          <!-- R108: 顶栏账号切换器已删除 —— 全功能账号池化后(群发多选/加群/采集/链接各自选号),
               全局切换失去存在意义; 账号添加入口保留在账号管理页 -->
          <button
            @click="showNotifications = true"
            class="relative p-2.5 rounded-[var(--t-r-md)] hover:bg-bg-hover transition-colors"
          >
            <BellRing class="w-5 h-5 text-text-secondary" stroke-width="2" />
            <span
              v-if="unreadCount"
              class="absolute -top-0.5 -right-0.5 min-w-[20px] h-[20px] rounded-full bg-rose text-white text-xs font-semibold flex items-center justify-center px-1"
            >
              {{ unreadCount }}
            </span>
          </button>
          <!-- R126 右上角租户头像 → 账号菜单(修改资料/修改密码/退出登录) -->
          <div class="relative" ref="avatarMenuRef">
            <button
              @click="avatarMenuOpen = !avatarMenuOpen"
              class="w-9 h-9 rounded-full grid place-items-center text-white text-sm font-extrabold active:scale-95 transition-transform"
              :style="{ background: avatarGradient }"
              :title="authStore.tenant?.name || '账号菜单'"
            >
              {{ tenantInitial }}
            </button>
            <div v-if="avatarMenuOpen"
                 class="absolute right-0 top-11 w-52 rounded-[var(--t-r-lg)] p-1.5 z-50 animate-fade-in-scale"
                 style="background: var(--t-surface-1); border: 1px solid var(--t-line); box-shadow: var(--t-shadow-pop); backdrop-filter: blur(20px);">
              <div class="px-3 py-2.5" style="border-bottom: 1px solid var(--t-line-light);">
                <div class="text-caption font-bold truncate">{{ authStore.tenant?.name || '租户' }}</div>
                <div class="text-[11px] mt-0.5" style="color: var(--t-text-3);">{{ authStore.tenant?.plan_id || '' }} · 到期 {{ (authStore.tenant?.expires_at || '').slice(0, 10) }}</div>
              </div>
              <router-link to="/settings" @click="avatarMenuOpen = false"
                           class="flex items-center gap-2.5 px-3 py-2.5 rounded-[var(--t-r-md)] text-caption font-semibold hover:opacity-80 transition-opacity"
                           style="color: var(--t-text-1);">
                <UserRound class="w-4 h-4" style="color: var(--t-text-3);" /> 修改资料
              </router-link>
              <router-link to="/settings" @click="avatarMenuOpen = false"
                           class="flex items-center gap-2.5 px-3 py-2.5 rounded-[var(--t-r-md)] text-caption font-semibold hover:opacity-80 transition-opacity"
                           style="color: var(--t-text-1);">
                <KeyRound class="w-4 h-4" style="color: var(--t-text-3);" /> 修改密码
              </router-link>
              <button @click="avatarMenuOpen = false; logout()"
                      class="w-full flex items-center gap-2.5 px-3 py-2.5 rounded-[var(--t-r-md)] text-caption font-semibold hover:opacity-80 transition-opacity"
                      style="color: var(--t-err);">
                <LogOut class="w-4 h-4" /> 退出登录
              </button>
            </div>
          </div>
        </div>
      </header>

      <!-- 页面内容: overlay 路由以全屏模态形式展现(返回/关闭条 + 独立滚动区), 子弹层(群组选择器等)不受影响 -->
      <!-- R127: pb-24 给移动端底部 Tab 留足空间 —— 此前缺失导致 Tab 悬浮遮挡页面底部内容(按钮点不到) -->
      <main id="main-content" ref="mainRef" class="flex-1 min-h-0 overflow-y-auto p-3 sm:p-5 md:p-6 pb-24 md:pb-6 relative z-10">
        <router-view v-slot="{ Component, route }">
          <!-- 统一内嵌布局(2026-10-20 重构): 功能页一律显示在顶部栏下方,
               不再 overlay 全屏覆盖, 去掉全屏❌按钮(返回用侧边栏导航) -->
          <component :is="Component" :key="route.fullPath" />
        </router-view>
      </main>
    </div>

    <!-- R123: 移动端底部 Tab Bar(示例3 语言) —— 五键直达, 桌面端组件内自隐藏 -->
    <MobileTabBar />

    <AnnouncementPanel v-model="showAnnouncements" />
    <NotificationPanel v-model="showNotifications" />

    <!-- 添加账号弹窗(三步向导, 统一走 Modal 封装) -->
    <Modal v-model="showAddAccount" :title="addStep === 'phone' ? '添加 Telegram 账号' : addStep === 'code' ? '输入验证码' : '两步验证'">
      <!-- 步骤1：手机号 -->
      <div v-if="addStep === 'phone'" class="space-y-4">
        <div>
          <label class="block text-sm text-text-secondary mb-1.5">手机号</label>
          <!-- 单号输入(2026-10-20 清理: 原多行批量加号使用率低已移除, 队列逻辑保留恒为 1) -->
          <div class="flex items-center">
            <span class="inline-flex items-center px-3 h-[44px] rounded-l-lg bg-bg-card border border-r-0 border-border text-text-secondary text-sm">+</span>
            <input v-model="phoneInputs[0]" placeholder="8613800138000" class="flex-1 px-3 h-[44px] rounded-r-lg glow-input text-sm" inputmode="numeric" />
          </div>
          <p class="text-xs text-text-secondary mt-1">只需输入号码数字（+ 前缀已自带）</p>
        </div>
        <div>
          <label class="block text-sm text-text-secondary mb-1.5">备注（可选）</label>
          <input v-model="newRemark" placeholder="如：主号-客服" class="w-full px-3 py-2.5 rounded-lg glow-input text-sm" aria-label="如：主号-客服" />
        </div>
      </div>
      <!-- 步骤2：验证码 -->
      <div v-else-if="addStep === 'code'" class="space-y-4 text-center">
        <p class="text-text-secondary">验证码已发送到 <span class="font-medium text-text-primary">{{ currentQueuePhone }}</span>
            <template v-if="queueProcessing">（队列 {{ queueIndex + 1 }}/{{ phoneQueue.length }}）</template></p>
        <div v-if="queueProcessing && phoneQueue.length" class="max-h-32 overflow-y-auto rounded-lg border border-border divide-y divide-border text-left mb-2">
          <div v-for="(q, i) in phoneQueue" :key="i" class="px-3 py-1.5 text-xs flex items-center gap-2" :class="i === queueIndex ? 'bg-cyan/5' : ''">
            <span class="text-text-secondary shrink-0">{{ q.phone }}</span>
            <span v-if="q.step === 'done'" class="text-green-500">完成</span>
            <span v-else-if="q.step === 'failed'" class="text-red-500">{{ q.error || '失败' }}</span>
            <span v-else-if="i === queueIndex" class="text-cyan">处理中</span>
            <span v-else class="text-text-muted">排队中</span>
          </div>
        </div>
        <input v-model="authCode" placeholder="请输入验证码" inputmode="numeric" pattern="[0-9]*" maxlength="6" class="w-full px-4 py-3 rounded-lg glow-input text-center text-lg font-mono tracking-widest" @keyup.enter="submitVerificationCode" aria-label="请输入验证码" />
        <p v-if="codeError" class="text-sm text-rose">{{ codeError }}</p>
        <button @click="resendCode" class="text-xs text-cyan hover:text-cyan-bright transition-colors">没收到？重新获取</button>
      </div>
      <!-- 步骤3：2FA -->
      <div v-else-if="addStep === '2fa'" class="space-y-4 text-center">
        <div class="w-12 h-12 rounded-full icon-glow flex items-center justify-center mx-auto">
          <Shield class="w-6 h-6 text-amber" />
        </div>
        <p class="text-text-secondary">该账号开启了两步验证</p>
        <input v-model="twoFAPassword" type="password" placeholder="请输入两步验证密码" class="w-full px-4 py-3 rounded-lg glow-input text-center" @keyup.enter="submit2FA" aria-label="请输入两步验证密码" />
        <p v-if="twoFAError" class="text-sm text-rose">{{ twoFAError }}</p>
      </div>
      <!-- footer 按步骤切换(footer 插槽必须在 Modal 直接子层级, 不能嵌套在条件分支内) -->
      <template #footer>
        <template v-if="addStep === 'phone'">
          <button class="modal-btn-ghost" @click="showAddAccount = false">取消</button>
          <button class="modal-btn-primary" :disabled="!phoneInputs.some(p => p.trim()) || adding || resendCountdown > 0" @click="getVerificationCode">
            <Loader2 v-if="adding" class="w-4 h-4 animate-spin" />
            {{ adding ? '发送中...' : resendCountdown > 0 ? `已发送(${resendCountdown}s)` : '获取验证码' }}
          </button>
        </template>
        <template v-else-if="addStep === 'code'">
          <button class="modal-btn-ghost" @click="showAddAccount = false">取消</button>
          <button class="modal-btn-primary" :disabled="!authCode || submitting" @click="submitVerificationCode">
            {{ submitting ? '提交中...' : '提交验证码' }}
          </button>
        </template>
        <template v-else>
          <button class="modal-btn-ghost" @click="showAddAccount = false">取消</button>
          <button class="modal-btn-primary" :disabled="!twoFAPassword || submitting" @click="submit2FA">
            {{ submitting ? '验证中...' : '完成登录' }}
          </button>
        </template>
      </template>
    </Modal>

    <!-- R113: 验证码弹窗链已删(showAuthModal 零调用; 待验证账号的验证码入口在账号管理页卡片) -->
    <!-- R87-M3: 移动端底部 Tab 已删除 —— 侧边栏(汉堡抽屉)为全站唯一导航, 底部栏职责重复 -->
  </div>
</template>

<script setup lang="ts">
// P1-06: ParticleField 的 import 已删(背景收敛为 2 层, 组件保留给 auth/Login 用)
import { ref, computed, onMounted, onUnmounted, onBeforeUnmount } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  Lock,
  Send,
  LayoutDashboard,
  Users,
  // R114 死图标清扫: 删 14 个零使用 import(切换器/旧菜单遗留)
  MessageSquare,
  ListTodo,
  Eye,
  Settings,
  LogOut,
  Bell,
  BellRing,
  Menu,
  Plus,
  KeyRound,
  Loader2,
  Shield,
  Download,
  Link,
  Timer,
  UserPlus,
  Compass, Gift, HelpCircle,
  Sun, Moon, Leaf, UserRound, Megaphone,
} from 'lucide-vue-next'
// R126 租户头像: 名称首字母 + 名字哈希生成稳定渐变色(自定义头像上传下批接后端)
const tenantInitial = computed(() => (authStore.tenant?.name || '租')[0].toUpperCase())
const avatarGradient = computed(() => {
  const name = authStore.tenant?.name || ''
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 360
  return `linear-gradient(135deg, hsl(${h}, 65%, 55%), hsl(${(h + 40) % 360}, 70%, 45%))`
})
const avatarMenuOpen = ref(false)
const avatarMenuRef = ref<HTMLElement | null>(null)
function closeAvatarMenu(e: MouseEvent) {
  if (avatarMenuRef.value && !avatarMenuRef.value.contains(e.target as Node)) avatarMenuOpen.value = false
}
onMounted(() => document.addEventListener('click', closeAvatarMenu))
onUnmounted(() => document.removeEventListener('click', closeAvatarMenu))

import { useAuthStore } from '@/stores/auth'
import { useTaskEvents, reconnectNow } from '@/composables/useTaskEvents'
import { useElasticScroll } from '@/composables/useElasticScroll'
import { BUILD_TAG } from '@/build-tag'
import { useAccountStore } from '@/stores/account'
import { useGroupStore } from '@/stores/group'
// R118: 恢复 SSE 状态导入 —— R114 误删导致侧边栏状态区(模板 127-139 行)渲染为空白,
// 用户失去连接可见性。状态为模块级 ref, 直接导入即用。
import { sseStatus, sseRetryCount } from '@/stores/sse'
import { useNotificationStore } from '@/stores/notification'
import { accountApi } from '@/api/account'
import { settingsApi } from '@/api/settings'
import { toast } from '@/utils/toast'
import { toggleTheme, currentTheme } from '@/utils/theme'

// R126 三主题: normal(靛蓝) → eye(陶土) → dark(玻璃紫) 循环切换
const themeMode = ref(currentTheme())
const themeLabel = computed(() => ({ normal: '正常', eye: '护眼', dark: '深色' }[themeMode.value as 'normal' | 'eye' | 'dark'] || '正常'))
function toggleDarkMode() {
  themeMode.value = toggleTheme() as typeof themeMode.value
}
import MobileTabBar from '@/components/MobileTabBar.vue'
import AnnouncementPanel from '@/components/ui/AnnouncementPanel.vue'
import Modal from '@/components/ui/Modal.vue'
import NotificationPanel from '@/components/ui/NotificationPanel.vue'
import type { Component } from 'vue'
// R114 已删 ask 导入(deleteAccount 删除后零调用)

const router = useRouter()
// R86 修复: script setup 里 $route 未定义(模板全局属性只在模板可用),
// isTabActive 引用它导致登录后 Layout 渲染即抛 ReferenceError 白屏。
const route = useRoute()

// 全屏模态功能页: 返回=回退历史, 关闭=回仪表盘
function closeOverlayPage() {
  if (window.history.length > 1) router.back()
  else router.push('/dashboard')
}
function goDashboard() {
  router.push('/dashboard')
}
const authStore = useAuthStore()
const accountStore = useAccountStore()
const groupStore = useGroupStore()

// SSE 实时：账号状态变更（在线/离线/受限）即刻更新侧边栏与列表，无需轮询等待
useTaskEvents((evt: any) => {
  if (evt?.type === 'account_status') accountStore.applyStatus(evt.account_id, evt.status)
  // R99: 群同步完成 → 自动刷新该账号的群缓存(同步完群组自动出现, 无需重开选择器)
  if (evt?.type === 'groups_synced' && evt?.account_id) groupStore.refresh(evt.account_id)
})
const notifStore = useNotificationStore()

const showAnnouncements = ref(false)
const showNotifications = ref(false)
const sidebarOpen = ref(false)
const isScrolled = ref(false)
// R113 已删 avatarLoaded/currentAccId/showAccountSwitcher/accountSwitcherEl(死侧栏切换器残留)
const mainRef = ref<HTMLElement | null>(null)
// R127 iOS 橡皮筋回弹: 到顶/底继续滑动时阻尼跟手+松手弹回(安卓补体感)
useElasticScroll(mainRef)
let heartbeatInterval: number | null = null
let usageES: EventSource | null = null

// 安全获取错误消息
function getErrorMessage(e: unknown, fallback = '请稍后重试'): string {
  if (e instanceof Error) return e.message
  if (typeof e === 'string') return e
  return fallback
}
function statusDotClass(status?: string): string {
  switch (status) {
    case 'online': return 'bg-emerald dot-online'
    case 'pending_auth': return 'bg-amber'
    case 'restricted': return 'bg-rose'
    case 'banned': return 'bg-rose'
    case 'authenticated': return 'bg-cyan'
    default: return 'bg-text-muted'
  }
}
// R113 已删 statusLabel/connectAccount/disconnectAccount(死侧栏切换器孤儿, 零调用)

// 添加账号弹窗
const showAddAccount = ref(false)
const addStep = ref<'phone' | 'code' | '2fa' | 'done'>('phone')
const newPhone = ref('')

// ===== 批量登录队列: 一次粘贴多手机号, 串行逐个走完验证码/2FA 流程 =====
type QueueItem = { phone: string; accountId: string; step: 'awaiting_code' | 'awaiting_2fa' | 'done' | 'failed'; error: string }
// 动态手机号输入行: 每行自带 + 前缀, 点"添加"加一行, 比多行粘贴更直观
const phoneInputs = ref<string[]>([''])
const phoneQueue = ref<QueueItem[]>([])
const queueIndex = ref(-1)
const queueProcessing = ref(false)
const currentQueuePhone = computed(() => phoneQueue.value[queueIndex.value]?.phone || phoneInputs.value[0] || '')
const newRemark = ref('')
const authCode = ref('')
const twoFAPassword = ref('')
const adding = ref(false)
const submitting = ref(false)
const codeError = ref('')
const twoFAError = ref('')
const newAccountId = ref('')

function openAddModal() {
  showAddAccount.value = true
  addStep.value = 'phone'
  newPhone.value = ''
  newRemark.value = ''
  authCode.value = ''
  twoFAPassword.value = ''
  codeError.value = ''
  twoFAError.value = ''
  newAccountId.value = ''
}

// 批量入口: 解析多行手机号(每行一个) → 建队列 → 串行逐个发起
async function getVerificationCode() {
  const phones = phoneInputs.value.map(s => s.trim()).filter(s => /^\+?\d{6,15}$/.test(s.replace(/\s/g, '')))
  if (!phones.length) {
    toast({ type: 'error', title: '请输入有效手机号', message: '每行一个号码（+ 前缀已自带，只需填数字）；单次最多 10 个防 TG 风控' })
    return
  }
  if (phones.length > 10) {
    toast({ type: 'warning', title: '单次最多 10 个', message: '分批添加可防止批量触发 TG 风控' })
    return
  }
  phoneQueue.value = phones.map(p => ({ phone: p.startsWith('+') ? p : '+' + p.replace(/\s/g, ''), accountId: '', step: 'awaiting_code', error: '' }))
  queueIndex.value = 0
  queueProcessing.value = true
  await processQueueItem()
}

// 轮询 TG 认证阶段。connect 接口只代表"认证流程已启动"，
// 必须等后端 phase 真正进入 waiting_code(TG 已要求输入=码真的发了)
// 才能显示"已发送"，否则 TG 风控/失败时用户会看到假"已发送"并永远卡住。
async function waitForAuthPhase(accountId: string, timeoutMs: number): Promise<{ phase: string }> {
  const start = Date.now()
  const interval = 2000
  while (Date.now() - start < timeoutMs) {
    await new Promise(r => setTimeout(r, interval))
    try {
      const state = await accountApi.getAuthState(accountId)
      const phase = (state as { phase?: string }).phase || ''
      // 初始空串和 connecting 都是中间态，继续等
      if (phase && phase !== 'connecting') return { phase }
    } catch {
      // 轮询失败(如客户端刚被后端移除)按 none 处理，由调用方判失败
      return { phase: 'none' }
    }
  }
  return { phase: 'timeout' }
}

// 登录成功的最终判据：账号状态变 online。
// phase 可能因时序（认证成功→客户端清理→phase 变 none）或超时窗口太短而不可靠，
// 以账号状态为准，避免"实际已登录但弹窗卡住、需手动刷新"的假失败。
async function isAccountOnline(accountId: string): Promise<boolean> {
  try {
    await accountStore.fetchAccounts()
    const acc = (accountStore.accounts as any[]).find(a => a.account_id === accountId)
    return !!acc && acc.status === 'online'
  } catch { return false }
}

// 队列核心: 为当前手机号创建账号并发起验证码, 成功后停在 code 步骤等用户输入
async function processQueueItem() {
  const item = phoneQueue.value[queueIndex.value]
  if (!item) { finishPhoneQueue(); return }
  adding.value = true
  codeError.value = ''
  authCode.value = ''
  try {
    const acc = await accountStore.createAccount(item.phone, newRemark.value || undefined)
    item.accountId = acc.account_id
    newAccountId.value = acc.account_id
    await accountApi.connect(acc.account_id)
    const result = await waitForAuthPhase(acc.account_id, 75000)
    if (result.phase === 'waiting_code') {
      item.step = 'awaiting_code'
      addStep.value = 'code'
      startResendCountdown()
      return
    }
    if (result.phase === 'waiting_password') {
      item.step = 'awaiting_2fa'
      addStep.value = '2fa'
      return
    }
    if (result.phase === 'success' || result.phase === 'connected' || result.phase === 'online') {
      // 会话仍有效，TG 直接放行，无需验证码
      item.step = 'done'
      toast({ type: 'success', title: `${item.phone} 登录态有效`, message: '该账号会话仍有效，无需验证码' })
      nextQueueItem()
      return
    }
    throw new Error(result.phase === 'timeout'
      ? '等待验证码超时：TG 未响应，请稍后重试'
      : 'TG 未发送验证码（号码可能被风控或 API 受限），请稍后重试')
  } catch (e: unknown) {
    item.step = 'failed'
    item.error = getErrorMessage(e, '发送失败')
    toast({ type: 'error', title: `${item.phone} 发送失败`, message: item.error })
    nextQueueItem()
  } finally {
    adding.value = false
  }
}

// 当前号完成/失败后推进队列
function nextQueueItem() {
  queueIndex.value++
  if (queueIndex.value < phoneQueue.value.length) {
    processQueueItem()
  } else {
    finishPhoneQueue()
  }
}

function finishPhoneQueue() {
  queueProcessing.value = false
  addStep.value = 'phone'
  phoneInputs.value = ['']
  phoneQueue.value = []
  queueIndex.value = -1
  accountStore.fetchAccounts().then(() => { accountStore.accounts.length /* P2-48: 已由 computed 自动跟踪, 手动赋值删除 */ })
  toast({ type: 'success', title: '批量添加流程结束', message: '成功的账号已上线，失败的见提示' })
}

// 验证码重发倒计时：模板按钮禁用逻辑(322/324行)早已就绪，
// 但状态和函数此前漏写——"获取验证码"后重发按钮永远不进入倒计时态。
const resendCountdown = ref(0)
let resendTimer: ReturnType<typeof setInterval> | null = null

function startResendCountdown() {
  if (resendTimer) clearInterval(resendTimer)
  resendCountdown.value = 60
  resendTimer = setInterval(() => {
    if (resendCountdown.value > 0) {
      resendCountdown.value--
    } else if (resendTimer) {
      clearInterval(resendTimer)
      resendTimer = null
    }
  }, 1000)
}

onBeforeUnmount(() => {
  if (resendTimer) clearInterval(resendTimer)
})

async function resendCode() {
  if (!newAccountId.value) return
  try {
    try {
      await accountApi.connect(newAccountId.value)
    } catch (e: unknown) {
      // 已有认证流程在跑（连点等）不必重开，直接复用轮询即可
      if (!getErrorMessage(e).includes('already in progress')) throw e
    }
    const result = await waitForAuthPhase(newAccountId.value, 75000)
    if (result.phase === 'waiting_code') {
      startResendCountdown()
      toast({ type: 'success', title: '验证码已重新发送' })
    } else if (result.phase === 'waiting_password') {
      addStep.value = '2fa'
    } else {
      toast({ type: 'error', title: '重新发送失败', message: 'TG 未响应，请稍后重试' })
    }
  } catch (e: unknown) {
    toast({ type: 'error', title: '重新发送失败', message: getErrorMessage(e) })
  }
}

async function submitVerificationCode() {
  if (!newAccountId.value || !authCode.value) return
  submitting.value = true
  codeError.value = ''
  try {
    await accountApi.submitCode(newAccountId.value, authCode.value)
    // 轮询认证状态
    let attempts = 0
    const maxAttempts = 30
    while (attempts < maxAttempts) {
      await new Promise(r => setTimeout(r, 1000))
      const state = await accountApi.getAuthState(newAccountId.value)
      if (state.phase === 'waiting_password') {
        twoFAPassword.value = ''
        addStep.value = '2fa'
        submitting.value = false
        return
      }
      if (state.phase === 'connected') {
        showAddAccount.value = false
        addStep.value = 'phone'
        await accountStore.fetchAccounts()
        accountStore.accounts.length /* P2-48: 已由 computed 自动跟踪, 手动赋值删除 */
        toast({ type: 'success', title: '登录成功' })
        submitting.value = false
        return
      }
      // 认证成功: 后端在账号置为 online 前将 phase 置为 success
      if (state.phase === 'success' || state.phase === 'connected' || state.phase === 'online') {
        submitting.value = false
        codeError.value = ''
        if (queueProcessing.value) {
          // 队列模式: 标记完成并推进下一个(弹窗保持打开显示队列进度)
          const item = phoneQueue.value[queueIndex.value]
          if (item) item.step = 'done'
          toast({ type: 'success', title: `${currentQueuePhone.value} 验证成功` })
          nextQueueItem()
        } else {
          addStep.value = 'done'
          showAddAccount.value = false
          await accountStore.fetchAccounts()
          toast({ type: 'success', title: '账号验证成功' })
        }
        return
      }
      // 认证错误反馈: 后端重试计数 >0 = 刚才的验证码被 TG 拒绝。
      // 必须立即解锁按钮让用户重新输入 —— 之前要等 30 秒超时, 期间按钮卡死"提交中"
      if (state.phase === 'waiting_code' && (state.code_retries ?? 0) > 0) {
        submitting.value = false
        codeError.value = (state.code_retries ?? 0) >= 2
          ? '验证码错误次数过多可能已失效，请重新获取验证码'
          : '验证码错误，请重新输入'
        authCode.value = ''
        return
      }
      // phase=none: 客户端可能刚被后端移除(认证成功后的清理)。拉账号列表核实真实状态, 不再无脑判失败
      if (state.phase === 'none' || state.phase === 'failed') {
        await accountStore.fetchAccounts()
        const acc = (accountStore.accounts as any[]).find(a => a.account_id === newAccountId.value)
        if (acc && (acc.status === 'online' || acc.status === 'active')) {
          submitting.value = false
          codeError.value = ''
          addStep.value = 'done'
          showAddAccount.value = false
          toast({ type: 'success', title: '账号验证成功' })
        } else {
          codeError.value = '认证失败，请重新连接'
          submitting.value = false
          if (queueProcessing.value) {
            const item = phoneQueue.value[queueIndex.value]
            if (item) { item.step = 'failed'; item.error = '认证失败' }
            nextQueueItem()
          }
        }
        return
      }
      attempts++
    }
    codeError.value = '认证超时，请重新连接'
  } catch (e: unknown) {
    codeError.value = getErrorMessage(e, '验证码错误')
  } finally {
    submitting.value = false
  }
}

async function submit2FA() {
  if (!newAccountId.value || !twoFAPassword.value) return
  submitting.value = true
  twoFAError.value = ''
  try {
    await accountApi.submit2FA(newAccountId.value, twoFAPassword.value)
    // 提交2FA密码后，轮询认证状态
    let attempts = 0
    const maxAttempts = 30
    while (attempts < maxAttempts) {
      await new Promise(r => setTimeout(r, 1000))
      const state = await accountApi.getAuthState(newAccountId.value)
      // 后端认证成功时 phase='success'(connected 是旧值, 永不命中导致卡死)
      if (state.phase === 'success' || state.phase === 'connected' || state.phase === 'online') {
        submitting.value = false
        if (queueProcessing.value) {
          const item = phoneQueue.value[queueIndex.value]
          if (item) item.step = 'done'
          twoFAPassword.value = ''
          twoFAError.value = ''
          toast({ type: 'success', title: `${currentQueuePhone.value} 验证成功(2FA)` })
          nextQueueItem()
        } else {
          showAddAccount.value = false
          addStep.value = 'phone'
          await accountStore.fetchAccounts()
          accountStore.accounts.length /* P2-48: 已由 computed 自动跟踪, 手动赋值删除 */
          toast({ type: 'success', title: '登录成功' })
        }
        return
      }
      // 2FA 密码错误: 立即解锁按钮让用户重输(与验证码逻辑同款)
      if (state.phase === 'waiting_password' && (state.pwd_retries ?? 0) > 0) {
        submitting.value = false
        twoFAError.value = '两步验证密码错误，请重新输入'
        twoFAPassword.value = ''
        return
      }
      if (state.phase === 'failed' || state.phase === 'none') {
        // 客户端可能刚被移除(成功清理): 先核实账号真实状态再判失败
        await accountStore.fetchAccounts()
        const acc = (accountStore.accounts as any[]).find(a => a.account_id === newAccountId.value)
        if (acc && (acc.status === 'online' || acc.status === 'active')) {
          submitting.value = false
          if (queueProcessing.value) {
            const item = phoneQueue.value[queueIndex.value]
            if (item) item.step = 'done'
            twoFAPassword.value = ''
            toast({ type: 'success', title: `${currentQueuePhone.value} 验证成功` })
            nextQueueItem()
          } else {
            showAddAccount.value = false
            addStep.value = 'phone'
            accountStore.accounts.length /* P2-48: 已由 computed 自动跟踪, 手动赋值删除 */
            toast({ type: 'success', title: '登录成功' })
          }
        } else {
          twoFAError.value = '认证失败，请重新连接'
          submitting.value = false
          if (queueProcessing.value) {
            const item = phoneQueue.value[queueIndex.value]
            if (item) { item.step = 'failed'; item.error = '认证失败' }
            nextQueueItem()
          }
        }
        return
      }
      // 认证错误反馈: 后端重试计数 >0 = 刚才的两步验证密码被 TG 拒绝
      if (state.phase === 'waiting_password' && (state.pwd_retries ?? 0) > 0) {
        twoFAError.value = '两步验证密码错误，请重新输入'
        twoFAPassword.value = ''
      }
      attempts++
    }
    // 超时兜底(修复: 30s 轮询耗尽只报超时不核实结果——密码对了但认证慢时,
    // 账号实际正在上线, 应先核实账号状态再决定提示)
    await accountStore.fetchAccounts()
    const accT = (accountStore.accounts as any[]).find((a: any) => a.account_id === newAccountId.value)
    if (accT && ['online', 'active', 'authenticated'].includes(accT.status)) {
      // 实际已成功, 只是 phase 上报慢——按成功处理, 不再报超时
      showAddAccount.value = false
      addStep.value = 'phone'
      newPhone.value = ''
      newAccountId.value = ''
      twoFAPassword.value = ''
      toast({ type: 'success', title: '登录成功', message: (accT.remark || accT.phone) + ' 已上线' })
      return
    }
    twoFAError.value = '认证超时，请重新连接'
  } catch (e: unknown) {
    twoFAError.value = getErrorMessage(e, '密码错误')
  } finally {
    submitting.value = false
  }
}

// R113: 验证码弹窗状态(showAuthModalFlag/authAccount/oldAuthCode/need2FA/
// oldTwoFAPassword/authError)与 switchAccount/deleteAccount/sortedAccounts/
// statusRank 已全部删除 —— 均为死侧栏切换器的孤儿, 零调用

const unreadCount = computed(() => notifStore.unreadCount)
const unreadAnnouncements = ref(0)

// 全局菜单（未选中账号时显示）
// group: 'protocol' 的项属于「协议号系统」折叠区, 仅专业版/企业版可见(与后端 PlanID 门槛一致)
const globalMenuItems: Array<{ path: string; label: string; icon: Component; group?: string; locked?: boolean }> = [
  { path: '/dashboard', label: '仪表盘', icon: LayoutDashboard },
  { path: '/accounts', label: '账号管理', icon: Users },
  // 发现群组入口已合并至工作区"发现群组·搜群"(原全局项与 workspace 项同页双入口, 删此)
  // R87-M3: 全局"验证记录"入口删除 —— 与 workspace 监控验证组功能重叠,
  // 需要看验证日志时从「入群验证」页面进入, 侧栏不再单列陌生入口
  // 协议号导入已整合进「账号管理→添加账号→批量导入」, 侧边栏不再单列入口
  // 协议号导入/管理已整合进「账号管理」(添加账号→批量导入/协议账号视图), 侧边栏不再单列
  // { path: '/sessions', label: '协议号管理', icon: KeyRound, group: 'protocol' },
  // Phase 4: 帮助中心/设置归入"系统"组(menuSections 按 group 分组渲染)
  { path: '/help', label: '帮助中心', icon: HelpCircle, group: 'system' },
  { path: '/settings', label: '设置', icon: Settings, group: 'system' },
]

const PROTOCOL_PLAN_IDS = ['pro', 'enterprise']
const canProtocol = computed(() => PROTOCOL_PLAN_IDS.includes(authStore.tenant?.plan_id || ''))

// 侧边栏分组展开/收缩(记住状态)

function showUpgradeTip() {
  toast({ type: 'warning', title: '该功能仅限专业版/企业版', message: '请联系管理员升级套餐后使用' })
}

// 区头中文映射(原始 group 键 → 显示名)
const GROUP_LABELS: Record<string, string> = { msg: '消息', group: '群组', monitor: '监控验证', account: '账号', data: '任务数据', system: '系统' }

// 单一世界菜单: 正常号与协议号功能全部并列展示（2026-09-21 取消世界切换）
const menuSections = computed(() => {
  // 每次 map 生成全新条目对象，绝不改写 globalMenuItems 原数组里的对象：
  // ① canProtocol 依赖的 tenant 是异步加载的，首次计算时 plan_id 可能还没回来
  //    → 误判锁上 → 锁痕迹留在原对象上 → 套餐正确也"永久锁"（表现为专业版仍锁着）；
  // ② tenant 加载完成后 computed 重算，新对象上的 locked 会随 canProtocol 自动纠正。
  const items = globalMenuItems.map(item => ({
    ...item,
    locked: item.path === '/sessions' ? !canProtocol.value : false,
  }))
  // Phase 4 信息架构: 全局菜单分 2 组——主导航(顶格无组标题, 功能入口) +
  // 系统组(带"系统"区头与缩进, 低频入口)。组标签经 GROUP_LABELS 渲染。
  const main = items.filter((i: any) => (i.group || 'main') !== 'system')
  const sys = items.filter((i: any) => (i.group || 'main') === 'system')
  const sections: Array<{ label: string; items: any[] }> = [{ label: '', items: main }]
  if (sys.length) sections.push({ label: 'system', items: sys })
  return sections
})

// 锁定的协议号管理：拦截跳转并提示升级
function onGlobalItemClick(item: any, evt: MouseEvent) {
  if (item.locked) {
    evt.preventDefault()
    toast({ type: 'warning', title: '协议号系统已锁定', message: '仅限专业版/企业版套餐，请联系管理员升级' })
    return
  }
  closeSidebarOnMobile()
}

// Phase 7 移动端底部 Tab: 主导航 4 项 + 高亮匹配(工作区页归入"任务")
const mobileTabs = [
  { path: '/dashboard', label: '仪表盘', icon: LayoutDashboard },
  { path: '/accounts', label: '账号', icon: Users, match: ['/accounts', '/workspace'] },
  { path: '/workspace', label: '任务', icon: ListTodo, match: ['/workspace', '/tasks'] },
  { path: '/settings', label: '设置', icon: Settings },
]
function isTabActive(t: any) {
  const m: string[] = t.match || [t.path]
  return m.some((p: string) => route.path.startsWith(p))
}

// P2-67 审计修复: visibleGlobalMenu 已死——模板零引用(侧边栏实际渲染 menuSections),
// 定义却被留下。删除。模板需要"过滤协议号项+分组区头"的能力时从 menuSections 处补。
// (原实现的区头中文映射若被 menuSections 需要, 逻辑在 menuSections 内已有——以模板引用为准)

// 账号工作区菜单（始终显示，没选账号时禁用）
const workspaceMenuItems = computed(() => {
  const accId = accountStore.currentAccount?.account_id || 'none'
  const pf = authStore.tenant?.plan_features
  const hasAccount = !!accountStore.currentAccount
  return [
    // 锁语义统一为"严格模式": pf 里该 key 不为 true 即锁(FfeatureMap 只含勾选项,
    // 未勾=key 不存在; 旧的 `=== false` 判断对不存在恒假导致不锁 —— 回归修复)
    // 分组重排(改进#2): 消息 / 群组 / 监控验证 / 账号 / 任务数据, 共 16 项
    { path: `/workspace/${accId}/broadcast`, label: '群发消息', icon: MessageSquare, disabled: !hasAccount || !pf?.broadcast, feature: 'broadcast', group: 'msg' },
    // P1-50 审计修复: 消息模板加 Tab 锚点(设置页已拆 5 Tab, 直接落到模板面板);
    // 文案变量入口删除——群发页已内置变量插入(msgVars), 再开一个跳设置页的
    // 入口用户点进去找不到对应功能(设置页无文案变量区块), 是纯死路。
    // R99: 消息模板入口删除 —— 功能已整合进设置页模板 Tab, 侧栏不再单列
    // R118: 加群/邀请合并为单入口"进群邀请"(四字) —— 两功能本就在同一页双 Tab,
    // 侧边栏不再占两个位; 权限取并集(任一可用即可进入, 页内 Tab 各自锁权限)
    { path: `/workspace/${accId}/join`, label: '进群邀请', icon: UserPlus, disabled: !hasAccount || !(pf?.join_groups || pf?.invite), feature: 'join_groups', group: 'group' },
    { path: '/groups/discover', label: '找群搜群', icon: Compass, disabled: !hasAccount || !(pf?.discover === true || pf?.search === true), feature: 'discover', group: 'group' },  // R99: 四字对齐, 原名"发现群组·搜群"过长
    { path: `/workspace/${accId}/scrape`, label: '成员采集', icon: Download, disabled: !hasAccount || !pf?.scrape_members, feature: 'scrape_members', group: 'group' },
    { path: `/workspace/${accId}/scrape-links`, label: '链接采集', icon: Link, disabled: !hasAccount || !pf?.scrape_links, feature: 'scrape_links', group: 'group' },
    { path: `/workspace/${accId}/monitor`, label: '消息监控', icon: Eye, disabled: !hasAccount || !pf?.monitor, feature: 'monitor', group: 'monitor' },
    // R99: 入群验证入口删除 —— 开关在设置页, 侧栏不再单列
    { path: '/raffle', label: '抽奖助手', icon: Gift, disabled: pf?.raffle !== true, feature: 'raffle', group: 'monitor' },
    { path: `/workspace/${accId}/tasks`, label: '任务列表', icon: ListTodo, disabled: !hasAccount, feature: '', group: 'data' },
  ]
})

// 工作区分组折叠状态(默认全收起, 点标题展开)


// 按 消息/增长/数据 分组渲染(空组不显示)
// 拍平：消息/增长/数据分组头移除，功能项全部一级常显
const GROUP_ORDER = ['account', 'msg', 'group', 'monitor', 'data']  // 账号组紧跟账号管理(用户操作直觉)
const workspaceGroups = computed(() => {
  const groups: Array<{ label: string; items: any[] }> = []
  for (const g of GROUP_ORDER) {
    const items = workspaceMenuItems.value.filter((i: any) => i.group === g)
    if (items.length) groups.push({ label: GROUP_LABELS[g] || g, items })
  }
  return groups
})

function handleMenuClick(item: any) {
  if (item.disabled) {
    if (!accountStore.currentAccount) {
      toast({ type: 'error', title: '请先选择账号', message: '请先添加或选择一个账号后再使用此功能' })
      return
    }
    const featureNames: Record<string, string> = {
      broadcast: '群发消息',
      scrape_members: '成员采集',
      scrape_links: '链接采集',
      join_groups: '加群任务',
      monitor: '消息监控',
      scheduled_tasks: '定时任务',
      invite: '邀请入群',
      search: '关键词搜群',
      templates: '消息模板',
      dict: '文案变量',
      export: '数据导出',
      checkup: '账号检测',
      // P2-73: 补 discover/loop_broadcast/api 三个套餐管理里可勾、但此处缺中文
      // 映射的 key——否则用户点群库/循环群发时提示笼统的"该功能"
      discover: '发现群组·群库',
      loop_broadcast: '循环群发',
      api: 'API 接入',
      raffle: '抽奖助手',
      proxy: '代理设置',
      verifier: '入群验证',
      ai_verify: 'AI 图片验证',
    }
    toast({ type: 'error', title: '功能未开通', message: `当前套餐不支持${featureNames[item.feature] || '该功能'}，请联系客服升级套餐` })
    return
  }
  router.push(item.path)
  closeSidebarOnMobile()
}

// P2-67 审计修复: bottomNavItems 模板零引用(移动端底部导航已改走 menuSections 方案),
// 死代码删除。

const planName = computed(() => {
  const m: Record<string, string> = {
    free: '免费版',
    basic: '基础版',
    pro: '专业版',
    enterprise: '企业版'}
  return m[authStore.tenant?.plan_id || 'free'] || '免费版'
})

const expiryText = computed(() => {
  const expires = authStore.tenant?.expires_at
  if (!expires) return '永久有效'
  const d = new Date(expires)
  if (isNaN(d.getTime())) return '永久有效'
  return d.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' })
})

const isExpiringSoon = computed(() => {
  const expires = authStore.tenant?.expires_at
  if (!expires) return false
  const d = new Date(expires)
  if (isNaN(d.getTime())) return false
  // 7天内到期标红
  return d.getTime() - Date.now() < 7 * 24 * 60 * 60 * 1000
})

// P2-48: 原 accountCount 是手动 ref(0) 在 10 处赋值——任何一处遗漏(其他组件删号、
// 轮询路径)进度条就不刷新。改 computed 跟随 accountStore.accounts 一处真相。
const accountCount = computed(() => accountStore.accounts.length)
const maxAccounts = computed(() => authStore.tenant?.max_accounts || 1)
const accountUsage = computed(() => accountCount.value / maxAccounts.value)

function closeSidebarOnMobile() {
  if (window.innerWidth < 768) sidebarOpen.value = false
}

function logout() {
  authStore.logout()
  router.push('/login')
}

// 滚动监听：苹果风格导航栏变化
function onScroll() {
  const el = mainRef.value
  if (!el) return
  isScrolled.value = el.scrollTop > 10
}

// R113 已删 onGlobalClick(唯一职责是关闭已删除的账号切换器)

onMounted(async () => {
  mainRef.value?.addEventListener('scroll', onScroll, { passive: true })
  onScroll()
  // 全局用量通知监听：每100条一次，只提示不限制
  // F-20: token 仅存内存（路由守卫已在此之前完成会话恢复）
  const token = useAuthStore().token
  if (token) {
    // 审计P0-03: 短期ticket替代URL里的JWT
    let usageTicket = ''
    // P2-04: 原硬编码 /api/v1——前端与 API 不同域名部署时 SSE 连错地址。
    // 与 request.ts 同源: 用 VITE_API_BASE_URL, 缺省回退 /api/v1。
    const apiBase = import.meta.env.VITE_API_BASE_URL || '/api/v1'
    try {
      const tres2 = await fetch(`${apiBase}/events/ticket`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } })
      usageTicket = (await tres2.json()).ticket || ''
    } catch { /* 静默 */ }
    usageES = new EventSource(`${apiBase}/events?ticket=${encodeURIComponent(usageTicket)}`)
    // P0-06 审计修复: 后端发的是命名事件 event: events, onmessage 只收无事件名
    // 默认消息——usage_milestone/内存告警等系统提示从未送达。改 addEventListener。
    usageES.addEventListener('events', (ev: MessageEvent) => {
      try {
        const d = JSON.parse(ev.data)
        // 系统资源告警与自适应档位（此前后端发进无人订阅的 topic，从未到达；
        // 修复 SSE 广播后这些事件真实送达，此处统一弹 toast）
        const p = d.payload || {}
        // P1-20: 通知面板接线——系统事件同步进通知中心(原 addNotification 全项目
        // 零调用, 面板永远空)。心跳类不落通知; 心跳外的真实事件都会成为面板内容。
        if (d.type !== 'heartbeat' && d.type !== 'pong') {
          notifStore.addNotification({
            notification_id: '',
            tenant_id: authStore.tenant?.tenant_id || '',
            type: d.type,
            title: d.title || d.type,
            message: String(d.message || p.message || ''),
            is_read: false,
          })
        }
        switch (d.type) {
          case 'usage_milestone':
            toast({ type: 'info', title: '发送量提醒', message: d.message || p.message || `今日已发送 ${d.used_today || p.used_today} 条` })
            break
          case 'memory_high':
            toast({ type: 'warning', title: '内存占用过高', message: p.message || `当前 ${p.value ?? '?'} MB，建议降低并发或重启` })
            break
          case 'queue_deep':
            toast({ type: 'warning', title: '任务队列堆积', message: p.message || `当前 ${p.value ?? '?'} 个任务排队中` })
            break
          case 'db_pool_high':
            toast({ type: 'warning', title: '数据库连接池紧张', message: p.message || `当前 ${p.value ?? '?'} 个连接` })
            break
          case 'adaptive_mode':
            toast({ type: 'info', title: '并发档位已调整', message: p.message || `当前模式: ${p.mode ?? 'unknown'}` })
            break
          case 'task_split':
            toast({ type: 'info', title: '任务已自动拆分', message: p.message || `拆分为 ${p.total_batches ?? '?'} 个子任务` })
            break
        }
      } catch { /* 非JSON消息忽略 */ }
    })
  }
  // 刷新租户信息 + 拉取账号列表: 两者无依赖, 并行发起省一次串行等待
  await Promise.all([
    authStore.refreshTenant(),
    (async () => {
      try {
        await accountStore.fetchAccounts()
        accountStore.accounts.length /* P2-48: 已由 computed 自动跟踪, 手动赋值删除 */
      } catch (e) {
        console.error('获取账号数量失败:', e)
      }
    })(),
    // 公告主动触达: 登录后检查是否有未提示过的公告, 有则自动弹出
    checkAnnouncements(),
  ])
  // 定时状态轮询：每 60 秒检查租户状态，被停用或过期自动退出
  let hbCount = 0
  heartbeatInterval = window.setInterval(async () => {
    try {
      await authStore.refreshTenant()
      // 精读修复: 必须 tenant 已加载且明确非 active 才踢 —— 原写法在 tenant
      // 未加载(undefined)瞬间误判为「已停用」把用户踢下线
      if (authStore.tenant && authStore.tenant.status !== 'active') {
        toast({ type: 'error', title: '账号已停用', message: '您的账号已被管理员停用' })
        authStore.logout()
        router.push('/login')
      }
    } catch {
      // 网络错误忽略，下次轮询再试
    }
    // N-02: 公告在线触达。原实现仅登录时拉取一次，长会话收不到新公告；
    // 借心跳每 5 分钟轮询一次（公告低频，成本可忽略）
    if (++hbCount % 5 === 0) checkAnnouncements()
  }, 60000)
})

// 登录后公告主动弹出: 有后端公告且本地未提示过(localStorage 按公告ID去重)则自动打开面板
async function checkAnnouncements() {
  try {
    const res = await settingsApi.getAnnouncements()
    const list: Array<{ announcement_id: string }> = Array.isArray(res) ? res : []
    if (!list.length) return
    let seen: string[] = []
    try { seen = JSON.parse(localStorage.getItem('ann_seen') || '[]') } catch { seen = [] }
    const fresh = list.filter(a => !seen.includes(a.announcement_id))
    if (fresh.length) {
      showAnnouncements.value = true
      localStorage.setItem('ann_seen', JSON.stringify([...seen, ...fresh.map(a => a.announcement_id)]))
    }
  } catch { /* 公告拉取失败静默, 不阻塞登录 */ }
}

onUnmounted(() => {
  mainRef.value?.removeEventListener('scroll', onScroll)
  // R113: onGlobalClick 监听器随函数一并删除
  if (heartbeatInterval) clearInterval(heartbeatInterval)
  usageES?.close()
})
</script>
