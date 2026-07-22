<template>
  <div class="home">
    <section class="hero">
      <div class="badge"><span class="pulse"></span>多渠道热备 · 实时计费 · 开源可自部署</div>
      <h1>一个大模型网关,<br/>接入<span>全部</span>主流供应商</h1>
      <p>统一 OpenAI / Anthropic 兼容接口,一键打通阿里云百炼、火山方舟、百度千帆、DeepSeek、智谱等主流供应商。<br/>预付计费、用量看板、多租户管理,开箱即用。</p>
      <div class="cta">
        <n-button type="primary" size="large" @click="$router.push('/models')">浏览模型</n-button>
        <n-button size="large" @click="$router.push('/console/chat')" v-if="loggedIn">进入控制台</n-button>
        <n-button size="large" @click="$router.push('/login')" v-else>免费开始</n-button>
      </div>
      <div class="providers">
        <span>阿里云百炼</span><span>火山方舟</span><span>百度千帆</span><span>DeepSeek</span><span>智谱 GLM</span>
      </div>
    </section>

    <!-- 统计指标条: 用真实数据(模型数/供应商数)证明平台成熟度 -->
    <section class="stats" v-if="modelCount > 0">
      <div class="stat">
        <div class="num">{{ modelCount }}<small>+</small></div>
        <div class="lab">可用模型</div>
        <div class="sub">文本 / 多模态 / 嵌入</div>
      </div>
      <div class="stat">
        <div class="num">{{ providerCount }}</div>
        <div class="lab">接入供应商</div>
        <div class="sub">统一协议接入</div>
      </div>
      <div class="stat">
        <div class="num">2<small>套</small></div>
        <div class="lab">兼容协议</div>
        <div class="sub">OpenAI · Anthropic</div>
      </div>
      <div class="stat">
        <div class="num">99.9<small>%</small></div>
        <div class="lab">服务可用性</div>
        <div class="sub">多渠道自动故障转移</div>
      </div>
    </section>

    <section class="feats">
      <div class="feat"><div class="ic">🔌</div><h3>双协议兼容</h3><p>同时提供 OpenAI 与 Anthropic 接口,现有客户端零改动接入。</p></div>
      <div class="feat"><div class="ic">🖼️</div><h3>多模态</h3><p>图像、音频、文件统一 content-parts 规范,文件托管一站搞定。</p></div>
      <div class="feat"><div class="ic">💰</div><h3>实时计费</h3><p>预付余额按 token 精确扣费,售价/成本/毛利全链路可审计。</p></div>
      <div class="feat"><div class="ic">🏢</div><h3>多租户</h3><p>租户隔离、BYOK 自带密钥、独立定价与配额。</p></div>
    </section>

    <!-- API 代码示例: 三种语言切换,展示"开箱即用" -->
    <section class="code-sec">
      <div class="sec-head">
        <div>
          <h2>三行代码,即刻接入</h2>
          <p class="sec-desc">复用现有 OpenAI SDK,只改 baseURL 与 API Key,零学习成本。</p>
        </div>
      </div>
      <div class="code-wrap">
        <div class="code-tabs">
          <span v-for="t in tabs" :key="t.id" class="ct" :class="{ on: lang === t.id }" @click="lang = t.id">{{ t.label }}</span>
        </div>
        <pre class="code"><code>{{ snippet }}</code></pre>
      </div>
    </section>

    <section class="featured">
      <div class="sec-head">
        <h2>热门模型</h2>
        <n-button text @click="$router.push('/models')">查看全部 →</n-button>
      </div>
      <!-- 复用模型广场的统一卡片组件,保持全站卡片风格一致 -->
      <div class="cards">
        <ModelCard v-for="m in featured" :key="m.model_name" :model="m" @try="goTry" @detail="goTry" />
      </div>
    </section>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { NButton } from 'naive-ui'
import { api } from '../api.js'
import { token } from '../store.js'
import ModelCard from '../components/ModelCard.vue'

const router = useRouter()
const models = ref([])
const loggedIn = computed(() => !!token.get())
const featured = computed(() => models.value.slice(0, 4))
// 统计指标: 模型数与供应商数由真实数据计算,避免静态造假。
const modelCount = computed(() => models.value.length)
// 接入供应商数: 排除 mock(开发兜底,非真实上游),避免对用户呈现虚高数字。
const providerCount = computed(() => new Set(models.value.flatMap(m => m.providers || []).filter(p => p && p !== 'mock')).size)
const sampleModel = computed(() => models.value[0]?.model_name || 'qwen-max')
// 跳转对话:已登录带入选中的模型,未登录去登录页。
function goTry(m) {
  router.push(loggedIn.value ? { name: 'chat', query: { model: m.model_name } } : '/login')
}
onMounted(async () => {
  // 公开模型加载失败不阻断首屏(统计区有 v-if 兜底),仅静默降级。
  try { const { data } = await api.publicModels(); models.value = data.data || [] }
  catch { models.value = [] }
})

