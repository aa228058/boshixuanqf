<template>
  <main class="max-w-2xl mx-auto space-y-6">
    <!-- Phase 4: 设置页拆 5 Tab(手册信息架构)——URL ?tab= 同步, 可直接跳转/刷新定位 -->
    <div class="flex gap-1 p-1 rounded-[var(--t-r-md)] bg-bg-card border border-border/50 mb-2 overflow-x-auto">
      <button v-for="t in tabs" :key="t.key"
              class="px-3 py-1.5 rounded-lg text-sm whitespace-nowrap transition-colors"
              :class="activeTab === t.key ? 'bg-cyan-glow/25 text-cyan' : 'text-text-secondary hover:text-text-primary'"
              @click="switchTab(t.key)">{{ t.label }}</button>
    </div>
    <!-- 套餐信息 -->
    <div v-show="activeTab === 'plan'" class="glass rounded-[var(--t-r-md)] p-5 gradient-border">
      <div class="flex items-center justify-between mb-4">
        <div class="flex items-baseline gap-2">
          <span class="text-sm text-text-muted shrink-0">当前套餐</span>
          <span class="text-xl font-semibold text-gradient">{{ planName }}</span>
        </div>
        <div class="flex items-baseline gap-1 shrink-0">
          <span class="text-2xl tracking-tight font-bold text-cyan">{{ planPrice }}</span>
          <span class="text-xs text-text-secondary">/月</span>
        </div>
      </div>
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-4">
        <div class="p-3 rounded-lg bg-bg-card">
          <p class="text-xs text-text-secondary">账号上限</p>
          <p class="text-lg font-mono font-semibold">{{ tenant?.max_accounts || 1 }}</p>
        </div>
        <div class="p-3 rounded-lg bg-bg-card">
          <p class="text-xs text-text-secondary">并发任务</p>
          <p class="text-lg font-mono font-semibold">{{ tenant?.max_concurrent_tasks || 1 }}</p>
        </div>
      </div>
      <div v-if="tenant?.expires_at" class="text-xs text-amber mb-4">
        到期时间: {{ formatDate(tenant.expires_at) }}
      </div>
      <t-button theme="primary" @click="showUpgrade = true">
        升级套餐
      </t-button>
    </div>

    <!-- R119: 通知中心 -->
    <div v-show="activeTab === 'notify'" class="glass rounded-[var(--t-r-md)] p-5 lift">
      <!-- 套餐权限锁: 邮件通知是套餐特性(plan_features.notify) -->
      <template v-if="!authStore.tenant?.plan_features?.notify">
        <div class="flex items-start gap-2.5 rounded-lg border border-border bg-bg-secondary p-3.5 text-xs text-text-muted leading-relaxed">
          <svg class="w-4 h-4 shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4" stroke-linecap="round"/></svg>
          <span>当前套餐不含<b class="text-text-secondary">邮件通知</b>功能，请联系管理员升级套餐后使用。</span>
        </div>
      </template>
      <template v-else>
        <!-- R122: 删旧"管理员未配置 SMTP"分支 —— r120 起 SMTP 由租户自己配置,
             原分支把整个表单挡住了且文案是旧架构遗留。改为常显表单 + 未配置小提示。 -->
        <div v-if="!smtpConfigured" class="flex items-start gap-2.5 rounded-lg border border-amber/30 bg-amber/10 p-3.5 text-xs text-amber leading-relaxed mb-4">
          <svg class="w-4 h-4 shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 9v4m0 4h.01M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z" stroke-linecap="round" stroke-linejoin="round"/></svg>
          <span>尚未配置发件邮箱 —— 在下方选择服务商并填写授权码后，通知功能即生效。</span>
        </div>
        <!-- R122: 表单内容直接在上方 else 分支内(原 56 行的 <template v-else> 与尾部闭合已删, 避免双 else 断链) -->
        <h3 class="text-sm font-medium text-text-secondary">接收通知的邮箱</h3>
        <p class="text-xs text-text-muted mt-1">中奖、任务等事件将推送到此邮箱（手机邮箱 App 可实时提醒）</p>
        <div class="flex gap-2 mt-3">
          <input v-model="notifyEmail" type="email" placeholder="you@example.com"
                 class="flex-1 px-3.5 py-2.5 rounded-lg bg-bg-secondary border border-border text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-cyan/50" />
        </div>

        <!-- R120: 发件邮箱 —— 租户自己的邮箱服务商, 通知从你自己的邮箱发出 -->
        <h3 class="text-sm font-medium text-text-secondary mt-6">发件邮箱（用你自己的邮箱发出通知）</h3>
        <p class="text-xs text-text-muted mt-1">选择一个邮箱服务商，按下方教程开启 SMTP 并填授权码</p>
        <div class="flex flex-wrap gap-1.5 mt-3">
          <button v-for="p in smtpProviders" :key="p.key" type="button" @click="pickProvider(p.key)"
                  class="px-3 py-1.5 rounded-full border text-xs transition-colors"
                  :class="smtpProvider === p.key ? 'border-cyan text-cyan bg-cyan/10' : 'border-border text-text-secondary hover:border-cyan/30'">
            {{ p.label }}
          </button>
        </div>
        <!-- 所选服务商的教程 -->
        <div class="rounded-lg border border-border/60 bg-ink-01 p-3.5 mt-3">
          <div class="text-[11px] font-medium text-text-secondary mb-1.5">📖 {{ smtpProviders.find(p => p.key === smtpProvider)?.label }} 配置教程</div>
          <ol class="text-[11px] text-text-muted space-y-1 list-decimal list-inside leading-relaxed">
            <li v-for="(step, i) in smtpProviders.find(p => p.key === smtpProvider)?.guide" :key="i">{{ step }}</li>
          </ol>
        </div>
        <!-- 配置表单 -->
        <div class="grid grid-cols-2 gap-2 mt-3">
          <input v-model="smtpForm.user" :placeholder="smtpProvider === 'tinkmail' ? '完整邮箱地址' : '发件邮箱账号'"
                 class="col-span-2 sm:col-span-1 px-3.5 py-2.5 rounded-lg bg-bg-secondary border border-border text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-cyan/50" />
          <input v-model="smtpForm.pass" type="password" :placeholder="smtpPassSet ? '授权码（已设置, 留空则不变）' : '授权码 / 应用专用密码'"
                 class="col-span-2 sm:col-span-1 px-3.5 py-2.5 rounded-lg bg-bg-secondary border border-border text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-cyan/50" />
          <input v-model="smtpForm.host" placeholder="SMTP 服务器"
                 class="px-3.5 py-2.5 rounded-lg bg-bg-secondary border border-border text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-cyan/50 font-mono" />
          <input v-model.number="smtpForm.port" type="number" placeholder="端口(465)"
                 class="px-3.5 py-2.5 rounded-lg bg-bg-secondary border border-border text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:border-cyan/50 font-mono" />
        </div>

        <div class="flex gap-2 mt-4">
          <button class="px-4 py-2.5 rounded-lg bg-cyan text-bg-primary text-sm font-medium active:scale-[0.97] transition-transform disabled:opacity-50" :disabled="notifyLoading || !notifyEmail" @click="saveNotify">保存全部设置</button>
          <button class="px-4 py-2.5 rounded-lg border border-border text-sm text-text-secondary hover:border-cyan/40 active:scale-[0.97] transition-transform disabled:opacity-50" :disabled="notifyLoading" @click="testNotifyMail">发送测试邮件</button>
          <span v-if="smtpConfigured" class="self-center text-[11px] text-emerald flex items-center gap-1">
            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="m5 13 4 4L19 7" stroke-linecap="round" stroke-linejoin="round"/></svg>发件配置已完成
          </span>
        </div>

        <h3 class="text-sm font-medium text-text-secondary mt-6">订阅事件</h3>
        <p class="text-xs text-text-muted mt-1">勾选需要邮件提醒的事件</p>
        <div class="mt-3 divide-y divide-border/60">
          <div v-for="(label, key) in notifyEvents" :key="key" class="flex items-center justify-between py-2.5">
            <span class="text-sm text-text-primary">{{ label }}</span>
            <button class="relative w-11 h-6 rounded-full transition-colors duration-200 shrink-0"
                    :class="notifySettings[key] ? 'bg-cyan' : 'bg-bg-secondary border border-border'"
                    @click="toggleNotifyEvent(String(key))">
              <span class="absolute top-0.5 w-5 h-5 rounded-full bg-white shadow transition-all duration-200"
                    :class="notifySettings[key] ? 'left-[22px]' : 'left-0.5'" />
            </button>
          </div>
        </div>
      </template>
    </div>

    <!-- AI 与入群验证 -->
    <div v-show="activeTab === 'verify'" class="glass rounded-[var(--t-r-md)] p-5 lift">
      <div class="flex items-center justify-between mb-1">
        <h3 class="text-sm font-medium text-text-secondary">入群验证（自动过验证）</h3>
        <label class="flex items-center gap-2 text-xs cursor-pointer select-none">
          <input type="checkbox" v-model="vfEnabled" class="accent-cyan w-4 h-4" @change="saveToggle" />
          <span :class="vfEnabled ? 'text-emerald' : 'text-text-muted'">{{ vfEnabled ? '已开启' : '已关闭' }}</span>
        </label>
      </div>
      <p class="text-xs text-text-muted mb-4">
        账号加群遇到验证时自动处理：点按钮 / 加入验证频道 / 答算式与投票免费可用；图片验证码需要下面的 AI（智谱 GLM-4V-Flash 长期免费）。
        处理记录在「验证记录」页。
      </p>
      <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-3">
        <div>
          <label class="block text-xs text-text-secondary mb-1">服务商</label>
          <select v-model="aiForm.provider" class="w-full px-3 py-2 rounded-lg glow-input text-sm bg-bg-card">
            <option value="glm">智谱（免费档）</option>
            <option value="qwen">阿里百炼（免费额）</option>
            <option value="custom">自定义（OpenAI兼容）</option>
          </select>
        </div>
        <div class="sm:col-span-2">
          <label class="block text-xs text-text-secondary mb-1">API Key{{ ai.has_key ? '（已保存，留空则不修改）' : '' }}</label>
          <input v-model="aiForm.api_key" type="password" placeholder="智谱免费key: open.bigmodel.cn 注册即得"
            class="w-full px-3 py-2 rounded-lg glow-input text-sm font-mono bg-bg-card" />
        </div>
        <div class="sm:col-span-3">
          <label class="block text-xs text-text-secondary mb-1">模型名（留空用默认）</label>
          <input v-model="aiForm.model" :placeholder="aiForm.provider === 'qwen' ? 'qwen-vl-max' : 'glm-4v-flash'"
            class="w-full px-3 py-2 rounded-lg glow-input text-sm font-mono bg-bg-card" />
        </div>
      </div>
      <div class="flex items-center gap-3">
        <t-button size="small" theme="primary" :disabled="aiSaving" @click="saveAI">
          <Loader2 v-if="aiSaving" class="w-3.5 h-3.5 animate-spin mr-1" />
          保存
        </t-button>
        <t-button size="small" variant="outline" :disabled="aiTesting" @click="testAI">
          <Loader2 v-if="aiTesting" class="w-3.5 h-3.5 animate-spin mr-1" />
          测试连通
        </t-button>
        <span v-if="aiTestResult" class="text-xs" :class="aiTestResult.ok ? 'text-emerald' : 'text-rose'">
          {{ aiTestResult.ok ? `连通正常 · 回答「${aiTestResult.answer}」` : aiTestResult.error }}
        </span>
      </div>
    </div>

    <!-- 卡密激活 -->
    <!-- R122: 补 v-show —— 此前缺失导致卡密栏在全部 Tab(通知/数据/模板...)重复显示 -->
    <div v-show="activeTab === 'plan'" class="glass rounded-[var(--t-r-md)] p-5 lift">
      <h3 class="text-sm font-medium text-text-secondary mb-4">激活卡密</h3>
      <div class="flex gap-3">
        <input
          v-model="activationCode"
          placeholder="输入卡密，如 TGS-PRO-XXXX-XXXX"
          class="flex-1 px-4 py-2.5 rounded-lg glow-input text-sm font-mono uppercase"
          @keyup.enter="activate" aria-label="输入卡密，如 TGS-PRO-XXXX-XXXX" />
        <t-button theme="primary" @click="activate">
          激活
        </t-button>
      </div>
    </div>

    <!-- G-05: 我的数据（合规自助导出） -->
    <div v-show="activeTab === 'data'" class="glass rounded-[var(--t-r-md)] p-5 lift">
      <div class="flex items-center justify-between">
        <div>
          <h3 class="text-sm font-medium text-text-secondary">我的数据</h3>
          <p class="text-xs text-text-muted mt-1">导出本租户全部自有数据（成员/群组/任务），不含任何账号凭证</p>
        </div>
        <!-- R122: tdesign outline 按钮在深色下是白底白字 —— 改用令牌类 -->
        <button :disabled="exporting" @click="exportMyData"
                class="px-4 py-2 rounded-lg border border-border bg-bg-secondary text-text-primary text-sm hover:border-cyan/40 active:scale-[0.97] transition-all disabled:opacity-50">
          {{ exporting ? '导出中…' : '导出 JSON' }}
        </button>
      </div>
    </div>

    <!-- 消息模板 -->
    <div v-show="activeTab === 'templates'" class="glass rounded-[var(--t-r-md)] p-5 lift">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-sm font-medium text-text-secondary">消息模板</h3>
        <button @click="showTemplateModal = true" class="text-xs text-cyan hover:text-cyan-bright transition-colors">管理模板</button>
      </div>
      <div class="space-y-2">
        <div
          v-for="t in templates.slice(0, 3)"
          :key="t.template_id"
          class="p-3 rounded-lg bg-bg-card text-sm truncate"
        >
          <span class="font-medium">{{ t.name }}:</span> {{ t.content }}
        </div>
        <EmptyState v-if="!templates.length" icon="MessageSquare" title="暂无模板" description="点击上方按钮添加，群发时可快速选择" class="py-6" />
      </div>
    </div>

    <!-- 联系客服 -->
    <div v-show="activeTab === 'support'" class="glass rounded-[var(--t-r-md)] p-5 lift">
      <h3 class="text-sm font-medium text-text-secondary mb-4">联系客服</h3>
      <div v-if="supportConfig" class="flex items-center gap-4">
        <div class="w-12 h-12 rounded-lg bg-cyan-glow/50 flex items-center justify-center">
          <MessageCircle class="w-6 h-6 text-cyan" />
        </div>
        <div>
          <p class="font-medium">{{ supportConfig.display_name }}</p>
          <p class="text-sm text-text-secondary font-mono max-w-[65ch]">{{ supportConfig.contact_value }}</p>
        </div>
        <button @click="copyContact" class="ml-auto px-3 py-1.5 rounded-lg border border-border text-xs hover:border-cyan/30 hover:text-cyan transition-colors h-8 inline-flex items-center" aria-label="复制">
          {{ copied ? '已复制' : '复制' }}
        </button>
      </div>
    </div>

    <!-- 升级弹窗 -->
    <Modal v-model="showUpgrade" title="升级套餐">
      <div class="space-y-4">
        <div
          v-for="plan in plans"
          :key="plan.plan_id"
          class="p-4 rounded-lg border transition-all cursor-pointer"
          :class="selectedPlan === plan.plan_id ? 'border-cyan/50 bg-cyan/5' : 'border-border hover:border-cyan/30'"
          @click="selectedPlan = plan.plan_id"
         tabindex="0" role="button">
          <div class="flex items-center justify-between">
            <div>
              <p class="font-medium">{{ plan.name }}</p>
              <p class="text-xs text-text-secondary mt-1">{{ plan.description }}</p>
              <div class="flex flex-wrap gap-1 mt-2">
                <!-- P1-14: 原直显英文 key(broadcast/scrape_members...)——用户看不懂。
                     按套餐管理的 featureList 中文映射, 未知 key 原样显示不撒谎。 -->
                <span v-for="f in plan.features" :key="f" class="text-xs px-1.5 py-0.5 rounded bg-bg-card text-text-secondary">{{ featureLabel(f) }}</span>
              </div>
            </div>
            <div class="text-right">
              <p class="text-xl tracking-tight font-bold text-cyan truncate">{{ formatPrice(plan.price_monthly) }}</p>
              <p class="text-xs text-text-secondary">/月</p>
            </div>
          </div>
          <div class="flex gap-4 mt-3 text-xs text-text-secondary">
            <span>{{ plan.max_accounts }} 个账号</span>
            <span>{{ plan.max_concurrent }} 并发</span>
          </div>
        </div>
      </div>
      <template #footer>
        <button @click="showUpgrade = false" class="px-4 py-2 rounded-lg text-sm text-text-secondary">取消</button>
        <t-button theme="primary" @click="copyContact">
          联系客服购买
        </t-button>
      </template>
    </Modal>

    <!-- 模板管理弹窗 -->
    <Modal v-model="showTemplateModal" title="消息模板管理">
      <div class="space-y-3 max-h-[400px] overflow-y-auto">
        <div
          v-for="t in templates"
          :key="t.template_id"
          class="p-3 rounded-lg bg-bg-card group"
        >
          <div class="flex items-center justify-between gap-2">
            <input 
              :value="t.name" 
              @input="(e) => updateTemplateName(t.template_id, (e.target as HTMLInputElement).value)"
              class="bg-transparent text-sm font-medium focus:outline-none flex-1 min-w-0" 
            />
            <div class="flex items-center gap-1 shrink-0">
              <button @click="saveTemplate(t)" class="text-xs px-2 py-1 rounded bg-cyan-glow/50 text-cyan hover:bg-cyan/20">
                保存
              </button>
              <button @click="deleteTemplate(t.template_id)" class="p-1 rounded hover:bg-rose/10 text-rose">
                <Trash2 class="w-4 h-4" />
              </button>
            </div>
          </div>
          <div class="relative">
            <t-textarea 
              :model-value="t.content"
              @update:model-value="(v: string) => updateTemplateContent(t.template_id, v)"
              :rows="2" 
              :maxlength="500"
              class="mt-2 pr-12"
            />
            <!-- R121: 删掉手写计数 —— tdesign textarea 的 maxlength 自带右下角计数器, 双份叠印成"重影" -->
          </div>
        </div>
        <button @click="addTemplate" class="w-full py-2 rounded-lg border border-dashed border-border text-sm text-text-secondary hover:border-cyan/30 hover:text-cyan transition-colors">
          + 添加模板
        </button>
      </div>
    </Modal>
  </main>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

