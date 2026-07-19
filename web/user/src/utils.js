// 用户端共享格式化与常量层。
// 集中货币/时间/单位/供应商映射,消除各页面重复定义与单位漂移。

// 分(cents) → 元字符串,固定 2 位小数。
export function formatCents(c) {
  if (c === null || c === undefined || isNaN(c)) return '0.00'
  return (Number(c) / 100).toFixed(2)
}
// 分 → 带前缀的元金额,如 ¥12.34。
export function formatYuan(c) {
  return '¥' + formatCents(c)
}
// 高精度元(账本/明细用,4 位小数)。
export function formatYuanPrecise(c) {
  if (c === null || c === undefined || isNaN(c)) return '0.0000'
  return (Number(c) / 100).toFixed(4)
}
// 整数千分位。
export function formatNum(n) {
  if (n === null || n === undefined || isNaN(n)) return '0'
  return Number(n).toLocaleString('en-US')
}
// 上下文长度简写: 32768 → "32K",1000000 → "1M"。
export function formatCtx(n) {
  if (!n) return '—'
  if (n >= 1000000) return (n / 1000000).toFixed(n % 1000000 === 0 ? 0 : 1) + 'M'
  if (n >= 1000) return Math.round(n / 1000) + 'K'
  return String(n)
}
// 时间 → "YYYY-MM-DD HH:mm",空值返回 "—"。
export function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (isNaN(d.getTime())) return '—'
  const p = (x) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}
// 安全复制,返回 Promise<boolean>。
export async function copyText(text) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch { /* 回退到 execCommand */ }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}

// CSV 单元格转义:含逗号/引号/换行时用双引号包裹并把内部 " 翻倍。
function csvCell(v) {
  const s = v === null || v === undefined ? '' : String(v)
  if (/[",\n\r]/.test(s)) return '"' + s.replace(/"/g, '""') + '"'
  return s
}

// exportCSV: 把行对象数组导出为 CSV 并触发下载(客户端,无需后端)。
// rows: 对象数组; columns: [{key, label}] 列定义。
export function exportCSV(filename, rows, columns) {
  const head = columns.map((c) => csvCell(c.label)).join(',')
  const body = (rows || [])
    .map((r) => columns.map((c) => csvCell(r[c.key])).join(','))
    .join('\n')
  // 带 BOM,确保 Excel 正确识别 UTF-8 中文。
  const blob = new Blob(['﻿' + head + '\n' + body], { type: 'text/csv;charset=utf-8;' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

