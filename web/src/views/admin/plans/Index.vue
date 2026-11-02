<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div>
        <h2 class="text-lg font-bold text-text-primary">套餐管理</h2>
        <p class="text-xs text-text-secondary mt-1">
          勾选控制每档套餐能用的功能；右侧数值改规模与限额（0 = 不限）。保存后立即生效。
        </p>
      </div>
      <t-button size="small" theme="primary" :disabled="saving" class="whitespace-nowrap shrink-0" @click="saveAll">
        <Loader2 v-if="saving" class="w-3.5 h-3.5 animate-spin mr-1 shrink-0" />
        保存全部
      </t-button>
    </div>

    <!-- 加载失败提示(不再静默空白) -->
    <div v-if="loadError" class="glass rounded-[var(--t-r-md)] p-4 bg-amber/10 text-amber text-sm lift">
      {{ loadError }}
    </div>

    <!-- 数值区: 每档一卡 -->
    <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-3">
      <div v-for="p in plans" :key="p.plan_id" class="glass rounded-[var(--t-r-md)] p-4 space-y-2.5 lift">
        <div class="flex items-center justify-between">
          <span class="font-semibold text-sm">{{ p.name }}</span>
          <span class="text-xs text-text-muted">{{ p.plan_id }}</span>
        </div>
        <label class="block text-xs text-text-secondary">月价（元）
          <input v-model.number="p.price_monthly" type="number" min="0" step="1"
            class="mt-0.5 w-full px-2.5 py-1.5 rounded-lg glow-input text-sm bg-bg-card" />
        </label>
        <div class="grid grid-cols-2 gap-2">
          <label class="block text-xs text-text-secondary">账号数
            <input v-model.number="p.max_accounts" type="number" min="1"
              class="mt-0.5 w-full px-2.5 py-1.5 rounded-lg glow-input text-sm bg-bg-card" />
          </label>
          <label class="block text-xs text-text-secondary">并发
            <input v-model.number="p.max_concurrent" type="number" min="1"
              class="mt-0.5 w-full px-2.5 py-1.5 rounded-lg glow-input text-sm bg-bg-card" />
          </label>
          <label class="block text-xs text-text-secondary">群发日限(0不限)
            <input v-model.number="p.max_daily_broadcasts" type="number" min="0"
              class="mt-0.5 w-full px-2.5 py-1.5 rounded-lg glow-input text-sm bg-bg-card" />
          </label>
          <label class="block text-xs text-text-secondary">搜群日限(0不限)
            <input v-model.number="p.daily_search_limit" type="number" min="0"
              class="mt-0.5 w-full px-2.5 py-1.5 rounded-lg glow-input text-sm bg-bg-card" />
          </label>
        </div>
      </div>
    </div>

    <!-- 功能矩阵 -->
    <div class="glass rounded-[var(--t-r-md)] overflow-hidden lift">
      <table class="w-full text-sm">
        <thead>
          <tr class="border-b border-border text-left">
            <th class="px-4 py-3 text-text-secondary font-medium w-56">功能</th>
            <th v-for="p in plans" :key="p.plan_id" class="px-3 py-3 text-center text-text-secondary font-medium">
              {{ p.name }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="f in featureList" :key="f.key" class="border-b border-border/40 last:border-0 hover:bg-milk-01">
            <td class="px-4 py-2.5 text-text-primary">{{ f.label }}</td>
            <td v-for="p in plans" :key="p.plan_id" class="px-3 py-2.5 text-center">
              <input type="checkbox" class="accent-cyan w-4 h-4 cursor-pointer"
                :checked="hasFeature(p, f.key)"
                @change="toggleFeature(p, f.key)" />
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <p class="text-xs text-text-muted">
      提示：「免费体验版」建议保持全功能（转化钩子）；基础版勾少一点形成阶梯；改动实时写入，租户下次请求即按新权限生效。
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { Loader2 } from 'lucide-vue-next'
import { adminApiMod } from '@/api/admin'
import { toast } from '@/utils/toast'

interface PlanRow {
  plan_id: string
  name: string
  price_monthly: number
  max_accounts: number
  max_concurrent: number
  max_daily_broadcasts: number
  daily_search_limit: number
  features: string[]
}

const plans = ref<PlanRow[]>([])
const saving = ref(false)

// 套餐功能矩阵(2026-10-20 适配): 已随 UI 重构清理——
// 删: 关键词搜群(search 并入 discover)/邀请入群(并入加群)/账号检测/代理设置/AI图片验证/数据导出
// 增: 循环群发(loop_broadcast)/抽奖助手(raffle)/邀请入群独立 feature
const featureList = [
  { key: 'broadcast', label: '群发消息' },
  { key: 'loop_broadcast', label: '循环群发' },
  { key: 'join_groups', label: '加群任务' },
  { key: 'invite', label: '邀请入群' },
  { key: 'scrape_members', label: '成员采集' },
  { key: 'scrape_links', label: '链接采集' },
  { key: 'monitor', label: '消息监控' },
  { key: 'templates', label: '消息模板' },
  { key: 'scheduled_tasks', label: '定时任务' },
  { key: 'discover', label: '发现群组·群库(含搜群)' },
  { key: 'verifier', label: '入群验证(AI 图片验证已并入)' },
  { key: 'dict', label: '文案变量（高级）' },
  { key: 'api', label: 'API 接入' },
  { key: 'raffle', label: '抽奖助手' },
  { key: 'notify', label: '邮件通知' },
]

function hasFeature(p: PlanRow, key: string) {
  return (p.features || []).includes(key)
}
function toggleFeature(p: PlanRow, key: string) {
  const set = new Set(p.features || [])
  set.has(key) ? set.delete(key) : set.add(key)
  p.features = [...set]
}

const loadError = ref('')

async function load() {
  loadError.value = ''
  try {
    const res = await adminApiMod.listPlans()
    plans.value = (res.list || []).map((p: any) => ({
      ...p,
      // 兼容双格式(2026-10-20): 后端存储已从数组改为 map {"raffle":true,...},
      // 加载时统一转成数组供勾选框使用(旧数组数据原样, 新 map 数据取 keys)
      features: Array.isArray(p.features)
        ? p.features
        : Object.keys(p.features || {}),
    }))
    if (!plans.value.length) {
      loadError.value = '套餐列表为空 —— 请检查迁移 051 是否已执行(plans 表结构是否含 daily_search_limit 列)'
    }
  } catch (e: any) {
    loadError.value = e?.message || '请求失败(401/500?)'
    toast({ type: 'error', message: '套餐加载失败: ' + loadError.value })
  }
}

// P2-47 审计修复: 原 4 档套餐 for+await 串行保存(每档一次 RTT, 全量保存 4 倍耗时)。
// 改并行: 各档独立 then/catch, 失败仍逐档提示, 成功计数不变。
async function saveAll() {
  saving.value = true
  let ok = 0
  const jobs = plans.value.map((p) =>
    adminApiMod.updatePlan(p.plan_id, {
      name: p.name,
      price_monthly: p.price_monthly,
      max_accounts: p.max_accounts,
      max_concurrent: p.max_concurrent,
      max_daily_broadcasts: p.max_daily_broadcasts,
      daily_search_limit: p.daily_search_limit,
      // 保持数组格式(2026-10-20 回滚): Plan.Features 后端结构体就是 []string,
      // 后端 FeatureMap() 会把数组转 map 供校验——数组才是全链路的原生格式。
      // 此前误改 map 存储导致 Unmarshal 失败/勾选全丢/功能校验瘫痪, 已回滚+迁移 059 修复存量。
      features: p.features,
    }).then(() => { ok++ }).catch((e: any) => {
      toast({ type: 'error', message: `${p.name} 保存失败: ${e?.message || '未知错误'}` })
    })
  )
  await Promise.all(jobs)
  saving.value = false
  if (ok === plans.value.length) {
    toast({ type: 'success', message: '全部套餐已保存，即时生效' })
  } else {
    toast({ type: 'info', message: `已保存 ${ok}/${plans.value.length} 档` })
  }
}

onMounted(load)
</script>
