// 管理端共享格式化与常量层。
// 后端列表字段多为 PascalCase,前端不做归一化,仅统一展示格式与单位(分→元)。

// 分(cents) → 元,2 位小数。
export function formatCents(c) {
  if (c === null || c === undefined || isNaN(c)) return '0.00'
  return (Number(c) / 100).toFixed(2)
}
// 分 → 元(4 位高精度,账本/明细用)。
export function formatCents4(c) {
  if (c === null || c === undefined || isNaN(c)) return '0.0000'
  return (Number(c) / 100).toFixed(4)
}
// 分 → 带前缀元金额。
export function yuan(c) {
  return '¥' + formatCents(c)
}
// 分 → "¥X.XX /M"(模型百万 token 单价)。
export function yuanPerM(c) {
  return '¥' + formatCents(c) + '/M'
}
// 整数千分位。
export function num(n) {
  if (n === null || n === undefined || isNaN(n)) return '0'
  return Number(n).toLocaleString('en-US')
}
// 上下文长度简写。
export function fmtCtx(n) {
  if (!n) return '—'
  if (n >= 1000000) return (n / 1000000).toFixed(n % 1000000 === 0 ? 0 : 1) + 'M'
  if (n >= 1000) return Math.round(n / 1000) + 'K'
  return String(n)
}
// 时间 → "YYYY-MM-DD HH:mm"。
export function fmtTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (isNaN(d.getTime())) return '—'
  const p = (x) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
// 统一错误信息抽取。
export function apiErr(e, fallback = '操作失败') {
  return e?.response?.data?.error || e?.message || fallback
}
// 通用状态(active/disabled/revoked)。
const STATUS = {
  active: { label: '启用', type: 'success' },
  enabled: { label: '启用', type: 'success' },
  disabled: { label: '禁用', type: 'error' },
  revoked: { label: '已吊销', type: 'error' },
}
export function statusLabel(s) { return STATUS[s]?.label || s || '—' }
export function statusType(s) { return STATUS[s]?.type || 'default' }

// 审计动作着色:危险动作红、创建绿、登录类蓝、其余默认。
const ACTION_PREFIX = {
  delete: 'error', remove: 'error', revoke: 'error', disable: 'error',
  create: 'success', add: 'success', enable: 'success',
  login: 'info', logout: 'info', register: 'info',
  update: 'warning', set: 'warning', reset: 'warning',
}
export function actionMeta(action) {
  const a = String(action || '').toLowerCase()
  for (const k of Object.keys(ACTION_PREFIX)) {
    if (a.includes(k)) return ACTION_PREFIX[k]
  }
  return 'default'
}

// 供应商预设: 下拉展示用户熟悉的名字,选中自动填 adapter(内部协议)+base_url+默认名称。
// adapter(openaicomp/mock)是实现细节,不暴露给用户。与 cmd/gateway/seed.go 同源,改动需同步。
// 未来加 anthropic/gemini 出口 adapter 后,在此追加对应预设即可,前端结构不变。
export const PROVIDER_PRESETS = [
  { label: '阿里云百炼', adapter: 'openaicomp', base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1', name: '百炼 · 通义千问' },
  { label: '火山方舟', adapter: 'openaicomp', base_url: 'https://ark.cn-beijing.volces.com/api/v3', name: '火山方舟 · 豆包' },
  { label: '百度千帆', adapter: 'openaicomp', base_url: 'https://qianfan.baidubce.com/v2', name: '千帆 · 文心' },
  { label: 'DeepSeek', adapter: 'openaicomp', base_url: 'https://api.deepseek.com', name: 'DeepSeek' },
  { label: '智谱 GLM', adapter: 'openaicomp', base_url: 'https://open.bigmodel.cn/api/paas/v4', name: '智谱 · GLM' },
  { label: 'Anthropic (Claude)', adapter: 'anthropic', base_url: 'https://api.anthropic.com', name: 'Anthropic · Claude' },
  { label: 'OpenAI 兼容(自定义)', adapter: 'openaicomp', base_url: '', name: '' },
  { label: 'Mock(开发/测试)', adapter: 'mock', base_url: '', name: 'Mock' },
]
// 据 adapter + base_url 反查预设名(列表展示 / 编辑回显)。匹配不上→自定义或 Mock。
export function presetLabel(adapter, baseURL) {
  const hit = PROVIDER_PRESETS.find(p => p.adapter === adapter && p.base_url && p.base_url === (baseURL || ''))
  if (hit) return hit.label
  if (adapter === 'mock') return 'Mock'
  if (adapter === 'openaicomp') return 'OpenAI 兼容'
  return adapter || '—'
}
// 退化显示(无 base_url 可推断时)。
export function provLabel(p) {
  return { openaicomp: 'OpenAI 兼容', mock: 'Mock' }[p] || p || '—'
}
// 统一分页配置。
export const PAGINATION = { pageSize: 20, showSizePicker: true, pageSizes: [20, 50, 100] }
