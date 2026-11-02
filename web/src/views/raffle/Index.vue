<template>
  <div class="max-w-3xl mx-auto px-4 md:px-8 py-6 space-y-5">
    <!-- 头部说明 R118: 大段文案改 3 条图标要点, 扫一眼即懂 -->
    <div class="rounded-[var(--t-r-lg)] border border-border bg-bg-card p-5 lift">
      <h2 class="text-lg font-bold text-text-primary">抽奖助手</h2>
      <ul class="mt-3 space-y-2 text-sm text-text-muted">
        <li class="flex items-start gap-2.5">
          <svg class="w-4 h-4 mt-0.5 text-cyan shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2 3 14h7l-1 8 10-12h-7l1-8z" stroke-linejoin="round"/></svg>
          <span>群内置顶抽奖公告出现时 <b class="text-text-primary">立即自动发送关键词</b>——抢答制，监控到就秒发</span>
        </li>
        <li class="flex items-start gap-2.5">
          <svg class="w-4 h-4 mt-0.5 text-cyan shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path d="m9 12 2 2 4-4" stroke-linecap="round" stroke-linejoin="round"/></svg>
          <span>同一活动（同群同关键词）<b class="text-text-primary">10 分钟内不重复参与</b>，防刷屏保护账号</span>
        </li>
        <li class="flex items-start gap-2.5">
          <svg class="w-4 h-4 mt-0.5 text-amber shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20 12v6a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-6"/><path d="m2 7 10-5 10 5-10 5L2 7z" stroke-linejoin="round"/></svg>
          <span>中奖后按下方 <b class="text-text-primary">中奖处理</b> 自动回复 + 私聊领奖双保险</span>
        </li>
      </ul>
    </div>

    <!-- 总开关(租户级) -->
    <div class="rounded-[var(--t-r-lg)] border border-border bg-bg-card p-5 lift">
      <div class="flex items-center justify-between">
        <div>
          <div class="font-semibold text-text-primary">自动参与抽奖</div>
          <div class="text-xs text-text-muted mt-0.5">关闭后所有账号停止参与；开启时按下方账号列表逐个生效</div>
        </div>
        <button
          role="switch"
          :aria-checked="raffleEnabled"
          :disabled="toggling"
          class="relative w-10 h-6 rounded-full transition-colors duration-200 shrink-0"
          :class="raffleEnabled ? 'bg-cyan' : 'bg-bg-primary'"
          @click="toggleMaster"
        >
          <span
            class="absolute top-1 w-4 h-4 rounded-full bg-white shadow transition-all duration-200"
            :class="raffleEnabled ? 'left-5' : 'left-1'"
          />
        </button>
      </div>

      <!-- 中奖回复(060): 确认中奖的账号自动在抽奖群发感谢语, 支持 {奖品} 变量 -->
      <!-- R87-M10: 内层分区卡拍平(套娃修复) —— surface-3 底与外层拉开层次 -->
      <div class="rounded-[var(--t-r-md)] border border-border bg-bg-elevated p-4 mt-3">
        <div class="flex items-center justify-between gap-3">
          <div>
            <div class="font-semibold text-text-primary">中奖自动回复</div>
            <div class="text-xs text-text-muted mt-0.5">中奖后 5~15 秒自动在抽奖群发送以下文案(普通群消息)</div>
          </div>
          <button
            role="switch"
            :aria-checked="replyEnabled"
            :disabled="replySaving"
            class="relative w-10 h-6 rounded-full transition-colors duration-200 shrink-0"
            :style="replyEnabled ? 'background: var(--t-brand);' : 'background: var(--t-surface-3);'"
            @click="saveReply(!replyEnabled, replyText)"
          >
            <span
              class="absolute top-1 w-4 h-4 rounded-full bg-white shadow transition-all duration-200"
              :class="replyEnabled ? 'left-5' : 'left-1'"
            />
          </button>
        </div>
        <!-- R127 紧凑化: 开关开启才展开输入区, 关闭时卡片只有一行 -->
        <template v-if="replyEnabled">
          <textarea
            v-model="replyText"
            rows="2"
            maxlength="200"
            :disabled="replySaving"
            placeholder="感谢群主, 中奖{奖品}已收到!"
            class="mt-3 w-full px-3 py-2 rounded-[var(--t-r-md)] text-sm resize-none disabled:opacity-50"
            style="background: var(--t-surface-2); border: 1px solid var(--t-line); color: var(--t-text-1);"
          />
          <div class="flex items-center justify-between mt-2">
            <span class="text-xs" style="color: var(--t-text-3);">{奖品} 自动替换为中奖金额 · 上限 200 字</span>
            <button
              @click="saveReply(replyEnabled, replyText)"
              :disabled="replySaving"
              class="px-4 py-1.5 rounded-lg text-xs glow-btn disabled:opacity-50 whitespace-nowrap shrink-0"
            >
              {{ replySaving ? '保存中…' : '保存' }}
            </button>
          </div>
        </template>
      </div>

      <!-- 私聊机器人激活(061 双重保险): 参与后私聊公告 bot 建立私聊通道, 中奖私聊通知=铁证 -->
      <!-- R87-M10: 内层分区卡拍平(套娃修复) —— surface-3 底与外层拉开层次 -->
      <div class="rounded-[var(--t-r-md)] border border-border bg-bg-elevated p-4 mt-3">
        <div class="flex items-center justify-between gap-3">
          <div>
            <div class="font-semibold text-text-primary">私聊激活 · 中奖双重保险</div>
            <div class="text-xs text-text-muted mt-0.5">参与后自动私聊抽奖机器人, 中奖时机器人私聊通知(100% 准确判定)</div>
          </div>
          <button
            role="switch"
            :aria-checked="pingEnabled"
            :disabled="pingSaving"
            class="relative w-10 h-6 rounded-full transition-colors duration-200 shrink-0"
            :style="pingEnabled ? 'background: var(--t-brand);' : 'background: var(--t-surface-3);'"
            @click="savePing(!pingEnabled, pingText)"
          >
            <span
              class="absolute top-1 w-4 h-4 rounded-full bg-white shadow transition-all duration-200"
              :class="pingEnabled ? 'left-5' : 'left-1'"
            />
          </button>
        </div>
        <!-- R127 紧凑化: 开关开启才显示激活语输入 -->
        <template v-if="pingEnabled">
          <div class="flex items-center gap-2 mt-3">
            <input
              v-model="pingText"
              :disabled="pingSaving"
              placeholder="/start"
              class="flex-1 px-3 py-2 rounded-[var(--t-r-md)] text-sm disabled:opacity-50"
              style="background: var(--t-surface-2); border: 1px solid var(--t-line); color: var(--t-text-1);"
            />
            <button
              @click="savePing(pingEnabled, pingText)"
              :disabled="pingSaving"
              class="px-4 py-2 rounded-lg text-xs glow-btn disabled:opacity-50 whitespace-nowrap shrink-0"
            >
              {{ pingSaving ? '保存中…' : '保存' }}
            </button>
          </div>
          <p class="text-xs mt-2" style="color: var(--t-text-3);">激活语默认 /start, 按目标机器人要求改</p>
        </template>
      </div>

      <!-- R128 结构重排: 参与账号紧跟总开关(开关→账号→记录 的操控动线) -->
      <div v-if="raffleEnabled" class="mt-4 pt-4 border-t border-border/50">
        <div class="flex items-center justify-between mb-2">
          <span class="text-xs text-text-muted">参与账号（关闭后该账号不再抢抽奖）</span>
          <button class="text-xs text-cyan hover:underline" @click="toggleAll">
            {{ allSelected ? '全不选' : '全选' }}
          </button>
        </div>
        <div class="max-h-64 overflow-y-auto space-y-1.5">
          <label
            v-for="acc in accounts"
            :key="acc.account_id"
            class="flex items-center gap-2.5 px-3 py-2.5 rounded-lg cursor-pointer hover:bg-ink-01"
          >
            <input
              type="checkbox"
              :checked="acc.raffle_enabled !== false"
              class="accent-cyan w-4 h-4"
              @change="toggleAccount(acc)"
            />
            <!-- R129 显示 TG 姓名优先(用户要求), 备注/手机号兜底 -->
            <span class="text-sm" :class="acc.raffle_enabled === false ? 'text-text-muted line-through' : ''">
              {{ acc.first_name || acc.tg_name || acc.remark || acc.phone }}
            </span>
            <!-- R127: 参与/中奖次数直接显示在账号行 —— 一眼看清哪个号中过奖 -->
            <span class="ml-auto flex items-center gap-2 shrink-0">
              <span v-if="accStats[acc.account_id]" class="text-[10.5px] font-semibold px-1.5 py-0.5 rounded-full" style="background: var(--t-surface-2); color: var(--t-text-3);">
                参与{{ accStats[acc.account_id].joins }}次
              </span>
              <span v-if="accStats[acc.account_id]?.wins > 0" class="text-[10.5px] font-bold px-1.5 py-0.5 rounded-full bg-amber/15 text-amber">
                中{{ accStats[acc.account_id].wins }}次
              </span>
            </span>
            <span class="text-xs text-text-muted/60 shrink-0">{{ statusLabel(acc.status) }}</span>
          </label>
          <p v-if="!accounts.length" class="text-xs text-text-muted px-3 py-3">
            暂无账号，先到「账号管理」添加
          </p>
        </div>
      </div>
    </div>

      <!-- 参与记录(058+061): 参与回执/开奖解析/私聊铁证落库, SSE 实时刷新。账号汇总条+左对齐明细表 -->
    <div class="rounded-[var(--t-r-lg)] border border-border bg-bg-card p-5 lift">
      <!-- R127 重写: 头部精简为标题行(统计胶囊已删 —— 参与/中奖次数在下方"参与账号"每行直接显示,
           两处重复展示是旧版"乱"的根源) -->
      <div class="flex items-center justify-between mb-3">
        <div class="font-extrabold text-body">参与记录</div>
        <div class="flex items-center gap-3 shrink-0">
          <!-- R128 清空记录: 确认后调 DELETE 端点, 日志不再无限叠加 -->
          <button class="text-xs font-bold hover:underline" style="color: var(--t-err);" @click="clearAllResults">清空</button>
          <button class="text-xs font-bold hover:underline" style="color: var(--t-brand);" @click="loadResults">刷新</button>
        </div>
      </div>

      <div v-if="!results.length" class="empty-pretty">
        <div class="ep-icon">
          <svg class="w-7 h-7" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
            <path d="M21 12a9 9 0 1 1-9-9" stroke-linecap="round" />
            <path d="M12 7v5l3 3" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </div>
        <p class="text-sm">暂无参与记录</p>
        <p class="text-xs">开启开关后, 账号参与抽奖的结果会实时显示在这里</p>
      </div>
      <div v-else class="max-h-80 overflow-y-auto space-y-2.5 pr-1">
        <!-- R148 按活动分组: 同一群+同一关键词 = 同一活动, 每活动一张卡(活动序号+关键词+参与账号数+中奖数) -->
        <div v-for="(g, gi) in groupedResults" :key="g.key"
             class="rounded-[var(--t-r-md)] border border-border overflow-hidden" style="background: var(--t-surface-2);">
          <div class="flex items-center gap-2 px-3 py-2" style="border-bottom: 1px solid var(--t-line-light);">
            <span class="text-[11px] font-extrabold px-1.5 py-0.5 rounded-md shrink-0"
                  style="background: var(--t-brand-dim); color: var(--t-brand);">活动{{ gi + 1 }}</span>
            <span class="text-small font-semibold truncate" style="color: var(--t-text-1);">{{ g.keyword || '未命名活动' }}</span>
            <span class="ml-auto flex items-center gap-1.5 shrink-0 text-caption">
              <span class="px-1.5 py-0.5 rounded-full" style="background: var(--t-surface-3); color: var(--t-text-3);">{{ g.items.length }}个号</span>
              <span v-if="g.wins" class="px-1.5 py-0.5 rounded-full bg-amber/15 text-amber font-bold">中{{ g.wins }}</span>
            </span>
          </div>
          <div class="px-3 py-1.5 text-[11px]" style="color: var(--t-text-3); border-bottom: 1px solid var(--t-line-light);">
            来源: {{ g.chatTitle || '群 ' + String(g.chatId).slice(-6) }} · {{ g.time }}
          </div>
          <div class="divide-y divide-border/30">
            <div v-for="r in g.items" :key="r.id" class="flex items-center gap-2 px-3 py-2">
              <span class="text-caption truncate" style="color: var(--t-text-1);">{{ r.tg_name || r.remark || r.phone }}</span>
              <span class="ml-auto flex items-center gap-2 shrink-0">
                <span v-if="r.prize" class="text-[11px] font-bold text-amber">{{ r.prize }}</span>
                <span class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10.5px] font-medium"
                      :class="r.kind === 'winner' ? 'bg-amber/15 text-amber' : r.kind === 'joined' ? 'bg-emerald/15 text-emerald' : 'bg-bg-secondary text-text-muted'">
                  <i class="w-1.5 h-1.5 rounded-full" :class="r.kind === 'winner' ? 'bg-amber' : r.kind === 'joined' ? 'bg-emerald' : 'bg-text-muted'"></i>
                  {{ r.kind === 'winner' ? '已中奖' : r.kind === 'joined' ? '待开奖' : '未中奖' }}
                </span>
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 参与账号块已上移(R128 结构重排): 开关卡 → 参与账号 → 参与记录 -->
  </div>
