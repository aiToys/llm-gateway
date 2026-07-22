<script setup>
import { useRouter } from 'vue-router'
import { NButton } from 'naive-ui'
const router = useRouter()
</script>
<template>
  <div class="nf">
    <div class="code">404</div>
    <p class="msg">页面不存在或已被移除</p>
    <n-button type="primary" @click="router.push('/')">返回首页</n-button>
  </div>
</template>
<style scoped>
.nf { min-height: 60vh; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 14px; }
.code { font-size: 72px; font-weight: 800; color: #134e4a; line-height: 1; }
.msg { color:var(--text); margin: 0; }
</style>
