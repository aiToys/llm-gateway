<template>
  <div>
    <div class="bar"><h3>模型与定价</h3><n-button type="primary" @click="openCreate">+ 新建模型</n-button></div>
    <n-data-table :columns="cols" :data="rows" :bordered="false" :loading="loading" :pagination="PAGINATION" />
    <n-modal v-model:show="show" preset="card" :title="editing ? '编辑模型: ' + form.model_name : '新建模型'" style="max-width:680px">
      <!-- ① 基础信息(定价/能力/展示) -->
      <h4 class="sec">基础信息</h4>
      <n-form ref="formRef" :model="form" :rules="rules" label-placement="top">
        <n-grid :cols="2" :x-gap="12">
          <n-form-item-gi path="model_name" label="模型名(model,对外暴露)"><n-input v-model:value="form.model_name" :disabled="editing" /></n-form-item-gi>
          <n-form-item-gi label="输入售价(元/百万)"><n-input-number v-model:value="form.input_price_cents_per_m" :min="0" /></n-form-item-gi>
          <n-form-item-gi label="输出售价(元/百万)"><n-input-number v-model:value="form.output_price_cents_per_m" :min="0" /></n-form-item-gi>
          <n-form-item-gi label="缓存读售价(0=输入全价)"><n-input-number v-model:value="form.cache_read_price_cents_per_m" :min="0" /></n-form-item-gi>
          <n-form-item-gi label="缓存写售价(0=输入全价)"><n-input-number v-model:value="form.cache_write_price_cents_per_m" :min="0" /></n-form-item-gi>
          <n-form-item-gi label="上下文长度"><n-input-number v-model:value="form.context_length" :min="0" /></n-form-item-gi>
          <n-form-item-gi label="能力(可多选)" :span="2">
            <n-select v-model:value="form.capabilities" multiple :options="capabilityOpts" />
          </n-form-item-gi>
        </n-grid>
        <n-form-item label="一句话介绍"><n-input v-model:value="form.description" /></n-form-item>
        <n-form-item label="详细介绍"><n-input v-model:value="form.long_desc" type="textarea" :autosize="{minRows:2}" /></n-form-item>
        <n-form-item label="标签(逗号分隔)"><n-input v-model:value="tagsStr" placeholder="视觉,推理,中文" /></n-form-item>
        <n-form-item label="启用"><n-switch v-model:value="form.enabled" /></n-form-item>
      </n-form>

      <!-- ② 供应商与负载均衡(核心) -->
      <h4 class="sec">供应商与负载均衡</h4>
      <p class="sub" v-if="!editing">保存模型后即可在此挂载渠道、配置权重与主备。</p>
      <template v-if="editing">
        <div class="route-row">
          <div class="route-label">负载均衡策略</div>
          <n-select v-model:value="form.routing_strategy" :options="strategyOpts" style="max-width:260px" />
        </div>
        <n-alert :type="routeAlertType" :show-icon="false" style="margin:8px 0 14px">{{ routeHint }}</n-alert>

        <!-- 挂载渠道表:在这里配权重/主备/挂载,而非另寻它处 -->
        <div class="ch-table" v-if="mountedChannels.length">
          <div class="ch-row ch-head">
            <span>供应商</span><span>渠道</span><span>角色</span><span>优先级</span><span>权重</span><span></span>
          </div>
          <div class="ch-row" v-for="c in mountedChannels" :key="c.id">
            <span><n-tag size="tiny" :type="c.provider==='mock'?'default':'info'">{{ presetLabel(c.provider, c.base_url) }}</n-tag></span>
            <span class="cname">{{ c.name }}</span>
            <span><n-tag size="tiny" :type="roleOf(c).type">{{ roleOf(c).label }}</n-tag></span>
            <span><n-input-number v-model:value="c.priority" size="tiny" style="width:90px" :show-button="false" @update:value="v => saveRouting(c, { priority: v })" /></span>
            <span><n-input-number v-model:value="c.weight" size="tiny" :min="1" style="width:80px" :show-button="false" @update:value="v => saveRouting(c, { weight: v })" /></span>
            <span><n-button size="tiny" quaternary type="error" @click="detach(c)">移除</n-button></span>
          </div>
        </div>
        <n-empty v-else size="small" description="该模型尚未挂载任何渠道" style="margin:10px 0">
          <template #extra>
            <span class="sub">在下方添加渠道,才能生效负载均衡</span>
          </template>
        </n-empty>

        <!-- 添加渠道 -->
        <div class="add-row" v-if="availableChannels.length">
          <n-select v-model:value="addChId" filterable :options="availableOptions" placeholder="选择渠道挂载到本模型" size="small" style="max-width:340px" />
          <n-button size="small" :disabled="!addChId" @click="attach">添加</n-button>
        </div>

        <!-- pinned 固定渠道 -->
        <div class="route-row" v-if="form.routing_strategy === 'pinned'" style="margin-top:12px">
          <div class="route-label">固定到渠道</div>
          <n-select v-if="mountedChannels.length" v-model:value="form.pinned_channel_id" filterable :options="pinnedOptions" placeholder="选择要固定的渠道" style="max-width:340px" />
          <span v-else class="sub">请先添加渠道</span>
        </div>
      </template>

      <div class="actions">
        <n-button @click="show = false">取消</n-button>
        <n-button type="primary" :loading="busy" :disabled="busy" @click="save">{{ editing ? '保存' : '创建并继续配置路由' }}</n-button>
      </div>
    </n-modal>
  </div>