// ===== Phase 4 设置页 Tab(手册信息架构): 5 Tab + URL ?tab= 双向同步 =====
const route = useRoute()
const router = useRouter()
const tabs = [
  { key: 'plan', label: '套餐' },
  { key: 'verify', label: '验证' },
  { key: 'notify', label: '通知' },
  { key: 'data', label: '数据' },
  { key: 'templates', label: '模板' },
  { key: 'support', label: '客服' },
]
// R119+R120: 通知中心状态(租户级 SMTP)
const notifyEmail = ref('')
const notifySettings = ref<Record<string, boolean>>({})
const notifyEvents = ref<Record<string, string>>({}) // 事件键 → 中文标签(后端下发)
const smtpConfigured = ref(false)
const notifyLoading = ref(false)
// R120: 发件邮箱(租户自己的 SMTP) —— 服务商选择 + 配置表单 + 教程
const smtpProviders = [
  { key: 'qq', label: 'QQ 邮箱', host: 'smtp.qq.com', port: 465,
    guide: ['网页登录 QQ 邮箱 → 设置 → 账户', '找到「POP3/IMAP/SMTP/Exchange/CardDAV/CalDAV服务」→ 开启', '按提示发短信验证 → 生成 16 位授权码', '授权码填到下方「授权码」栏（不是 QQ 密码）'] },
  { key: '163', label: '网易 163', host: 'smtp.163.com', port: 465,
    guide: ['网页登录 163 邮箱 → 设置 → POP3/SMTP/IMAP', '开启 SMTP 服务', '按提示验证 → 复制授权码（客户端专用密码）', '授权码填到下方「授权码」栏'] },
  { key: 'gmail', label: 'Gmail', host: 'smtp.gmail.com', port: 465,
    guide: ['Google 账户 → 安全 → 开启两步验证', '搜索「应用专用密码」→ 生成一个', '16 位应用专用密码填到「授权码」栏', '需要服务器能访问谷歌（海外服务器没问题）'] },
  { key: 'tinkmail', label: 'TinkMail', host: 'smtp.tinkmail.me', port: 465,
    guide: ['登录 tinkmail.me 控制台', '生成 Client Token', '「发件账号」填完整邮箱地址', '「授权码」栏填 Client Token'] },
  { key: 'custom', label: '自定义', host: '', port: 465,
    guide: ['填写你的邮箱服务商提供的 SMTP 服务器地址、端口和授权码', '主流邮箱均已内置, 一般不需要选这项'] },
]
const smtpProvider = ref('qq')
const smtpForm = ref({ host: 'smtp.qq.com', port: 465, user: '', pass: '', from: '' })
const smtpPassSet = ref(false) // 后端标记: 是否已存过授权码(不回传密码本身)
function pickProvider(key: string) {
  smtpProvider.value = key
  const p = smtpProviders.find(x => x.key === key)!
  smtpForm.value.host = p.host
  smtpForm.value.port = p.port
}
async function loadNotify() {
  notifyLoading.value = true
  try {
    const d: any = await settingsApi.getNotify()
    notifyEmail.value = d.email || ''
    notifySettings.value = d.settings || {}
    notifyEvents.value = d.events || {}
    smtpConfigured.value = !!d.smtp_configured
    // R120: 回填已保存的 SMTP 配置(授权码不回传)
    const s = d.smtp || {}
    if (s.provider) smtpProvider.value = s.provider
    if (s.host) smtpForm.value.host = s.host
    if (s.port) smtpForm.value.port = s.port
    if (s.user) smtpForm.value.user = s.user
    if (s.from) smtpForm.value.from = s.from
    smtpPassSet.value = !!s.pass_set
  } catch { /* toast 已由拦截器统一处理 */ }
  finally { notifyLoading.value = false }
}
async function saveNotify() {
  try {
    await settingsApi.updateNotify(notifyEmail.value, notifySettings.value, {
      provider: smtpProvider.value,
      host: smtpForm.value.host,
      port: Number(smtpForm.value.port) || 465,
      user: smtpForm.value.user,
      // 未改密码时不传 pass, 后端自动保留旧授权码
      ...(smtpForm.value.pass ? { pass: smtpForm.value.pass } : {}),
      from: smtpForm.value.from || smtpForm.value.user,
    })
    toast({ type: 'success', title: '通知设置已保存' })
    loadNotify()
  } catch { /* 拦截器已提示 */ }
}
async function testNotifyMail() {
  try {
    const res: any = await settingsApi.testNotify()
    toast({ type: 'success', title: res?.message || '测试邮件已发送, 请查收(含垃圾邮件文件夹)' })
  } catch { /* 拦截器已提示 */ }
}
function toggleNotifyEvent(key: string) {
  notifySettings.value = { ...notifySettings.value, [key]: !notifySettings.value[key] }
}
const activeTab = ref('plan')
// 初始值取 URL(外部跳转/刷新可定位到指定 Tab)
const validTabs = tabs.map((t) => t.key)
const q = route.query.tab as string | undefined
if (q && validTabs.includes(q)) activeTab.value = q
function switchTab(key: string) {
  activeTab.value = key
  router.replace({ query: { ...route.query, tab: key } })
  if (key === 'notify') loadNotify() // R119: 切到通知 Tab 加载订阅状态
}
// URL 变化(浏览器前进后退)同步回 Tab
watch(() => route.query.tab, (t) => {
  if (typeof t === 'string' && validTabs.includes(t)) activeTab.value = t
})

