<template>
  <div class="chat-wrap">
    <div class="messages" ref="msgs" role="log" aria-live="polite">
      <div v-if="!messages.length" class="empty">
        <div class="big">🤖</div>
        <p>选择一个模型,开始对话</p>
        <div class="examples">
          <button v-for="ex in EXAMPLES" :key="ex" class="ex" @click="draft = ex">{{ ex }}</button>
        </div>
      </div>
      <div v-for="(m, i) in messages" :key="i" :class="['msg', m.role, { err: m.isError }]">
        <div class="avatar">{{ m.role === 'user' ? '我' : 'AI' }}</div>
        <div class="bubble-wrap">
          <div class="bubble">
            <pre v-if="m.content">{{ m.content }}</pre><span v-if="m.typing" class="cursor">▍</span>
          </div>
          <div class="msg-actions" v-if="!m.typing && m.content && !m.isError">
            <button class="act" title="复制" @click="copyMsg(m)">复制</button>
            <button v-if="m.role === 'assistant'" class="act" title="基于上一条问题重新生成" @click="regen(i)">重生成</button>
            <button class="act" title="删除该条" @click="delMsg(i)">删除</button>
          </div>
          <div class="msg-actions" v-if="m.isError">
            <button class="act" title="重试" @click="retryLast">重试</button>
          </div>
        </div>
      </div>
    </div>
    <div class="composer">
      <div class="bar">
        <n-select v-model:value="model" :options="modelOptions" size="small" style="width:220px" placeholder="选择模型" />
        <n-input-number v-model:value="maxTokens" :min="64" :max="8192" size="small" style="width:150px">
          <template #prefix>max_tokens</template>
        </n-input-number>
        <n-input-number v-model:value="temperature" :min="0" :max="2" :step="0.1" size="small" style="width:130px">
          <template #prefix>temp</template>
        </n-input-number>
        <n-popconfirm @positive-click="clearAll">
          <template #trigger>
            <n-button size="small" quaternary>清空</n-button>
          </template>
          将清空当前模型的全部对话,确定?
        </n-popconfirm>
      </div>
      <div class="input-row">
        <n-input v-model:value="draft" type="textarea" :autosize="{ minRows: 1, maxRows: 6 }"
          placeholder="输入消息,Enter 发送,Shift+Enter 换行" @keydown="onKey" />
        <n-button :loading="uploading" @click="pickFile" title="上传图片/文件" aria-label="上传附件">📎</n-button>
        <input ref="fileInput" type="file" style="display:none" @change="onFile" />
        <n-button v-if="streaming" type="error" @click="stop">停止</n-button>
        <n-button v-else type="primary" :disabled="!draft.trim() && !pendingImgs.length" @click="send">发送</n-button>
      </div>
      <div class="attaches" v-if="pendingImgs.length">
        <div class="att" v-for="(img, i) in pendingImgs" :key="i">
          <img v-if="img.isImg" :src="img.url" :alt="img.filename" />
          <span v-else>📄 {{ img.filename }}</span>
          <button class="rm" aria-label="移除附件" @click="pendingImgs.splice(i, 1)">×</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, nextTick, onMounted, watch } from 'vue'
import { NSelect, NInput, NInputNumber, NButton, NPopconfirm, useMessage } from 'naive-ui'
import { api, apiErr } from '../api.js'
import { token } from '../store.js'
import { copyText } from '../utils.js'

const message = useMessage()
const model = ref(null)
const modelOptions = ref([])
const maxTokens = ref(1024)
const temperature = ref(0.7)
const draft = ref('')
const messages = ref([])
const streaming = ref(false)
const msgs = ref(null)
const uploading = ref(false)
const pendingImgs = ref([])
const fileInput = ref(null)
const controller = ref(null) // AbortController,用于"停止生成"

const EXAMPLES = ['用一句话介绍你自己', '写一个 Go 的 HTTP 中间件示例', '帮我润色一段产品文案']
const HISTORY_KEY = (m) => `chat:hist:${m}`
const MAX_HISTORY = 60

// 切换模型时恢复对应会话。
watch(model, (m) => { if (m) loadHistory(m) })

onMounted(async () => {
  try {
    const { data } = await api.models()
    modelOptions.value = data.data.map(m => ({ label: m.model_name, value: m.model_name }))
    const q = new URLSearchParams(location.search).get('model')
    if (q && modelOptions.value.some(o => o.value === q)) model.value = q
    else if (modelOptions.value.length) model.value = modelOptions.value[0].value
  } catch (e) { message.error(apiErr(e, '模型列表加载失败')) }
})

function onKey(e) {
  // 输入法组合中(选词回车)不发送。
  if (e.isComposing || e.keyCode === 229) return
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() }
}

function pickFile() { fileInput.value && fileInput.value.click() }
async function onFile(e) {
  const f = e.target.files && e.target.files[0]
  if (!f) return
  if (f.size > 10 * 1024 * 1024) { message.error('文件不能超过 10MB'); e.target.value = ''; return }
  uploading.value = true
  try {
    const { data } = await api.upload(f)
    pendingImgs.value.push({ url: data.url, filename: data.filename, isImg: data.mime_type && data.mime_type.startsWith('image/') })
  } catch (err) { message.error(apiErr(err, '上传失败')) }
  finally { uploading.value = false; e.target.value = '' }
}