</template>
<!-- ACCOUNT_BLOCK_MOVED -->

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { accountApi } from '@/api/account'
import { api as request } from '@/utils/request'  // request 是函数无 .post 方法, api 对象才有——修 f.post is not a function
import { toast } from '@/utils/toast'

const raffleEnabled = ref(false)
const toggling = ref(false)
const accounts = ref<any[]>([])
// 中奖回复设置(060)
const replyEnabled = ref(false)
const replyText = ref('')
const replySaving = ref(false)
// 私聊机器人激活(061): 参与后私聊 bot, 中奖私聊通知=铁证
const pingEnabled = ref(false)
const pingText = ref('/start')
const pingSaving = ref(false)

// 保存私聊激活设置
async function savePing(enabled: boolean, text: string) {
  if (pingSaving.value) return
  pingSaving.value = true
  try {
    await request.post('/raffle/ping', { ping_enabled: enabled, ping_text: text })
    pingEnabled.value = enabled
    pingText.value = text
    toast({ type: 'success', message: '私聊激活设置已保存' })
  } catch (e: any) {
    toast({ type: 'error', message: e?.message || '保存失败' })
  } finally {
    pingSaving.value = false
  }
}

// 账号汇总条: 按 account_id 分组统计 参与/中奖 次数(061 UI 重排, 一眼看清哪些号中过奖)
const accountStats = computed(() => {
  const m = new Map<string, { name: string; joins: number; wins: number }>()
  for (const r of results.value as any[]) {
    const key = r.account_id || r.phone || '?'
    const name = r.tg_name || r.remark || r.phone || '未知账号'
    if (!m.has(key)) m.set(key, { name, joins: 0, wins: 0 })
    const s = m.get(key)!
    if (r.kind === 'joined') s.joins++
    else if (r.kind === 'winner') s.wins++
  }
  return [...m.entries()].map(([accountId, s]) => ({ accountId, ...s }))
})
// R127: accountId → 统计 map, 供参与账号列表每行直接显示参与/中奖次数
const accStats = computed(() => {
  const map: Record<string, { joins: number; wins: number }> = {}
  for (const s of accountStats.value as any[]) map[s.accountId] = { joins: s.joins, wins: s.wins }
  return map
})

