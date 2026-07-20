<template>
  <div class="page">
    <div class="head">
      <h3>API 密钥</h3>
      <n-button type="primary" @click="openCreate">+ 新建密钥</n-button>
    </div>
    <n-data-table :columns="cols" :data="keys" :bordered="false" :loading="loading"
      :pagination="{ pageSize: 20, showSizePicker: true, pageSizes: [20, 50, 100] }" />
    <n-modal v-model:show="showCreate" preset="card" title="新建 API 密钥" style="max-width:460px"
      @after-leave="resetCreate">
      <n-form ref="formRef" :model="form" :rules="rules">
        <n-form-item path="name" label="名称"><n-input v-model:value="form.name" placeholder="例如:生产环境" /></n-form-item>
        <n-form-item path="rpm" label="限速 RPM(0=不限)"><n-input-number v-model:value="form.rpm" :min="0" style="width:100%" /></n-form-item>
        <n-form-item path="tpm" label="限速 TPM(0=不限,按 token 控成本)"><n-input-number v-model:value="form.tpm" :min="0" style="width:100%" /></n-form-item>
        <div class="quota-title">用量配额(0=不限,跨自然日/月滚动)</div>
        <div class="quota-grid">
          <n-form-item label="每日请求数"><n-input-number v-model:value="form.dailyReq" :min="0" style="width:100%" placeholder="0" /></n-form-item>
          <n-form-item label="每月请求数"><n-input-number v-model:value="form.monthlyReq" :min="0" style="width:100%" placeholder="0" /></n-form-item>
          <n-form-item label="每日 token 上限"><n-input-number v-model:value="form.dailyTok" :min="0" style="width:100%" placeholder="0" /></n-form-item>
          <n-form-item label="每月 token 上限"><n-input-number v-model:value="form.monthlyTok" :min="0" style="width:100%" placeholder="0" /></n-form-item>
        </div>
        <n-form-item path="ips" label="IP 白名单(可选,逗号分隔,支持 CIDR)"><n-input v-model:value="form.ips" placeholder="留空=不限;例: 10.0.0.5, 192.168.0.0/24" /></n-form-item>
        <n-form-item path="expires_at" label="过期时间(可选,留空=永不过期)">
          <n-date-picker v-model:value="form.expiresAt" type="datetime" clearable style="width:100%" placeholder="留空=永不过期" />
        </n-form-item>
        <n-button type="primary" :loading="creating" :disabled="creating" @click="create">创建</n-button>
      </n-form>
      <n-alert v-if="newKey" type="warning" title="密钥仅此一次显示,请立即复制保存" style="margin-top:12px">
        <code>{{ newKey }}</code>
        <div style="margin-top:8px; display:flex; gap:8px">
          <n-button size="small" @click="copy(newKey)">复制密钥</n-button>
          <n-button size="small" quaternary @click="ackKey">我已保存,关闭</n-button>
        </div>
      </n-alert>
    </n-modal>
  </div>
</template>

<script setup>
import { ref, h, onMounted } from 'vue'
import { NButton, NDataTable, NModal, NForm, NFormItem, NInput, NInputNumber, NAlert, NTag, NPopconfirm, NDatePicker, useMessage } from 'naive-ui'
import { api, apiErr } from '../api.js'
import { formatTime, copyText } from '../utils.js'
import { statusLabel, statusType } from '../constants.js'

const message = useMessage()
const keys = ref([])
const loading = ref(false)
const showCreate = ref(false)
const creating = ref(false)
const formRef = ref(null)
const form = ref({ name: '', rpm: 0, tpm: 0, ips: '', dailyReq: 0, monthlyReq: 0, dailyTok: 0, monthlyTok: 0, expiresAt: null })
const newKey = ref('')
const rules = {
  name: { required: true, message: '请输入密钥名称', trigger: 'input' },
}

const cols = [
  { title: '名称', key: 'name' },
  { title: '前缀', key: 'prefix', render: r => r.prefix + '…' },
  { title: '限速(RPM/TPM)', key: 'limits', render: r => `${r.rpm_limit || '∞'}/${r.tpm_limit || '∞'}` },
  { title: '日/月配额(请求)', key: 'req_quota', render: r => `${r.daily_request_limit || '∞'} / ${r.monthly_request_limit || '∞'}` },
  { title: 'IP 白名单', key: 'ip_whitelist', render: r => (r.ip_whitelist && r.ip_whitelist.length) ? r.ip_whitelist.join(', ') : '不限' },
  { title: '状态', key: 'status', render: r => h(NTag, { size: 'small', type: statusType(r.status), round: true }, () => statusLabel(r.status)) },
  { title: '最后使用', key: 'last_used_at', render: r => formatTime(r.last_used_at) },
  { title: '创建时间', key: 'created_at', render: r => formatTime(r.created_at) },
  {
    title: '操作', key: 'op', render(row) {
      return h(NPopconfirm, { onPositiveClick: () => revoke(row.id) }, {
        trigger: () => h(NButton, { size: 'small', tertiary: true, type: 'error' }, () => '吊销'),
        default: () => '吊销后,使用此密钥的服务将立即失败,且不可恢复。确定吊销?'
      })
    }
  },
]

function openCreate() {
  resetCreate()
  showCreate.value = true
}
function resetCreate() {
  form.value = { name: '', rpm: 0, tpm: 0, ips: '', dailyReq: 0, monthlyReq: 0, dailyTok: 0, monthlyTok: 0, expiresAt: null }
  newKey.value = ''
}

async function load() {
  loading.value = true
  try { const { data } = await api.keys(); keys.value = data.data }
  catch (e) { message.error(apiErr(e, '加载失败')) }
  finally { loading.value = false }
}
// 把逗号分隔的 IP 输入拆成数组(去空白、去空)。
function parseIPs(s) {
  return (s || '').split(/[,\s]+/).map(x => x.trim()).filter(Boolean)
}

async function create() {
  try { await formRef.value?.validate() } catch { return }
  creating.value = true
  try {
    const { data } = await api.createKey({
      name: form.value.name || 'default',
      rpm_limit: form.value.rpm,
      tpm_limit: form.value.tpm,
      daily_request_limit: form.value.dailyReq,
      monthly_request_limit: form.value.monthlyReq,
      daily_token_limit: form.value.dailyTok,
      monthly_token_limit: form.value.monthlyTok,
      ip_whitelist: parseIPs(form.value.ips),
      expires_at: form.value.expiresAt ? new Date(form.value.expiresAt).toISOString() : null,
    })
    newKey.value = data.key
    await load()
    message.success('已创建,请立即复制保存')
  } catch (e) { message.error(apiErr(e, '创建失败')) }
  finally { creating.value = false }
}
async function revoke(id) {
  try { await api.revokeKey(id); message.success('已吊销'); load() }
  catch (e) { message.error(apiErr(e, '吊销失败')) }
}
function ackKey() { showCreate.value = false }
async function copy(t) {
  const ok = await copyText(t)
  ok ? message.success('已复制到剪贴板') : message.error('复制失败,请手动选择')
}
onMounted(load)
</script>

<style scoped>
.page { padding:24px }
.head { display:flex; justify-content:space-between; align-items:center; margin-bottom:16px }
.head h3 { margin:0 }
code { background:#f0f2f5; padding:2px 8px; border-radius:4px; user-select:all; word-break:break-all }
.quota-title { font-size:13px; color:#666; margin:4px 0 6px }
.quota-grid { display:grid; grid-template-columns:1fr 1fr; gap:0 12px }
</style>
