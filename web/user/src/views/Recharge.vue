<template>
  <div class="page">
    <h3>账户充值</h3>
    <div class="balance-card">
      <div class="k">当前余额</div>
      <div class="v" v-if="balanceLoaded">¥{{ yuanDisplay }}</div>
      <div class="v" v-else>--</div>
      <n-button quaternary size="tiny" style="margin-top:8px; color:#fff" :loading="balLoading" @click="refresh">刷新余额</n-button>
    </div>

    <!-- 金额: 固定档位 + 自定义 -->
    <div class="amounts">
      <button v-for="a in PRESETS" :key="a" type="button"
        :class="['amt', selected === a && !customMode ? 'on' : '']" :aria-pressed="selected === a"
        @click="selectPreset(a)">¥{{ (a / 100).toFixed(2) }}</button>
    </div>
    <div class="custom">
      <span class="clabel">自定义金额</span>
      <n-input-number v-model:value="customYuan" :min="0.01" :precision="2" :step="1" size="small"
        placeholder="元" style="width:140px" @update:value="customMode = true" />
    </div>

    <!-- 渠道选择 -->
    <div class="prov-row">
      <button v-for="p in providers" :key="p.id" type="button"
        :class="['prov', provider === p.id ? 'on' : '']" @click="provider = p.id">
        <span class="plogo" :style="{ background: p.color }">{{ p.glyph }}</span>
        <span>{{ p.label }}</span>
      </button>
    </div>

    <n-button type="primary" size="large" :loading="loading" :disabled="loading || !amountCents"
      style="margin-top:18px" @click="pay">确认充值 ¥{{ amountDisplay }}</n-button>

    <!-- 兑换码充值: 卡密直充,不经第三方。 -->
    <div class="redeem">
      <div class="rlabel">兑换码</div>
      <n-input v-model:value="redeemCode" placeholder="输入卡密兑换码" :disabled="redeeming" @keyup.enter="redeem" />
      <n-button type="primary" :loading="redeeming" :disabled="redeeming || !redeemCode.trim()" @click="redeem">兑换</n-button>
    </div>

    <n-alert :type="isMock ? 'info' : 'warning'" style="margin-top:16px">
      <template v-if="isMock">当前为模拟支付(mock),下单后用手机扫页面二维码或调用回调接口即可模拟到账,无需真实付款。</template>
      <template v-else>支付由第三方(微信/支付宝)收单,到账后余额自动更新。订单 {{ EXPIRES_MIN }} 分钟内未支付自动关闭。</template>
    </n-alert>

    <!-- 支付中: 微信二维码 / 支付宝已跳转 -->
    <n-modal v-model:show="showQR" preset="card" :title="qrTitle" style="max-width:360px" :mask-closable="false" :close-on-esc="false">
      <div class="qr-wrap">
        <div v-if="provider === 'wechat' || provider === 'mock'" class="qr-box">
          <img v-if="qrDataUrl" :src="qrDataUrl" alt="支付二维码" class="qr-img" />
          <n-spin v-else />
          <div class="qr-tip">请使用{{ providerLabel }}扫描二维码完成支付</div>
        </div>
        <div v-else class="qr-tip">已跳转到{{ providerLabel }}收银台,支付完成后自动返回。</div>
        <div class="qr-status">
          <n-spin v-if="polling" size="small" />
          <span>{{ statusText }}</span>
        </div>
        <div class="qr-actions">
          <n-button size="small" quaternary @click="cancelPoll">我已完成 / 关闭</n-button>
        </div>
      </div>
    </n-modal>

    <h4 style="margin-top:28px">交易明细</h4>
    <n-data-table :columns="ledgerCols" :data="ledger" :bordered="false" size="small"
      :loading="ledgerLoading" :pagination="{ pageSize: 15 }" />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, h } from 'vue'
import { NButton, NAlert, NDataTable, NTag, NModal, NInputNumber, NSpin, NInput, useMessage } from 'naive-ui'
import QRCode from 'qrcode'
import { api, user, apiErr } from '../api.js'
import { formatCents, formatTime } from '../utils.js'

const message = useMessage()
const PRESETS = [1000, 5000, 10000, 50000]
const EXPIRES_MIN = 15
const isDev = import.meta.env.DEV

const selected = ref(5000)
const customMode = ref(false)
const customYuan = ref(null)
const provider = ref('wechat')
const loading = ref(false)

