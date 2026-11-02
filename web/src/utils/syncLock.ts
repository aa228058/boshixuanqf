// Global sync lock to prevent multiple components from syncing simultaneously for the same accountId.
// This prevents duplicate Telegram API calls and reduces flood wait risk.

let syncingAccountId: string | null = null
let syncStartTime = 0
const SYNC_LOCK_TTL = 60000 // 60 seconds max lock duration (safety net)
const SYNC_INTERVAL = 10 * 60 * 1000 // 10 minutes between syncs

export const syncLock = {
  isSyncing(accountId?: string): boolean {
    if (!syncingAccountId) return false
    // Safety: auto-release if lock is too old
    if (Date.now() - syncStartTime > SYNC_LOCK_TTL) {
      this.release()
      return false
    }
    // If no accountId specified, any sync is considered active
    if (!accountId) return true
    return syncingAccountId === accountId
  },
  acquire(accountId: string): boolean {
    if (this.isSyncing(accountId)) return false
    syncingAccountId = accountId
    syncStartTime = Date.now()
    return true
  },
  release() {
    syncingAccountId = null
    syncStartTime = 0
  },
  getAccountId(): string | null {
    return syncingAccountId
  },
}

// Persistent sync cache: prevents re-syncing within 10 minutes
export function needsSync(accountID: string): boolean {
  const key = `last_sync_${accountID}`
  const last = localStorage.getItem(key)
  if (!last) return true
  // P2-34: 原 parseInt 对脏数据(手写/损坏值)返回 NaN, NaN > interval 恒 false——
  // 同步逻辑永远判断"不需要同步"。NaN 视为需要同步。
  const t = parseInt(last)
  if (Number.isNaN(t)) return true
  return Date.now() - t > SYNC_INTERVAL
}

export function markSyncing(accountID: string): void {
  localStorage.setItem(`last_sync_${accountID}`, Date.now().toString())
}

// R117: clearSyncCache 已删(零外部引用)
export default syncLock
