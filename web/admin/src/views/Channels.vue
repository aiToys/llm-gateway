<template>
  <div>
    <div class="bar"><h3>渠道</h3><n-button type="primary" @click="openCreate">+ 新建渠道</n-button></div>
    <n-data-table :columns="cols" :data="rows" :bordered="false" :loading="loading" :pagination="PAGINATION" />
    <n-modal v-model:show="show" preset="card" :title="editing ? '编辑渠道: ' + form.name : '新建渠道'" style="max-width:680px">
      <n-form ref="formRef" :model="form" :rules="rules" label-placement="top">
        <n-grid :cols="2" :x-gap="12">
          <n-form-item-gi label="供应商"><n-select :value="presetKey" :options="presetOpts" placeholder="选择供应商" @update:value="onPreset" /></n-form-item-gi>
          <n-form-item-gi path="name" label="名称"><n-input v-model:value="form.name" /></n-form-item-gi>
          <n-form-item-gi path="base_url" label="Base URL">
            <n-input v-model:value="form.base_url" :placeholder="form.provider === 'openaicomp' ? 'OpenAI 兼容端点(必填)' : 'mock 无需 base_url'" :disabled="form.provider === 'mock'" />
          </n-form-item-gi>
          <n-form-item-gi label="优先级(高=主)"><n-input-number v-model:value="form.priority" /></n-form-item-gi>
          <n-form-item-gi label="权重"><n-input-number v-model:value="form.weight" :min="1" /></n-form-item-gi>
          <n-form-item-gi label="租户ID(选填,BYOK)"><n-input v-model:value="form.tenant_id" placeholder="留空=平台默认" :disabled="editing" /></n-form-item-gi>
        </n-grid>
        <n-form-item label="模型配置(每行一个模型;可下拉选已注册模型或手动输入;可单独设上游名/成本/启停)">
          <div class="model-table">
            <div class="mhead">
              <span style="min-width:150px">模型</span>
              <span style="min-width:130px">上游名(空=同名)</span>
              <span>输入</span><span>输出</span><span>缓存读</span><span>缓存写</span>
              <span style="max-width:46px">启用</span><span style="max-width:36px"></span>
            </div>
            <div v-for="(row, i) in modelRows" :key="i" class="mrow">
              <n-select v-model:value="row.model" filterable tag :options="modelOpts" placeholder="模型" style="width:150px" />
              <n-input v-model:value="row.upstream" placeholder="同名则留空" style="width:130px" />
              <n-input-number v-model:value="row.in" :min="0" :show-button="false" placeholder="0" />
              <n-input-number v-model:value="row.out" :min="0" :show-button="false" placeholder="0" />
              <n-input-number v-model:value="row.cacheRead" :min="0" :show-button="false" placeholder="0" />
              <n-input-number v-model:value="row.cacheWrite" :min="0" :show-button="false" placeholder="0" />
              <n-switch v-model:value="row.enabled" size="small" style="max-width:46px" />
              <n-button size="small" quaternary type="error" @click="modelRows.splice(i, 1)" title="移除" style="max-width:36px">✕</n-button>
            </div>
            <n-button size="small" dashed block @click="addModelRow">+ 添加模型</n-button>
            <div class="hint">成本单位:分/百万 token(168 = ¥0.168/百万 token)。留 0 = 按下方渠道级默认成本核算;缓存读/写仅 DeepSeek/GLM 等少数供应商计价。可关闭单个模型而不影响同渠道其他模型。</div>
          </div>
        </n-form-item>
        <n-form-item path="api_key" :label="editing ? '上游 API Key(留空保留原密钥)' : '上游 API Key'">
          <n-input v-model:value="form.api_key" type="password" show-password-on="click" :placeholder="editing ? '••••(不改则留空)' : '供应商密钥'" />
          <template #feedback>
            <n-tag v-if="editing && !hasKey" type="error" size="small" style="margin-top:6px">该渠道尚未配置密钥,启用后将立即 401</n-tag>
            <n-tag v-else-if="editing && hasKey" type="success" size="small" style="margin-top:6px">已配置密钥</n-tag>
          </template>
        </n-form-item>
        <n-grid :cols="2" :x-gap="12">
          <n-form-item-gi label="渠道级默认输入成本(元/M)"><n-input-number v-model:value="form.input_cost_cents_per_m" :min="0" /></n-form-item-gi>
          <n-form-item-gi label="渠道级默认输出成本(元/M)"><n-input-number v-model:value="form.output_cost_cents_per_m" :min="0" /></n-form-item-gi>
        </n-grid>
        <n-button type="primary" :loading="busy" :disabled="busy" @click="submit">{{ editing ? '保存' : '创建' }}</n-button>
      </n-form>
    </n-modal>
  </div>
