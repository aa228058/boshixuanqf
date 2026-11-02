// Phase 10: utils/health 单测——P1-13 修复点: health_score undefined → NaN
// 曾导致新账号健康分落到玫瑰红"风险"档; 修复后缺分按中间档处理。
import { describe, it, expect } from 'vitest'
import { getHealthBg } from '../health'

describe('getHealthBg', () => {
  it('undefined/null 不落入风险红(中间档兜底)', () => {
    expect(getHealthBg(undefined)).not.toContain('rose')
    expect(getHealthBg(null)).not.toContain('rose')
  })
  it('NaN 同样兜底(P1-13 根因)', () => {
    expect(getHealthBg(Number.NaN)).not.toContain('rose')
  })
  it('正常分档: >=80 绿, 60-79 黄, <60 红', () => {
    expect(getHealthBg(90)).toContain('emerald')
    expect(getHealthBg(70)).toContain('amber')
    expect(getHealthBg(30)).toContain('rose')
  })
  it('边界与越界收敛到 [0,100]', () => {
    expect(getHealthBg(150)).toBe(getHealthBg(100))
    expect(getHealthBg(-5)).toBe(getHealthBg(0))
  })
})
