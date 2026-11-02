<template>
  <!-- R124 推倒重做(示例1 语言): hero 大标题双栏 + 右侧登录卡。script 逻辑原封保留。 -->
  <div class="min-h-[100dvh] grid lg:grid-cols-[1fr_430px] gap-8 lg:gap-[6vw] items-center px-5 sm:px-8 lg:px-[5vw] py-10 max-w-[1400px] mx-auto">

    <!-- Hero 区(移动端隐藏标语排版, 但保留品牌感) -->
    <div class="hidden lg:block">
      <div class="w-16 h-16 rounded-[22px] grid place-items-center text-white text-2xl font-black"
           style="background: var(--t-brand); box-shadow: 0 12px 36px color-mix(in srgb, var(--t-brand) 30%, transparent);">➤</div>
      <div class="mt-7 text-[11px] font-extrabold tracking-[0.14em] uppercase" style="color: var(--t-brand);">TG SENDER · CONTROL CENTER</div>
      <h1 class="mt-4 text-[clamp(38px,4.5vw,64px)] font-black leading-[1.06] tracking-[-0.05em]">
        把 Telegram<br><span style="color: var(--t-brand);">运营效率</span>交还给你。
      </h1>
      <p class="mt-5 text-[16px] leading-relaxed max-w-[520px]" style="color: var(--t-text-2);">
        账号、群发、采集、任务与监控，一处掌控。从手机体验出发的轻量界面，触控友好、卡片柔和。
      </p>
      <div class="mt-7 flex gap-2.5 flex-wrap">
        <span v-for="f in ['✦ 多账号工作区', '↗ 任务实时状态', '◉ 操作记录可查']" :key="f"
              class="px-3 py-2 rounded-full text-[12px] font-semibold"
              style="border: 1px solid var(--t-line); color: var(--t-text-3);">{{ f }}</span>
      </div>
    </div>

    <!-- 登录卡 -->
    <div class="w-full max-w-[420px] mx-auto lg:mx-0 animate-fade-in-scale">
      <!-- R124 移动端 hero: 图标+博士轩同行 / 标题一行全中文无句号 / 全部居中 -->
      <div class="lg:hidden mb-9 text-center">
        <div class="flex items-center justify-center gap-2.5">
          <div class="w-11 h-11 rounded-2xl grid place-items-center text-white text-lg font-black"
               style="background: var(--t-brand); box-shadow: 0 10px 28px color-mix(in srgb, var(--t-brand) 32%, transparent);">➤</div>
          <span class="text-[19px] font-black tracking-tight">博士轩TG助手</span>
          <!-- R130 版本徽章(令牌色): 一眼确认部署的包版本 -->
          <span class="px-1.5 py-0.5 rounded-md text-[10px] font-bold"
                style="background: var(--t-brand-dim); color: var(--t-brand);">{{ BUILD_TAG }}</span>
        </div>
        <h1 class="mt-5 text-[26px] font-black leading-tight tracking-[-0.03em] whitespace-nowrap">
          把<span style="color: var(--t-brand);">营销效率</span>还给你
        </h1>
        <p class="mt-2.5 text-[13.5px] leading-relaxed" style="color: var(--t-text-2);">
          账号、群发、采集、任务与监控，一处掌控
        </p>
        <div class="mt-4 flex gap-2 flex-wrap justify-center">
          <span v-for="f in ['✦ 多账号工作区', '↗ 任务实时状态', '◉ 操作记录可查']" :key="f"
                class="px-2.5 py-1.5 rounded-full text-[11px] font-semibold"
                style="border: 1px solid var(--t-line); color: var(--t-text-3);">{{ f }}</span>
        </div>
      </div>

      <div class="rounded-[24px] p-7 sm:p-8"
           style="background: var(--t-surface-1); border: 1px solid var(--t-line); box-shadow: var(--t-shadow-card);">
        <div class="text-[10px] font-extrabold tracking-[0.14em] uppercase" style="color: var(--t-brand);">WELCOME BACK</div>
        <h2 class="mt-1.5 text-[24px] font-black tracking-tight">登录工作台</h2>
        <p class="mt-1 text-[13px]" style="color: var(--t-text-3);">使用租户账号进入控制台</p>

        <form @submit.prevent="handleLogin" class="mt-6 grid gap-4">
          <div class="grid gap-1.5">
            <label class="text-[12px] font-bold" style="color: var(--t-text-3);">用户名</label>
            <!-- R140: 与密码框统一纯 CSS focus(品牌紫描边+柔光), 删除 JS 内联焦点方案 -->
            <input v-model="form.username" autocomplete="username" required
                   class="w-full rounded-xl px-4 py-3 text-[16px] outline-none transition-all focus:border-[var(--t-brand)] focus:shadow-[0_0_0_3px_color-mix(in_srgb,var(--t-brand)_15%,transparent)]"
                   style="background: var(--t-surface-2); border: 1px solid var(--t-line); color: var(--t-text-1);"
                   placeholder="请输入用户名" />
          </div>
          <div class="grid gap-1.5">
            <label class="text-[12px] font-bold" style="color: var(--t-text-3);">密码</label>
            <div class="relative">
              <input v-model="form.password" :type="showPassword ? 'text' : 'password'"
                     autocomplete="current-password" required
                     class="w-full rounded-xl px-4 py-3 pr-11 text-[16px] outline-none transition-all focus:border-[var(--t-brand)] focus:shadow-[0_0_0_3px_color-mix(in_srgb,var(--t-brand)_15%,transparent)]"
                     style="background: var(--t-surface-2); border: 1px solid var(--t-line); color: var(--t-text-1);"
                     placeholder="请输入密码" @keyup.enter="handleLogin" />
              <button type="button" @click="showPassword = !showPassword"
                      class="absolute right-3 top-1/2 -translate-y-1/2 transition-colors"
                      style="color: var(--t-text-3);">
                <Eye v-if="showPassword" class="w-[18px] h-[18px]" />
                <EyeOff v-else class="w-[18px] h-[18px]" />
              </button>
            </div>
          </div>

          <button type="submit" :disabled="loading"
                  class="mt-1 w-full rounded-xl py-3.5 text-[15px] font-extrabold text-white active:scale-[0.98] transition-transform disabled:opacity-60"
                  :style="{ background: loginFailed ? 'var(--t-err)' : 'var(--t-brand)' }">
            <!-- R140 用户拍板: 删除转圈加载动画, 保留"登录中…"文字反馈 -->
            {{ loading ? '登录中…' : '登录并进入工作台 →' }}
          </button>
        </form>

        <div v-if="error" class="mt-4 p-3.5 rounded-xl text-[13px] text-center"
             style="background: color-mix(in srgb, var(--t-err) 10%, transparent); color: var(--t-err); border: 1px solid color-mix(in srgb, var(--t-err) 25%, transparent);">
          {{ error }}
        </div>

        <!-- 订阅过期自助续费(逻辑原样保留) -->
        <div v-if="isExpired" class="mt-4 rounded-xl p-4 grid gap-3"
             style="background: color-mix(in srgb, var(--t-warn) 10%, transparent); border: 1px solid color-mix(in srgb, var(--t-warn) 30%, transparent);">
          <div class="text-[13px] text-center" style="color: var(--t-warn);">订阅已到期，输入卡密立即续费</div>
          <input v-model="renewCode" type="text" placeholder="输入续费卡密"
                 class="w-full rounded-lg px-4 py-2.5 text-[13.5px] outline-none"
                 style="background: var(--t-surface-2); border: 1px solid var(--t-line); color: var(--t-text-1);"
                 @keyup.enter="doRenew" />
          <button type="button" :disabled="renewing || !renewCode.trim()"
                  class="w-full rounded-lg py-2.5 text-[13.5px] font-bold text-white disabled:opacity-50"
                  style="background: var(--t-warn);"
                  @click="doRenew">{{ renewing ? '续费中…' : '立即续费并登录' }}</button>
          <div v-if="renewMsg" class="text-xs text-center" :style="{ color: renewOk ? 'var(--t-ok)' : 'var(--t-err)' }">{{ renewMsg }}</div>
        </div>

        <div class="mt-6 pt-5 flex items-center justify-between text-[13.5px]"
             style="border-top: 1px solid var(--t-line-light);">
          <span style="color: var(--t-text-3);">还没有账号？</span>
          <router-link to="/register" class="font-bold" style="color: var(--t-brand);">立即注册 →</router-link>
        </div>
      </div>

      <!-- R130 底部版本行已删: 版本号并入品牌区徽章(跟主题+一眼可见) -->

    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { authApi } from '@/api/auth'
