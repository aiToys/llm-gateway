<template>
  <div class="page">
    <div class="head">
      <div>
        <h3 v-if="!editing">{{ team.name || '团队' }}</h3>
        <n-input v-else v-model:value="editName" size="small" style="width:240px" />
        <n-tag size="small" :type="team.is_admin ? 'success' : 'default'" round style="margin-left:8px">
          {{ team.is_admin ? '管理员' : '成员' }}
        </n-tag>
      </div>
      <div v-if="team.is_admin">
        <n-button v-if="!editing" size="small" tertiary @click="startEdit">改名</n-button>
        <n-button v-else size="small" type="primary" :loading="saving" @click="saveName">保存</n-button>
        <n-button v-if="editing" size="small" quaternary @click="editing = false">取消</n-button>
      </div>
    </div>

    <n-grid :cols="3" :x-gap="16" :y-gap="16" style="margin-top:18px" responsive="screen">
      <n-gi>
        <div class="stat"><div class="k">成员</div><div class="v">{{ members.length }}</div></div>
      </n-gi>
      <n-gi>
        <div class="stat"><div class="k">团队余额合计</div><div class="v">¥{{ totalBalance }}</div></div>
      </n-gi>
      <n-gi>
        <div class="stat"><div class="k">活跃渠道</div><div class="v">{{ channels.length }}</div></div>
      </n-gi>
    </n-grid>

    <h4>成员</h4>
    <n-data-table :columns="memberCols" :data="members" :bordered="false" size="small" :loading="loading" :pagination="false" />

    <div v-if="team.is_admin" style="margin-top:24px">
      <h4>邀请链接</h4>
      <div class="invite-row">
        <n-button type="primary" size="small" :loading="invBusy" @click="genInvite">+ 生成邀请链接(member)</n-button>
        <n-button size="small" tertiary @click="genInvite('admin')">+ 生成管理员邀请</n-button>
      </div>
      <n-data-table v-if="invites.length" :columns="inviteCols" :data="invites" :bordered="false" size="small" :pagination="false" style="margin-top:10px" />
    </div>

    <h4 style="margin-top:24px">团队渠道(平台默认为只读)</h4>
    <n-data-table :columns="chCols" :data="channels" :bordered="false" size="small" :pagination="false" />
  </div>
</template>

<script setup>
import { ref, h, computed, onMounted } from 'vue'
import { NDataTable, NButton, NTag, NInput, NGrid, NGi, NPopconfirm, useMessage } from 'naive-ui'
import { api, apiErr } from '../api.js'
import { formatCents } from '../utils.js'

const message = useMessage()
const team = ref({ name: '', is_admin: false })
const members = ref([])
const channels = ref([])
const invites = ref([])
const loading = ref(false)
const editing = ref(false)
const editName = ref('')
const saving = ref(false)
const invBusy = ref(false)

const totalBalance = computed(() => formatCents(members.value.reduce((s, m) => s + (m.balance_cents || 0), 0)))
const roleLabel = { admin: { label: '管理员', type: 'success' }, member: { label: '成员', type: 'default' } }

const memberCols = [
  { title: '邮箱', key: 'email', render: r => r.email + (r.is_me ? '（我）' : '') },
  { title: '角色', key: 'role', render: r => { const m = roleLabel[r.role] || { label: r.role, type: 'default' }; return h(NTag, { size: 'small', type: m.type }, () => m.label) } },
  { title: '余额', key: 'balance_cents', render: r => formatCents(r.balance_cents) },
  { title: '操作', key: 'op', render(r) {
    if (!team.value.is_admin || r.is_me) return '—'
    return h(NPopconfirm, { onPositiveClick: () => doTransfer(r) }, {
      trigger: () => h(NButton, { size: 'tiny', tertiary: true }, () => '转账'),
      default: () => '给该成员转账?在下方输入金额。'
    })
  } },
]