const balance = ref(0)
const balanceLoaded = ref(false)
const balLoading = ref(false)
const yuanDisplay = computed(() => (balance.value / 100).toFixed(2))

// 实付金额(分): 自定义模式取 customYuan,否则取档位。
const amountCents = computed(() => {
  if (customMode.value && customYuan.value > 0) return Math.round(customYuan.value * 100)
  return selected.value
})
const amountDisplay = computed(() => (amountCents.value / 100).toFixed(2))

const PROVIDERS = [
  { id: 'wechat', label: '微信支付', glyph: '微', color: '#07c160' },
  { id: 'alipay', label: '支付宝', glyph: '支', color: '#1677ff' },
  { id: 'mock', label: '模拟支付', glyph: '拟', color: '#8b5cf6' },
]
// mock 仅 dev/mock 场景显示;真实部署由后端注册的渠道决定。前端保守展示,后端为权威。
const providers = computed(() => (isDev ? PROVIDERS : PROVIDERS.filter((p) => p.id !== 'mock')))
const providerLabel = computed(() => (PROVIDERS.find((p) => p.id === provider.value) || {}).label || provider.value)
const isMock = computed(() => provider.value === 'mock')

function selectPreset(a) { selected.value = a; customMode.value = false }

// 交易明细
const ledger = ref([])
const ledgerLoading = ref(false)
const typeLabel = { recharge: { label: '充值', type: 'success' }, usage: { label: '消费', type: 'info' }, refund: { label: '退款', type: 'warning' } }
const ledgerCols = [
  { title: '时间', key: 'created_at', render: r => formatTime(r.created_at) },
  { title: '类型', key: 'type', render: r => { const m = typeLabel[r.type] || { label: r.type, type: 'default' }; return h(NTag, { size: 'small', type: m.type }, { default: () => m.label }) } },
  { title: '模型', key: 'model', render: r => r.model || '—' },
  { title: '金额(元)', key: 'price_cents', render: r => (r.price_cents > 0 ? '-' : '+') + formatCents(Math.abs(r.price_cents)) },
  { title: '余额(元)', key: 'balance_after', render: r => formatCents(r.balance_after) },
]

async function refresh() {
  balLoading.value = true
  try {
    const { data } = await api.me()
    balance.value = data.balance_cents
    balanceLoaded.value = true
    user.set(data)
  } catch (e) { message.error(apiErr(e, '余额获取失败,请点击重试')) }
  finally { balLoading.value = false }
}
async function loadLedger() {
  ledgerLoading.value = true
  try { const { data } = await api.ledger(50); ledger.value = data.data || [] }
  catch { /* 表格自带空态 */ }
  finally { ledgerLoading.value = false }
}

// --- 真实支付下单 + 轮询 ---
const showQR = ref(false)
const qrDataUrl = ref('')
const polling = ref(false)
const orderStatus = ref('') // pending | paid | closed
const qrTitle = computed(() => `${providerLabel.value}支付`)
const statusText = computed(() => {
  if (orderStatus.value === 'paid') return '支付成功,余额已更新'
  if (orderStatus.value === 'closed') return '订单已关闭,请重新发起'
  return '等待支付结果…'
})
let pollTimer = null
let pollExpiresAt = 0

function clearPoll() {
  polling.value = false
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
}
function cancelPoll() { clearPoll(); showQR.value = false }

async function pay() {
  // dev/mock 直充快捷路径: 不经第三方,立即到账(沿用原 /api/recharge)。
  if (provider.value === 'mock') return devMockRecharge()
  loading.value = true
  try {
    const { data } = await api.createRechargeOrder(amountCents.value, provider.value)
    const o = data.data
    pollExpiresAt = (o.expires_at || 0) * 1000
    orderStatus.value = 'pending'
    if (provider.value === 'alipay') {
      qrDataUrl.value = ''
      showQR.value = true
      polling.value = true
      window.open(o.prepay_data, '_blank') // 跳转支付宝收银台
    } else {
      // wechat / mock: 渲染二维码(code_url)。
      qrDataUrl.value = await QRCode.toDataURL(o.prepay_data, { margin: 1, width: 240 })
      showQR.value = true
      polling.value = true
    }
    startPoll(o.order_no)
  } catch (e) { message.error(apiErr(e, '下单失败')) }
  finally { loading.value = false }
}

