<template>
  <div class="mp">
    <!-- 标题区:左侧说明 + 右侧精选/全部标签 -->
    <div class="head">
      <div class="head-l">
        <h2>模型广场</h2>
        <p>浏览全部可用模型。点击「试用」直接发起对话,或「详情」查看规格与定价。</p>
      </div>
      <div class="tabs">
        <span class="tab" :class="{ on: tab === 'all' }" @click="tab = 'all'">全部模型 <b>{{ models.length }}</b></span>
        <span class="tab" :class="{ on: tab === 'featured' }" @click="tab = 'featured'">⭐ 精选 <b>{{ featuredCount }}</b></span>
      </div>
    </div>

    <!-- 服务条款提示条 -->
    <div class="notice">
      <span class="nb">i</span>
      模型由阿里云百炼、火山方舟、百度千帆等第三方供应商提供,调用即视为接受相应服务条款。价格为平台零售价(元 / 百万 token)。
    </div>

    <div class="body">
      <!-- 左侧筛选面板:状态上抛到 filter -->
      <ModelFilters :models="models" @update="(s) => (filter = s)" />

      <div class="main">
        <div class="result-bar" v-if="visible.length">
          共 <b>{{ visible.length }}</b> 个模型<span v-if="filter.category || filter.prov || filter.caps.length || filter.kw"> · 已筛选</span>
        </div>

        <div class="grid" v-if="visible.length">
          <ModelCard
            v-for="(m, i) in visible" :key="m.model_name"
            :model="m" :featured="i === 0 && showFeature"
            @try="tryModel" @detail="openDetail"
          />
        </div>

        <div class="grid" v-else-if="loading">
          <div class="skel" v-for="i in 6" :key="i"></div>
        </div>

        <n-empty v-else-if="loadError" description="加载失败">
          <template #extra><n-button size="small" @click="load">重试</n-button></template>
        </n-empty>
        <n-empty v-else description="没有匹配的模型,试试调整筛选条件" style="margin: 60px 0" />
      </div>
    </div>

    <!-- 详情:规格 + 定价 -->
    <n-modal v-model:show="showDetail" preset="card" :title="cur?.model_name" style="max-width: 560px">
      <template v-if="cur">
        <p class="d-desc">{{ cur.long_desc || cur.description || '通用对话模型' }}</p>
        <n-descriptions :column="2" bordered size="small">
          <n-descriptions-item label="供应商">
            <n-tag v-for="p in (cur.providers || [])" :key="p" size="tiny" :type="provTagType(p)" style="margin-right: 4px">{{ provLabel(p) }}</n-tag>
            <span v-if="!(cur.providers || []).length">—</span>
          </n-descriptions-item>
          <n-descriptions-item label="能力">
            <n-tag v-for="c in (cur.capabilities || [])" :key="c" size="tiny" :type="capTagType(c)" style="margin-right: 4px">{{ capIcon(c) }} {{ capLabel(c) }}</n-tag>
            <span v-if="!(cur.capabilities || []).length">—</span>
          </n-descriptions-item>
          <n-descriptions-item label="模型类型">{{ catMeta(cur).icon }} {{ catMeta(cur).label }}</n-descriptions-item>
          <n-descriptions-item label="上下文长度">{{ formatCtx(cur.context_length) }}</n-descriptions-item>
          <n-descriptions-item label="输入价">¥{{ formatCents(cur.input_price_cents_per_m) }} / 百万 token</n-descriptions-item>
          <n-descriptions-item label="输出价">¥{{ formatCents(cur.output_price_cents_per_m) }} / 百万 token</n-descriptions-item>
          <n-descriptions-item label="标签" :span="2">{{ (cur.tags || []).join('、') || '—' }}</n-descriptions-item>
        </n-descriptions>
        <div class="d-act">
          <n-button type="primary" @click="tryModel(cur)">立即试用</n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { NEmpty, NButton, NModal, NDescriptions, NDescriptionsItem, NTag } from 'naive-ui'
import { api } from '../api.js'
import { token } from '../store.js'
import { formatCents, formatCtx } from '../utils.js'
import {
  provLabel, provTagType, capIcon, capLabel, capTagType,
  modelCategory, MODEL_CATEGORIES,
} from '../constants.js'
import ModelCard from '../components/ModelCard.vue'
import ModelFilters from '../components/ModelFilters.vue'

const router = useRouter()
const models = ref([])
const loading = ref(false)
const loadError = ref(false)
// 筛选状态由子组件(ModelFilters)上抛。
const filter = ref({ kw: '', category: null, prov: null, caps: [] })
const tab = ref('all')
const showDetail = ref(false)
const cur = ref(null)

