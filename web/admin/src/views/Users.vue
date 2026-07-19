<template>
  <div>
    <div class="bar"><h3>用户</h3><n-button type="primary" @click="openCreate">+ 新建用户</n-button></div>
    <n-data-table :columns="cols" :data="rows" :bordered="false" :loading="loading" :pagination="PAGINATION" />

    <!-- 新建 -->
    <n-modal v-model:show="show" preset="card" title="新建用户" style="max-width:460px">
      <n-form ref="formRef" :model="form" :rules="rules">
        <n-form-item path="tenant_id" label="租户"><n-select v-model:value="form.tenant_id" filterable tag :options="tenantOptions" placeholder="选择或输入租户 ID" /></n-form-item>
        <n-form-item path="email" label="邮箱"><n-input v-model:value="form.email" /></n-form-item>
        <n-form-item path="password" label="密码"><n-input v-model:value="form.password" type="password" show-password-on="click" placeholder="至少 6 位" /></n-form-item>
        <n-form-item label="角色"><n-select v-model:value="form.role" :options="roleOpts" /></n-form-item>
        <n-button type="primary" :loading="busy" :disabled="busy" @click="create">创建</n-button>
      </n-form>
    </n-modal>

    <!-- 编辑邮箱/角色 -->
    <n-modal v-model:show="editShow" preset="card" :title="'编辑用户: ' + (editForm.email || '')" style="max-width:440px">
      <n-form label-placement="top">
        <n-form-item label="邮箱"><n-input v-model:value="editForm.email" /></n-form-item>
        <n-form-item label="角色"><n-select v-model:value="editForm.role" :options="roleOpts" /></n-form-item>
        <n-button type="primary" :loading="busy" :disabled="busy" @click="saveEdit">保存</n-button>
      </n-form>
    </n-modal>

    <!-- 重置密码 -->
    <n-modal v-model:show="pwdShow" preset="card" :title="'重置密码: ' + (pwdTarget || '')" style="max-width:440px">
      <n-form label-placement="top">
        <n-form-item label="新密码(至少 6 位)"><n-input v-model:value="pwdVal" type="password" show-password-on="click" /></n-form-item>
        <n-button type="primary" :loading="busy" :disabled="busy" @click="savePwd">重置</n-button>
      </n-form>
    </n-modal>

    <!-- 调整余额 -->
    <n-modal v-model:show="balShow" preset="card" :title="'调整余额: ' + (balTarget || '')" style="max-width:440px">
      <n-form label-placement="top">
        <n-form-item label="增减金额(元,负数为扣减)"><n-input-number v-model:value="balDelta" :precision="2" :step="1" /></n-form-item>
        <n-button type="primary" :loading="busy" :disabled="busy" @click="saveBal">调整</n-button>
      </n-form>
    </n-modal>
  </div>
</template>
<script setup>
import { ref, h, computed, onMounted } from 'vue'
import { NDataTable, NButton, NModal, NForm, NFormItem, NInput, NInputNumber, NSelect, NPopconfirm, NTag, useMessage } from 'naive-ui'
import { api } from '../api.js'
import { yuan, statusLabel, statusType, apiErr, PAGINATION } from '../format.js'

const message = useMessage()
const rows = ref([])
const tenants = ref([])
const loading = ref(false)
const show = ref(false); const busy = ref(false)
const formRef = ref(null)
const form = ref({ tenant_id: null, email: '', password: '', role: 'member' })
const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
const rules = {
  tenant_id: { required: true, message: '请选择租户', trigger: 'change' },
  email: [{ required: true, message: '请输入邮箱', trigger: 'blur' }, { pattern: emailPattern, message: '邮箱格式不正确', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }, { min: 6, message: '密码至少 6 位', trigger: 'blur' }],
}
// 后端 User/Tenant 等结构均带 snake_case json tag,前端一律按 snake_case 读取。
const roleOpts = [
  { label: 'member(普通成员)', value: 'member' },
  { label: 'admin(租户管理员)', value: 'admin' },
]
const tenantOptions = computed(() => tenants.value.map(t => ({ label: `${t.name || t.id} (${t.id})`, value: t.id })))
const tenantName = computed(() => {
  const m = new Map()
  tenants.value.forEach(t => m.set(t.id, t.name || t.id))
  return m
})

