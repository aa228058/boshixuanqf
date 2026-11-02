// Phase 10: utils/export 单测——测导出函数 parseLinks 的契约。
// 注: escapeCSV(公式注入防护, P2-15) 是模块私有, 经 downloadCSV 间接生效;
// downloadCSV 触发真实浏览器下载, 单测环境不测副作用, 防护语义由 Code Review
// 与后续集成测试覆盖。此处聚焦纯函数 parseLinks。
import { describe, it, expect } from 'vitest'
import { parseLinks } from '../export'

describe('parseLinks', () => {
  it('提取 t.me 链接(实现只认 telegram 域名, 按磁盘行为断言)', () => {
    const r = parseLinks('https://t.me/abc\nhttps://t.me/xyz123')
    expect(r.valid.length).toBe(2)
  })
  it('非 telegram 链接进 invalid 不混进 valid', () => {
    const r = parseLinks('https://example.com/a\n随便一行字')
    expect(r.valid.length).toBe(0)
    expect(r.invalid.length).toBe(2)
  })
})
