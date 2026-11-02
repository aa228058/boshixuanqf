// P2-15: 原实现不防公式注入——单元格以 = + - @ 开头时, Excel/WPS 打开会当公式
// 执行(=cmd|'/c...' 类 CSV 注入)。导出是备份/交接场景, 中招面真实存在。
// 处理: 危险前缀前加单引号(Excel 约定为纯文本), 再照常做引号转义。
function escapeCSV(value: string | number): string {
  let str = String(value)
  if (/^[=+\-@]/.test(str)) {
    str = `'${str}`
  }
  // 如果包含逗号、引号或换行符，需要用引号包裹
  if (/[",\n\r]/.test(str)) {
    return `"${str.replace(/"/g, '""')}"`
  }
  return str
}

export function downloadCSV(filename: string, headers: string[], rows: (string | number)[][]) {
  const csv = [headers.map(escapeCSV).join(','), ...rows.map(r => r.map(escapeCSV).join(','))].join('\n')
  const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}

// R117: downloadJSON 已删(零外部引用; downloadCSV 自拼 CSV 不依赖它)

export function parseLinks(text: string): { valid: string[]; invalid: string[] } {
  const lines = text.split(/\n/).map(l => l.trim()).filter(Boolean)
  const valid: string[] = []
  const invalid: string[] = []
  const pattern = /^https?:\/\/(t\.me|telegram\.me)\/[-_a-zA-Z0-9+]+$/
  for (const line of lines) {
    if (pattern.test(line)) valid.push(line)
    else invalid.push(line)
  }
  return { valid, invalid }
}