</template>
<script setup>
import { ref, h, computed, onMounted, watch } from 'vue'
import { NDataTable, NButton, NModal, NForm, NFormItem, NFormItemGi, NGrid, NInputNumber, NInput, NSelect, NSwitch, NPopconfirm, NTag, useMessage } from 'naive-ui'
import { api } from '../api.js'
import { statusLabel, statusType, presetLabel, yuanPerM, apiErr, PAGINATION, PROVIDER_PRESETS } from '../format.js'

const message = useMessage()
const rows = ref([]); const loading = ref(false); const show = ref(false); const busy = ref(false)
const testing = ref({})
// 模型行(对应 channel_models 实体): 模型名 + 上游名 + 独立成本 + 单模型启停。
const modelRows = ref([])
const modelOpts = ref([])
function addModelRow() { modelRows.value.push({ model: '', upstream: '', in: 0, out: 0, cacheRead: 0, cacheWrite: 0, enabled: true }) }
async function loadModelOpts() {
  try { const { data } = await api.models(); modelOpts.value = (data.data || []).map(m => ({ label: m.model_name, value: m.model_name })) }
  catch { /* 空选项,手动输入 */ }
}
// 供应商预设: 下拉展示用户熟悉的名字(百炼/DeepSeek/...),选中由 onPreset 填 adapter+base_url+默认名。
// presetKey 仅驱动展示,不参与提交;真正提交的是 form.provider(adapter)+base_url。
const presetOpts = PROVIDER_PRESETS.map(p => ({ label: p.label, value: p.label }))
const presetKey = ref(null)
const formRef = ref(null)
const editing = ref(false)
const editId = ref(null)
const hasKey = ref(false)
const blank = () => ({ provider: 'openaicomp', name: '', base_url: '', api_key: '', priority: 0, weight: 1, tenant_id: '', input_cost_cents_per_m: 0, output_cost_cents_per_m: 0 })
const form = ref(blank())
const rules = computed(() => ({
  name: { required: true, message: '请输入名称', trigger: 'blur' },
  // openaicomp 无 defaultBaseURL,base_url 必填;mock 无需。
  base_url: form.value.provider === 'openaicomp'
    ? { required: true, message: 'OpenAI 兼容供应商必须填 base_url', trigger: 'blur' }
    : {},
  api_key: editing.value ? {} : { required: true, message: '请输入上游 API Key', trigger: 'blur' },
}))
// 选供应商预设: 填 adapter + base_url + 默认名(名仅在空时填,不覆盖已改)。
function onPreset(label) {
  presetKey.value = label
  const p = PROVIDER_PRESETS.find(x => x.label === label)
  if (!p) return
  form.value.provider = p.adapter
  if (p.base_url) form.value.base_url = p.base_url
  if (!form.value.name) form.value.name = p.name
}const cols = [
  { title: '名称', key: 'name' },
  { title: '供应商', key: 'provider', render: r => h(NTag, { size: 'small', type: r.provider === 'mock' ? 'default' : 'info' }, () => presetLabel(r.provider, r.base_url)) },
  { title: '租户', key: 'tenant_id', render: r => r.tenant_id ? r.tenant_id : h(NTag, { size: 'small', type: 'default', bordered: false }, () => '平台默认·只读') },
  { title: '模型', key: 'models', render: r => {
    const cms = r.channel_models || []
    if (!cms.length) return '—'
    const active = cms.filter(x => x.status === 'active').length
    return `${cms.length} 个` + (active < cms.length ? ` (${active} 启用)` : '')
  }},
  { title: '默认成本(元/M) 入/出', key: 'cost', render: r => `${yuanPerM(r.input_cost_cents_per_m)} / ${yuanPerM(r.output_cost_cents_per_m)}` },
  { title: '优先级/权重', key: 'pw', render: r => `${r.priority}/${r.weight}` },
  { title: '状态', key: 'status', render: r => h(NTag, { size: 'small', type: statusType(r.status), round: true }, () => statusLabel(r.status)) },
  { title: '熔断', key: 'breaker', render: r => r.status !== 'active' ? '—' : h(NTag, { size: 'small', type: r.breaker_open ? 'error' : 'success', round: true }, () => r.breaker_open ? '熔断中' : '正常') },
  { title: '操作', key: 'op', render(r) {
    return h('div', { style: 'display:flex;gap:6px' }, [
      h(NButton, { size: 'tiny', tertiary: true, onClick: () => openEdit(r) }, () => '编辑'),
      h(NButton, { size: 'tiny', tertiary: true, loading: !!testing.value[r.id], onClick: () => testConn(r) }, () => '测试'),
      h(NPopconfirm, { onPositiveClick: () => toggle(r) }, {
        trigger: () => h(NButton, { size: 'tiny', tertiary: true }, () => r.status === 'active' ? '禁用' : '启用'),
        default: () => r.status === 'active'
          ? `禁用「${r.name}」后,引用此渠道的模型将立即无可用通道而失败,确认?`
          : `启用「${r.name}」,确认?`
      }),
      h(NPopconfirm, { onPositiveClick: () => del(r) }, {
        trigger: () => h(NButton, { size: 'tiny', tertiary: true, type: 'error' }, () => '删除'),
        default: () => `将删除渠道「${r.name}」(${presetLabel(r.provider, r.base_url)}),引用此渠道的模型将失效,不可恢复。确认?`
      })
    ])
  } },
]
function openCreate() { editing.value = false; editId.value = null; form.value = blank(); presetKey.value = null; modelRows.value = []; show.value = true }
function openEdit(r) {
  editing.value = true; editId.value = r.id
  hasKey.value = !!r.has_key
  presetKey.value = presetLabel(r.provider, r.base_url)
  form.value = { provider: r.provider, name: r.name, base_url: r.base_url || '', api_key: '',
    priority: r.priority, weight: r.weight, tenant_id: r.tenant_id || '',
    input_cost_cents_per_m: r.input_cost_cents_per_m || 0, output_cost_cents_per_m: r.output_cost_cents_per_m || 0 }
  // 从 channel_models 回填表格行(含上游名/成本/启停)。
  modelRows.value = (r.channel_models || []).map(cm => ({
    model: cm.model_name,
    upstream: cm.upstream_model || '',
    in: cm.input_cost_cents_per_m || 0,
    out: cm.output_cost_cents_per_m || 0,
    cacheRead: cm.cache_read_cost_cents_per_m || 0,
    cacheWrite: cm.cache_write_cost_cents_per_m || 0,
    enabled: cm.status !== 'disabled',
  }))
  show.value = true
}
async function load() {
  loading.value = true
  try { const { data } = await api.channels(); rows.value = data.data }
  catch (e) { message.error(apiErr(e, '加载失败')) }
  finally { loading.value = false }
}
async function toggle(r) {
  try { await api.setChannelStatus(r.id, r.status === 'active' ? 'disabled' : 'active'); load() }
  catch (e) { message.error(apiErr(e, '操作失败')) }
}
async function testConn(r) {
  testing.value[r.id] = true
  try {
    const { data } = await api.testChannel(r.id)
    if (data.ok) message.success(`${r.name}: 连通正常 (${data.latency_ms}ms, ${data.model})`)
    else message.warning(`${r.name}: ${data.error || '连通失败'}`)
  } catch (e) { message.error(`${r.name}: ${apiErr(e, '测试失败')}`) }
  finally { testing.value[r.id] = false }
}
async function del(r) {
  try { await api.deleteChannel(r.id); message.success('已删除'); load() }
  catch (e) { message.error(apiErr(e, '删除失败')) }
}
function parsePayload() {
  // 模型表格行 → channel_models[](每行一个渠道×模型实体)。
  const channel_models = []
  for (const row of modelRows.value) {
    const name = (row.model || '').trim()
    if (!name) continue
    channel_models.push({
      model_name: name,
      upstream_model: (row.upstream || '').trim(),
      input_cost_cents_per_m: row.in || 0,
      output_cost_cents_per_m: row.out || 0,
      cache_read_cost_cents_per_m: row.cacheRead || 0,
      cache_write_cost_cents_per_m: row.cacheWrite || 0,
      weight: 0,
      status: row.enabled === false ? 'disabled' : 'active',
    })
  }
  return { ...form.value, channel_models }
}
async function submit() {
  try { await formRef.value?.validate() } catch { return }
  if (!presetKey.value) { message.warning('请选择供应商'); return }
  if (!modelRows.value.some(r => (r.model || '').trim())) {
    message.warning('请至少添加一个模型'); return
  }
  busy.value = true
  try {
    const payload = parsePayload()
    if (editing.value) {
      await api.updateChannel(editId.value, payload)
      message.success('已保存')
    } else {
      await api.createChannel(payload)
      message.success('已创建')
    }
    show.value = false; load()
  } catch (e) { message.error(apiErr(e, editing.value ? '保存失败' : '创建失败')) } finally { busy.value = false }
}
onMounted(() => { loadModelOpts(); load() })
</script>
<style scoped>
.bar { display:flex; justify-content:space-between; align-items:center; margin-bottom:14px } .bar h3 { margin:0 }
.model-table { width:100%; display:flex; flex-direction:column; gap:6px }
.mhead, .mrow { display:flex; align-items:center; gap:6px }
.mhead { font-size:12px; color:#999; padding:0 0 2px 2px }
.mhead span { flex:1; min-width:0 }
.mrow .n-input-number, .mrow .n-input { flex:1; min-width:0 }
.hint { font-size:12px; color:#999; line-height:1.5; margin-top:2px }
</style>
