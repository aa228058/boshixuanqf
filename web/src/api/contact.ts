import { api } from '@/utils/request'

// 私聊联系人缓存(同步时入库), 群发目标选择器秒开用
export const contactApi = {
  // api.get 签名只收 endpoint 一个参数（不支持 params 选项），拼 query string
  list: (accountId: string) => api.get('/contacts?account_id=' + encodeURIComponent(accountId)),
}
