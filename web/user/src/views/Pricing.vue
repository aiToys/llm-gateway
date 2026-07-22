<template>
  <div class="pg">
    <div class="head">
      <h2>定价</h2>
      <p>价格以「元 / 百万 token」计,支持按租户自定义覆盖。</p>
    </div>
    <div class="toolbar">
      <n-input v-model:value="kw" placeholder="搜索模型名" size="small" clearable style="width:220px" />
    </div>
    <n-data-table :columns="cols" :data="filtered" :bordered="false" :loading="loading"
      :pagination="{ pageSize: 20, showSizePicker: true, pageSizes: [20, 50, 100] }" />
    <p class="note">* 展示为零售价。管理员可在控制台按租户设定专属价格与成本价,系统自动核算毛利。</p>
  </div>
</template>

<script setup>
import { ref, h, computed, onMounted } from 'vue'
import { NDataTable, NTag, NInput } from 'naive-ui'
import { api, apiErr } from '../api.js'
import { formatCents, formatCtx } from '../utils.js'
import { provLabel, provTagType, CAPABILITIES } from '../constants.js'

const rows = ref([])
const loading = ref(false)
const kw = ref('')
const filtered = computed(() => {
  if (!kw.value) return rows.value
  const k = kw.value.toLowerCase()
  return rows.value.filter(r => (r.model_name || '').toLowerCase().includes(k))
})
const numSort = (key) => (a, b) => (a[key] || 0) - (b[key] || 0)
const cols = [
  { title: '模型', key: 'model_name' },
  { title: '供应商', key: 'providers', render: r => {
    const ps = r.providers || []
    if (!ps.length) return h('span', { style: 'color:#bbb' }, '—')
    return h('div', { style: 'display:flex;gap:4px;flex-wrap:wrap' }, ps.map(p => h(NTag, { size: 'small', type: provTagType(p) }, () => provLabel(p))))
  } },
  { title: '能力', key: 'capabilities', render: r => {
    const cs = r.capabilities || []
    if (!cs.length) return h('span', { style: 'color:#bbb' }, '—')
    return h('span', null, cs.map(c => CAPABILITIES[c]?.icon || '').join(' '))
  } },
  { title: '上下文', key: 'context_length', render: r => formatCtx(r.context_length), sorter: (a, b) => (a.context_length || 0) - (b.context_length || 0) },
  { title: '输入(元/M)', key: 'in', render: r => '¥' + formatCents(r.input_price_cents_per_m), sorter: numSort('input_price_cents_per_m') },
  { title: '输出(元/M)', key: 'out', render: r => '¥' + formatCents(r.output_price_cents_per_m), sorter: numSort('output_price_cents_per_m') },
]
onMounted(async () => {
  loading.value = true
  try { const { data } = await api.publicModels(); rows.value = data.data || [] }
  catch (e) { /* 公开页静默,表格自带空态 */ }
  finally { loading.value = false }
})
</script>

<style scoped>
.pg { max-width:1000px; margin:0 auto; padding:28px 24px 64px }
.head { margin-bottom:20px }
.head h2 { margin:0 0 6px; font-size:26px; color:var(--text-strong) }
.head p { color:var(--text); margin:0; line-height:1.6 }
.toolbar { margin-bottom:12px }
.note { color:var(--text-muted); font-size:13px; margin-top:16px }
</style>
