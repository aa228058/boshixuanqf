// Phase 10: utils/importTargets 单测——P2-16 相关: 引号剥离/空行/注释处理。
// 目标列表解析是群发/加群任务的第一道关, 解析错一批就要浪费一轮发送。
import { describe, it, expect } from 'vitest'
import { parseTargets } from '../importTargets'

describe('parseTargets', () => {
  it('按行拆分并去空白', () => {
    const r = parseTargets('  aaa  \nbbb\n\nccc')
    expect(r).toEqual(['aaa', 'bbb', 'ccc'])
  })
  it('剥掉包裹的引号', () => {
    const r = parseTargets('"group_one"')
    expect(r[0]).not.toContain('"')
  })
  it('空输入返回空数组不抛错', () => {
    expect(parseTargets('')).toEqual([])
  })
})