import { useCopy } from '@/composables/useCopy'
import { MessageCircle, Trash2, Loader2 } from 'lucide-vue-next'
import { settingsApi } from '@/api/settings'
import { toast } from '@/utils/toast'
import { getAIConfig, saveAIConfig, testAIConfig, setVerifierEnabled } from '@/api/verifier'
import EmptyState from '@/components/ui/EmptyState.vue'

import { formatDate, formatPrice } from '@/utils/format'
import Modal from '@/components/ui/Modal.vue'
import { useAuthStore } from '@/stores/auth'
import type { Tenant, Plan, SupportConfig, MessageTemplate } from '@/types'

const authStore = useAuthStore()
const { copied: keyCopied, copy: copyKey } = useCopy()
const tenant = ref<Tenant | null>(null)
const plans = ref<Plan[]>([])
const supportConfig = ref<SupportConfig | null>(null)
const templates = ref<MessageTemplate[]>([])

const showUpgrade = ref(false)

// AI 与入群验证
const ai = ref({ provider: 'glm', model: '', has_key: false, enabled: false })
const aiForm = ref({ provider: 'glm', api_key: '', model: '' })
const vfEnabled = ref(false)
const aiSaving = ref(false)
const aiTesting = ref(false)
const aiTestResult = ref<{ ok: boolean; answer?: string; error?: string } | null>(null)

