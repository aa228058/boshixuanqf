// R117: getHealthColor/getHealthLabel 已删(零外部引用——账号页用 Tailwind 类版 getHealthBg)

// P1-13 审计修复: 原实现 undefined → Math.min(100, undefined)=NaN → Math.max(0,NaN)=NaN
// → 落到玫瑰红——健康分尚未计算的新账号全部显示"风险"。缺分按中间档处理。
export function getHealthBg(score: number | undefined | null): string {
  if (score === undefined || score === null || Number.isNaN(score)) {
    return 'bg-amber/10 text-amber border-amber/20'
  }
  const s = Math.max(0, Math.min(100, score))
  if (s >= 80) return 'bg-emerald/10 text-emerald border-emerald/20'
  if (s >= 60) return 'bg-amber/10 text-amber border-amber/20'
  return 'bg-rose/10 text-rose border-rose/20'
}