// 精选判定:能力丰富或多供应商(说明是重点维护的主力模型)。
const isFeatured = (m) => (m.capabilities || []).length >= 2 || (m.providers || []).length >= 2
const featuredCount = computed(() => models.value.filter(isFeatured).length)
function catMeta(m) { return MODEL_CATEGORIES[modelCategory(m)] || { icon: '📝', label: '文本模型' } }

// 应用筛选:模型类型 / 供应商 / 能力(须全具备) / 关键词。
const filtered = computed(() => models.value.filter(m => {
  if (filter.value.category && modelCategory(m) !== filter.value.category) return false
  if (filter.value.prov && !(m.providers || []).includes(filter.value.prov)) return false
  if (filter.value.caps.length && !filter.value.caps.every(c => (m.capabilities || []).includes(c))) return false
  if (filter.value.kw) {
    const k = filter.value.kw.toLowerCase()
    const blob = (m.model_name + ' ' + (m.tags || []).join(' ') + ' ' + (m.description || '')).toLowerCase()
    if (!blob.includes(k)) return false
  }
  return true
}))
// 精选 tab 只看精选模型。
const visible = computed(() => (tab.value === 'featured' ? filtered.value.filter(isFeatured) : filtered.value))
// 仅在无任何筛选时,首张卡片用 feature 样式跨列首推;有筛选时退回普通卡片,避免占用网格空间。
const showFeature = computed(() => !filter.value.category && !filter.value.prov && !filter.value.caps.length && !filter.value.kw)

function tryModel(m) {
  cur.value = m
  if (token.get()) router.push({ name: 'chat', query: { model: m.model_name } })
  else showDetail.value = true
}
function openDetail(m) { cur.value = m; showDetail.value = true }

async function load() {
  loading.value = true; loadError.value = false
  try { const { data } = await api.publicModels(); models.value = data.data || [] }
  catch (e) { loadError.value = true }
  finally { loading.value = false }
}
onMounted(load)
</script>

<style scoped>
.mp { max-width: 1240px; margin: 0 auto; padding: 28px 24px 64px }
.head { display: flex; justify-content: space-between; align-items: flex-end; gap: 16px; margin-bottom: 18px; flex-wrap: wrap }
.head h2 { margin: 0 0 6px; font-size: 28px; color: #1f2330 }
.head p { color: #6b7280; margin: 0; font-size: 14px }
.tabs { display: flex; gap: 8px; background: #fff; border: 1px solid #eef0f5; border-radius: 10px; padding: 4px }
.tab { padding: 7px 14px; border-radius: 7px; font-size: 13px; color: #5b6270; cursor: pointer; transition: .15s; white-space: nowrap }
.tab:hover { color: #3D6EFF }
.tab.on { background: #3D6EFF; color: #fff; font-weight: 600 }
.tab b { font-weight: 700; margin-left: 2px }
.tab.on b { color: #fff }
.notice { display: flex; align-items: center; gap: 10px; background: #fffbeb; border: 1px solid #fef0c7; color: #92700a; font-size: 13px; padding: 11px 16px; border-radius: 10px; margin-bottom: 22px; line-height: 1.5 }
.notice .nb { width: 18px; height: 18px; border-radius: 50%; background: #f5c518; color: #fff; display: inline-flex; align-items: center; justify-content: center; font-size: 12px; font-weight: 700; font-style: italic; flex-shrink: 0 }
.body { display: flex; gap: 28px; align-items: flex-start }
.main { flex: 1; min-width: 0 }
.result-bar { font-size: 13px; color: #6b7280; margin-bottom: 14px }
.result-bar b { color: #1f2330 }
.grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; align-items: start }
.skel { min-height: 200px; background: linear-gradient(90deg, #f3f4f7 25%, #e9ebf2 37%, #f3f4f7 63%); background-size: 400% 100%; animation: sk 1.4s ease infinite; border-radius: 14px }
@keyframes sk { 0% { background-position: 100% 50% } 100% { background-position: 0 50% } }
.d-desc { color: #6b7280; font-size: 14px; line-height: 1.7; margin: 0 0 16px }
.d-act { display: flex; justify-content: flex-end; margin-top: 16px }
@media (max-width: 1024px) { .grid { grid-template-columns: repeat(2, 1fr) } }
@media (max-width: 860px) {
  /* 窄屏:筛选面板堆到顶部横向展开,主区域独占整行,避免横向溢出 */
  .body { flex-direction: column; gap: 18px; align-items: stretch }
  .mp { padding: 20px 16px 48px }
  .head { align-items: flex-start }
}
@media (max-width: 640px) { .grid { grid-template-columns: 1fr } }
</style>