async function loadAI() {
  try {
    const res = await getAIConfig()
    ai.value = res
    aiForm.value = { provider: res.provider || 'glm', api_key: '', model: res.model || '' }
    vfEnabled.value = !!res.enabled
  } catch { /* 配置读取失败不阻断页面 */ }
}
async function saveAI() {
  aiSaving.value = true
  aiTestResult.value = null
  try {
    await saveAIConfig({ provider: aiForm.value.provider, api_key: aiForm.value.api_key, model: aiForm.value.model })
    aiForm.value.api_key = ''
    toast({ type: 'success', message: 'AI 配置已保存' })
    loadAI()
  } catch (e: any) {
    toast({ type: 'error', message: e?.message || '保存失败' })
  } finally {
    aiSaving.value = false
  }
}
async function testAI() {
  aiTesting.value = true
  aiTestResult.value = null
  try {
    aiTestResult.value = await testAIConfig()
  } catch (e: any) {
    aiTestResult.value = { ok: false, error: e?.message || '测试失败' }
  } finally {
    aiTesting.value = false
  }
}
async function saveToggle() {
  try {
    await setVerifierEnabled(vfEnabled.value)
    toast({ type: 'success', message: vfEnabled.value ? '入群验证已开启' : '入群验证已关闭' })
  } catch (e: any) {
    vfEnabled.value = !vfEnabled.value
    toast({ type: 'error', message: e?.message || '操作失败' })
  }
}
let copyTimer: ReturnType<typeof setTimeout> | null = null
onUnmounted(() => { if (copyTimer) clearTimeout(copyTimer) })
const showTemplateModal = ref(false)
const exporting = ref(false)

