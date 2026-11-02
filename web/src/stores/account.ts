import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import { accountApi } from '@/api/account'
import type { Account, AccountGroup, AccountStatus } from '@/types'
import { onMutation } from '@/utils/mutationBus'

const CURRENT_ACCOUNT_KEY = 'tg_current_account'

export const useAccountStore = defineStore('account', () => {
  const accounts = ref<Account[]>([])
  const groups = ref<AccountGroup[]>([])
  const loading = ref(false)
  // 当前选中账号（从 localStorage 恢复）
  const currentAccount = ref<Account | null>(null)

  // ===== 正常号 / 协议号 两个世界 =====
  const phoneAccounts = computed<Account[]>(() => accounts.value.filter((a: any) => a && a.source !== 'protocol'))
  const protocolAccounts = computed<Account[]>(() => accounts.value.filter((a: any) => a && a.source === 'protocol'))
  // P0-25 最终决策(与产品确认): 协议号就是普通号——侧边栏全量账号一起展示
  // (List.vue viewAccounts 不分来源), 选中即操作, 任务引擎后端对 source 无任何
  // 限制。因此不搞"有协议号就整体切协同视图"的自动劫持(会破坏普通号操作),
  // view 保持 'phone' 常量。协同 campaign 模式(view='protocol', 多协议号联合
  // 任务)为休眠功能, 待产品决定独立入口后再启用。
  const view = ref<'phone' | 'protocol'>('phone')
  // 视图切换后的选中账号归位(留待协同模式启用时使用)
  function syncCurrentAccountToView() {
    const list = view.value === 'phone' ? phoneAccounts.value : protocolAccounts.value
    if (!currentAccount.value || !list.find((a: Account) => a.account_id === (currentAccount.value as any)?.account_id)) {
      const online = list.find((a: Account) => a.status === 'online')
      currentAccount.value = online || list[0] || null
    }
  }
  // view 变化时归位选中账号(承接原 setView 的副作用)
  watch(view, syncCurrentAccountToView)

  // 60 秒新鲜期: 切页面/重复进入时不重复拉取, 避免每次进页面都等待
  let lastAccountsFetch = 0
let lastAccountsSnapshot = ''
  // force=true 用于轮询：绕过 60s 新鲜期，否则状态轮询全部空转
  async function fetchAccounts(force = false) {
    if (!force && Date.now() - lastAccountsFetch < 60_000 && accounts.value.length) return
    lastAccountsFetch = Date.now()
    // R155: 轮询刷新不置 loading —— 否则每 30s 列表被加载转圈整体替换, 界面持续闪烁
    if (force || !accounts.value.length) loading.value = true
    try {
      const res: any = await accountApi.list(!force) // 轮询(非force)走 silent: 不触发顶部进度条
      // 后端 ListResponse 返回 data 字段, 兼容直返数组
      // 兼容 ListResponse 三种形态（res.data 可能是 {list,total} 对象）
      const raw = res.list || res.data?.list || (Array.isArray(res.data) ? res.data : []) || (Array.isArray(res) ? res : [])
      // R122 根治: 源头过滤非法元素 —— 后端返回的数组里偶发混入 null/undefined,
      // 下游任何对元素读 .status/.level 的计算属性/模板都会整页崩溃(reading 'status')。
      // 在 store 赋值处一次性清洗, 比给几十个读取点打补丁彻底。
      const next = (Array.isArray(raw) ? raw : []).filter((a: any) => a && typeof a === 'object')
      // 2026-10-21 轮询根治: 数据与当前完全一致时跳过赋值——数组引用不变 →
      // Vue 不会重渲染列表 → 交错入场/行浮现动画不会重播, "页面像刷新"的观感根除
      const snap = JSON.stringify(next)
      if (snap !== lastAccountsSnapshot) {
        lastAccountsSnapshot = snap
        accounts.value = next
      }
      // 尝试从 localStorage 恢复选中账号
      const savedId = localStorage.getItem(CURRENT_ACCOUNT_KEY)
      if (savedId) {
        const saved = accounts.value.find((a: Account) => a.account_id === savedId)
        if (saved) {
          currentAccount.value = saved
          return
        }
      }
      // 如果没有保存的账号，默认选第一个在线账号
      if (!currentAccount.value && accounts.value.length > 0) {
        const online = accounts.value.find((a: Account) => a.status === 'online')
        currentAccount.value = online || accounts.value[0]
      }
    } finally {
      loading.value = false
    }
  }

  // 监听 currentAccount 变化，自动持久化
  watch(currentAccount, (acc) => {
    if (acc) {
      localStorage.setItem(CURRENT_ACCOUNT_KEY, acc.account_id)
    } else {
      localStorage.removeItem(CURRENT_ACCOUNT_KEY)
    }
  }, { immediate: true })

  async function createAccount(phone: string, remark?: string) {
    const res = await accountApi.create(phone, remark)
    const acc = (res && typeof res === 'object' && 'data' in res) ? (res as Record<string, unknown>).data as Account : res as Account
    accounts.value.push(acc)
    return acc
  }

  async function deleteAccount(id: string) {
    await accountApi.delete(id)
    accounts.value = accounts.value.filter(a => a.account_id !== id)
    if (currentAccount.value?.account_id === id) {
      currentAccount.value = accounts.value[0] || null
    }
  }

  // SSE account_status 事件：就地更新状态，无需整表重拉
  function applyStatus(accountID: string, status: string) {
    const hit = accounts.value.find((a: any) => a.account_id === accountID)
    if (hit) hit.status = status as AccountStatus
    if ((currentAccount.value as any)?.account_id === accountID) {
      ;(currentAccount.value as any).status = status
    }
  }

  // 全局变更总线：/accounts 写操作成功后自动强刷列表。
  // P2-70: 原 /\/accounts/ 过宽——/accounts/import、/accounts/profile-batch、
  // /accounts/clone-profile 等批量端点每处理完一个号就触发一次全量列表刷新,
  // 批量改 100 个号 = 100 次全量拉取。排除已知批量子路径, 只响应单号增删改。
  onMutation(/\/accounts(?!\/(import|profile-batch|clone-profile)\b)/, () => fetchAccounts(true))

  return { accounts, groups, loading, currentAccount, fetchAccounts, createAccount, deleteAccount,
    view, phoneAccounts, protocolAccounts, applyStatus }
})
