<template>
  <div class="min-h-dvh flex items-center justify-center relative overflow-hidden">
    <!-- R87-M7: 网格背景已移除 -->

    <div class="relative z-10 w-full max-w-md px-6">
      <div class="text-center mb-8">
        <div class="w-16 h-16 rounded-[var(--t-r-lg)] bg-cyan/10 border border-cyan/20 flex items-center justify-center mx-auto mb-4">
          <Shield class="w-8 h-8 text-cyan" />
        </div>
        <h1 class="text-2xl font-bold tracking-tight">管理员登录</h1>
        <p class="text-text-secondary mt-2">博士轩群发器 管理后台</p>
      </div>

      <div class="glass rounded-[var(--t-r-lg)] p-8 gradient-border">
        <div class="space-y-4">
          <div>
            <div class="text-sm text-text-secondary mb-1.5">用户名</div>
            <t-input
              v-model="username"
              size="large"
              placeholder="请输入管理员用户名"
              clearable
              autofocus
              @keyup="handleKeyup" />
            <!-- P1-30: 原 errors 只在 script 赋值, 模板从不显示——校验失败点登录毫无反馈 -->
            <div v-if="errors.username" class="text-xs text-danger mt-1">{{ errors.username }}</div>
          </div>
          <div>
            <div class="text-sm text-text-secondary mb-1.5">密码</div>
            <t-input
              v-model="password"
              size="large"
              :type="showPassword ? 'text' : 'password'"
              placeholder="请输入密码"
              clearable
              @keyup="handleKeyup">
              <template #suffix>
                <button
                  type="button"
                  @click="showPassword = !showPassword"
                  class="text-text-muted hover:text-text-primary transition-colors"
                  :aria-label="showPassword ? '隐藏密码' : '显示密码'">
                  <Eye v-if="!showPassword" class="w-4 h-4" />
                  <EyeOff v-else class="w-4 h-4" />
                </button>
              </template>
            </t-input>
          </div>
          <p v-if="errorMsg" class="text-sm text-rose">{{ errorMsg }}</p>
          <t-button theme="primary"
            class="w-full"
            :disabled="loading"
            @click="handleLogin"
          >
            <Loader2 v-if="loading" class="w-4 h-4 animate-spin" />
            <span>{{ loading ? '登录中...' : '登录' }}</span>
          </t-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
// R87-M7: DynamicBackground 已移除
import { useRouter } from 'vue-router'
import { Shield, Loader2, Eye, EyeOff } from 'lucide-vue-next'
import { useAdminStore } from '@/stores/admin'

const router = useRouter()
const adminStore = useAdminStore()

const username = ref('')
const password = ref('')
const loading = ref(false)
const errorMsg = ref('')
const showPassword = ref(false)
const errors = ref<Record<string, string>>({})

function validateForm(): boolean {
  errors.value = {}
  if (!username.value.trim()) errors.value.username = '请输入用户名'
  if (!password.value.trim()) errors.value.password = '请输入密码'
  return Object.keys(errors.value).length === 0
}

// TDesign Input 的 keyup emit 签名是 (value, {e}) —— 第一个参数是字符串。
// 直接用 @keyup.enter 修饰符, Vue 3.5 的 withKeys 会对字符串执行 "key" in value 而崩溃。
// 必须手动从 context.e 取原生 KeyboardEvent 判断按键。
function handleKeyup(_value: string, ctx?: { e?: KeyboardEvent }) {
  if (ctx?.e?.key === 'Enter') handleLogin()
}

async function handleLogin() {
  if (!validateForm()) return
  loading.value = true
  errorMsg.value = ''
  try {
    await adminStore.login(username.value, password.value)
    router.push('/admin/dashboard')
  } catch (e: unknown) {
    errorMsg.value = (e instanceof Error ? e.message : '登录失败，请重试')
  } finally {
    loading.value = false
  }
}
</script>
