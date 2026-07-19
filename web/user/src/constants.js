// 用户端共享常量:供应商/能力映射,供筛选下拉与展示标签复用。

// provider 内部标识 → 中文展示名。
export const PROVIDER_LABELS = {
  mock: 'Mock(开发)',
  bailian: '阿里云百练',
  volcark: '火山方舟',
  qianfan: '百度千帆',
  deepseek: 'DeepSeek',
  zhipuai: '智谱 GLM',
}
// provider → 标签颜色类型(Naive UI NTag type)。
export const PROVIDER_TAG_TYPES = {
  mock: 'default',
  bailian: 'success',
  volcark: 'warning',
  qianfan: 'info',
  deepseek: 'error',
  zhipuai: 'info',
}
export function provLabel(p) {
  return PROVIDER_LABELS[p] || p
}
export function provTagType(p) {
  return PROVIDER_TAG_TYPES[p] || 'default'
}

// 公开筛选用供应商选项(mock 仅开发环境显示)。
export const PROVIDER_OPTIONS = [
  { label: '阿里云百练', value: 'bailian' },
  { label: '火山方舟', value: 'volcark' },
  { label: '百度千帆', value: 'qianfan' },
  { label: 'Mock(开发)', value: 'mock' },
]
// 能力多标签(参考智谱/OpenAI:一个模型可同时具备多种能力)。
// cap → {label, icon}
export const CAPABILITIES = {
  text:           { label: '文本',   icon: '💬' },
  vision:         { label: '视觉',   icon: '🖼️' },
  audio:          { label: '音频',   icon: '🔊' },
  file:           { label: '文件',   icon: '📎' },
  function_call:  { label: '工具调用', icon: '🛠️' },
  reasoning:      { label: '推理',   icon: '🧠' },
  code:           { label: '代码',   icon: '💻' },
  web_search:     { label: '联网',   icon: '🌐' },
}
export const CAPABILITY_OPTIONS = Object.entries(CAPABILITIES).map(([v, m]) => ({ label: `${m.icon} ${m.label}`, value: v }))
export function capIcon(c) { return CAPABILITIES[c]?.icon || '' }
export function capLabel(c) { return CAPABILITIES[c]?.label || c }
// 模型是否具备某能力(用于筛选)。
export function hasCapability(caps, key) {
  return (caps || []).includes(key)
}
export const STATUS_META = {
  active: { label: '启用', type: 'success' },
  enabled: { label: '启用', type: 'success' },
  disabled: { label: '禁用', type: 'error' },
  revoked: { label: '已吊销', type: 'error' },
  expired: { label: '已过期', type: 'warning' },
}
export function statusLabel(s) {
  return STATUS_META[s]?.label || s || '—'
}
export function statusType(s) {
  return STATUS_META[s]?.type || 'default'
}

export function hasModality(m, key) {
  const mod = m.modality || ''
  return mod.includes(key) || mod === 'multimodal'
}

// ── 模型分类:左侧筛选面板的"模型类型"分组 ──
// 同时参考 modality 字段与 capabilities,适配不同供应商返回粒度。
export const MODEL_CATEGORIES = {
  text:       { label: '文本模型',   icon: '📝' },
  vision:     { label: '视觉模型',   icon: '🖼️' },
  audio:      { label: '音频模型',   icon: '🔊' },
  multimodal: { label: '全模态模型', icon: '🌐' },
  embedding:  { label: '向量模型',   icon: '📊' },
}
// 分类下拉选项(NSelect / NTag 复用)。
export const CATEGORY_OPTIONS = Object.entries(MODEL_CATEGORIES).map(
  ([v, m]) => ({ label: `${m.icon} ${m.label}`, value: v })
)
// 模型归类:优先 modality 字段,回退 capabilities 推断。供左侧筛选判断归属。
export function modelCategory(m) {
  const caps = m.capabilities || []
  const mod = m.modality || ''
  if (mod === 'multimodal' || (caps.includes('vision') && caps.includes('audio'))) return 'multimodal'
  if (mod.includes('vision') || caps.includes('vision')) return 'vision'
  if (mod.includes('audio') || caps.includes('audio')) return 'audio'
  if (mod.includes('embedding') || caps.includes('embedding')) return 'embedding'
  return 'text'
}

// ── 能力标签颜色:让卡片上的能力 chip 有区分度,而非全部 default ──
export const CAPABILITY_TAG_TYPES = {
  text:          'default',
  vision:        'success',
  audio:         'warning',
  file:          'info',
  function_call: 'info',
  reasoning:     'error',
  code:          'info',
  web_search:    'success',
}
export function capTagType(c) {
  return CAPABILITY_TAG_TYPES[c] || 'default'
}
