// createIdempotencyKey 负责为写接口生成幂等键。
// 职责边界：
// 1. 只负责生成前端唯一键，不负责持久化或重试策略。
// 2. 优先使用浏览器原生 randomUUID，缺失时退回时间戳方案。
export function createIdempotencyKey(prefix = 'smartflow') {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return `${prefix}-${crypto.randomUUID()}`
  }

  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`
}
