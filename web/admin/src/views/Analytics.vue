<template>
  <div>
    <div class="bar">
      <div class="filters">
        <n-select v-model:value="groupBy" :options="dimOpts" size="small" style="width:140px" />
        <n-select v-model:value="bucket" :options="bucketOpts" size="small" style="width:110px" />
        <n-select v-model:value="scope" :options="scopeOpts" size="small" style="width:120px" />
        <n-select v-model:value="days" :options="dayOpts" size="small" style="width:100px" />
      </div>
      <n-button size="small" @click="load">刷新</n-button>
    </div>

    <div class="cards">
      <div class="card"><div class="k">总请求</div><div class="v">{{ totalReq }}</div></div>
      <div class="card"><div class="k">总 token</div><div class="v">{{ totalTok.toLocaleString() }}</div></div>
      <div class="card green"><div class="k">收入(元)</div><div class="v">¥{{ yuan(totalPrice) }}</div></div>
      <div class="card"><div class="k">维度数</div><div class="v">{{ dims }}</div></div>
    </div>

    <div class="chart">
      <n-spin :show="loading">
        <v-chart v-if="rows.length" :option="chartOption" autoresize style="height:300px" />
        <n-empty v-else size="small" description="暂无数据,调整筛选或时间范围试试" style="padding:80px 0" />
      </n-spin>
    </div>

    <h4 style="margin:20px 0 10px">明细 · {{ dimLabel }} / {{ bucketLabel }}粒度</h4>
    <n-data-table :columns="cols" :data="tableRows" :bordered="false" size="small" :loading="loading" />
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { NSelect, NButton, NDataTable, NSpin, NEmpty, useMessage } from 'naive-ui'
import VChart from 'vue-echarts'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import { api } from '../api.js'
import { provLabel, apiErr } from '../format.js'

use([CanvasRenderer, BarChart, GridComponent, TooltipComponent, LegendComponent])

const message = useMessage()
const groupBy = ref('model')
const bucket = ref('hour')
const scope = ref('all')
const days = ref(1)
const rows = ref([])
const loading = ref(false)

const dimOpts = [
  { label: '按模型', value: 'model' },
  { label: '按供应商', value: 'provider' },
  { label: '按 API Key', value: 'api_key' },
]
const bucketOpts = [{ label: '按分钟', value: 'minute' }, { label: '按小时', value: 'hour' }, { label: '按天', value: 'day' }]
const scopeOpts = [{ label: '全局', value: 'all' }, { label: '按租户', value: 'tenant' }, { label: '按用户', value: 'user' }]
const dayOpts = [{ label: '今天', value: 1 }, { label: '7天', value: 7 }, { label: '30天', value: 30 }]
const dimLabel = computed(() => dimOpts.find(o => o.value === groupBy.value)?.label)
const bucketLabel = computed(() => bucketOpts.find(o => o.value === bucket.value)?.label)
const yuan = (c) => (c / 100).toFixed(2)

const totalReq = computed(() => rows.value.reduce((s, r) => s + r.requests, 0))
const totalTok = computed(() => rows.value.reduce((s, r) => s + r.input_tokens + r.output_tokens, 0))
const totalPrice = computed(() => rows.value.reduce((s, r) => s + r.price_cents, 0))
const dims = computed(() => new Set(rows.value.map(r => r.dim)).size)

const tableRows = computed(() => {
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
    const e = byBucket.get(r.bucket) || { req: 0, tok: 0, price: 0 }
    e.req += r.requests; e.tok += r.input_tokens + r.output_tokens; e.price += r.price_cents
    byBucket.set(r.bucket, e)
  }
  const xs = [...byBucket.keys()].sort()
  return {
    tooltip: { trigger: 'axis' },
    legend: { data: ['请求', 'Token', '收入(分)'] },
    grid: { left: 40, right: 40, top: 36, bottom: 30 },
    xAxis: { type: 'category', data: xs },
    yAxis: [{ type: 'value' }, { type: 'value' }],
    series: [
      { name: '请求', type: 'bar', data: xs.map(x => byBucket.get(x).req), itemStyle: { color: '#0F766E' } },
      { name: 'Token', type: 'line', yAxisIndex: 1, data: xs.map(x => byBucket.get(x).tok), itemStyle: { color: '#22d3ee' } },
      { name: '收入(分)', type: 'line', data: xs.map(x => byBucket.get(x).price), itemStyle: { color: '#f59e0b' } },
    ],
  }
})

const cols = [
  // provider 维度的 dim 是 adapter 类型(openaicomp),用 provLabel 映射成友好名,避免技术术语泄露。
  { title: '维度', key: 'dim', render: r => (groupBy.value === 'provider' ? provLabel(r.dim) : (r.dim || '(未知)')) },
  { title: '请求数', key: 'requests' },
  { title: '输入 token', key: 'input_tokens' },
  { title: '输出 token', key: 'output_tokens' },
  { title: '收入(元)', key: 'price_cents', render: r => '¥' + (r.price_cents / 100).toFixed(4) },
]

async function load() {
  loading.value = true
  try {
    const { data } = await api.usageAggregate({ group_by: groupBy.value, bucket: bucket.value, scope: scope.value, days: days.value })
    rows.value = data.data || []
  } catch (e) {
    message.error(apiErr(e, '数据加载失败'))
    rows.value = []
  } finally {
    loading.value = false
  }
}
watch([groupBy, bucket, scope, days], load)
onMounted(load)
</script>

<style scoped>
.bar { display:flex; justify-content:space-between; align-items:center; margin-bottom:16px }
.filters { display:flex; gap:8px; flex-wrap:wrap }
.cards { display:grid; grid-template-columns:repeat(4,1fr); gap:14px; margin-bottom:18px }
.card { background:#fff; border-radius:12px; padding:16px 18px; box-shadow:0 1px 3px rgba(0,0,0,.04) }
.card .k { color:#9097a3; font-size:13px }
.card .v { font-size:22px; font-weight:700; color:#1f2330; margin-top:6px }
.card.green .v { color:#0F766E }
.chart { background:#fff; border-radius:12px; padding:12px }
</style>
