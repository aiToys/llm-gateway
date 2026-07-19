<template>
  <div>
    <div class="bar">
      <h3>计费审计</h3>
      <div class="filters">
        <n-input v-model:value="kw" placeholder="搜索 模型/请求ID" size="small" clearable style="width:200px" />
        <n-button size="small" :loading="loading" @click="load">刷新</n-button>
        <n-button size="small" type="primary" :loading="exporting" @click="exportCsv">导出 CSV</n-button>
      </div>
    </div>
    <n-data-table :columns="cols" :data="filtered" :bordered="false" size="small" :loading="loading"
      :pagination="PAGINATION" :summary="summary" />
    <p class="note">合计 · 售价 <b>{{ yuan(sum.price) }}</b> · 成本 <b>{{ yuan(sum.cost) }}</b> · 毛利 <b>{{ yuan(sum.margin) }}</b></p>
  </div>
</template>
<script setup>
import { ref, computed, onMounted } from 'vue'
import { NDataTable, NButton, NInput, useMessage } from 'naive-ui'
import { api } from '../api.js'
import { fmtTime, num, yuan, formatCents4, apiErr, PAGINATION } from '../format.js'

const message = useMessage()
const rows = ref([])
const loading = ref(false)
const exporting = ref(false)
const kw = ref('')
const filtered = computed(() => {
  if (!kw.value) return rows.value
  const k = kw.value.toLowerCase()
  return rows.value.filter(r => (r.model || '').toLowerCase().includes(k) || (r.request_id || '').toLowerCase().includes(k))
})
const sum = computed(() => filtered.value.reduce((a, r) => {
  a.price += r.price_cents || 0; a.cost += r.cost_cents || 0; a.margin += r.margin_cents || 0; return a
}, { price: 0, cost: 0, margin: 0 }))

const cols = [
  { title: '时间', key: 'created_at', render: r => fmtTime(r.created_at) },
  { title: '类型', key: 'type' },
  { title: '租户', key: 'tenant_id', ellipsis: { tooltip: true } },
  { title: '用户', key: 'user_id', ellipsis: { tooltip: true } },
  { title: '模型', key: 'model' },
  { title: '请求ID', key: 'request_id', ellipsis: { tooltip: true } },
  { title: '入/出 token', key: 'tok', render: r => `${num(r.input_tokens)}/${num(r.output_tokens)}` },
  { title: '售价(元)', key: 'price_cents', render: r => formatCents4(r.price_cents) },
  { title: '成本(元)', key: 'cost_cents', render: r => formatCents4(r.cost_cents) },
  { title: '毛利(元)', key: 'margin_cents', render: r => formatCents4(r.margin_cents) },
  { title: '余额(元)', key: 'balance_after', render: r => yuan(r.balance_after) },
]
function summary() {
  return { 售价: { value: yuan(sum.value.price) }, 成本: { value: yuan(sum.value.cost) }, 毛利: { value: yuan(sum.value.margin) } }
}
async function load() {
  loading.value = true
  try { const { data } = await api.ledger(); rows.value = data.data }
  catch (e) { /* 表格自带空态 */ }
  finally { loading.value = false }
}
// 导出最近 10000 条账本为 CSV(对账/归档/报销)。直接走浏览器下载,带 UTF-8 BOM 兼容 Excel。
async function exportCsv() {
  exporting.value = true
  try {
    const res = await api.ledgerExport({ responseType: 'blob' })
    const url = URL.createObjectURL(new Blob([res.data]))
    const a = document.createElement('a')
    a.href = url
    a.download = 'ledger.csv'
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) { message.error(apiErr(e, '导出失败')) }
  finally { exporting.value = false }
}
onMounted(load)
</script>
<style scoped>
.bar { display:flex; justify-content:space-between; align-items:center; margin-bottom:14px; flex-wrap:wrap; gap:10px } .bar h3 { margin:0 }
.filters { display:flex; gap:8px }
.note { color:#9097a3; font-size:13px; margin-top:12px }
.note b { color:#1f2330 }
</style>
