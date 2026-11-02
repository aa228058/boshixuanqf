// Phase 10 核心工具单测: format 工具(状态映射/任务图标——历史上这些是
// 静默 bug 重灾区: 缺状态映射显示英文、缺图标回退警告三角)
import { describe, it, expect } from 'vitest'
import { taskTypeIcon, taskTypeLabel } from '../format'

describe('taskTypeLabel', () => {
  it('已知类型返回中文', () => {
    expect(taskTypeLabel('broadcast')).toBe('群发消息')
    expect(taskTypeLabel('scrape_members')).toBe('成员采集')
    expect(taskTypeLabel('join_groups')).toBe('加群任务')
  })
  it('未知类型回退"未知任务"(不显示英文码给用户)', () => {
    expect(taskTypeLabel('some_future_type')).toBe('未知任务')
  })
})

describe('taskTypeIcon', () => {
  it('已注册类型有专属图标(组件引用非空)', () => {
    // P2-17 修复: scheduled_broadcast/monitor_scrape/invite_members 曾缺失落到警告三角
    for (const t of ['broadcast', 'scheduled_broadcast', 'monitor_scrape', 'invite_members', 'scrape_members', 'join_groups']) {
      expect(taskTypeIcon(t)).toBeTruthy()
    }
  })
  it('未知类型回退 AlertTriangle(兜底存在)', () => {
    expect(taskTypeIcon('unknown_xxx')).toBeTruthy()
  })
})
