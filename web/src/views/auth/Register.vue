<template>
  <div class="min-h-[100dvh] bg-bg-secondary flex items-center justify-center px-4">
    <div class="w-full max-w-full max-w-[380px] animate-fade-in-scale">
      <!-- Logo -->
      <div class="text-center mb-10">
        <div class="w-16 h-16 rounded-[var(--t-r-lg)] bg-cyan-glow/50 flex items-center justify-center mx-auto mb-5">
          <Send class="w-8 h-8 text-cyan" stroke-width="1.5" />
        </div>
        <h1 class="text-3xl font-semibold text-text-primary tracking-tight mb-2">注册账号</h1>
        <p class="text-base text-text-secondary">创建您的 博士轩群发器 账号</p>
      </div>

      <!-- 注册卡片 -->
      <div class="glass-strong rounded-[var(--t-r-lg)] p-8 border border-border">
        <form @submit.prevent="handleRegister" class="space-y-5">
          <div>
            <label class="block text-sm font-medium text-text-primary mb-2">用户名</label>
            <input
              v-model="form.username"
              type="text"
              placeholder="请输入用户名"
              class="w-full bg-bg-card rounded-[var(--t-r-md)] px-4 py-3.5 text-text-primary placeholder:text-text-muted border border-transparent focus:outline-none focus:border-cyan/50 focus:ring-2 focus:ring-cyan/10 transition-[transform,opacity,border-color,background-color] duration-200"
              required aria-label="请输入用户名" />
          </div>

          <div>
            <label class="block text-sm font-medium text-text-primary mb-2">昵称</label>
            <input
              v-model="form.name"
              type="text"
              placeholder="请输入昵称"
              class="w-full bg-bg-card rounded-[var(--t-r-md)] px-4 py-3.5 text-text-primary placeholder:text-text-muted border border-transparent focus:outline-none focus:border-cyan/50 focus:ring-2 focus:ring-cyan/10 transition-[transform,opacity,border-color,background-color] duration-200"
              required aria-label="请输入昵称" />
          </div>

          <div>
            <label class="block text-sm font-medium text-text-primary mb-2">密码</label>
            <div class="relative">
              <input
                v-model="form.password"
                :type="showPassword ? 'text' : 'password'"
                placeholder="请输入密码"
                class="w-full bg-bg-card rounded-[var(--t-r-md)] px-4 py-3.5 pr-12 text-text-primary placeholder:text-text-muted border border-transparent focus:outline-none focus:border-cyan/50 focus:ring-2 focus:ring-cyan/10 transition-[transform,opacity,border-color,background-color] duration-200"
                required aria-label="请输入密码" />
              <button
                type="button"
                @click="showPassword = !showPassword"
                class="absolute right-3.5 top-1/2 -translate-y-1/2 text-text-muted hover:text-text-primary transition-colors"
              >
                <Eye v-if="showPassword" class="w-5 h-5" stroke-width="1.5" />
                <EyeOff v-else class="w-5 h-5" stroke-width="1.5" />
              </button>
            </div>
          </div>

          <div>
            <label class="block text-sm font-medium text-text-primary mb-2">确认密码</label>
            <div class="relative">
              <input
                v-model="form.confirmPassword"
                :type="showConfirmPassword ? 'text' : 'password'"
                placeholder="请再次输入密码"
                class="w-full bg-bg-card rounded-[var(--t-r-md)] px-4 py-3.5 pr-12 text-text-primary placeholder:text-text-muted border border-transparent focus:outline-none focus:border-cyan/50 focus:ring-2 focus:ring-cyan/10 transition-[transform,opacity,border-color,background-color] duration-200"
                required aria-label="请再次输入密码" />
              <button
                type="button"
                @click="showConfirmPassword = !showConfirmPassword"
                class="absolute right-3.5 top-1/2 -translate-y-1/2 text-text-muted hover:text-text-primary transition-colors"
              >
                <Eye v-if="showConfirmPassword" class="w-5 h-5" stroke-width="1.5" />
                <EyeOff v-else class="w-5 h-5" stroke-width="1.5" />
              </button>
            </div>
          </div>

          <div>
            <label class="block text-sm font-medium text-text-primary mb-2">
              卡密 <span class="text-rose">*</span>
            </label>
            <input
              v-model="form.code"
              type="text"
              placeholder="请输入卡密"
              class="w-full bg-bg-card rounded-[var(--t-r-md)] px-4 py-3.5 text-text-primary placeholder:text-text-muted border border-transparent focus:outline-none focus:border-cyan/50 focus:ring-2 focus:ring-cyan/10 transition-[transform,opacity,border-color,background-color] duration-200"
              required aria-label="请输入卡密" />
            <p class="mt-1.5 text-xs text-text-secondary">没有卡密？请联系管理员获取</p>
          </div>

          <button
            type="submit"
            :disabled="loading"
            class="w-full apple-button py-3.5 text-base disabled:opacity-50"
            :class="[regFailed ? '!bg-rose !border-rose shake-x' : '']"
          >
            {{ loading ? '注册中...' : '注册' }}
          </button>
        </form>

        <div v-if="error" class="mt-4 p-3.5 bg-rose/5 border border-rose/10 rounded-[var(--t-r-md)] text-rose text-sm text-center">
          {{ error }}
        </div>

        <div v-if="success" class="mt-4 p-3.5 bg-emerald/5 border border-emerald/10 rounded-[var(--t-r-md)] text-emerald text-sm text-center">
          注册成功，3 秒后自动跳转登录页
        </div>

        <div class="mt-6 text-center text-sm text-text-secondary max-w-[65ch]">
          已有账号？
          <router-link to="/login" class="text-cyan hover:text-cyan font-medium transition-colors">
            立即登录
          </router-link>
        </div>
      </div>

      <p class="text-center text-text-muted text-xs mt-8">
        博士轩群发器 v{{ appVersion }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { Send, Eye, EyeOff } from 'lucide-vue-next'
import { authApi } from '@/api/auth'

const router = useRouter()
const appVersion = __APP_VERSION__ || '3.0.0'
const loading = ref(false)
const regFailed = ref(false)
const error = ref('')
const success = ref(false)
let redirectTimer: ReturnType<typeof setTimeout> | null = null
const showPassword = ref(false)
const showConfirmPassword = ref(false)

onUnmounted(() => {
  if (redirectTimer) {
    clearTimeout(redirectTimer)
  }
})

const form = reactive({
  username: '',
  name: '',
  password: '',
  confirmPassword: '',
  code: '',
})

async function handleRegister() {
  if (form.password !== form.confirmPassword) {
    error.value = '两次输入的密码不一致'
    return
  }
  loading.value = true
  error.value = ''
  success.value = false
  try {
    await authApi.register({
      username: form.username,
      name: form.name,
      password: form.password,
      code: form.code,
    })
    success.value = true
    // 3秒后自动跳转到登录页
    redirectTimer = setTimeout(() => {
      router.push('/login')
    }, 3000)
  } catch (err: unknown) {
    error.value = (err instanceof Error ? err.message : String(err)) || '注册失败，请稍后重试'
    regFailed.value = true
    setTimeout(() => { regFailed.value = false }, 2000)
  } finally {
    loading.value = false
  }
}
</script>
