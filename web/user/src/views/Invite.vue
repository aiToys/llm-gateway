<template>
  <div class="wrap">
    <div class="card">
      <div class="logo">◆</div>
      <h2 v-if="info && info.valid">加入「{{ info.tenant_name || '团队' }}」</h2>
      <h2 v-else-if="info">邀请无效或已过期</h2>
      <h2 v-else>加载邀请…</h2>
      <p class="sub" v-if="info && info.valid">填写邮箱与密码即可加入团队,角色: {{ roleText }}</p>

      <template v-if="info && info.valid">
        <n-form ref="formRef" :model="form" :rules="rules">
          <n-form-item path="email" label="邮箱"><n-input v-model:value="form.email" placeholder="you@team.com" /></n-form-item>
          <n-form-item path="password" label="设置密码"><n-input v-model:value="form.password" type="password" show-password-on="click" placeholder="至少 6 位" /></n-form-item>
          <n-button type="primary" block :loading="busy" :disabled="busy" @click="accept">加入团队</n-button>
        </n-form>
      </template>
      <n-button v-else-if="info" text @click="goHome" style="margin-top:18px">返回首页</n-button>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NForm, NFormItem, NInput, NButton, useMessage } from 'naive-ui'
import { api, apiErr } from '../api.js'
import { token as tokenStore } from '../store.js'

const router = useRouter(); const route = useRoute(); const message = useMessage()
const info = ref(null)
const busy = ref(false)
const formRef = ref(null)
const form = reactive({ email: '', password: '' })
const rules = {
  email: [{ required: true, message: '请输入邮箱', trigger: 'blur' }, { pattern: /^[^\s@]+@[^\s@]+\.[^\s@]+$/, message: '邮箱格式不正确', trigger: 'blur' }],
  password: [{ required: true, message: '请设置密码', trigger: 'blur' }, { min: 6, message: '至少 6 位', trigger: 'blur' }],
}
const roleText = ref('成员')

onMounted(async () => {
  const tk = route.query.token
  if (!tk) { info.value = { valid: false }; return }
  try {
    const { data } = await api.inviteInfo(tk)
    info.value = data.data
    roleText.value = info.value.role === 'admin' ? '管理员' : '成员'
  } catch { info.value = { valid: false } }
})

async function accept() {
  try { await formRef.value?.validate() } catch { return }
  busy.value = true
  try {
    const { data } = await api.acceptInvite({ token: route.query.token, email: form.email, password: form.password })
    tokenStore.set(data.token)
    message.success('已加入团队')
    router.push('/console/team')
  } catch (e) { message.error(apiErr(e, '加入失败')) }
  finally { busy.value = false }
}
function goHome() { router.push('/') }
</script>

<style scoped>
.wrap { min-height:100vh; display:flex; align-items:center; justify-content:center; background:linear-gradient(135deg,#3D6EFF22,#22d3ee22) }
.card { background:var(--bg-card); border-radius:16px; padding:32px; width:380px; box-shadow:0 8px 30px #1f233022; text-align:center }
.logo { font-size:30px; color:#3D6EFF; margin-bottom:6px }
h2 { margin:6px 0 4px; font-size:20px }
.sub { color:var(--text); font-size:13px; margin-bottom:18px }
</style>
