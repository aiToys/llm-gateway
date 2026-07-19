<template>
  <div>
    <div class="bar">
      <h3>审计日志</h3>
      <div class="filters">
        <n-input v-model:value="kw" placeholder="搜索 动作/对象" size="small" clearable style="width:200px" />
        <n-button size="small" :loading="loading" @click="load">刷新</n-button>
      </div>
    </div>
    <n-data-table :columns="cols" :data="filtered" :bordered="false" size="small" :loading="loading" :pagination="PAGINATION" />
  </div>
</template>
<script setup>
import { ref, h, computed, onMounted } from 'vue'
import { NDataTable, NButton, NTag, NInput } from 'naive-ui'
import { api } from '../api.js'
import { fmtTime, actionMeta, apiErr, PAGINATION } from '../format.js'

const rows = ref([])
const loading = ref(false)
const kw = ref('')
const filtered = computed(() => {
  if (!kw.value) return rows.value
  const k = kw.value.toLowerCase()
  return rows.value.filter(r => (r.action || '').toLowerCase().includes(k) || (r.target || '').toLowerCase().includes(k))
})
const cols = [
  { title: '时间', key: 'created_at', render: r => fmtTime(r.created_at) },
  { title: '操作人', key: 'actor_id', render: r => r.actor_id || '—' },
  { title: '动作', key: 'action', render: r => h(NTag, { size: 'small', type: actionMeta(r.action), round: true }, () => r.action) },
  { title: '对象', key: 'target', ellipsis: { tooltip: true } },
  { title: 'IP', key: 'ip', render: r => r.ip || '—' },
]
async function load() {
  loading.value = true
  try { const { data } = await api.audit(); rows.value = data.data }
  catch (e) { /* 表格自带空态 */ }
  finally { loading.value = false }
}
onMounted(load)
</script>
<style scoped>
.bar { display:flex; justify-content:space-between; align-items:center; margin-bottom:14px; flex-wrap:wrap; gap:10px } .bar h3 { margin:0 }
.filters { display:flex; gap:8px }
</style>
