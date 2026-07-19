<template>
  <div>
    <div class="bar"><h3>仪表盘</h3><n-button size="small" :loading="loading" @click="load">刷新</n-button></div>
    <div class="cards">
      <div class="card"><div class="k">总请求数</div><div class="v">{{ loading ? '—' : num(s.total_requests) }}</div></div>
      <div class="card"><div class="k">总收入</div><div class="v">{{ loading ? '—' : yuan(s.total_revenue) }}</div></div>
      <div class="card"><div class="k">总成本</div><div class="v">{{ loading ? '—' : yuan(s.total_cost) }}</div></div>
      <div class="card green"><div class="k">毛利</div><div class="v">{{ loading ? '—' : yuan(s.total_revenue - s.total_cost) }}</div></div>
      <div class="card"><div class="k">活跃租户</div><div class="v">{{ loading ? '—' : num(s.active_tenants) }}</div></div>
      <div class="card"><div class="k">活跃用户</div><div class="v">{{ loading ? '—' : num(s.active_users) }}</div></div>
    </div>
  </div>
</template>
<script setup>
import { ref, onMounted } from 'vue'
import { NButton, useMessage } from 'naive-ui'
import { api } from '../api.js'
import { yuan, num, apiErr } from '../format.js'

const message = useMessage()
const s = ref({ total_requests: 0, total_revenue: 0, total_cost: 0, active_tenants: 0, active_users: 0 })
const loading = ref(false)
async function load() {
  loading.value = true
  try { const { data } = await api.stats(); s.value = data }
  catch (e) { message.error(apiErr(e, '加载失败')) }
  finally { loading.value = false }
}
onMounted(load)
</script>
<style scoped>
.bar { display:flex; justify-content:space-between; align-items:center; margin-bottom:14px } .bar h3 { margin:0 }
.cards { display:grid; grid-template-columns:repeat(3,1fr); gap:16px }
.card { background:#fff; border-radius:12px; padding:20px; box-shadow:0 1px 3px rgba(0,0,0,.04) }
.card .k { color:#9097a3; font-size:13px }
.card .v { font-size:26px; font-weight:700; margin-top:6px; color:#1f2330 }
.card.green .v { color:#0F766E }
@media (max-width:768px) { .cards { grid-template-columns:repeat(2,1fr) } }
@media (max-width:480px) { .cards { grid-template-columns:1fr } }
</style>