// G-05: 触发浏览器下载导出的 JSON（后端已审计该行为）
async function exportMyData() {
  if (exporting.value) return
  exporting.value = true
  try {
    const token = useAuthStore().token
    const res = await fetch('/api/v1/tenant/export', { headers: { Authorization: `Bearer ${token}` } })
    if (!res.ok) throw new Error('导出失败: ' + res.status)
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'my-data-export.json'
    a.click()
    URL.revokeObjectURL(url)
    toast({ type: 'success', title: '导出成功', message: '已下载 my-data-export.json' })
  } catch (e) {
    toast({ type: 'error', title: '导出失败', message: e instanceof Error ? e.message : '请稍后重试' })
  } finally {
    exporting.value = false
  }
}
const selectedPlan = ref('')
const activationCode = ref('')
const copied = ref(false)

const planName = computed(() => {
  const map: Record<string, string> = { free: '免费版', basic: '基础版', pro: '专业版', enterprise: '企业版' }
  return map[tenant.value?.plan_id || 'free'] || '免费版'
})

const planPrice = computed(() => {
  if (!tenant.value) return formatPrice(0)
  const list = Array.isArray(plans.value) ? plans.value : []
  const p = list.find(plan => plan.plan_id === tenant.value!.plan_id)
  return formatPrice(p?.price_monthly || 0)
})

