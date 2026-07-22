<template>
  <div class="page">
    <div class="head">
      <h3>用量统计</h3>
      <div class="controls">
        <n-select v-model:value="groupBy" :options="dimOpts" size="small" style="width:130px" />
        <n-select v-model:value="bucket" :options="bucketOpts" size="small" style="width:110px" />
        <n-select v-model:value="days" :options="[{label:'今天',value:1},{label:'7天',value:7},{label:'30天',value:30}]" size="small" style="width:100px" @update:value="load" />
        <n-button size="small" @click="exportRows" :disabled="!tableRows.length">导出 CSV</n-button>
      </div>
    </div>

    <div class="cards">
      <div class="card"><div class="k">请求数</div><div class="v">{{ totalReq }}</div><small>{{ rateLabel }}</small></div>
      <div class="card"><div class="k">输入 token</div><div class="v">{{ totalIn.toLocaleString() }}</div><small>{{ tpmLabel }}</small></div>
      <div class="card"><div class="k">输出 token</div><div class="v">{{ totalOut.toLocaleString() }}</div></div>
      <div class="card"><div class="k">费用</div><div class="v">¥{{ formatCents(totalPrice) }}</div></div>
    </div>

    <div class="chart"><v-chart :option="chartOption" autoresize style="height:320px" /></div>

    <h4 style="margin-top:22px">明细 · {{ dimLabel }}</h4>
    <n-data-table :columns="cols" :data="tableRows" :bordered="false" size="small" :loading="loading"
      :pagination="{ pageSize: 20 }" />
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { NSelect, NDataTable, NButton } from 'naive-ui'
import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import { api } from '../api.js'
import { formatCents, formatNum, exportCSV } from '../utils.js'

use([CanvasRenderer, BarChart, GridComponent, TooltipComponent, LegendComponent])

const groupBy = ref('model')
const bucket = ref('day')
const days = ref(7)
const rows = ref([])

const dimOpts = [
  { label: '按模型', value: 'model' },
  { label: '按供应商', value: 'provider' },
  { label: '按 API Key', value: 'api_key' },
]
const bucketOpts = [
  { label: '按分钟', value: 'minute' },
  { label: '按小时', value: 'hour' },
  { label: '按天', value: 'day' },
]
const dimLabel = computed(() => dimOpts.find(o => o.value === groupBy.value)?.label || '')
const isMinute = computed(() => bucket.value === 'minute')
const rateLabel = computed(() => isMinute.value ? '≈ RPM 求和' : '总请求')
const tpmLabel = computed(() => isMinute.value ? '≈ TPM 求和' : '总量')

const totalReq = computed(() => rows.value.reduce((s, r) => s + r.requests, 0))
const totalIn = computed(() => rows.value.reduce((s, r) => s + r.input_tokens, 0))
const totalOut = computed(() => rows.value.reduce((s, r) => s + r.output_tokens, 0))
const totalPrice = computed(() => rows.value.reduce((s, r) => s + r.price_cents, 0))

const tableRows = computed(() => {
  // 按维度汇总(跨时间桶)
  const m = new Map()
  for (const r of rows.value) {
    const e = m.get(r.dim) || { dim: r.dim, requests: 0, input_tokens: 0, output_tokens: 0, price_cents: 0 }
    e.requests += r.requests; e.input_tokens += r.input_tokens; e.output_tokens += r.output_tokens; e.price_cents += r.price_cents
    m.set(r.dim, e)
  }
  return [...m.values()].sort((a, b) => b.requests - a.requests)
})

const chartOption = computed(() => {
  const byBucket = new Map()
  for (const r of rows.value) {
    const e = byBucket.get(r.bucket) || { req: 0, tok: 0 }
    e.req += r.requests; e.tok += r.input_tokens + r.output_tokens
    byBucket.set(r.bucket, e)
  }
  const xs = [...byBucket.keys()].sort()
  return {
    tooltip: { trigger: 'axis' },
    legend: { data: ['请求数', 'Token'] },
    grid: { left: 40, right: 20, top: 36, bottom: 30 },
    xAxis: { type: 'category', data: xs },
    yAxis: [
      { type: 'value', name: isMinute.value ? 'RPM' : '请求' },
      { type: 'value', name: isMinute.value ? 'TPM' : 'Token' },
    ],
    series: [
      { name: '请求数', type: 'bar', data: xs.map(x => byBucket.get(x).req), itemStyle: { color: '#3D6EFF' } },
      { name: 'Token', type: 'line', yAxisIndex: 1, smooth: true, data: xs.map(x => byBucket.get(x).tok), itemStyle: { color: '#22d3ee' } },
    ],
  }
})

const cols = [
  { title: dimLabel, key: 'dim', render: r => r.dim || '(未知)' },
  { title: '请求数', key: 'requests', render: r => formatNum(r.requests) },
  { title: '输入 token', key: 'input_tokens', render: r => formatNum(r.input_tokens) },
  { title: '输出 token', key: 'output_tokens', render: r => formatNum(r.output_tokens) },
  { title: '费用(元)', key: 'price_cents', render: r => '¥' + formatCents(r.price_cents) },
]

const loading = ref(false)
async function load() {
  loading.value = true
  try {
    const { data } = await api.usageAggregate({ group_by: groupBy.value, bucket: bucket.value, days: days.value })
    rows.value = data.data || []
  } catch { /* 切换失败保留旧数据,不打断 */ }
  finally { loading.value = false }
}
watch([groupBy, bucket], load)
onMounted(load)

// 导出当前维度汇总为 CSV(客户端,带 BOM 兼容 Excel)。
function exportRows() {
  exportCSV(`usage-${groupBy.value}-${days}d.csv`, tableRows.value, [
    { key: 'dim', label: dimLabel.value },
    { key: 'requests', label: '请求数' },
    { key: 'input_tokens', label: '输入token' },
    { key: 'output_tokens', label: '输出token' },
    { key: 'price_cents', label: '费用(分)' },
  ])
}
</script>

<style scoped>
.page { padding:24px; overflow-y:auto; height:100% }
.head { display:flex; justify-content:space-between; align-items:center; margin-bottom:16px; flex-wrap:wrap; gap:10px }
.head h3 { margin:0 }
.controls { display:flex; gap:8px }
.cards { display:grid; grid-template-columns:repeat(4,1fr); gap:14px; margin-bottom:20px }
.card { background:var(--bg-card); border-radius:12px; padding:16px 18px; box-shadow:0 1px 3px rgba(0,0,0,.04) }
.card .k { color:#9097a3; font-size:13px }
.card .v { font-size:22px; font-weight:700; color:var(--text-strong); margin-top:6px }
.card small { color:#b5bac4; font-size:11px }
.chart { background:var(--bg-card); border-radius:12px; padding:12px }
</style>
