// 共享模型类型(与后端 internal/domain 对齐)
export interface SystemGroup {
  sg_id: string
  source: string // scraped | joined | bio
  title: string
  username?: string
  tme_link?: string
  gtype: string // public | private | channel | unknown
  member_count: number
  status: string // active | joined | invalid
  ref_count: number
  last_seen_at: string
}