const inviteCols = [
  { title: '角色', key: 'role', render: r => (roleLabel[r.role] || {}).label || r.role },
  // 链接列:后端 listInvites 不返回明文 token(安全:明文只返回一次),
  // 故只有"本次会话内刚生成"的邀请(_link 非空)可复制,其余显示提示。
  { title: '链接', key: 'link', render: r => r._link
    ? h('span', { style: 'color:#3D6EFF;cursor:pointer', onClick: () => copyLink(r._link) }, '复制链接')
    : h('span', { style: 'color:#9ca3af' }, '生成时已复制') },
  { title: '状态', key: 'used', render: r => h(NTag, { size: 'small', type: r.used ? 'success' : 'info' }, () => r.used ? '已接受' : '待接受') },
  { title: '操作', key: 'op', render: r => h(NPopconfirm, { onPositiveClick: () => revoke(r.id) }, {
    trigger: () => h(NButton, { size: 'tiny', tertiary: true, type: 'error' }, () => '吊销'),
    default: () => '吊销此邀请链接?'
  }) },
]

const chCols = [
  { title: '名称', key: 'name' },
  { title: '供应商', key: 'provider' },
  { title: '归属', key: 'owner', render: r => h(NTag, { size: 'small', type: r.owner === 'platform' ? 'default' : 'info' }, () => r.owner === 'platform' ? '平台默认·只读' : '团队') },
  { title: '模型', key: 'models', render: r => (r.models || []).join(', ') },
  { title: '状态', key: 'status', render: r => h(NTag, { size: 'small', type: r.status === 'active' ? 'success' : 'warning', round: true }, () => r.status === 'active' ? '正常' : '停用') },
]

async function load() {
  loading.value = true
  try {
    const [t, m, c] = await Promise.all([api.team(), api.teamMembers(), api.teamChannels()])
    team.value = t.data.data
    members.value = m.data.data || []
    channels.value = c.data.data || []
    if (team.value.is_admin) {
      const { data } = await api.listInvites()
      invites.value = (data.data || []).map(i => ({ ...i, _link: '' })) // 链接只在生成时持有
    }
  } catch (e) { message.error(apiErr(e, '加载失败')) }
  finally { loading.value = false }
}
function startEdit() { editing.value = true; editName.value = team.value.name }
async function saveName() {
  saving.value = true
  try { await api.updateTeam(editName.value); team.value.name = editName.value; editing.value = false; message.success('已保存') }
  catch (e) { message.error(apiErr(e, '保存失败')) }
  finally { saving.value = false }
}
async function genInvite(role) {
  invBusy.value = true
  try {
    const { data } = await api.createInvite(role)
    const link = data?.data?.link
    if (!link) { message.error('生成失败:未返回链接'); return }
    copyLink(link)
    message.success('邀请链接已生成并复制到剪贴板')
    const { data: d2 } = await api.listInvites()
    // 回填本次刚生成的 link(后端列表不返回明文 token),并保留会话内已知的其它 _link。
    const prev = Object.fromEntries(invites.value.filter(i => i._link).map(i => [i.id, i._link]))
    invites.value = (d2.data || []).map(i => ({ ...i, _link: i.id === data?.data?.id ? link : (prev[i.id] || '') }))
  } catch (e) { message.error(apiErr(e, '生成失败')) }
  finally { invBusy.value = false }
}
async function copyLink(link) {
  try { await navigator.clipboard.writeText(link); message.success('链接已复制') }
  catch { message.info(link) }
}
async function revoke(id) {
  try { await api.revokeInvite(id); message.success('已吊销'); load() }
  catch (e) { message.error(apiErr(e, '吊销失败')) }
}
async function doTransfer(m) {
  const amt = window.prompt(`转账给 ${m.email} 的金额(元)`)
  if (!amt) return
  const cents = Math.round(Number(amt) * 100)
  if (!(cents > 0)) { message.warning('金额无效'); return }
  try { await api.teamTransfer(m.id, cents); message.success('已转账'); load() }
  catch (e) { message.error(apiErr(e, '转账失败')) }
}
onMounted(load)
</script>

<style scoped>
.page { padding:24px; max-width:860px }
.head { display:flex; justify-content:space-between; align-items:center }
.head h3 { margin:0; display:inline }
.stat { background:#f7f8fc; border-radius:12px; padding:16px }
.stat .k { font-size:12px; color:#6b7280 }
.stat .v { font-size:24px; font-weight:700; margin-top:4px }
h4 { margin:20px 0 10px; font-size:14px; border-left:3px solid #3D6EFF; padding-left:8px }
.invite-row { display:flex; gap:8px }
</style>
