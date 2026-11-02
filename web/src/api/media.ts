import { request } from '@/utils/request'

// 上传媒体文件(图片/视频), 返回服务器相对路径, 用于广播 payload 的 media.source
export const mediaApi = {
  uploadMedia(file: File): Promise<{ path: string }> {
    const fd = new FormData()
    fd.append('file', file)
    // request 对 FormData 自动跳过 JSON 序列化并去掉 Content-Type 让浏览器带 boundary
    return request<{ path: string }>('/media', { method: 'POST', body: fd })
  },
}
