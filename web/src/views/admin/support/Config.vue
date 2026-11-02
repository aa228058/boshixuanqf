<template>
  <main class="max-w-2xl mx-auto space-y-6">
    <Breadcrumb :items="[{ path: '/admin/dashboard', label: '仪表盘' }, { path: '', label: '客服配置' }]" />
    <div class="glass rounded-[var(--t-r-md)] p-5">
      <h3 class="text-lg font-semibold mb-6">客服信息配置</h3>
      <div class="space-y-4">
        <div>
          <label for="support-type" class="block text-sm text-text-secondary mb-1.5">联系方式类型</label>
          <div class="flex gap-2">
            <button
              v-for="type in contactTypes"
              :key="type.value"
              @click="form.contact_type = type.value as SupportConfig['contact_type']"
              class="flex-1 py-2 rounded-lg text-sm border transition-colors"
              :class="form.contact_type === type.value ? 'border-cyan/30 bg-cyan/5 text-cyan' : 'border-border text-text-secondary'"
            >
              {{ type.label }}
            </button>
          </div>
        </div>
        <div>
          <label class="block text-sm text-text-secondary mb-1.5">联系账号/链接</label>
          <input
            v-model="form.contact_value"
            placeholder="如微信号、TG号、QQ号或邮箱"
            class="w-full px-3 py-2.5 rounded-lg bg-bg-card border border-border focus:border-cyan focus:outline-none text-sm" aria-label="如微信号、TG号、QQ号或邮箱" />
        </div>
        <div>
          <label class="block text-sm text-text-secondary mb-1.5">显示名称</label>
          <input
            v-model="form.display_name"
            placeholder="如：联系客服开通套餐"
            class="w-full px-3 py-2.5 rounded-lg bg-bg-card border border-border focus:border-cyan focus:outline-none text-sm" aria-label="如：联系客服开通套餐" />
        </div>
        <div>
          <label class="block text-sm text-text-secondary mb-1.5">二维码图片（可选）</label>
          <div class="flex items-center gap-3">
            <label class="px-3 py-1.5 rounded-lg border border-border text-xs cursor-pointer hover:border-cyan/40 transition-colors">
              选择图片
              <input type="file" accept="image/*" class="hidden" @change="onQrFileChange" />
            </label>
            <span class="text-xs text-text-muted">支持 JPG、PNG 格式</span>
            <img v-if="form.qr_code_url" :src="form.qr_code_url" class="w-16 h-16 rounded-lg object-cover" alt="二维码" loading="lazy">
            <button v-if="form.qr_code_url" type="button" @click="clearQr" class="px-3 py-1.5 rounded-lg border border-border text-xs text-text-secondary hover:border-red-400 hover:text-red-500 transition-colors">
              清除图片
            </button>
          </div>
        </div>
      </div>
      <div class="flex justify-end gap-3 mt-6 pt-6 border-t border-border">
        <button @click="fetchConfig" class="px-4 py-2 rounded-lg text-sm text-text-secondary">重置</button>
        <t-button theme="primary" @click="saveConfig">保存配置</t-button>
      </div>
    </div>

    <!-- 预览 -->
    <div class="glass rounded-[var(--t-r-md)] p-5">
      <h3 class="text-sm font-medium text-text-secondary mb-4">租户端预览</h3>
      <div class="bg-bg-card rounded-lg p-4">
        <div class="flex items-center gap-3">
          <MessageCircle class="w-8 h-8 text-cyan" />
          <div>
            <p class="font-medium">{{ form.display_name || '联系客服' }}</p>
            <p class="text-sm text-text-secondary font-mono max-w-[65ch]">{{ form.contact_value || '未配置' }}</p>
          </div>
        </div>
        <div v-if="form.qr_code_url" class="mt-3 flex justify-center">
          <img :src="form.qr_code_url" class="w-32 h-32 rounded-lg" alt="二维码" loading="lazy">
        </div>
      </div>
    </div>
  </main>
</template>

<script setup lang="ts">
import { toast } from '@/utils/toast'
import Breadcrumb from '@/components/ui/Breadcrumb.vue'
import { ref, onMounted } from 'vue'
import { MessageCircle } from 'lucide-vue-next'
import { adminApiMod } from '@/api/admin'
import type { SupportConfig } from '@/types'

const contactTypes = [
  { label: '微信', value: 'wechat' },
  { label: 'Telegram', value: 'telegram' },
  { label: 'QQ', value: 'qq' },
  { label: '邮箱', value: 'email' },
  { label: '自定义', value: 'custom' },
]

const qrFile = ref<File | null>(null)
const form = ref<SupportConfig>({
  id: 0,
  contact_type: 'wechat',
  contact_value: '',
  display_name: '',
  qr_code_url: '',
  is_active: true,
})

async function fetchConfig() {
  try {
    // P0-04: 原兜底默认值用了第三套互不相同的字段名(contact_wechat/telegram/url),
    // 与后端真实返回(contact_type/contact_value/display_name/qr_code_url)完全对不上——
    // 配置永远存不进正确的字段。以 007 迁移 + repository 的真实字段为准统一。
    const EMPTY = { id: 0, contact_type: 'wechat', contact_value: '', display_name: '', qr_code_url: '', is_active: true }
    const config = await adminApiMod.getSupport()
    form.value = { ...EMPTY, ...(config || {}) }
  } catch (e: unknown) {
    // 首次部署无客服配置时接口可能报错/返回空, 兜底默认空值避免页面异常
    form.value = { id: 0, contact_type: 'wechat', contact_value: '', display_name: '', qr_code_url: '', is_active: true }
  }
}

async function saveConfig() {
  try {
    // qr_code_file 是 File 对象（仅本地预览用），JSON 序列化为空对象{}；
    // 剥离后提交，二维码内容走 qr_code_url（base64 data URL 或外链）。
    const { qr_code_file, ...payload } = form.value as any
    await adminApiMod.updateSupport(payload as any)
    toast({ type: 'success', title: '配置已保存' })
  } catch (e: unknown) {
    toast({ type: 'error', title: '保存失败', message: (e instanceof Error ? e.message : '请稍后重试') })
  }
}

// 清除已上传的二维码图片
function clearQr() {
  form.value.qr_code_url = ''
  form.value.qr_code_file = null as any
}

function onQrFileChange(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (f) handleQrUpload(f)
}

function handleQrUpload(file: File) {
  if (file.size > 512 * 1024) {
    toast({ type: 'warning', title: '图片过大', message: '二维码图片请小于 500KB' })
    return
  }
  // 半成品警示：这里只做 base64 预览并直接塞进 qr_code_url 提交——
  // File 本体从未上传到媒体服务，大图的 base64 会让保存报文臃肿；
  // 且 qr_code_file 字段（File 对象）JSON 序列化后为空对象，后端拿不到文件。
  // TODO: 走 mediaApi.upload 上传拿 URL，或后端支持 base64 落库并限制大小。
  const reader = new FileReader()
  reader.onload = () => { form.value.qr_code_url = reader.result as string }
  reader.readAsDataURL(file)
}

onMounted(fetchConfig)
</script>
