import axios from 'axios'
import { token, logout } from './store.js'

const http = axios.create({ timeout: 30000 })
http.interceptors.request.use((cfg) => {
  const t = token.get()
  if (t) cfg.headers.Authorization = `Bearer ${t}`
  return cfg
})
http.interceptors.response.use((r) => r, (err) => {
  if (err.response && err.response.status === 401) {
    logout()
    // 管理端登录页在 /admin/login,/login 是用户端登录页,跳错会导致"刷新变成用户界面"。
    if (!location.pathname.startsWith('/admin/login')) {
      const redirect = encodeURIComponent(location.pathname + location.search)
      location.href = `/admin/login?redirect=${redirect}`
    }
  }
  return Promise.reject(err)
})

export const api = {
  login: (email, password) => http.post('/api/auth/login', { email, password }),
  stats: () => http.get('/api/admin/stats'),
  tenants: () => http.get('/api/admin/tenants'),
  createTenant: (data) => http.post('/api/admin/tenants', data),
  updateTenant: (id, data) => http.put(`/api/admin/tenants/${id}`, data),
  setTenantStatus: (id, status) => http.patch(`/api/admin/tenants/${id}/status`, { status }),
  users: () => http.get('/api/admin/users'),
  createUser: (data) => http.post('/api/admin/users', data),
  setUserStatus: (id, status) => http.patch(`/api/admin/users/${id}/status`, { status }),
  updateUser: (id, data) => http.put(`/api/admin/users/${id}`, data),
  resetUserPassword: (id, password) => http.post(`/api/admin/users/${id}/password`, { password }),
  adjustUserBalance: (id, deltaCents) => http.post(`/api/admin/users/${id}/balance`, { delta_cents: deltaCents }),
  channels: () => http.get('/api/admin/channels'),
  createChannel: (data) => http.post('/api/admin/channels', data),
  updateChannel: (id, data) => http.put(`/api/admin/channels/${id}`, data),
  deleteChannel: (id) => http.delete(`/api/admin/channels/${id}`),
  setChannelStatus: (id, status) => http.patch(`/api/admin/channels/${id}/status`, { status }),
  updateChannelRouting: (id, data) => http.patch(`/api/admin/channels/${id}/routing`, data),
  addChannelModel: (id, model) => http.post(`/api/admin/channels/${id}/models/${encodeURIComponent(model)}`),
  removeChannelModel: (id, model) => http.delete(`/api/admin/channels/${id}/models/${encodeURIComponent(model)}`),
  models: () => http.get('/api/admin/models'),
  upsertModel: (data) => http.post('/api/admin/models', data),
  deleteModel: (name) => http.delete(`/api/admin/models/${name}`),
  ledger: () => http.get('/api/admin/ledger'),
  // 导出账本 CSV(blob 下载,调用方传 { responseType: 'blob' })。
  ledgerExport: (cfg) => http.get('/api/admin/ledger/export', cfg),
  usageAggregate: (params) => http.get('/api/admin/usage/aggregate', { params }),
  testChannel: (id) => http.post(`/api/admin/channels/${id}/test`),
  audit: () => http.get('/api/admin/audit'),
  requestLogs: (params) => http.get('/api/admin/request-logs', { params }),
  requestLog: (id) => http.get(`/api/admin/request-logs/${id}`),
  tenantKeys: () => http.get('/api/admin/tenant-keys'),
  revokeTenantKey: (id) => http.delete(`/api/admin/tenant-keys/${id}`),
  providers: () => http.get('/api/public/providers'),
}

// unwrap 统一取响应数据:优先 body.data(后端标准 {data:...} 响应),否则返回 body(兼容 login 等历史顶层响应)。
// 新代码建议: const list = unwrap(await api.tenants())  —— 不再纠结 data.data vs data.xxx。
export function unwrap(resp) {
  const body = resp.data
  return body && typeof body === 'object' && Object.prototype.hasOwnProperty.call(body, 'data') ? body.data : body
}
