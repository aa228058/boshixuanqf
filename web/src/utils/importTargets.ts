// 双格式目标导入解析: 支持纯行(.txt)与CSV(.csv)两种导出格式
// 每行/每行第一列是目标(@用户名 / t.me链接 / 纯ID), 其余列(昵称等)自动忽略
export function parseTargets(text: string, opts?: { keepTmeLinks?: boolean }): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim()
    if (!line) continue
    // CSV行: 拆列, 找第一列像目标的
    const cells = line.split(',').map(c => c.trim().replace(/^["']|["']$/g, ''))
    let target = ''
    for (const c of cells) {
      if (/^@?[\w.]{3,}$/.test(c) && !/^(https?|www)/i.test(c) || /^-?\d{5,}$/.test(c) || /t\.me\//.test(c)) {
        target = c
        break
      }
    }
    if (!target) continue
    if (opts?.keepTmeLinks) {
      // 加群任务: 保留 t.me 链接原样(私有邀请链接 https://t.me/+hash 改写成 @ 前缀会失效)
      if (/^\+[\w-]{5,}$/.test(target)) target = 'https://t.me/' + target
      else if (!/^@/.test(target) && !/^https?:\/\//.test(target) && !/^-?\d+$/.test(target)) target = '@' + target
    } else {
      // 归一化: t.me链接 → @用户名
      target = target.replace(/^https?:\/\/t\.me\//, '@').replace(/^t\.me\//, '@')
    }
    if (/^(内容|目标|用户名|username|link|target|昵称|name)/i.test(target)) continue // 表头
    if (!seen.has(target)) {
      seen.add(target)
      out.push(target)
    }
  }
  return out
}
