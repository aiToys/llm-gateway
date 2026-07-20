<template>
  <div class="page">
    <div class="card">
      <div class="brand"><img src="/logo.svg" class="logo-img" alt="logo" /><h2>管理控制台</h2></div>
      <n-form @keyup.enter="doLogin">
        <n-form-item label="管理员邮箱"><n-input v-model:value="form.email" placeholder="admin@demo.com" /></n-form-item>
        <n-form-item label="密码"><n-input v-model:value="form.password" type="password" show-password-on="click" /></n-form-item>
        <n-button type="primary" block :loading="loading" @click="doLogin">登录</n-button>
      </n-form>
    </div>
  </div>
</template>
<script setup>
import { ref, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NForm, NFormItem, NInput, NButton, useMessage } from 'naive-ui'
import { api } from '../api.js'
import { token, user } from '../store.js'
const router = useRouter(); const route = useRoute(); const message = useMessage()
const form = ref({ email: '', password: '' }); const loading = ref(false)
async function doLogin() {
  if (!form.value.email || !form.value.password) { message.warning('请输入邮箱和密码'); return }
  loading.value = true
  try {
    const { data } = await api.login(form.value.email, form.value.password)
    // 平台超级管理员(platform_admin)与租户管理员(admin)均可登录控制台。
    if (data.user.role !== 'admin' && data.user.role !== 'platform_admin') { message.error('非管理员账号'); return }
    token.set(data.token); user.set(data.user)
    // 优先回到登录前页面;仅允许站内相对路径(防开放重定向)。
    // redirect 由 api.js 存为完整路径(含 SPA base /admin/),而 router.push 会再拼一次 base,
    // 直接 push 会导致 /admin/admin/channels 双重前缀。这里剥掉 base,统一为 router 相对路径。
    const base = '/admin/'
    const redirect = route.query.redirect
    let target = '/dashboard'
    if (typeof redirect === 'string' && redirect.startsWith('/') && !redirect.startsWith('//')) {
      target = redirect.startsWith(base) ? redirect.slice(base.length - 1) : redirect
      // 兜底:确保剥成 /xxx 形态(避免 // 或空)。
      if (!target.startsWith('/') || target.startsWith('//')) target = '/dashboard'
    }
    router.push(target)
  } catch (e) { message.error(e.response?.data?.error || '登录失败') }
  finally { loading.value = false }
}
onMounted(() => {
  if (route.query.expired) message.warning('登录已过期,请重新登录')
})
</script>
<style scoped>
.page { min-height:100vh; display:flex; align-items:center; justify-content:center; background:linear-gradient(135deg,#0f172a,#134e4a) }
.card { width:380px; background:#fff; border-radius:16px; padding:30px; box-shadow:0 16px 50px rgba(0,0,0,.2) }
.brand { display:flex; align-items:center; gap:10px; margin-bottom:18px }
.brand h2 { margin:0; font-size:18px }
.brand .logo-img { width:24px; height:24px }
</style>