async function send(reuseText, reuseImgs) {
  if (streaming.value) return
  // reuseText 可能为历史消息数组(异常恢复)或非字符串,强制 String 化避免 .trim 报错。
  const raw = reuseText !== undefined ? reuseText : draft.value
  const text = (raw == null ? '' : typeof raw === 'string' ? raw : JSON.stringify(raw)).trim()
  const imgs = (reuseImgs !== undefined ? reuseImgs : pendingImgs.value.slice())
  if (!text && !imgs.length) return
  // 仅在非复用场景下清空输入框(失败时再恢复)。
  const isFresh = reuseText === undefined
  if (isFresh) { draft.value = ''; pendingImgs.value = [] }

  let userContent
  if (imgs.length) {
    userContent = buildParts(text, imgs)
    messages.value.push({ role: 'user', content: text || '(图片)', imgs })
  } else {
    userContent = text
    messages.value.push({ role: 'user', content: text })
  }
  const asst = { role: 'assistant', content: '', typing: true }
  messages.value.push(asst)
  await scroll()
  streaming.value = true

  try {
    await streamOnce(userContent, asst)
    persist()
  } catch (e) {
    if (e.name === 'AbortError') {
      // 用户主动停止:保留已生成内容,移除空 assistant 气泡。
      if (!asst.content) messages.value = messages.value.filter(m => m !== asst)
      else asst.typing = false
    } else {
      message.error(e.message)
      // 失败:移除该 assistant 气泡,恢复用户输入与附件。
      messages.value = messages.value.filter(m => m !== asst)
      if (isFresh) {
        draft.value = text
        pendingImgs.value = imgs
      }
      // 标记一条错误气泡(不参与 history 组装)。
      messages.value.push({ role: 'assistant', content: '', isError: true, errMsg: e.message })
    }
  } finally {
    asst.typing = false
    streaming.value = false
    controller.value = null
    persist()
  }
}

// 组装发送给上游的 history,跳过错误气泡。
function buildHistory(userContent) {
  const usable = messages.value.filter(m => !m.isError)
  return usable.slice(0, -1).map(m => {
    if (m.imgs && m.imgs.length) {
      return { role: m.role, content: buildParts(m.content, m.imgs) }
    }
    return { role: m.role, content: m.content }
  }).concat([{ role: 'user', content: userContent }])
}

// buildParts 组装多模态消息内容(OpenAI vision 格式): 文本在前 + 图片/文件附件。
// send 与 buildHistory 共用,避免两处手写 parts 拼接导致格式漂移。
function buildParts(text, imgs) {
  const parts = []
  if (text) parts.push({ type: 'text', text })
  for (const im of imgs) parts.push({ type: 'image_url', image_url: { url: im.url } })
  return parts
}

// classifyChatError 把网关错误响应归为对用户有行动指引的中文提示。
// 区分"该充值了/换模型/稍后重试/重新登录",避免一律显示 HTTP 状态码。
function classifyChatError(status, body) {
  const e = body?.error || {}
  const type = typeof e === 'string' ? '' : e.type || ''
  const raw = typeof e === 'string' ? e : (e.message || '')
  if (status === 401 || type === 'authentication_error') return '登录已过期,请重新登录'
  if (status === 402 || type === 'insufficient_balance') return '余额不足,请前往充值后再试'
  if (status === 404 || type === 'model_not_found') return '模型不可用或未启用,请切换模型'
  if (status === 429 || type === 'rate_limit_exceeded') return '请求过于频繁(触发限流),请稍后再试'
  if (status === 503 || type === 'no_channel') return '当前无可用渠道,请稍后重试或联系管理员'
  if (status >= 500) return '服务暂时不可用,请稍后重试'
  return raw || `请求失败(HTTP ${status})`
}