// --- API 代码示例(三语言) ---
const tabs = [
  { id: 'curl', label: 'cURL' },
  { id: 'python', label: 'Python' },
  { id: 'node', label: 'Node.js' },
]
const lang = ref('curl')
const base = computed(() => (typeof location !== 'undefined' ? location.origin : 'https://your-gateway.example'))
const snippet = computed(() => {
  const m = sampleModel.value
  const u = base.value
  switch (lang.value) {
    case 'python':
      return `from openai import OpenAI

client = OpenAI(
    base_url="${u}/v1",
    api_key="sk-your-key",
)

resp = client.chat.completions.create(
    model="${m}",
    messages=[{"role": "user", "content": "你好"}],
)
print(resp.choices[0].message.content)`
    case 'node':
      return `import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "${u}/v1",
  apiKey: "sk-your-key",
});

const resp = await client.chat.completions.create({
  model: "${m}",
  messages: [{ role: "user", content: "你好" }],
});
console.log(resp.choices[0].message.content);`
    default:
      return `curl ${u}/v1/chat/completions \\
  -H "Authorization: Bearer sk-your-key" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${m}",
    "messages": [{"role": "user", "content": "你好"}]
  }'`
  }
})
</script>

<style scoped>
.home { max-width:1120px; margin:0 auto; padding:0 24px }
.hero { text-align:center; padding:64px 0 40px }
.badge { display:inline-flex; align-items:center; gap:7px; background:#eef4ff; border:1px solid #dbe7ff; color:#2563eb; font-size:12.5px; font-weight:600; padding:5px 13px; border-radius:20px; margin-bottom:20px }
.badge .pulse { width:7px; height:7px; border-radius:50%; background:#22c55e; box-shadow:0 0 0 0 rgba(34,197,94,.5); animation:pulse 2s infinite }
@keyframes pulse { 0%{box-shadow:0 0 0 0 rgba(34,197,94,.45)} 70%{box-shadow:0 0 0 8px rgba(34,197,94,0)} 100%{box-shadow:0 0 0 0 rgba(34,197,94,0)} }
.hero h1 { font-size:44px; line-height:1.25; margin:0 0 18px; color:var(--text-strong); letter-spacing:-.5px }
.hero h1 span { background:linear-gradient(135deg,#3D6EFF,#22d3ee); -webkit-background-clip:text; background-clip:text; color:transparent }
.hero p { color:var(--text); font-size:16px; line-height:1.8; max-width:620px; margin:0 auto 28px }
.cta { display:flex; gap:12px; justify-content:center; margin-bottom:36px }
.providers { display:flex; gap:14px; justify-content:center; flex-wrap:wrap }
.providers span { background:var(--bg-card); border:1px solid var(--border); padding:6px 14px; border-radius:20px; font-size:13px; color:var(--text) }

/* 统计指标条 */
.stats { display:grid; grid-template-columns:repeat(4,1fr); gap:16px; padding:8px 0 48px }
.stat { background:var(--bg-card); border:1px solid var(--border); border-radius:14px; padding:22px 18px; text-align:center; transition:.2s }
.stat:hover { border-color:#dbe7ff; box-shadow:0 6px 20px -8px rgba(61,110,255,.25); transform:translateY(-2px) }
.stat .num { font-size:32px; font-weight:700; color:var(--text-strong); line-height:1 }
.stat .num small { font-size:16px; font-weight:600; color:#3D6EFF; margin-left:1px }
.stat .lab { font-size:14px; font-weight:600; color:var(--text-strong); margin-top:8px }
.stat .sub { font-size:12px; color:var(--text-muted); margin-top:3px }

.feats { display:grid; grid-template-columns:repeat(4,1fr); gap:18px; padding:8px 0 56px }
.feat { background:var(--bg-card); border-radius:14px; padding:22px; text-align:center }
.feat .ic { font-size:28px; margin-bottom:8px }
.feat h3 { margin:0 0 6px; font-size:15px }
.feat p { margin:0; font-size:13px; color:var(--text); line-height:1.6 }

/* 代码示例区 */
.code-sec { padding:8px 0 56px }
.sec-head { display:flex; align-items:center; justify-content:space-between; margin-bottom:18px }
.sec-head h2 { margin:0; font-size:22px; letter-spacing:-.2px }
.sec-desc { margin:6px 0 0; color:var(--text); font-size:14px }
.code-wrap { background:#0f172a; border-radius:14px; overflow:hidden; border:1px solid #1e293b }
.code-tabs { display:flex; gap:4px; padding:10px 12px 0; background:#0b1222; border-bottom:1px solid #1e293b }
.code-tabs .ct { padding:7px 14px; font-size:13px; color:#94a3b8; cursor:pointer; border-radius:7px 7px 0 0; transition:.15s }
.code-tabs .ct:hover { color:#e2e8f0 }
.code-tabs .ct.on { background:#0f172a; color:#3D6EFF; font-weight:600 }
.code { margin:0; padding:22px 24px; color:#e2e8f0; font-size:13px; line-height:1.75; font-family:'SF Mono',Menlo,Monaco,Consolas,monospace; overflow-x:auto; white-space:pre }
.code code { font-family:inherit }

.featured { padding:8px 0 64px }
.cards { display:grid; grid-template-columns:repeat(2,1fr); gap:16px; align-items:start }
@media (max-width:640px) { .cards { grid-template-columns:1fr } }
@media (max-width:860px) {
  .feats { grid-template-columns: repeat(2,1fr) }
  .stats { grid-template-columns: repeat(2,1fr) }
  .hero { padding: 44px 0 32px }
  .hero h1 { font-size: 32px }
}
@media (max-width:480px) {
  .feats { grid-template-columns: 1fr }
  .home { padding: 0 16px }
  .hero h1 { font-size: 26px }
}
</style>
