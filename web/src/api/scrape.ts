import { api } from '@/utils/request'
import type { Member, Task, ListResponse } from '@/types'

// P2-72: exportMembers 已删——全项目零调用(protocol/Scrape.vue 用 fetch+blob 自实现),
// 且此实现走 api.get 的 JSON 解析, 对文件下载语义错误。真要用导出请走 fetch+blob。
export const scrapeApi = {
}
