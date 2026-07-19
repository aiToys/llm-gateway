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
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { NForm, NFormItem, NInput, NButton, useMessage } from 'naive-ui'
import { api } from '../api.js'
import { token, user } from '../store.js'
const router = useRouter(); const message = useMessage()
const form = ref({ email: '', password: '' }); const loading = ref(false)
async function doLogin() {
  loading.value = true
  try {
    const { data } = await api.login(form.value.email, form.value.password)
    // 平台超级管理员(platform_admin)与租户管理员(admin)均可登录控制台。
    if (data.user.role !== 'admin' && data.user.role !== 'platform_admin') { message.error('非管理员账号'); return }
    token.set(data.token); user.set(data.user); router.push('/dashboard')
  } catch (e) { message.error(e.response?.data?.error || '登录失败') }
  finally { loading.value = false }
}
</script>
<style scoped>
.page { min-height:100vh; display:flex; align-items:center; justify-content:center; background:linear-gradient(135deg,#0f172a,#134e4a) }
.card { width:380px; background:#fff; border-radius:16px; padding:30px; box-shadow:0 16px 50px rgba(0,0,0,.2) }
.brand { display:flex; align-items:center; gap:10px; margin-bottom:18px }
.brand h2 { margin:0; font-size:18px }
.brand .logo-img { width:24px; height:24px }
</style>