// 编辑/改密/调余额 状态
const editShow = ref(false)
const editForm = ref({ id: '', email: '', role: 'member' })
const pwdShow = ref(false); const pwdTarget = ref(''); const pwdVal = ref('')
const balShow = ref(false); const balTarget = ref(''); const balDelta = ref(0)

const cols = [
  { title: 'ID', key: 'id' },
  { title: '邮箱', key: 'email' },
  { title: '租户', key: 'tenant_id', render: r => tenantName.value.get(r.tenant_id) || r.tenant_id || '—' },
  { title: '角色', key: 'role', render: r => h(NTag, { type: r.role === 'admin' ? 'success' : 'default', size: 'small' }, () => r.role) },
  { title: '余额', key: 'balance', render: r => yuan(r.balance_cents) },
  { title: '状态', key: 'status', render: r => h(NTag, { size: 'small', type: statusType(r.status), round: true }, () => statusLabel(r.status)) },
  { title: '操作', key: 'op', render(r) {
    return h('div', { style: 'display:flex;gap:4px;flex-wrap:wrap' }, [
      h(NButton, { size: 'tiny', tertiary: true, onClick: () => openEdit(r) }, () => '编辑'),
      h(NButton, { size: 'tiny', tertiary: true, onClick: () => openPwd(r) }, () => '改密'),
      h(NButton, { size: 'tiny', tertiary: true, onClick: () => openBal(r) }, () => '调余额'),
      h(NPopconfirm, { onPositiveClick: () => toggle(r) }, {
        trigger: () => h(NButton, { size: 'tiny', tertiary: true, type: r.status === 'active' ? 'warning' : 'success' }, () => r.status === 'active' ? '禁用' : '启用'),
        default: () => `确定${r.status === 'active' ? '禁用' : '启用'}该用户?`
      }),
    ])
  } },
]
function openCreate() { form.value = { tenant_id: tenantOptions.value[0]?.value || null, email: '', password: '', role: 'member' }; show.value = true }
function openEdit(r) { editForm.value = { id: r.id, email: r.email, role: r.role === 'platform_admin' ? 'admin' : r.role }; editShow.value = true }
function openPwd(r) { pwdTarget.value = r.email; pwdVal.value = ''; pwdShow.value = true; editForm.value.id = r.id }
function openBal(r) { balTarget.value = r.email; balDelta.value = 0; balShow.value = true; editForm.value.id = r.id }
async function load() {
  loading.value = true
  try {
    const [u, t] = await Promise.all([api.users(), api.tenants()])
    rows.value = u.data.data
    tenants.value = t.data.data || []
  } catch (e) { message.error(apiErr(e, '加载失败')) }
  finally { loading.value = false }
}
async function toggle(r) {
  try { await api.setUserStatus(r.id, r.status === 'active' ? 'disabled' : 'active'); message.success('已更新'); load() }
  catch (e) { message.error(apiErr(e, '操作失败')) }
}
async function create() {
  try { await formRef.value?.validate() } catch { return }
  busy.value = true
  try { await api.createUser(form.value); message.success('已创建'); show.value = false; load() }
  catch (e) { message.error(apiErr(e, '创建失败')) } finally { busy.value = false }
}
async function saveEdit() {
  busy.value = true
  try { await api.updateUser(editForm.value.id, { email: editForm.value.email, role: editForm.value.role }); message.success('已保存'); editShow.value = false; load() }
  catch (e) { message.error(apiErr(e, '保存失败')) } finally { busy.value = false }
}
async function savePwd() {
  if ((pwdVal.value || '').length < 6) { message.warning('密码至少 6 位'); return }
  busy.value = true
  try { await api.resetUserPassword(editForm.value.id, pwdVal.value); message.success('已重置'); pwdShow.value = false }
  catch (e) { message.error(apiErr(e, '重置失败')) } finally { busy.value = false }
}
async function saveBal() {
  const cents = Math.round((balDelta.value || 0) * 100)
  if (!cents) { message.warning('金额不能为 0'); return }
  busy.value = true
  try { await api.adjustUserBalance(editForm.value.id, cents); message.success('已调整'); balShow.value = false; load() }
  catch (e) { message.error(apiErr(e, '调整失败')) } finally { busy.value = false }
}
onMounted(load)
</script>
<style scoped>.bar { display:flex; justify-content:space-between; align-items:center; margin-bottom:14px } .bar h3 { margin:0 }</style>