</template>
<script setup>
import { ref, h, computed, onMounted } from 'vue'
import { NDataTable, NButton, NModal, NForm, NFormItem, NFormItemGi, NGrid, NInput, NInputNumber, NSelect, NSwitch, NPopconfirm, NTag, NAlert, NEmpty, useMessage } from 'naive-ui'
import { api } from '../api.js'
import { yuanPerM, fmtCtx, presetLabel, apiErr, PAGINATION } from '../format.js'

const message = useMessage()
const rows = ref([]); const loading = ref(false); const show = ref(false); const busy = ref(false)
const tagsStr = ref('')
const editing = ref(false)
const formRef = ref(null)
const channels = ref([])
const addChId = ref(null)
// 能力多标签(参考智谱/OpenAI:一个模型可同时具备多种能力)。
const CAP_META = { text: '💬 文本', vision: '🖼️ 视觉', audio: '🔊 音频', file: '📎 文件', function_call: '🛠️ 工具调用', reasoning: '🧠 推理', code: '💻 代码', web_search: '🌐 联网' }
const capabilityOpts = Object.entries(CAP_META).map(([v, l]) => ({ label: l, value: v }))
const strategyOpts = [
  { label: '加权随机 (weighted)', value: 'weighted' },
  { label: '轮询 (round_robin)', value: 'round_robin' },
  { label: '主备 (failover)', value: 'failover' },
  { label: '随机 (random)', value: 'random' },
  { label: '固定渠道 (pinned)', value: 'pinned' },
]
const strategyHints = {
  weighted: '同优先级组内按权重加权随机分流,其余渠道作为故障转移候选。',
  round_robin: '同优先级组内严格轮询(Redis 跨副本游标),均匀分担流量。',
  failover: '只用优先级最高的「主」渠道,其余为「备」;主渠道熔断/失败才切备,主恢复前不回流。',
  random: '同优先级组内纯随机,忽略权重。',
  pinned: '固定只用选定的渠道,其余渠道仅在它熔断/失败时兜底。',
}
const blank = () => ({ model_name: '', input_price_cents_per_m: 0, output_price_cents_per_m: 0, cache_read_price_cents_per_m: 0, cache_write_price_cents_per_m: 0, enabled: true, description: '', long_desc: '', capabilities: ['text'], context_length: 0, routing_strategy: 'weighted', pinned_channel_id: '' })
const form = ref(blank())
const rules = {
  model_name: { required: true, message: '请输入模型名', trigger: 'blur' },
}

// 渠道挂载的模型名集合(规范化后从 channel_models 推导)。
const cModelNames = c => (c.channel_models || []).map(cm => cm.model_name)

