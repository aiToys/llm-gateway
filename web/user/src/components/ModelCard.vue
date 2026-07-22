<template>
  <!-- 统一模型卡片:普通模式(网格单元) / feature 模式(跨列渐变首推)。
       整体点击=查看详情;按钮分别触发详情/试用,用 .stop 避免冒泡重复触发。 -->
  <div class="mcard" :class="{ feature: featured }" @click="$emit('detail', model)">
    <span class="feat-badge" v-if="featured">⭐ 推荐模型</span>
    <div class="row1">
      <div class="title">
        <span class="name">
          {{ model.model_name }}
          <!-- hover 卡片时浮现的复制按钮;.stop 阻止冒泡,避免触发卡片整体点击(详情)。 -->
          <button
            class="copy-btn"
            :class="{ copied: justCopied }"
            :title="justCopied ? '已复制' : '复制名称'"
            @click.stop="copyName"
          >
            <svg v-if="!justCopied" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><path fill="currentColor" d="M11 2H4a1 1 0 0 0-1 1v9h2V4h6V2zm3 3H7a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1h7a1 1 0 0 0 1-1V6a1 1 0 0 0-1-1z"/></svg>
            <svg v-else viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><path fill="currentColor" d="M13.7 4.3a1 1 0 0 1 0 1.4l-6 6a1 1 0 0 1-1.4 0l-3-3a1 1 0 1 1 1.4-1.4L7 9.6l5.3-5.3a1 1 0 0 1 1.4 0z"/></svg>
          </button>
        </span>
        <span class="cat">{{ catMeta.icon }} {{ catMeta.label }}</span>
      </div>
      <div class="prov-tags" v-if="(model.providers || []).length">
        <n-tag v-for="p in model.providers" :key="p" size="tiny" round :type="provTagType(p)">{{ provLabel(p) }}</n-tag>
      </div>
    </div>

    <p class="desc">{{ model.description || '通用对话模型' }}</p>

    <div class="caps" v-if="(model.capabilities || []).length">
      <span v-for="c in model.capabilities" :key="c" class="cap" :data-type="capTagType(c)">{{ capIcon(c) }} {{ capLabel(c) }}</span>
    </div>

    <div class="tags" v-if="(model.tags || []).length">
      <span v-for="t in model.tags" :key="t" class="tag">{{ t }}</span>
    </div>

    <div class="foot">
      <div class="meta">
        <span v-if="model.context_length">📋 上下文 {{ formatCtx(model.context_length) }}</span>
        <span v-if="featured" class="hint">双渠道负载均衡 · 毫秒级故障转移</span>
      </div>
      <div class="bar">
        <div class="price">
          <div><small>输入</small>¥{{ formatCents(model.input_price_cents_per_m) }}<small>/M</small></div>
          <div><small>输出</small>¥{{ formatCents(model.output_price_cents_per_m) }}<small>/M</small></div>
        </div>
        <div class="acts">
          <n-button size="small" tertiary @click.stop="$emit('detail', model)">详情</n-button>
          <n-button size="small" type="primary" @click.stop="$emit('try', model)">试用</n-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { NTag, NButton } from 'naive-ui'
import { formatCents, formatCtx } from '../utils.js'
import {
  provLabel, provTagType, capIcon, capLabel, capTagType,
  modelCategory, MODEL_CATEGORIES,
} from '../constants.js'

const props = defineProps({
  model: { type: Object, required: true },
  featured: { type: Boolean, default: false },
})
defineEmits(['try', 'detail'])

const catMeta = computed(() => MODEL_CATEGORIES[modelCategory(props.model)] || { icon: '📝', label: '文本模型' })

// 复制模型名称:.stop 已阻止冒泡(不触发详情);复制成功后图标切绿勾 1.5s 作即时反馈。
const justCopied = ref(false)
async function copyName() {
  try {
    await navigator.clipboard.writeText(props.model.model_name)
    justCopied.value = true
    setTimeout(() => { justCopied.value = false }, 1500)
  } catch (e) { /* clipboard 不可用(非 HTTPS / 无权限)时静默忽略 */ }
}
</script>

