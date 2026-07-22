<template>
  <div class="page">
    <div class="card">
      <div class="brand">
        <img src="/logo.svg" class="logo-img" alt="logo" />
        <h2>LLM Gateway</h2>
      </div>
      <p class="sub">统一接入百炼 / 火山方舟 / 千帆,兼容 OpenAI 与 Anthropic 协议</p>
      <n-tabs v-model:value="tab" justify-content="space-evenly">
        <n-tab-pane name="login" tab="登录">
          <n-form ref="lf" :model="loginForm" :rules="loginRules">
            <n-form-item path="email" label="邮箱">
              <n-input v-model:value="loginForm.email" placeholder="admin@demo.com" @keyup.enter="doLogin" />
            </n-form-item>
            <n-form-item path="password" label="密码">
              <n-input v-model:value="loginForm.password" type="password" show-password-on="click" placeholder="••••••" @keyup.enter="doLogin" />
            </n-form-item>
            <n-button type="primary" block :loading="loading" :disabled="loading" @click="doLogin">登录</n-button>
          </n-form>
        </n-tab-pane>
        <n-tab-pane name="register" tab="注册">
          <n-form ref="rf" :model="regForm" :rules="regRules">
            <n-form-item path="tenant" label="工作空间名">
              <n-input v-model:value="regForm.tenant" placeholder="选填,留空使用默认" />
            </n-form-item>
            <n-form-item path="email" label="邮箱">
              <n-input v-model:value="regForm.email" placeholder="you@example.com" />
            </n-form-item>
            <n-form-item path="password" label="密码">
              <n-input v-model:value="regForm.password" type="password" show-password-on="click" placeholder="至少 6 位" />
            </n-form-item>
            <n-button type="primary" block :loading="loading" :disabled="loading" @click="doRegister">注册并登录</n-button>
          </n-form>
        </n-tab-pane>
      </n-tabs>
      <p class="hint" v-if="showDemo">演示账号: admin@demo.com / admin123 &nbsp; demo@demo.com / demo123</p>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NTabs, NTabPane, NForm, NFormItem, NInput, NButton, useMessage } from 'naive-ui'
import { api, user, apiErr } from '../api.js'
import { token } from '../store.js'

const router = useRouter()
const route = useRoute()
const message = useMessage()
const tab = ref('login')
const loading = ref(false)
const lf = ref(null)
const rf = ref(null)
const showDemo = import.meta.env.DEV
const loginForm = ref({ email: '', password: '' })
const regForm = ref({ email: '', password: '', tenant: '' })
const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/
const loginRules = {
  email: [
    { required: true, message: '请输入邮箱', trigger: 'blur' },
    { pattern: emailPattern, message: '邮箱格式不正确', trigger: 'blur' },
  ],
  password: { required: true, message: '请输入密码', trigger: 'blur' },
}
const regRules = {
  email: [
    { required: true, message: '请输入邮箱', trigger: 'blur' },
    { pattern: emailPattern, message: '邮箱格式不正确', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 6, message: '密码至少 6 位', trigger: 'blur' },
  ],
}

async function doLogin() {
  try { await lf.value?.validate() } catch { return }
  loading.value = true
  try {
    const { data } = await api.login(loginForm.value.email, loginForm.value.password)
    afterAuth(data)
  } catch (e) {
    message.error(apiErr(e, '登录失败'))
  } finally { loading.value = false }
}
async function doRegister() {
  try { await rf.value?.validate() } catch { return }
  loading.value = true
  try {
    const { data } = await api.register(regForm.value.email, regForm.value.password, regForm.value.tenant)
    afterAuth(data)
  } catch (e) {
    message.error(apiErr(e, '注册失败'))
  } finally { loading.value = false }
}
function afterAuth(data) {
  token.set(data.token)
  user.set(data.user)
  message.success('登录成功')
  // 优先回到登录前试图访问的页面;仅允许站内相对路径(防开放重定向)。
  const redirect = route.query.redirect
  const safe = typeof redirect === 'string' && redirect.startsWith('/') && !redirect.startsWith('//')
  router.push(safe ? redirect : '/console/chat')
}

onMounted(() => {
  // 401 过期跳转来此: 提示用户为何被登出,而非无声踢回登录页。
  if (route.query.expired) message.warning('登录已过期,请重新登录')
})
</script>

<style scoped>
.page { min-height:100vh; display:flex; align-items:center; justify-content:center;
  background: radial-gradient(circle at 20% 20%, #e8efff, #f6f8fc 40%), linear-gradient(135deg,#f6f8fc,#eef2ff) }
.card { width:min(420px, calc(100vw - 32px)); background:var(--bg-card); border-radius:16px; padding:32px; box-shadow:0 12px 40px rgba(40,60,120,.08) }
.brand { display:flex; align-items:center; gap:10px }
.brand h2 { margin:0; font-size:20px }
.brand .logo-img { width:26px; height:26px }
.sub { color:#7a8190; font-size:13px; margin:6px 0 16px }
.hint { color:#a0a6b2; font-size:12px; margin-top:14px; text-align:center }
</style>