// 本模型挂载的渠道(供应商来源 / 负载均衡对象)。
const mountedChannels = computed(() => channels.value.filter(c => cModelNames(c).includes(form.value.model_name)))
const availableChannels = computed(() => channels.value.filter(c => c.status === 'active' && !cModelNames(c).includes(form.value.model_name)))
const availableOptions = computed(() => availableChannels.value.map(c => ({ label: `${c.name} (${presetLabel(c.provider, c.base_url)})`, value: c.id })))
const pinnedOptions = computed(() => mountedChannels.value.map(c => ({ label: `${c.name} (${presetLabel(c.provider, c.base_url)})`, value: c.id })))

// 角色:取挂载渠道中最高优先级=主,其余=备;最高组多于一个=同级(参与均衡)。
const maxPriority = computed(() => mountedChannels.value.reduce((m, c) => Math.max(m, c.priority || 0), -Infinity))
function roleOf(c) {
  const top = mountedChannels.value.filter(x => (x.priority || 0) === maxPriority.value).length
  if ((c.priority || 0) === maxPriority.value) return { label: top > 1 ? '同级·主' : '主', type: 'success' }
  return { label: '备', type: 'warning' }
}

const routeHint = computed(() => {
  const n = mountedChannels.value.length
  if (n === 0) return '该模型尚未挂载渠道,请在下方添加渠道后,负载均衡才会生效。'
  if (n === 1) return `仅挂载 1 个渠道,均衡策略不生效(固定走该渠道)。策略: ${strategyHints[form.value.routing_strategy]}`
  return strategyHints[form.value.routing_strategy]
})
const routeAlertType = computed(() => mountedChannels.value.length < 2 ? 'warning' : 'info')

const cols = [
  { title: '模型', key: 'model_name' },
  { title: '供应商(挂载渠道)', key: 'ch', render: r => {
    const cs = (channels.value || []).filter(c => cModelNames(c).includes(r.model_name))
    if (!cs.length) return h('span', { style: 'color:#bbb' }, '— 无')
    return h('div', { style: 'display:flex;gap:4px;flex-wrap:wrap' }, cs.map(c => h(NTag, { size: 'tiny', type: c.provider === 'mock' ? 'default' : 'info' }, () => presetLabel(c.provider, c.base_url))))
  } },
  { title: '能力', key: 'capabilities', render: r => {
    const cs = r.capabilities || []
    if (!cs.length) return h('span', { style: 'color:#bbb' }, '—')
    return h('span', null, cs.map(c => (CAP_META[c] || c).split(' ')[0]).join(' '))
  } },
  { title: '上下文', key: 'context_length', render: r => fmtCtx(r.context_length) },
  { title: '输入售价', key: 'in', render: r => yuanPerM(r.input_price_cents_per_m) },
  { title: '输出售价', key: 'out', render: r => yuanPerM(r.output_price_cents_per_m) },
  { title: '路由策略', key: 'routing_strategy', render: r => h(NTag, { size: 'small', type: r.routing_strategy === 'weighted' ? 'default' : 'info' }, () => r.routing_strategy || 'weighted') },
  { title: '启用', key: 'enabled', render: r => h(NTag, { size: 'small', type: r.enabled ? 'success' : 'default' }, () => r.enabled ? '启用' : '停用') },
  { title: '操作', key: 'op', render(r) {
    return h('div', { style: 'display:flex;gap:6px' }, [
      h(NButton, { size: 'tiny', tertiary: true, onClick: () => edit(r) }, () => '编辑'),
      h(NPopconfirm, { onPositiveClick: () => del(r) }, { trigger: () => h(NButton, { size: 'tiny', tertiary: true, type: 'error' }, () => '删除'), default: () => `删除模型「${r.model_name}」?引用此模型的渠道将自动失效,确认?` }),
    ])
  } },
]
function edit(r) {
  form.value = { ...blank(), model_name: r.model_name,
    input_price_cents_per_m: r.input_price_cents_per_m, output_price_cents_per_m: r.output_price_cents_per_m,
    cache_read_price_cents_per_m: r.cache_read_price_cents_per_m || 0, cache_write_price_cents_per_m: r.cache_write_price_cents_per_m || 0,
    enabled: r.enabled, description: r.description || '', long_desc: r.long_desc || '', capabilities: r.capabilities && r.capabilities.length ? r.capabilities : ['text'], context_length: r.context_length || 0,
    routing_strategy: r.routing_strategy || 'weighted', pinned_channel_id: r.pinned_channel_id || '' }
  tagsStr.value = (r.tags || []).join(',')
  editing.value = true
  addChId.value = null
  show.value = true
}
function openCreate() {
  form.value = blank(); tagsStr.value = ''; editing.value = false; addChId.value = null; show.value = true
}
async function load() {
  loading.value = true
  try {
    const [m, c] = await Promise.all([api.models(), api.channels()])
    rows.value = m.data.data
    channels.value = c.data.data || []
  } catch (e) { message.error(apiErr(e, '加载失败')) }
  finally { loading.value = false }
}
onMounted(load)
async function del(r) {
  try { await api.deleteModel(r.model_name); message.success('已删除'); load() }
  catch (e) { message.error(apiErr(e, '删除失败')) }
}
async function save() {
  try { await formRef.value?.validate() } catch { return }
  busy.value = true
  try {
    await api.upsertModel({ ...form.value, tags: [...new Set(tagsStr.value.split(',').map(s => s.trim()).filter(Boolean))] })
    message.success(editing.value ? '已保存' : '已创建,可继续配置路由')
    if (!editing.value) { editing.value = true; await load() }
    else { show.value = false; load() }
  } catch (e) { message.error(apiErr(e, '保存失败')) } finally { busy.value = false }
}

