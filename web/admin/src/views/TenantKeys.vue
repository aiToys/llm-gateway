<template>
  <div>
    <div class="bar">
      <h3>密钥管理</h3>
      <div class="filters">
        <n-input v-model:value="kw" placeholder="搜索 用户邮箱/名称/前缀" size="small" clearable style="width:240px" />
        <n-button size="small" :loading="loading" @click="load">刷新</n-button>
      </div>
    </div>
    <n-alert type="info" :bordered="false" style="margin-bottom:12px">
      统一查看与吊销本租户下所有成员的 API 密钥。成员离职或密钥泄露时,可在此即时吊销(清除缓存后立即生效)。
    </n-alert>
    <n-data-table :columns="cols" :data="filtered" :bordered="false" size="small" :loading="loading" :pagination="PAGINATION" />
  </div>
</template>
<script setup>
import { ref, h, computed, onMounted } from 'vue'
import { NDataTable, NButton, NInput, NTag, NPopconfirm, NAlert, useMessage } from 'naive-ui'
import { api } from '../api.js'
import { fmtTime, apiErr, PAGINATION } from '../format.js'

const message = useMessage()
const rows = ref([])
const loading = ref(false)
const kw = ref('')
const filtered = computed(() => {
  if (!kw.value) return rows.value
  const k = kw.value.toLowerCase()
  return rows.value.filter(r =>
    (r.user_email || '').toLowerCase().includes(k) ||
    (r.name || '').toLowerCase().includes(k) ||
    (r.key_prefix || '').toLowerCase().includes(k))
})
const cols = [
  { title: '用户', key: 'user_email', render: r => r.user_email || r.user_id },
  { title: '密钥名称', key: 'name', render: r => r.name || '—' },
  { title: '前缀', key: 'key_prefix', render: r => r.key_prefix + '…' },
  { title: '限速(RPM/TPM)', key: 'limits', render: r => `${r.rpm_limit || '∞'}/${r.tpm_limit || '∞'}` },
  { title: '日/月配额', key: 'quota', render: r => `${r.daily_request_limit || '∞'} / ${r.monthly_request_limit || '∞'}` },
  { title: '最后使用', key: 'last_used_at', render: r => fmtTime(r.last_used_at) },
  { title: '创建时间', key: 'created_at', render: r => fmtTime(r.created_at) },
  { title: '状态', key: 'status', render: r => h(NTag, { size: 'small', type: statusType(r.status), round: true }, () => statusLabel(r.status)) },
  {
    title: '操作', key: 'op', render: r => r.status === 'revoked' ? '—' : h(NPopconfirm, { onPositiveClick: () => revoke(r) }, {
      trigger: () => h(NButton, { size: 'tiny', tertiary: true, type: 'error' }, () => '吊销'),
      default: () => `吊销「${r.user_email}」的密钥「${r.name || r.key_prefix + '…'}」?使用此密钥的服务将立即失败且不可恢复。确认?`,
    })
  },
]
function statusLabel(s) { return s === 'active' ? '启用' : '已吊销' }
function statusType(s) { return s === 'active' ? 'success' : 'default' }
async function load() {
  loading.value = true
  try { const { data } = await api.tenantKeys(); rows.value = data.data || [] }
  catch (e) { message.error(apiErr(e, '加载失败')) }
  finally { loading.value = false }
}
async function revoke(r) {
  try { await api.revokeTenantKey(r.id); message.success('已吊销,即时生效'); load() }
  catch (e) { message.error(apiErr(e, '吊销失败')) }
}
onMounted(load)
</script>
<style scoped>
.bar { display:flex; justify-content:space-between; align-items:center; margin-bottom:14px; flex-wrap:wrap; gap:10px }
.bar h3 { margin:0 }
.filters { display:flex; gap:8px }
</style>
