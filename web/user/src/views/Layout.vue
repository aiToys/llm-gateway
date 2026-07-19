<template>
  <n-layout has-sider style="height: 100vh">
    <n-layout-sider bordered :width="220" content-style="display:flex;flex-direction:column">
      <div class="logo">
        <img src="/logo.svg" class="logo-img" alt="logo" />
        <span>模型广场</span>
      </div>
      <n-menu :value="active" :options="menuOptions" @update:value="onMenu" />
      <div class="balance">
        余额 <b>¥{{ yuan }}</b>
        <n-button size="tiny" tertiary type="primary" @click="$router.push('/console/recharge')">充值</n-button>
      </div>
    </n-layout-sider>
    <n-layout>
      <n-layout-header bordered class="header">
        <span class="title">{{ currentTitle }}</span>
        <n-dropdown :options="userOptions" @select="onUser">
          <n-button quaternary>{{ userInfo?.email || '未登录' }}</n-button>
        </n-dropdown>
      </n-layout-header>
      <n-layout-content content-style="padding:0" style="height:calc(100vh - 56px)">
        <router-view />
      </n-layout-content>
    </n-layout>
  </n-layout>
</template>

<script setup>
import { ref, computed, onMounted, h } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NLayout, NLayoutSider, NLayoutHeader, NLayoutContent, NMenu, NButton, NIcon, NDropdown } from 'naive-ui'
import { api, user } from '../api.js'
import { logout } from '../store.js'

const router = useRouter()
const route = useRoute()
const userInfo = ref(user.get())

const titles = { chat: '模型对话', plaza: '模型广场', 'my-models': '模型管理', keys: 'API 密钥', usage: '用量统计', recharge: '账户充值' }
const active = computed(() => route.name)
const currentTitle = computed(() => titles[route.name] || '')
const yuan = computed(() => ((userInfo.value?.balance_cents || 0) / 100).toFixed(2))

const menuOptions = [
  { label: '模型对话', key: 'chat' },
  { label: '模型广场', key: 'plaza' },
  { label: '模型管理', key: 'my-models' },
  { label: 'API 密钥', key: 'keys' },
  { label: '用量统计', key: 'usage' },
  { label: '账户充值', key: 'recharge' },
  { label: '我的团队', key: 'team' },
]
function onMenu(key) { router.push({ name: key }) }

const userOptions = [
  { label: '刷新余额', key: 'refresh' },
  { label: '退出登录', key: 'logout' },
]
async function onUser(key) {
  if (key === 'logout') { logout(); router.push('/login') }
  if (key === 'refresh') {
    const { data } = await api.me()
    userInfo.value = data
    user.set(data)
  }
}
onMounted(async () => {
  if (!userInfo.value) {
    const { data } = await api.me()
    userInfo.value = data
    user.set(data)
  } else {
    api.me().then(({ data }) => { userInfo.value = data; user.set(data) }).catch(() => {})
  }
})
</script>

<style scoped>
.logo { display:flex; align-items:center; gap:8px; padding:18px 20px; font-weight:700; font-size:16px; color:#1f2330 }
.logo-img { width:24px; height:24px; flex-shrink:0 }
.balance { margin-top:auto; padding:14px 20px; font-size:13px; color:#666; display:flex; align-items:center; gap:6px; flex-wrap:wrap }
.header { height:56px; display:flex; align-items:center; justify-content:space-between; padding:0 24px; background:#fff }
.header .title { font-weight:600; color:#1f2330 }
</style>
