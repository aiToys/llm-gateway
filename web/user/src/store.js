// 极简认证 + 主题 store(无 Pinia 依赖,localStorage 持久化)。
import { ref } from 'vue'

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

// 主题 store: localStorage 'light' | 'dark',默认跟随系统偏好。
// 用 Vue ref 作单一真值源,App.vue 的 watchEffect 会自动跟随其变化。
const THEME_KEY = 'theme'
function detectInitial() {
  const saved = localStorage.getItem(THEME_KEY)
  if (saved === 'light' || saved === 'dark') return saved
  return (typeof matchMedia !== 'undefined' && matchMedia('(prefers-color-scheme: dark)').matches) ? 'dark' : 'light'
}
const _themeRef = ref(detectInitial())
export const theme = {
  get() { return _themeRef.value },
  set(v) {
    if (v !== 'light' && v !== 'dark') return
    _themeRef.value = v
    localStorage.setItem(THEME_KEY, v)
  },
  toggle() { this.set(_themeRef.value === 'dark' ? 'light' : 'dark') },
  ref: _themeRef,
}
