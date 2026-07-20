import axios from 'axios'
import { token, user, logout } from './store.js'

// timeout: 防止非流式请求永久挂起(流式 chat 走 fetch,不受此限)。
const http = axios.create({ baseURL: '', timeout: 30000 })

http.interceptors.request.use((cfg) => {
  const t = token.get()
  if (t) cfg.headers.Authorization = `Bearer ${t}`
  return cfg
})

http.interceptors.response.use(
  (r) => r,
  (err) => {
    if (err.response && err.response.status === 401) {
      logout()
      if (location.pathname !== '/login') {
        // 携带 redirect,登录后回到原页面;expired=1 让登录页提示"登录已过期"。
        const redirect = encodeURIComponent(location.pathname + location.search)
        location.href = `/login?redirect=${redirect}&expired=1`
      }
    }
    return Promise.reject(err)
  }
)

// 统一错误信息抽取,供页面直接 message.error(apiErr(e))。
export function apiErr(e, fallback = '操作失败') {
  return e?.response?.data?.error || e?.message || fallback
}

// unwrap 统一取响应数据:优先 body.data(后端标准 {data:...} 响应),否则返回 body(兼容 login 等历史顶层响应)。
// 新代码建议: const list = unwrap(await api.keys())  —— 不再纠结 data.data vs data.xxx。
export function unwrap(resp) {
  const body = resp.data
  return body && typeof body === 'object' && Object.prototype.hasOwnProperty.call(body, 'data') ? body.data : body
}

export const api = {
  login: (email, password) => http.post('/api/auth/login', { email, password }),
  register: (email, password, tenant) => http.post('/api/auth/register', { email, password, tenant }),
  me: () => http.get('/api/me'),
  modelPrefs: () => http.get('/api/me/models'),
  setModelEnabled: (name, enabled) => http.put(`/api/me/models/${name}/enabled`, { enabled }),
  models: () => http.get('/api/models'),
  publicModels: () => http.get('/api/public/models'),
  keys: () => http.get('/api/keys'),
  createKey: (data) => http.post('/api/keys', data),
  revokeKey: (id) => http.delete(`/api/keys/${id}`),
  usageByDay: (days = 30) => http.get(`/api/usage/day?days=${days}`),
  usageAggregate: (params) => http.get('/api/usage/aggregate', { params }),
  ledger: (limit = 50) => http.get(`/api/usage/ledger?limit=${limit}`),
  recharge: (cents) => http.post('/api/recharge', { amount_cents: cents }),
  createRechargeOrder: (amountCents, provider) => http.post('/api/recharge/order', { amount_cents: amountCents, provider }),
  orderStatus: (no) => http.get(`/api/recharge/order/${no}`),
  // 团队
  team: () => http.get('/api/team'),
  updateTeam: (name) => http.put('/api/team', { name }),
  teamMembers: () => http.get('/api/team/members'),
  teamTransfer: (toUserId, amountCents) => http.post('/api/team/transfer', { to_user_id: toUserId, amount_cents: amountCents }),
  teamUsage: () => http.get('/api/team/usage'),
  teamChannels: () => http.get('/api/team/channels'),
  createInvite: (role) => http.post('/api/team/invites', role ? { role } : {}),
  listInvites: () => http.get('/api/team/invites'),
  revokeInvite: (id) => http.delete(`/api/team/invites/${id}`),
  inviteInfo: (token) => http.get(`/api/invites/info?token=${encodeURIComponent(token)}`),
  acceptInvite: (data) => http.post('/api/invites/accept', data),
  upload: (file) => { const fd = new FormData(); fd.append('file', file); return http.post('/api/playground/upload', fd, { headers: { 'Content-Type': 'multipart/form-data' } }) },
}

export { http, user }
