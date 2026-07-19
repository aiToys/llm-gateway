<template>
  <n-layout has-sider style="height:100vh">
    <n-layout-sider bordered :width="210" content-style="background:#0f172a">
      <div class="logo"><img src="/logo.svg" class="logo-img" alt="logo" />管理控制台</div>
      <n-menu dark :value="active" :options="menu" @update:value="go" :inverted="true" />
    </n-layout-sider>
    <n-layout>
      <n-layout-header bordered class="hdr">
        <span class="t">{{ titles[route.name] }}</span>
        <n-button quaternary @click="doLogout">{{ email }} · 退出</n-button>
      </n-layout-header>
      <n-layout-content content-style="padding:20px;height:calc(100vh - 56px);overflow:auto;background:#f4f6f9">
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>
<script setup>
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NLayout, NLayoutSider, NLayoutHeader, NLayoutContent, NMenu, NButton } from 'naive-ui'
import { user, logout } from '../store.js'

const route = useRoute(); const router = useRouter()
const active = computed(() => route.name)
const email = computed(() => user.get()?.email || '管理员')
const titles = { dashboard: '仪表盘', tenants: '租户管理', users: '用户管理', channels: '渠道管理', models: '模型与定价', analytics: '用量分析', ledger: '计费审计', audit: '审计日志', 'request-logs': '请求日志', 'tenant-keys': '密钥管理' }
const menu = [
  { label: '仪表盘', key: 'dashboard' },
  { label: '租户管理', key: 'tenants' },
  { label: '用户管理', key: 'users' },
  { label: '渠道管理', key: 'channels' },
  { label: '模型与定价', key: 'models' },
  { label: '用量分析', key: 'analytics' },
  { label: '计费审计', key: 'ledger' },
  { label: '密钥管理', key: 'tenant-keys' },
  { label: '请求日志', key: 'request-logs' },
  { label: '审计日志', key: 'audit' },
]
function go(k) { router.push({ name: k }) }
function doLogout() { logout(); router.push('/login') }
</script>
<style scoped>
.logo { color:#e2e8f0; font-weight:700; padding:18px 20px; display:flex; align-items:center; gap:8px }
.logo-img { width:24px; height:24px; flex-shrink:0 }
.hdr { height:56px; display:flex; align-items:center; justify-content:space-between; padding:0 20px; background:#fff }
.hdr .t { font-weight:600 }
</style>
