<template>
  <main class="flex-1 p-4 sm:p-6 overflow-y-auto pb-24 lg:pb-6">
    <div class="max-w-2xl">
      <h1 class="text-xl font-bold text-text-primary mb-1">风控探测配置</h1>
      <p class="text-xs text-text-muted mb-5">账号能力检测（8 项矩阵）的全局探测目标，保存后所有租户共用。任一留空，租户端检测按钮将提示"管理员未配置"。</p>

      <div class="glass rounded-[var(--t-r-md)] p-5 space-y-5">
        <div>
          <label class="text-sm font-medium text-text-secondary">主动私聊测试号 <span class="text-rose">*</span></label>
          <p class="text-[11px] text-text-muted mt-1 mb-2">必须是<b class="text-text-secondary">真实用户小号</b>（不能用 bot——被限制私聊的号仍能和 bot 聊天，会误判）。探测时给此号发一条消息并立即撤回。</p>
          <input v-model="form.test_username" placeholder="@your_test_account 或 your_test_account"
                 class="w-full px-3.5 py-2.5 rounded-lg bg-bg-secondary border border-border text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-cyan/50 font-mono" />
        </div>
        <div>
          <label class="text-sm font-medium text-text-secondary">私有群永久邀请链接 <span class="rose text-rose">*</span></label>
          <p class="text-[11px] text-text-muted mt-1 mb-2">必须是<b class="text-text-secondary">永久链接</b>（群设置里生成，勿设有效期/次数）。被测号通过此链接加入→发消息→撤回→退群。</p>
          <input v-model="form.private_invite" placeholder="https://t.me/+AbCdEfGhIjK"
                 class="w-full px-3.5 py-2.5 rounded-lg bg-bg-secondary border border-border text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-cyan/50 font-mono" />
        </div>
        <div>
          <label class="text-sm font-medium text-text-secondary">公开群链接 <span class="text-rose">*</span></label>
          <p class="text-[11px] text-text-muted mt-1 mb-2">一个你持有的公开群 username 或链接。用于检测"公开群发言"能力（L2 梯度限制的关键判定项）。</p>
          <input v-model="form.public_link" placeholder="https://t.me/your_public_group"
                 class="w-full px-3.5 py-2.5 rounded-lg bg-bg-secondary border border-border text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-cyan/50 font-mono" />
        </div>
        <div class="flex items-center justify-between pt-1">
          <span class="text-[11px] text-text-muted">最后更新：{{ form.updated_at?.slice(0, 16).replace('T', ' ') || '—' }}</span>
          <button @click="save" :disabled="saving"
                  class="px-5 py-2.5 rounded-lg bg-cyan text-bg-primary text-sm font-medium active:scale-[0.97] transition-transform disabled:opacity-50 flex items-center gap-1.5">
            <Loader2 v-if="saving" class="w-3.5 h-3.5 animate-spin" />{{ saving ? '保存中…' : '保存配置' }}
          </button>
        </div>
      </div>

      <div class="rounded-lg border border-border/60 bg-ink-01 p-4 mt-4 text-[11px] text-text-muted leading-relaxed">
        <b class="text-text-secondary">检测矩阵说明：</b>读取消息 → 基础发送(收藏夹) → 主动私聊(此页小号) → 加入私有群 → 私有群发送 → 加入公开群 → 公开群发送 → 回复消息。
        全部真实操作后立即撤回/退群；同一被测号 24 小时限测 2 次。结论自动分级：正常 / 私聊受限 / 公开群受限(L2 梯度) / 群组功能受限 / 严重受限。
      </div>
    </div>
  </main>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { Loader2 } from 'lucide-vue-next'
import { adminApiMod } from '@/api/admin'
import { toast } from '@/utils/toast'

const form = ref<any>({ test_username: '', private_invite: '', public_link: '' })
const saving = ref(false)

async function load() {
  try {
    const d: any = await adminApiMod.getProbe()
    form.value = { test_username: d.test_username || '', private_invite: d.private_invite || '', public_link: d.public_link || '', updated_at: d.updated_at }
  } catch { /* 拦截器已提示 */ }
}

async function save() {
  saving.value = true
  try {
    await adminApiMod.updateProbe({
      test_username: form.value.test_username.trim(),
      private_invite: form.value.private_invite.trim(),
      public_link: form.value.public_link.trim(),
    })
    toast({ type: 'success', title: '探测配置已保存' })
    load()
  } catch { /* 拦截器已提示 */ }
  finally { saving.value = false }
}

onMounted(load)
</script>
