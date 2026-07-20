<template>
  <div class="pub">
    <header class="nav">
      <div class="brand" @click="$router.push('/')"><img src="/logo.svg" class="logo-img" alt="logo" />LLM Gateway</div>
      <nav class="links">
        <router-link to="/">首页</router-link>
        <router-link to="/models">模型广场</router-link>
        <router-link to="/pricing">定价</router-link>
        <a href="https://docs.cncf.vip/llm-gateway/" target="_blank" rel="noopener" class="ext">文档</a>
      </nav>
      <div class="right">
        <n-button quaternary size="small" :title="isDark ? '切换到浅色' : '切换到深色'" @click="toggleTheme">{{ isDark ? '☀️' : '🌙' }}</n-button>
        <a class="gh" href="https://github.com/aitoys/llm-gateway" target="_blank" rel="noopener" title="GitHub">
          <svg viewBox="0 0 16 16" width="18" height="18" aria-hidden="true"><path fill="currentColor" d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0016 8c0-4.42-3.58-8-8-8z"/></svg>
        </a>
        <template v-if="loggedIn">
          <n-button size="small" @click="$router.push('/console/chat')">控制台</n-button>
        </template>
        <template v-else>
          <n-button size="small" quaternary @click="$router.push('/login')">登录</n-button>
          <n-button size="small" type="primary" @click="$router.push('/login')">免费开始</n-button>
        </template>
      </div>
    </header>
    <main><router-view /></main>
    <footer class="foot">
      <div class="fcols">
        <div class="fcol fbrand">
          <div class="brand sm"><img src="/logo.svg" class="logo-img" alt="logo" />LLM Gateway</div>
          <p>统一接入主流大模型供应商的开源网关,双协议兼容、实时计费、多租户。</p>
        </div>
        <div class="fcol">
          <h4>产品</h4>
          <router-link to="/models">模型广场</router-link>
          <router-link to="/pricing">定价</router-link>
          <router-link to="/console/chat">控制台</router-link>
        </div>
        <div class="fcol">
          <h4>资源</h4>
          <a href="https://github.com/aitoys/llm-gateway" target="_blank" rel="noopener">GitHub</a>
          <a href="https://github.com/aitoys/llm-gateway#readme" target="_blank" rel="noopener">快速开始</a>
          <a href="https://github.com/aitoys/llm-gateway/issues" target="_blank" rel="noopener">问题反馈</a>
        </div>
        <div class="fcol">
          <h4>供应商</h4>
          <span>阿里云百炼</span>
          <span>火山方舟</span>
          <span>百度千帆</span>
        </div>
      </div>
      <div class="fbot">
        <span>© 2026 LLM Gateway · MIT License</span>
        <span class="status"><i class="ok"></i> 全部服务正常</span>
      </div>
    </footer>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { NButton } from 'naive-ui'
import { token, theme } from '../store.js'
const loggedIn = computed(() => !!token.get())
const isDark = computed(() => theme.ref.value === 'dark')
function toggleTheme() { theme.toggle() }
</script>

<style scoped>
.pub { min-height:100vh; display:flex; flex-direction:column; background:#f7f8fc }
.nav { height:60px; display:flex; align-items:center; padding:0 32px; gap:32px; background:#fff; border-bottom:1px solid #eef0f5; position:sticky; top:0; z-index:10 }
.brand { font-weight:700; font-size:17px; display:flex; align-items:center; gap:8px; cursor:pointer }
.brand .logo-img { width:22px; height:22px }
.brand.sm .logo-img { width:18px; height:18px }
.links { display:flex; gap:22px; flex:1; align-items:center }
.links a { color:#5b6270; text-decoration:none; font-size:14px; font-weight:500 }
.links a:hover { color:#3D6EFF }
.links a.router-link-exact-active { color:#3D6EFF }
.links a.ext { display:inline-flex; align-items:center }
.right { display:flex; gap:8px; align-items:center }
.gh { display:inline-flex; align-items:center; justify-content:center; color:#5b6270; padding:6px; border-radius:8px; transition:.15s }
.gh:hover { color:#1f2330; background:#f3f4f7 }
main { flex:1 }

/* footer 分栏 */
.foot { background:#0f172a; color:#94a3b8; padding:44px 32px 24px; margin-top:auto }
.fcols { display:grid; grid-template-columns:2fr 1fr 1fr 1fr; gap:36px; max-width:1120px; margin:0 auto }
.fbrand p { font-size:13px; line-height:1.7; margin:12px 0 0; max-width:280px }
.fcol h4 { color:#fff; font-size:13px; margin:0 0 14px; font-weight:600 }
.fcol a, .fcol span { display:block; color:#94a3b8; text-decoration:none; font-size:13px; line-height:2 }
.fcol a:hover { color:#fff }
.brand.sm { font-size:15px; color:#fff }
.fbot { display:flex; align-items:center; justify-content:space-between; max-width:1120px; margin:32px auto 0; padding-top:18px; border-top:1px solid #1e293b; font-size:12.5px }
.status { display:inline-flex; align-items:center; gap:7px }
.status .ok { width:7px; height:7px; border-radius:50%; background:#22c55e }

@media (max-width: 768px) {
  .fcols { grid-template-columns:1fr 1fr; gap:24px }
  .fbrand { grid-column:1 / -1 }
}
@media (max-width: 600px) {
  .nav { padding: 0 14px; gap: 10px; height: 54px }
  .brand { font-size: 15px; gap: 6px }
  .brand .logo-img { width: 18px; height: 18px }
  .links { gap: 14px }
  .links a { font-size: 13px }
  .links a.ext { display:none }
  .foot { padding: 32px 16px 20px }
  .fcols { grid-template-columns:1fr 1fr; gap:20px }
  .fbot { flex-direction:column; gap:8px; text-align:center }
}
</style>