// 保存中奖回复设置(开关拨动和按钮共用)
async function saveReply(enabled: boolean, text: string) {
  if (replySaving.value) return
  replySaving.value = true
  try {
    await request.post('/raffle/reply', { reply_enabled: enabled, reply_text: text })
    replyEnabled.value = enabled
    replyText.value = text
    toast({ type: 'success', message: '中奖回复设置已保存' })
  } catch (e: any) {
    toast({ type: 'error', message: e?.message || '保存失败' })
  } finally {
    replySaving.value = false
  }
}

const allSelected = computed(() => accounts.value.length > 0 && accounts.value.every(a => a.raffle_enabled !== false))

function statusLabel(s: string) {
  const map: Record<string, string> = {
    online: '在线', offline: '离线', pending_auth: '待验证', authenticated: '已认证',
    restricted: '受限', banned: '封禁', pending_verification: '待验证'
  }
  return map[s] || s
}

async function loadRaffleStatus() {
  try {
    const res = await request.get<{ enabled: boolean; reply_enabled?: boolean; reply_text?: string; ping_enabled?: boolean; ping_text?: string }>('/raffle/status')
    raffleEnabled.value = !!res.enabled
    replyEnabled.value = !!res.reply_enabled
    if (res.reply_text) replyText.value = res.reply_text
    pingEnabled.value = !!res.ping_enabled
    if (res.ping_text) pingText.value = res.ping_text
  } catch (e: any) {
    // 403 可见化: 套餐没勾抽奖时开关置灰的原因要说清楚。
    // P0-16: 原只判英文 'feature', 但 request.ts 的 translateError 已把后端
    // 403 消息翻成中文——提示从未触发。中英双判兜底。
    if (e?.message?.includes('feature') || e?.message?.includes('功能') || e?.message?.includes('套餐')) {
      toast({ type: 'warning', message: '当前套餐未开通抽奖助手, 请联系管理员在套餐管理勾选' })
    }
  }
}