async function activate() {
  if (!activationCode.value) return
  try {
    const res = await settingsApi.activateCode(activationCode.value.trim())
    activationCode.value = ''
    await fetchProfile()
    // 同步更新 authStore，确保侧边栏等全局组件数据一致
    await authStore.refreshTenant()
    const exp = (res && typeof res === 'object' && 'expires_at' in res) ? (res as unknown as Record<string, unknown>).expires_at : null
    if (exp) {
      toast({ type: 'success', title: '激活成功', message: '套餐到期时间: ' + formatDate(String(exp)) })
    } else {
      toast({ type: 'success', title: '激活成功' })
    }
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : '请检查卡密是否正确'
    toast({ type: 'error', title: '激活失败', message: msg })
  }
}

function copyContact() {
  if (!supportConfig.value?.contact_value) return
  const text = supportConfig.value.contact_value
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(text).catch(() => fallbackCopy(text))
  } else {
    fallbackCopy(text)
  }
  copied.value = true
  if (copyTimer) clearTimeout(copyTimer)
  copyTimer = setTimeout(() => copied.value = false, 2000)
}

// clipboard API 不可用时的兜底（HTTP 环境/旧浏览器）：临时 textarea + execCommand
function fallbackCopy(text: string) {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.select()
  try { document.execCommand('copy') } catch { /* 放弃 */ }
  document.body.removeChild(ta)
}