<style scoped>
.mcard {
  background:var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 14px;
  padding: 18px;
  display: flex;
  flex-direction: column;
  position: relative;
  transition: border-color .18s, box-shadow .18s, transform .18s;
  cursor: pointer;
}
.mcard:hover {
  border-color: #3D6EFF;
  box-shadow: 0 10px 28px rgba(61, 110, 255, .1);
  transform: translateY(-2px);
}
.mcard.feature {
  background: var(--feature-bg);
  border-color: transparent;
  grid-column: span 2;
  padding: 24px;
}
.feat-badge {
  position: absolute;
  top: 16px; right: 16px;
  background: linear-gradient(135deg, #3D6EFF, #22d3ee);
  color: #fff;
  font-size: 11px;
  font-weight: 600;
  padding: 4px 11px;
  border-radius: 11px;
  box-shadow: 0 4px 10px rgba(61, 110, 255, .25);
}
.row1 { display: flex; justify-content: space-between; align-items: flex-start; gap: 10px }
.title { display: flex; flex-direction: column; gap: 4px }
.name { display: inline-flex; align-items: center; gap: 6px; font-weight: 700; font-size: 16px; color:var(--text-strong); line-height: 1.3 }
/* 复制按钮:默认隐藏,卡片 hover 时浮现;.stop 已防冒泡,此处只管视觉。 */
.copy-btn {
  display: inline-flex; align-items: center; justify-content: center;
  width: 22px; height: 22px; padding: 0; flex-shrink: 0;
  border: none; background: transparent; color: var(--text-muted);
  border-radius: 5px; cursor: pointer;
  opacity: 0; transition: opacity .15s, background .15s, color .15s;
}
.mcard:hover .copy-btn,
.copy-btn:focus-visible { opacity: 1 }
.copy-btn:hover { background: var(--hover-bg); color: var(--text-strong) }
.copy-btn.copied { opacity: 1; color: #18a058 }
/* 触屏设备无 hover,复制按钮常显半透明,保持可达。 */
@media (hover: none) { .copy-btn { opacity: .55 } }
.feature .name { font-size: 22px }
.cat { font-size: 12px; color:var(--text) }
.prov-tags { display: flex; gap: 4px; flex-wrap: wrap; flex-shrink: 0; margin-top: 2px }
.desc { color:var(--text); font-size: 13px; line-height: 1.6; margin: 12px 0 10px; min-height: 38px }
.feature .desc { font-size: 14px; min-height: auto }
.caps { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 10px }
.cap { font-size: 12px; padding: 3px 9px; border-radius: 8px; background:var(--hover-bg); color:var(--text); border: 1px solid var(--border) }
.cap[data-type="success"] { background: #e8f8ee; color: #18a058; border-color: #d6f0e0 }
.cap[data-type="warning"] { background: #fff7e6; color: #d99000; border-color: #ffe8c2 }
.cap[data-type="info"]    { background: #e8f0ff; color: #3D6EFF; border-color: #d4e3ff }
.cap[data-type="error"]   { background: #fde8ea; color: #d03050; border-color: #fbd5d9 }
.tags { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 10px }
.tag { background: #f0f4ff; color: #3D6EFF; font-size: 11px; padding: 2px 8px; border-radius: 10px }
.foot { margin-top: auto; display: flex; flex-direction: column; gap: 12px }
.meta { font-size: 12px; color:var(--text-muted); display: flex; gap: 14px; flex-wrap: wrap }
.feature .meta .hint { color: #3D6EFF; font-weight: 500 }
.bar { display: flex; justify-content: space-between; align-items: flex-end; gap: 12px }
.price { display: flex; gap: 18px; font-size: 15px; font-weight: 700; color:var(--text-strong); line-height: 1.4 }
.feature .price { font-size: 16px }
.price small { color:var(--text-muted); font-weight: 400; font-size: 11px; margin: 0 2px }
.acts { display: flex; gap: 8px; flex-shrink: 0 }
@media (max-width: 640px) {
  .mcard.feature { grid-column: span 1 }
  .bar { flex-direction: column; align-items: stretch }
  .acts { justify-content: flex-end }
}
</style>
