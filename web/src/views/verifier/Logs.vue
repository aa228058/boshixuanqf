<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between flex-wrap gap-3">
      <div>
        <h2 class="text-lg font-bold text-text-primary">验证记录</h2>
        <p class="text-xs text-text-secondary mt-1">入群验证的自动处理记录：通过的、失败的、以及为什么停止（付费验证/外部链接等）。</p>
      </div>
      <t-button size="small" variant="outline" @click="load">刷新</t-button>
    </div>

    <div v-if="!list.length && !loading" class="glass rounded-[var(--t-r-md)] p-12 text-center text-text-muted text-sm lift">
      还没有验证记录——在设置里打开「入群验证」开关，账号加群遇到验证时会自动处理并记录在这里。
    </div>

    <div v-else class="space-y-2">
      <div v-for="v in list" :key="v.vl_id" class="glass rounded-[var(--t-r-md)] p-4 lift">
        <div class="flex items-center gap-3 flex-wrap">
          <!-- R128 徽章统一: 圆点+胶囊(示例2 badge 语言) -->
          <span class="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-bold whitespace-nowrap"
            :class="v.status === 'pass' ? 'bg-emerald/10 text-emerald' : v.status === 'fail' ? 'bg-rose/10 text-rose' : v.status === 'stopped' ? 'bg-amber/10 text-amber' : 'bg-bg-card text-text-secondary'">
            <i class="w-1.5 h-1.5 rounded-full bg-current opacity-80"></i>{{ statusLabel(v.status) }}
          </span>
          <span class="text-sm text-text-primary">{{ v.group_title || ('群 ' + v.group_chat_id) }}</span>
          <span class="text-xs text-text-muted">账号 {{ shortId(v.account_id) }}</span>
          <span class="flex-1" />
          <span class="text-xs text-text-muted">{{ fmt(v.created_at) }}</span>
        </div>
        <!-- 修复: 报错/原因完整显示(自动换行+长串断开), 不再被截断 -->
        <p v-if="v.reason" class="text-xs text-text-secondary mt-2 whitespace-pre-wrap break-all leading-relaxed">{{ v.reason }}</p>
        <div v-if="v.steps?.length" class="mt-2 flex flex-wrap gap-1.5">
          <span v-for="(s, i) in v.steps" :key="i"
            class="px-2 py-0.5 rounded bg-bg-card text-[11px] text-text-secondary"
            :title="s.detail">
            {{ s.at }} {{ actionLabel(s.action) }}
          </span>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { getVerificationLogs, type VerificationLog } from '@/api/verifier'

const list = ref<VerificationLog[]>([])
const loading = ref(false)

function statusLabel(s: string) {
  return s === 'pass' ? '验证通过' : s === 'fail' ? '验证失败' : s === 'stopped' ? '已停止' : '处理中'
}
function actionLabel(a: string) {
  const m: Record<string, string> = {
    detect: '识别', stop: '停止', poll: '投票', poll_fail: '投票失败', url_fail: '跳转失败',
    join: '已加入目标', bot_start: '已启动验证Bot', click: '点按钮', click_fail: '点按钮失败',
    click_answer: '点答案', math_reply: '回复答案', ai_skip: '跳过AI', ai_fail: 'AI失败',
    ai_answer: 'AI答案', retry: '重试', retest: '复测通过', retest_fail: '复测未过', retest_send_fail: '复测发送失败',
  }
  return m[a] || a
}
function shortId(id: string) { return id ? id.slice(0, 8) : '-' }
function fmt(t: string) {
  if (!t) return ''
  const d = new Date(t)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

async function load() {
  loading.value = true
  try {
    const res = await getVerificationLogs()
    list.value = res.list || []
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>
