<template>
  <div>
    <div class="bar">
      <h3>请求日志</h3>
      <div class="filters">
        <n-input v-model:value="filters.model" placeholder="模型" size="small" clearable style="width:150px" @keyup.enter="load" />
        <n-input v-model:value="filters.api_key_id" placeholder="API Key ID" size="small" clearable style="width:180px" @keyup.enter="load" />
        <n-input v-if="isPlatform" v-model:value="filters.tenant_id" placeholder="租户 ID" size="small" clearable style="width:160px" @keyup.enter="load" />
        <n-select v-model:value="filters.status" :options="statusOpts" placeholder="状态" size="small" clearable style="width:120px" />
        <n-button size="small" type="primary" :loading="loading" @click="load">查询</n-button>
      </div>
    </div>
    <n-data-table :columns="cols" :data="rows" :bordered="false" size="small" :loading="loading" :pagination="PAGINATION" />

    <!-- 请求/响应原文详情抽屉 -->
    <n-drawer v-model:show="showDrawer" :width="720" placement="right">
      <n-drawer-content :title="detailTitle" closable>
        <n-spin :show="detailLoading">
          <div v-if="detail" class="detail">
            <n-descriptions label-placement="left" :column="2" size="small" bordered>
              <n-descriptions-item label="request_id">{{ detail.request_id }}</n-descriptions-item>
              <n-descriptions-item label="状态">
                <n-tag size="small" :type="detail.status === 200 ? 'success' : 'error'">{{ detail.status }}</n-tag>
              </n-descriptions-item>
              <n-descriptions-item label="模型">{{ detail.model || '—' }}</n-descriptions-item>
              <n-descriptions-item label="供应商">{{ provLabel(detail.provider) || '—' }}</n-descriptions-item>
              <n-descriptions-item label="渠道">{{ detail.channel_id || '—' }}</n-descriptions-item>
              <n-descriptions-item label="API Key">{{ detail.api_key_id || '—' }}</n-descriptions-item>
              <n-descriptions-item label="Token(in/out)">{{ detail.input_tokens }} / {{ detail.output_tokens }}</n-descriptions-item>
              <n-descriptions-item label="费用">¥{{ yuan(detail.price_cents) }}</n-descriptions-item>
              <n-descriptions-item label="耗时">{{ detail.latency_ms }} ms</n-descriptions-item>
              <n-descriptions-item label="时间">{{ fmtTime(detail.created_at) }}</n-descriptions-item>
            </n-descriptions>
            <h4 class="blk-title">错误</h4>
            <pre v-if="detail.error" class="code err">{{ detail.error }}</pre>
            <n-empty v-else description="无" size="small" />
            <h4 class="blk-title">请求体</h4>
            <pre v-if="detail.request_body" class="code">{{ pretty(detail.request_body) }}</pre>
            <n-empty v-else description="未记录(可能 LogBodies=false 或采样未命中)" size="small" />
            <h4 class="blk-title">响应体</h4>
            <pre v-if="detail.response_body" class="code">{{ pretty(detail.response_body) }}</pre>
            <n-empty v-else description="未记录" size="small" />
          </div>
        </n-spin>
      </n-drawer-content>
    </n-drawer>
  </div>
</template>
<script setup>
import { ref, h, onMounted } from 'vue'
import { NDataTable, NButton, NTag, NInput, NSelect, NDrawer, NDrawerContent, NDescriptions, NDescriptionsItem, NSpin, NEmpty, useMessage } from 'naive-ui'
import { api } from '../api.js'
import { fmtTime, yuan, provLabel, apiErr, PAGINATION } from '../format.js'
import { user } from '../store.js'

const message = useMessage()
const isPlatform = ref(user.get()?.role === 'platform_admin')
const rows = ref([])
const loading = ref(false)
const filters = ref({ model: '', api_key_id: '', tenant_id: '', status: null })
const statusOpts = [
  { label: '成功(200)', value: 200 },
  { label: '失败(502)', value: 502 },
]

const showDrawer = ref(false)
const detailLoading = ref(false)
const detail = ref(null)
const detailTitle = ref('请求详情')

const cols = [
  { title: '时间', key: 'created_at', render: r => fmtTime(r.created_at), width: 170 },
  { title: 'request_id', key: 'request_id', width: 200, ellipsis: { tooltip: true } },
  { title: '模型', key: 'model', width: 160, ellipsis: { tooltip: true } },
  { title: '供应商', key: 'provider', width: 100, render: r => provLabel(r.provider) || r.provider || '—' },
  { title: 'Token', key: 'tokens', width: 110, render: r => `${r.input_tokens}/${r.output_tokens}` },
  { title: '费用', key: 'price_cents', width: 90, render: r => `¥${yuan(r.price_cents)}` },
  { title: '耗时', key: 'latency_ms', width: 90, render: r => `${r.latency_ms}ms` },
  {
    title: '状态', key: 'status', width: 90,
    render: r => h(NTag, { size: 'small', type: r.status === 200 ? 'success' : 'error', round: true }, () => r.status),
  },
  {
    title: '操作', key: 'op', width: 90,
    render: r => h(NButton, { size: 'tiny', quaternary: true, onClick: () => openDetail(r) }, () => '查看'),
  },
]

function pretty(body) {
  try { return JSON.stringify(JSON.parse(body), null, 2) } catch { return body }
}

async function load() {
  loading.value = true
  try {
    const params = {}
    for (const [k, v] of Object.entries(filters.value)) {
      if (v !== '' && v !== null && v !== undefined) params[k] = v
    }
    const { data } = await api.requestLogs(params)
    rows.value = data.data || []
  } catch (e) { message.error(apiErr(e, '查询失败')) }
  finally { loading.value = false }
}

async function openDetail(r) {
  showDrawer.value = true
  detail.value = null
  detailLoading.value = true
  detailTitle.value = `请求详情 · ${r.request_id}`
  try {
    const { data } = await api.requestLog(r.id)
    detail.value = data.data
  } catch (e) { message.error(apiErr(e, '加载详情失败')) }
  finally { detailLoading.value = false }
}

onMounted(load)
</script>
<style scoped>
.bar { display:flex; justify-content:space-between; align-items:center; margin-bottom:14px; flex-wrap:wrap; gap:10px }
.bar h3 { margin:0 }
.filters { display:flex; gap:8px; flex-wrap:wrap }
.blk-title { margin:16px 0 6px; font-size:13px; color:#666 }
.code { background:#f6f7f9; border:1px solid #eee; border-radius:6px; padding:10px; font-size:12px; line-height:1.5; max-height:280px; overflow:auto; white-space:pre-wrap; word-break:break-all; margin:0 }
.code.err { background:#fff2f0; border-color:#ffccc7; color:#c0392b }
</style>