async function streamOnce(userContent, asst) {
  const ctrl = new AbortController()
  controller.value = ctrl
  const history = buildHistory(userContent)
  const body = { model: model.value, messages: history, stream: true, max_tokens: maxTokens.value, temperature: temperature.value }
  const resp = await fetch('/api/playground/chat/stream', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token.get()}` },
    body: JSON.stringify(body),
    signal: ctrl.signal,
  })
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({}))
    throw new Error(classifyChatError(resp.status, err))
  }
  const reader = resp.body.getReader()
  const dec = new TextDecoder()
  let buf = ''
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buf += dec.decode(value, { stream: true })
    const lines = buf.split('\n')
    buf = lines.pop()
    for (const line of lines) {
      const t = line.trim()
      if (!t.startsWith('data:')) continue
      const payload = t.slice(5).trim()
      if (payload === '[DONE]') continue
      try {
        const chunk = JSON.parse(payload)
        if (chunk.choices && chunk.choices[0]) asst.content += chunk.choices[0].delta?.content || ''
      } catch {
        // SSE 偶有非 JSON 行(心跳/事件帧),跳过但记录便于上游格式异常时排障。
        console.debug('skip sse line', payload)
      }
      await scroll()
    }
  }
}

function stop() { controller.value?.abort() }

function clearAll() { messages.value = []; persist(); message.success('已清空') }
function copyMsg(m) { copyText(m.content).then(ok => ok ? message.success('已复制') : message.error('复制失败')) }
function delMsg(i) { messages.value.splice(i, 1); persist() }
// 重生成:移除该 assistant 及其前一条 user,用 user 内容重新请求。
function regen(i) {
  let userIdx = -1
  for (let j = i - 1; j >= 0; j--) {
    if (messages.value[j].role === 'user') { userIdx = j; break }
  }
  const userMsg = userIdx >= 0 ? messages.value[userIdx] : null
  // 从后往前删:先 assistant 再 user,避免索引漂移。
  messages.value.splice(i, 1)
  if (userIdx >= 0) messages.value.splice(userIdx, 1)
  if (userMsg) send(userMsg.content, userMsg.imgs || [])
  else persist()
}
// 重试:移除错误气泡与最后一条 user,用其内容重发。
function retryLast() {
  const errIdx = messages.value.findIndex(m => m.isError)
  if (errIdx >= 0) messages.value.splice(errIdx, 1)
  const lastUser = [...messages.value].reverse().find(m => m.role === 'user')
  if (lastUser) {
    messages.value = messages.value.filter(m => m !== lastUser)
    send(lastUser.content, lastUser.imgs || [])
  }
}

function persist() {
  if (!model.value) return
  try {
    const clean = messages.value.filter(m => !m.isError).slice(-MAX_HISTORY)
    localStorage.setItem(HISTORY_KEY(model.value), JSON.stringify(clean))
  } catch {}
}
function loadHistory(m) {
  try {
    const raw = localStorage.getItem(HISTORY_KEY(m))
    if (raw) { messages.value = JSON.parse(raw); nextTick(scroll) }
    else messages.value = []
  } catch { messages.value = [] }
}

async function scroll() {
  await nextTick()
  if (msgs.value) msgs.value.scrollTop = msgs.value.scrollHeight
}
</script>

<style scoped>
.chat-wrap { display:flex; flex-direction:column; height:100%; background:#f7f8fa }
.messages { flex:1; overflow-y:auto; padding:24px max(24px,calc((100% - 820px)/2)) }
.empty { text-align:center; color:#9aa1ad; margin-top:16vh }
.empty .big { font-size:48px }
.empty p { margin:8px 0 16px }
.examples { display:flex; flex-direction:column; gap:8px; align-items:center }
.ex { background:#fff; border:1px solid #eef0f5; border-radius:8px; padding:8px 14px; cursor:pointer; color:#4b5160; font:inherit; font-size:13px }
.ex:hover { border-color:#3D6EFF; color:#3D6EFF }
.msg { display:flex; gap:12px; margin-bottom:20px; max-width:820px }
.msg.user { flex-direction:row-reverse; margin-left:auto }
.msg.err .bubble { background:#fff1f0; border:1px solid #ffccc7 }
.avatar { width:32px;height:32px;border-radius:8px;flex-shrink:0;display:flex;align-items:center;justify-content:center;
  font-size:13px;font-weight:600;color:#fff;background:linear-gradient(135deg,#3D6EFF,#22d3ee) }
.msg.user .avatar { background:#8b93a7 }
.bubble-wrap { max-width:680px }
.bubble { background:#fff;border-radius:12px;padding:12px 16px;box-shadow:0 1px 3px rgba(0,0,0,.04) }
.msg.user .bubble { background:#3D6EFF;color:#fff }
.bubble pre { margin:0; white-space:pre-wrap; word-break:break-word; font-family:inherit; font-size:14px; line-height:1.7 }
.cursor { animation:blink 1s steps(2) infinite; color:#3D6EFF }
@keyframes blink { 50% { opacity:0 } }
.msg-actions { display:flex; gap:10px; margin-top:6px; opacity:0; transition:.15s }
.msg:hover .msg-actions, .msg.err .msg-actions { opacity:1 }
.act { background:none; border:none; color:#9aa1ad; cursor:pointer; font-size:12px; padding:0 }
.act:hover { color:#3D6EFF }
.composer { border-top:1px solid #eee; background:#fff; padding:12px 24px 16px }
.bar { display:flex; gap:10px; align-items:center; margin-bottom:10px; flex-wrap:wrap }
.input-row { display:flex; gap:10px; align-items:flex-end }
.attaches { display:flex; gap:8px; margin-top:8px; flex-wrap:wrap }
.att { position:relative; border:1px solid #eef0f5; border-radius:8px; padding:4px 8px; font-size:12px; background:#fafbfc }
.att img { width:48px; height:48px; object-fit:cover; border-radius:4px; display:block }
.rm { position:absolute; top:-6px; right:-6px; background:#666; color:#fff; width:18px; height:18px; border-radius:50%; border:none; cursor:pointer; font-weight:400; line-height:16px; padding:0 }
@media (max-width:768px) {
  .messages { padding:16px 12px }
  .composer { padding:10px 12px 14px }
}
</style>
