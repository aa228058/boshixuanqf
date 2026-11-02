import { api } from '@/utils/request'

export interface AIConfig {
  provider: string
  model: string
  has_key: boolean
  enabled: boolean
}

export function getAIConfig(): Promise<AIConfig> {
  return api.get('/ai-config')
}

export function saveAIConfig(data: { provider?: string; api_key?: string; model?: string }): Promise<{ ok: boolean }> {
  return api.put('/ai-config', data)
}

export function testAIConfig(): Promise<{ ok: boolean; answer: string }> {
  return api.post('/ai-config/test')
}

export function setVerifierEnabled(enabled: boolean): Promise<{ ok: boolean }> {
  return api.post('/verifier/toggle', { enabled })
}

// 抽奖助手: 群内抽奖公告自动参与
export function getRaffleStatus(): Promise<{ enabled: boolean }> {
  return api.get('/raffle/status')
}

export function setRaffleEnabled(enabled: boolean): Promise<{ ok: boolean }> {
  return api.post('/raffle/toggle', { enabled })
}

export interface VerificationLog {
  vl_id: string
  account_id: string
  group_chat_id: number
  group_title: string
  status: 'pending' | 'pass' | 'fail' | 'stopped'
  reason: string
  steps: { action: string; detail: string; at: string }[]
  created_at: string
}

export function getVerificationLogs(limit = 50): Promise<{ list: VerificationLog[] }> {
  return api.get('/verification-logs', { params: { limit } })
}
