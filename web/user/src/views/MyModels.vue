<template>
  <div class="page">
    <div class="head">
      <h3>模型管理</h3>
      <p class="sub">选择本工作空间启用的模型。关闭后,工作空间所有成员将无法调用该模型。价格为平台零售价(元/百万 token)。</p>
    </div>
    <n-data-table :columns="cols" :data="rows" :bordered="false" :loading="loading"
      :pagination="{ pageSize: 20 }" />
  </div>
</template>

<script setup>
import { ref, h, onMounted } from 'vue'
import { NDataTable, NTag, NSwitch, useMessage } from 'naive-ui'
import { api, apiErr } from '../api.js'
import { formatCents } from '../utils.js'
import { provLabel, provTagType } from '../constants.js'

const message = useMessage()
const rows = ref([])
const loading = ref(false)

const cols = [
  { title: '模型', key: 'model_name' },
  { title: '供应商', key: 'providers', render: r => {
    const ps = r.providers || []
    if (!ps.length) return h('span', { style: 'color:#bbb' }, '—')
    return h('div', { style: 'display:flex;gap:4px;flex-wrap:wrap' }, ps.map(p => h(NTag, { size: 'tiny', type: provTagType(p) }, () => provLabel(p))))
  } },
  { title: '输入价(元/M)', key: 'in', render: r => '¥' + formatCents(r.input_price_cents_per_m) },
  { title: '输出价(元/M)', key: 'out', render: r => '¥' + formatCents(r.output_price_cents_per_m) },
  { title: '启用', key: 'tenant_enabled', render: r => h(NSwitch, {
    value: r.tenant_enabled,
    loading: !!r._saving,
    onUpdateValue: (v) => toggle(r, v),
  }) },
]

async function load() {
  loading.value = true
  try { const { data } = await api.modelPrefs(); rows.value = data.data || [] }
  catch (e) { message.error(apiErr(e, '加载失败')) }
  finally { loading.value = false }
}
async function toggle(r, v) {
  r._saving = true
  try {
    await api.setModelEnabled(r.model_name, v)
    r.tenant_enabled = v
    message.success(v ? '已启用' : '已关闭')
  } catch (e) { message.error(apiErr(e, '操作失败')) }
  finally { r._saving = false }
}
onMounted(load)
</script>

<style scoped>
.page { padding:24px; overflow-y:auto; height:100%; max-width:1200px }
.head { margin-bottom:18px }
.head h3 { margin:0; font-size:22px; color:#1f2330 }
.sub { color:#6b7280; font-size:13px; margin:6px 0 0; line-height:1.6 }
</style>
