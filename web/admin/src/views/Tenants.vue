<template>
  <div>
    <div class="bar"><h3>租户</h3><n-button type="primary" @click="openCreate">+ 新建租户</n-button></div>
    <n-data-table :columns="cols" :data="rows" :bordered="false" :loading="loading" :pagination="PAGINATION" />

    <!-- 新建/编辑 -->
    <n-modal v-model:show="show" preset="card" :title="editing ? '编辑租户' : '新建租户'" style="max-width:440px">
      <n-form ref="formRef" :model="form" :rules="rules">
        <n-form-item path="name" label="名称"><n-input v-model:value="form.name" /></n-form-item>
        <n-form-item path="slug" label="slug(选填,小写字母/数字/连字符)"><n-input v-model:value="form.slug" placeholder="my-team" /></n-form-item>
        <n-button type="primary" :loading="busy" :disabled="busy" @click="save">{{ editing ? '保存' : '创建' }}</n-button>
      </n-form>
    </n-modal>

    <!-- 详情:成员 + 渠道聚合 -->
    <n-modal v-model:show="detailShow" preset="card" :title="detailTitle" style="max-width:760px">
      <div class="dsec"><span>成员 <b>{{ detailMembers.length }}</b></span><span>渠道 <b>{{ detailChannels.length }}</b></span></div>
      <h4 class="dh">成员</h4>
      <n-data-table v-if="detailMembers.length" :columns="memberCols" :data="detailMembers" :bordered="false" size="small" :pagination="false" />
      <n-empty v-else size="small" description="该租户暂无成员" style="margin:10px 0" />
      <h4 class="dh" style="margin-top:14px">渠道</h4>
      <n-data-table v-if="detailChannels.length" :columns="chCols" :data="detailChannels" :bordered="false" size="small" :pagination="false" />
      <n-empty v-else size="small" description="该租户暂无渠道(含平台默认渠道)" style="margin:10px 0" />
    </n-modal>
  </div>
</template>
<script setup>
import { ref, h, computed, onMounted } from 'vue'
import { NDataTable, NButton, NModal, NForm, NFormItem, NInput, NTag, NPopconfirm, NEmpty, useMessage } from 'naive-ui'
import { api } from '../api.js'
import { fmtTime, statusLabel, statusType, provLabel, yuan, apiErr, PAGINATION } from '../format.js'

const message = useMessage()
const rows = ref([]); const loading = ref(false); const show = ref(false); const busy = ref(false)
const editing = ref(false)
const formRef = ref(null)
const form = ref({ name: '', slug: '' })
const allUsers = ref([]); const allChannels = ref([])
const rules = {
  name: { required: true, message: '请输入名称', trigger: 'blur' },
  slug: { pattern: /^[a-z0-9-]*$/, message: '仅小写字母、数字、连字符', trigger: 'blur' },
}
const PLATFORM = 'tenant-platform'

// 详情状态
const detailShow = ref(false); const detailID = ref(''); const detailName = ref('')
const detailTitle = computed(() => '租户详情: ' + (detailName.value || detailID.value))
const detailMembers = computed(() => allUsers.value.filter(u => u.tenant_id === detailID.value))
// 租户渠道 = 租户私有渠道(tenant_id 匹配) + 平台默认渠道(tenant_id 为空,所有租户共用)。
const detailChannels = computed(() => allChannels.value.filter(c => c.tenant_id === detailID.value || !c.tenant_id))
const memberCols = [
  { title: '邮箱', key: 'email' },
  { title: '角色', key: 'role', render: r => h(NTag, { size: 'small', type: r.role === 'admin' ? 'success' : 'default' }, () => r.role) },
  { title: '余额', key: 'balance_cents', render: r => yuan(r.balance_cents) },
  { title: '状态', key: 'status', render: r => h(NTag, { size: 'small', type: statusType(r.status), round: true }, () => statusLabel(r.status)) },
]
const chCols = [
  { title: '名称', key: 'name' },
  { title: '供应商', key: 'provider', render: r => h(NTag, { size: 'small' }, () => provLabel(r.provider)) },
  { title: '归属', key: 'own', render: r => r.tenant_id ? '租户私有' : '平台默认' },
  { title: '状态', key: 'status', render: r => h(NTag, { size: 'small', type: statusType(r.status), round: true }, () => statusLabel(r.status)) },
]

const cols = [
  { title: 'id', key: 'id' },
  { title: '名称', key: 'name' },
  { title: 'slug', key: 'slug' },
  { title: '状态', key: 'status', render: r => h(NTag, { size: 'small', type: statusType(r.status), round: true }, () => statusLabel(r.status)) },
  { title: '创建时间', key: 'created_at', render: r => fmtTime(r.created_at) },
  { title: '操作', key: 'op', render(r) {
    if (r.id === PLATFORM) return h('span', { style: 'color:#bbb;font-size:12px' }, '平台内置')
    return h('div', { style: 'display:flex;gap:6px' }, [
      h(NButton, { size: 'tiny', tertiary: true, onClick: () => openDetail(r) }, () => '详情'),
      h(NButton, { size: 'tiny', tertiary: true, onClick: () => openEdit(r) }, () => '编辑'),
      h(NPopconfirm, { onPositiveClick: () => toggle(r) }, {
        trigger: () => h(NButton, { size: 'tiny', tertiary: true, type: r.status === 'active' ? 'warning' : 'success' }, () => r.status === 'active' ? '禁用' : '启用'),
        default: () => `确定${r.status === 'active' ? '禁用' : '启用'}该租户?禁用后其用户 API 调用将被拦截(受鉴权缓存最多约 2 分钟延迟)。`
      }),
    ])
  } },
]
function openCreate() { editing.value = false; form.value = { name: '', slug: '' }; show.value = true }
function openEdit(r) { editing.value = true; form.value = { id: r.id, name: r.name, slug: r.slug }; show.value = true }
function openDetail(r) { detailID.value = r.id; detailName.value = r.name; detailShow.value = true }
async function load() {
  loading.value = true
  try {
    const [t, u, c] = await Promise.all([api.tenants(), api.users(), api.channels()])
    rows.value = t.data.data
    allUsers.value = u.data.data || []
    allChannels.value = c.data.data || []
  } catch (e) { message.error(apiErr(e, '加载失败')) }
  finally { loading.value = false }
}
async function save() {
  try { await formRef.value?.validate() } catch { return }
  busy.value = true
  try {
    if (editing.value) {
      await api.updateTenant(form.value.id, { name: form.value.name, slug: form.value.slug })
      message.success('已保存')
    } else {
      await api.createTenant(form.value)
      message.success('已创建')
    }
    show.value = false; load()
  } catch (e) { message.error(apiErr(e, '保存失败')) } finally { busy.value = false }
}
async function toggle(r) {
  try { await api.setTenantStatus(r.id, r.status === 'active' ? 'disabled' : 'active'); message.success('已更新'); load() }
  catch (e) { message.error(apiErr(e, '操作失败')) }
}
onMounted(load)
</script>
<style scoped>
.bar { display:flex; justify-content:space-between; align-items:center; margin-bottom:14px } .bar h3 { margin:0 }
.dsec { display:flex; gap:18px; font-size:13px; color:#6b7280; margin-bottom:6px } .dsec b { color:#1f2330 }
.dh { margin:8px 0 6px; font-size:13px; color:#1f2330; border-left:3px solid #0F766E; padding-left:8px }
</style>
