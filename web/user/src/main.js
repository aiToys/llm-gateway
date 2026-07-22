import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import Layout from './views/Layout.vue'
import PublicLayout from './views/PublicLayout.vue'
import { token } from './store.js'
import './styles.css'

const routes = [
  { path: '/login', name: 'login', component: () => import('./views/Login.vue') },
  { path: '/invite', name: 'invite', component: () => import('./views/Invite.vue') },
  {
    path: '/', component: PublicLayout,
    children: [
      { path: '', name: 'home', component: () => import('./views/Home.vue') },
      { path: 'models', name: 'marketplace', component: () => import('./views/Marketplace.vue') },
      { path: 'pricing', name: 'pricing', component: () => import('./views/Pricing.vue') },
    ]
  },
  {
    path: '/console', component: Layout,
    children: [
      { path: '', redirect: '/console/chat' },
      { path: 'chat', name: 'chat', component: () => import('./views/Chat.vue') },
      { path: 'plaza', name: 'plaza', component: () => import('./views/Marketplace.vue') },
      { path: 'keys', name: 'keys', component: () => import('./views/Keys.vue') },
      { path: 'models', name: 'my-models', component: () => import('./views/MyModels.vue') },
      { path: 'usage', name: 'usage', component: () => import('./views/Usage.vue') },
      { path: 'recharge', name: 'recharge', component: () => import('./views/Recharge.vue') },
      { path: 'team', name: 'team', component: () => import('./views/Team.vue') },
    ]
  },
  // 404: 未匹配路由展示独立页(而非静默跳转,便于用户察觉链接错误)。
  { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('./views/NotFound.vue') },
]

// scrollBehavior: 切换页面重置滚动位置(展示页顶部锚点等)。
const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior() { return { top: 0 } },
})
router.beforeEach((to) => {
  // 用 startsWith 匹配,避免带 query(如 /console/chat?model=x) 时守卫失效。
  const isConsole = to.path.startsWith('/console')
  if (isConsole && !token.get()) {
    return { name: 'login', query: to.fullPath !== '/console' ? { redirect: to.fullPath } : undefined }
  }
  if (to.name === 'login' && token.get()) return { name: 'chat' }
})

createApp(App).use(router).mount('#app')