// 即时更新某渠道路由(权重/优先级),无需整体保存。
// 按 channel id 维护独立防抖定时器: 单一全局定时器会让多个渠道的连续编辑互相覆盖,
// 或丢弃同一渠道的中间修改,导致最终落库与界面不一致。
const saveTimers = new Map()
function saveRouting(c) {
  if (saveTimers.has(c.id)) clearTimeout(saveTimers.get(c.id))
  saveTimers.set(c.id, setTimeout(async () => {
    saveTimers.delete(c.id)
    try { await api.updateChannelRouting(c.id, { priority: c.priority, weight: c.weight }); message.success('已更新路由') }
    catch (e) { message.error(apiErr(e, '更新失败')); load() }
  }, 350))
}
async function attach() {
  if (!addChId.value) return
  const c = channels.value.find(x => x.id === addChId.value)
  if (!c) return
  try { await api.addChannelModel(c.id, form.value.model_name); message.success('已挂载'); addChId.value = null; load() }
  catch (e) { message.error(apiErr(e, '挂载失败')) }
}
async function detach(c) {
  try { await api.removeChannelModel(c.id, form.value.model_name); message.success('已移除挂载'); load() }
  catch (e) { message.error(apiErr(e, '移除失败')) }
}
</script>
<style scoped>
.bar { display:flex; justify-content:space-between; align-items:center; margin-bottom:14px } .bar h3 { margin:0 }
.sec { margin:18px 0 10px; font-size:14px; color:#1f2330; border-left:3px solid #3D6EFF; padding-left:8px }
.sub { color:#9097a3; font-size:12px; margin:4px 0 }
.route-row { display:flex; align-items:center; gap:12px; margin-bottom:4px }
.route-label { font-size:13px; color:#4b5160; min-width:96px }
.ch-table { border:1px solid #eef0f5; border-radius:8px; overflow:hidden; margin-bottom:10px }
.ch-row { display:grid; grid-template-columns:90px 1fr 80px 96px 86px 56px; gap:8px; align-items:center; padding:8px 10px; border-bottom:1px solid #f2f3f7; font-size:13px }
.ch-row:last-child { border-bottom:none }
.ch-head { background:#fafbfc; color:#9097a3; font-size:12px }
.cname { white-space:nowrap; overflow:hidden; text-overflow:ellipsis }
.add-row { display:flex; gap:8px; align-items:center; margin-top:8px }
.actions { display:flex; justify-content:flex-end; gap:8px; margin-top:18px; border-top:1px solid #f2f3f7; padding-top:14px }
@media (max-width:640px) { .ch-row { grid-template-columns:1fr 1fr; } .ch-head { display:none } }
</style>