async function loadAccounts() {
  try {
    const res = await accountApi.list()
    accounts.value = res.list || []
  } catch { /* 列表加载失败不阻断 */ }
}

// R128 清空全部参与记录: 确认 → 删除接口 → 重载 → toast
// 注: 本页请求走裸 request(与 toggleMaster 等一致), 不走 taskApi
const clearing = ref(false)
async function clearAllResults() {
  if (clearing.value) return
  if (!confirm('确定清空全部参与记录？此操作不可恢复。')) return
  clearing.value = true
  try {
    const res: any = await request.delete('/raffle/results')
    results.value = []
    toast({ type: 'success', message: res?.message || '已清空全部参与记录' })
  } catch (e: any) {
    toast({ type: 'error', message: e?.message || '清空失败' })
  } finally { clearing.value = false }
}

async function toggleMaster() {
  if (toggling.value) return
  toggling.value = true
  try {
    const res = await request.post<{ enabled: boolean }>('/raffle/toggle', { enabled: !raffleEnabled.value })
    raffleEnabled.value = !!res.enabled
    toast({ type: 'success', message: res.enabled ? '抽奖助手已开启' : '抽奖助手已关闭' })
  } catch (e: any) {
    // 失败必须可见(此前静默吞错, 用户以为开关坏了): 403=套餐未开通/权限缓存未刷新
    toast({ type: 'error', message: (e?.message || '开启失败') + (e?.message?.includes('feature') ? '(套餐未勾选抽奖或刚保存权限未生效, 等1分钟重试)' : '') })
  }
  finally { toggling.value = false }
}