import { Eye, EyeOff } from 'lucide-vue-next'
import { BUILD_TAG } from '@/build-tag'
import { toast } from '@/utils/toast'

const router = useRouter()
const authStore = useAuthStore()

// R130: 版本号移至品牌区徽章(BUILD_TAG), appVersion 不再使用

const loading = ref(false)
const loginFailed = ref(false)
const error = ref('')
const showPassword = ref(false)

const form = reactive({
  username: '',
  password: '',
})

// R140: inputFocus/inputBlur 已删(JS 焦点方案与用户框/密码框纯 CSS focus 统一)

async function handleLogin() {
  if (!form.username || !form.password) {
    error.value = '请输入用户名和密码'
    return
  }

  loading.value = true
  error.value = ''

  try {
    await authStore.login(form.username, form.password)
    router.push('/').finally(() => {
      toast({ type: 'success', message: '登录成功，欢迎回来' })
    })
  } catch (err: unknown) {
    error.value = (err instanceof Error ? err.message : String(err)) || '登录失败，请稍后重试'
    loginFailed.value = true
    setTimeout(() => { loginFailed.value = false }, 2000)
  } finally {
    loading.value = false
  }
}

// ===== 订阅过期自助续费(逻辑原样保留) =====
const renewCode = ref('')
const renewing = ref(false)
const renewMsg = ref('')
const renewOk = ref(false)
const isExpired = computed(() =>
  !!error.value && (error.value.includes('expired') || error.value.includes('过期')))

async function doRenew() {
  const code = renewCode.value.trim()
  if (!code || renewing.value) return
  renewing.value = true
  renewMsg.value = ''
  try {
    await authApi.renew(form.username, form.password, code)
    renewOk.value = true
    renewMsg.value = '续费成功，正在登录…'
    setTimeout(() => handleLogin(), 800)
  } catch (err: unknown) {
    renewOk.value = false
    renewMsg.value = (err instanceof Error ? err.message : String(err)) || '续费失败，请检查卡密'
  } finally {
    renewing.value = false
  }
}
</script>