async function fetchTemplates() {
  try {
    const res = await settingsApi.getTemplates()
    templates.value = res.list || []
  } catch (e: unknown) {
    // console.error("Settings error:", e)
  }
}

async function addTemplate() {
  try {
    const t = await settingsApi.createTemplate({ name: '新模板', content: '' })
    templates.value.push(t)
  } catch (err: unknown) {
    // P1-38: 模板创建失败原来静默吞掉——用户以为成功实际没有
    toast({ type: 'error', title: '创建模板失败', message: (err as Error)?.message || '请稍后重试' })
  }
}

// P1-14: 套餐功能 key → 中文映射(与 admin/plans/Index.vue 的 featureList 对齐)
const featureLabelMap: Record<string, string> = {
  broadcast: '群发消息', loop_broadcast: '循环群发', join_groups: '加群任务',
  invite: '邀请入群', scrape_members: '成员采集', scrape_links: '链接采集',
  monitor: '消息监控', templates: '消息模板', scheduled_tasks: '定时任务',
  discover: '发现群组·群库', verifier: '入群验证', dict: '文案变量',
  api: 'API 接入', raffle: '抽奖助手', search: '关键词搜群',
  export: '数据导出', checkup: '账号检测',
}
function featureLabel(key: string): string {
  return featureLabelMap[key] || key
}

