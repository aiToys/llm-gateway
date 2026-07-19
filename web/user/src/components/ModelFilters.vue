<template>
  <!-- 左侧筛选面板:模型类型 / 供应商 / 能力 三组。
       各项带实时计数,让用户对筛选结果有预期;状态变化通过 update 事件上抛,父组件负责过滤。 -->
  <aside class="filters">
    <div class="search">
      <n-input v-model:value="kw" placeholder="搜索模型 / 标签" size="medium" clearable>
        <template #prefix><span class="s-ic">🔍</span></template>
      </n-input>
    </div>

    <div class="group">
      <div class="ghead">模型类型</div>
      <div class="opt" :class="{ on: !category }" @click="setCategory(null)">
        <span>🗂️ 全部模型</span><b>{{ models.length }}</b>
      </div>
      <div v-for="cat in CATEGORY_OPTIONS" :key="cat.value" class="opt" :class="{ on: category === cat.value, dim: !countByCat[cat.value] }" @click="setCategory(cat.value)">
        <span>{{ cat.label }}</span><b>{{ countByCat[cat.value] || 0 }}</b>
      </div>
    </div>

    <div class="group" v-if="provList.length">
      <div class="ghead">供应商</div>
      <div class="opt" :class="{ on: !prov }" @click="setProv(null)">
        <span>全部</span><b>{{ models.length }}</b>
      </div>
      <div v-for="p in provList" :key="p.value" class="opt" :class="{ on: prov === p.value }" @click="setProv(p.value)">
        <span><n-tag size="tiny" round :type="provTagType(p.value)">{{ p.label }}</n-tag></span>
        <b>{{ p.count }}</b>
      </div>
    </div>

    <div class="group">
      <div class="ghead">能力</div>
      <p class="gdesc" v-if="!caps.length">选择模型须同时具备的能力</p>
      <div class="caps">
        <span v-for="c in CAPABILITY_OPTIONS" :key="c.value" class="capopt" :class="{ on: caps.includes(c.value) }" @click="toggleCap(c.value)">{{ c.label }}</span>
      </div>
    </div>

    <div class="reset" v-if="hasFilter">
      <n-button size="small" quaternary block @click="reset">✕ 清除筛选</n-button>
    </div>
  </aside>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { NInput, NTag, NButton } from 'naive-ui'
import {
  CATEGORY_OPTIONS, CAPABILITY_OPTIONS, provTagType, provLabel, modelCategory,
} from '../constants.js'

const props = defineProps({ models: { type: Array, default: () => [] } })
const emit = defineEmits(['update'])

const kw = ref('')
const category = ref(null)
const prov = ref(null)
const caps = ref([])

// 供应商按 models 实际出现的 provider 聚合(带计数),避免列出无模型的供应商。
const provList = computed(() => {
  const map = new Map()
  props.models.forEach(m => (m.providers || []).forEach(p => map.set(p, (map.get(p) || 0) + 1)))
  return [...map.entries()].map(([value, count]) => ({ value, label: provLabel(value), count }))
})
// 各分类模型数,用于显示计数;无模型的分类置灰(dim)。
const countByCat = computed(() => {
  const map = {}
  props.models.forEach(m => { const c = modelCategory(m); map[c] = (map[c] || 0) + 1 })
  return map
})
const hasFilter = computed(() => !!(kw.value || category.value || prov.value || caps.value.length))

function setCategory(v) { category.value = v }
function setProv(v) { prov.value = v }
function toggleCap(c) {
  const i = caps.value.indexOf(c)
  if (i >= 0) caps.value.splice(i, 1)
  else caps.value.push(c)
}
function reset() { kw.value = ''; category.value = null; prov.value = null; caps.value = [] }

// 筛选状态聚合为单一对象向上抛出;父组件据此 computed 过滤。
const state = computed(() => ({ kw: kw.value, category: category.value, prov: prov.value, caps: [...caps.value] }))
watch(state, (s) => emit('update', s), { deep: true })
</script>

<style scoped>
.filters { width: 220px; flex-shrink: 0; position: sticky; top: 76px; align-self: flex-start; max-height: calc(100vh - 96px); overflow-y: auto }
.search { margin-bottom: 18px }
.s-ic { opacity: .5 }
.group { margin-bottom: 20px; padding-bottom: 18px; border-bottom: 1px solid #eef0f5 }
.group:last-of-type { border-bottom: none }
.ghead { font-size: 13px; font-weight: 600; color: #1f2330; margin-bottom: 10px }
.gdesc { font-size: 11px; color: #9aa1ad; margin: 0 0 8px }
.opt { display: flex; justify-content: space-between; align-items: center; padding: 7px 10px; border-radius: 8px; font-size: 13px; color: #5b6270; cursor: pointer; transition: background .12s }
.opt:hover { background: #f3f5fa }
.opt.on { background: #eef4ff; color: #3D6EFF; font-weight: 600 }
.opt.dim { opacity: .45 }
.opt b { font-size: 12px; color: #9aa1ad; font-weight: 500 }
.opt.on b { color: #3D6EFF }
.caps { display: flex; gap: 6px; flex-wrap: wrap }
.capopt { font-size: 12px; padding: 4px 10px; border-radius: 8px; background: #f3f5fa; color: #5b6270; cursor: pointer; border: 1px solid transparent; transition: .12s }
.capopt:hover { border-color: #d4e3ff }
.capopt.on { background: #eef4ff; color: #3D6EFF; border-color: #3D6EFF; font-weight: 600 }
.reset { padding-top: 4px }
@media (max-width: 860px) {
  .filters { width: 100%; position: static; max-height: none; display: flex; flex-wrap: wrap; gap: 14px }
  .group { flex: 1 1 200px; border-bottom: none; padding-bottom: 0; margin-bottom: 0 }
}
</style>
