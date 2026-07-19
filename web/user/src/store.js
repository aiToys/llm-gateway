// 极简认证 store(无 Pinia 依赖,localStorage 持久化)。
const KEY = 'gw_token'
const USER = 'gw_user'

export const token = {
  get: () => localStorage.getItem(KEY),
  set: (t) => localStorage.setItem(KEY, t),
  clear: () => { localStorage.removeItem(KEY); localStorage.removeItem(USER) },
}

export const user = {
  get: () => {
    const raw = localStorage.getItem(USER)
    return raw ? JSON.parse(raw) : null
  },
  set: (u) => localStorage.setItem(USER, JSON.stringify(u)),
}

export function logout() { token.clear() }
