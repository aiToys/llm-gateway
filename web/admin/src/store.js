const KEY = 'admin_token'
const USER = 'admin_user'
export const token = {
  get: () => localStorage.getItem(KEY),
  set: (t) => localStorage.setItem(KEY, t),
  clear: () => { localStorage.removeItem(KEY); localStorage.removeItem(USER) },
}
export const user = {
  get: () => { const r = localStorage.getItem(USER); return r ? JSON.parse(r) : null },
  set: (u) => localStorage.setItem(USER, JSON.stringify(u)),
}
export function logout() { token.clear() }