function startPoll(orderNo) {
  clearPoll()
  pollTimer = setInterval(async () => {
    if (Date.now() > pollExpiresAt) { orderStatus.value = 'closed'; clearPoll(); return }
    try {
      const { data } = await api.orderStatus(orderNo)
      const d = data.data
      if (d.balance_cents != null) { balance.value = d.balance_cents; balanceLoaded.value = true }
      if (d.status === 'paid') {
        orderStatus.value = 'paid'
        clearPoll()
        message.success('充值成功')
        await loadLedger()
        setTimeout(() => { showQR.value = false }, 1200)
      } else if (d.status === 'closed') {
        orderStatus.value = 'closed'; clearPoll()
      }
    } catch { /* 轮询容错:静默重试 */ }
  }, 2000)
}

// dev/mock 直充(不经第三方,立即到账)。
async function devMockRecharge() {
  loading.value = true
  try {
    const { data } = await api.recharge(amountCents.value)
    balance.value = data.balance_cents
    balanceLoaded.value = true
    message.success('模拟充值成功')
    await loadLedger()
  } catch (e) { message.error(apiErr(e, '充值失败')) }
  finally { loading.value = false }
}

// --- 兑换码充值(卡密直充) ---
const redeemCode = ref('')
const redeeming = ref(false)
async function redeem() {
  const code = redeemCode.value.trim()
  if (!code) return
  redeeming.value = true
  try {
    const { data } = await api.redeem(code)
    if (data.balance_cents != null) { balance.value = data.balance_cents; balanceLoaded.value = true }
    const amt = data.amount_cents != null ? (data.amount_cents / 100).toFixed(2) : ''
    message.success(amt ? `兑换成功 +¥${amt}` : '兑换成功')
    redeemCode.value = ''
    await loadLedger()
  } catch (e) { message.error(apiErr(e, '兑换失败')) }
  finally { redeeming.value = false }
}

// 支付宝 return_url 带回 order 参数时自动轮询该订单。
function pickOrderFromQuery() {
  const no = new URLSearchParams(location.search).get('order')
  if (no) { provider.value = 'alipay'; showQR.value = true; polling.value = true; startPoll(no) }
}

onMounted(() => { refresh(); loadLedger(); pickOrderFromQuery() })
onUnmounted(clearPoll)
</script>

<style scoped>
.page { padding:24px; max-width:680px }
h3 { margin-top:0 }
.balance-card { background:linear-gradient(135deg,#3D6EFF,#22d3ee); color:#fff; border-radius:16px; padding:28px; margin-bottom:20px }
.balance-card .k { opacity:.85; font-size:14px }
.balance-card .v { font-size:36px; font-weight:700; margin-top:6px }
.amounts { display:grid; grid-template-columns:repeat(4,1fr); gap:12px }
.amt { background:#fff; border:2px solid #ebedf2; border-radius:12px; padding:18px; text-align:center; font-weight:600; cursor:pointer; transition:.15s; font:inherit; color:#1f2330 }
.amt:hover { border-color:#3D6EFF }
.amt.on { border-color:#3D6EFF; background:#f0f4ff; color:#3D6EFF }
.custom { display:flex; align-items:center; gap:10px; margin-top:14px }
.clabel { font-size:13px; color:#6b7280 }
.prov-row { display:flex; gap:12px; margin-top:18px; flex-wrap:wrap }
.prov { display:flex; align-items:center; gap:8px; background:#fff; border:2px solid #ebedf2; border-radius:12px; padding:10px 16px; cursor:pointer; font:inherit; color:#1f2330; transition:.15s }
.prov:hover { border-color:#3D6EFF }
.prov.on { border-color:#3D6EFF; background:#f0f4ff }
.plogo { width:22px; height:22px; border-radius:6px; color:#fff; font-size:12px; display:flex; align-items:center; justify-content:center; font-weight:700 }
.qr-wrap { text-align:center }
.qr-box { display:flex; flex-direction:column; align-items:center; gap:8px }
.qr-img { width:220px; height:220px; border-radius:8px; border:1px solid #eef }
.qr-tip { font-size:13px; color:#6b7280; margin:6px 0 }
.qr-status { display:flex; align-items:center; justify-content:center; gap:8px; font-size:13px; margin-top:8px; min-height:22px }
.qr-actions { margin-top:10px }
.redeem { display:flex; align-items:center; gap:10px; margin-top:18px; max-width:480px }
.redeem .rlabel { font-size:13px; color:#6b7280; flex-shrink:0 }
@media (max-width:480px) { .amounts { grid-template-columns:repeat(2,1fr) } }
</style>