async function toggleAccount(acc: any) {
  const next = acc.raffle_enabled === false
  try {
    await accountApi.setRaffle(acc.account_id, next)
    acc.raffle_enabled = next
    toast({ type: 'success', message: `${acc.remark || acc.phone} 抽奖已${next ? '开启' : '关闭'}` })
  } catch { /* 请求层统一报错 */ }
}

async function toggleAll() {
  const target = !allSelected.value
  for (const acc of accounts.value) {
    if ((acc.raffle_enabled !== false) !== target) await toggleAccount(acc)
  }
}

// ===== 参与记录(058) =====
const results = ref<any[]>([])
async function loadResults() {
  try {
    const res = await request.get<{ list: any[] }>('/raffle/results')
    results.value = res.list || []
  } catch { /* 未开通套餐时 403, 静默 */ }
}
// R148: 记录按活动分组(同一群+同一关键词=同一活动), 最新活动在前, 卡内账号按时间排序
const groupedResults = computed(() => {
  const m = new Map()
  for (const r of results.value) {
    const k = `${r.tg_chat_id}|${r.keyword || ''}`
    if (!m.has(k)) {
      m.set(k, { key: k, keyword: r.keyword || '', chatId: r.tg_chat_id, chatTitle: r.chat_title, time: fmtTime(r.created_at), items: [] })
    }
    m.get(k).items.push(r)
  }
  const list = [...m.values()].sort((a, b) => {
    const ta = a.items[0]?.created_at || ''
    const tb = b.items[0]?.created_at || ''
    return ta < tb ? 1 : -1
  })
  for (const g of list) {
    g.items.sort((a, b) => (a.created_at < b.created_at ? -1 : 1))
    g.wins = g.items.filter((r: any) => r.kind === 'winner').length
  }
  return list
})

function fmtTime(v: any): string {
  if (!v) return ''
  const d = new Date(v)
  return isNaN(d.getTime()) ? String(v) : `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

onMounted(() => {
  loadRaffleStatus()
  loadAccounts()
  loadResults()
})
</script>