async function deleteTemplate(id: string) {
  try {
    await settingsApi.deleteTemplate(id)
    templates.value = templates.value.filter(t => t.template_id !== id)
  } catch (err: unknown) {
    toast({ type: 'error', title: '删除模板失败', message: (err as Error)?.message || '请稍后重试' })
  }
}

function updateTemplateName(id: string, name: string) {
  const t = templates.value.find(x => x.template_id === id)
  if (t) t.name = name
}
function updateTemplateContent(id: string, content: string) {
  const t = templates.value.find(x => x.template_id === id)
  if (t) t.content = content
}

async function saveTemplate(t: MessageTemplate) {
  try {
    await settingsApi.updateTemplate(t.template_id, { name: t.name, content: t.content })
  } catch (err: unknown) {
    toast({ type: 'error', title: '保存模板失败', message: (err as Error)?.message || '请稍后重试' })
  }
}

async function fetchProfile() {
  try {
    const res = await settingsApi.getProfile()
    tenant.value = res
  } catch (e: unknown) {
    // console.error("Settings error:", e)
  }
}

onMounted(async () => {
  await fetchProfile()
  await fetchTemplates()
  loadAI()
  // R142 修复: 通知设置(邮箱/SMTP/事件订阅)此前只在保存后回填, 刷新页面从不加载 —— 补挂载加载
  loadNotify()
  try {
    const p = await settingsApi.getPlans()
    // R86 热修: 接口可能返回 {list:[...]} 等包装对象, p||[] 会把对象塞进数组变量
    // 导致 planPrice 里 plans.value.find 抛 "find is not a function"
    plans.value = Array.isArray(p) ? p : (Array.isArray((p as any)?.data) ? (p as any).data : (Array.isArray((p as any)?.list) ? (p as any).list : []))
  } catch (e: unknown) { /* ignore */ }
  try {
    const s = await settingsApi.getSupport()
    supportConfig.value = s || null
  } catch (e: unknown) {
    // console.error("Settings error:", e)
  }
})
</script>
