import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import Layout from './views/Layout.vue'
import { token } from './store.js'

const routes = [
  { path: '/login', name: 'login', component: () => import('./views/Login.vue') },
  {
    path: '/', component: Layout,
    children: [
      { path: '', redirect: '/dashboard' },
      { path: 'dashboard', name: 'dashboard', component: () => import('./views/Dashboard.vue') },
      { path: 'tenants', name: 'tenants', component: () => import('./views/Tenants.vue') },
      { path: 'users', name: 'users', component: () => import('./views/Users.vue') },
      { path: 'channels', name: 'channels', component: () => import('./views/Channels.vue') },
      { path: 'models', name: 'models', component: () => import('./views/Models.vue') },
      { path: 'ledger', name: 'ledger', component: () => import('./views/Ledger.vue') },
      { path: 'analytics', name: 'analytics', component: () => import('./views/Analytics.vue') },
      { path: 'audit', name: 'audit', component: () => import('./views/Audit.vue') },
      { path: 'request-logs', name: 'request-logs', component: () => import('./views/RequestLogs.vue') },
      { path: 'tenant-keys', name: 'tenant-keys', component: () => import('./views/TenantKeys.vue') },
    ]
  },
  // 404: 未匹配路由展示独立页。
  { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('./views/NotFound.vue') },
]
const router = createRouter({ history: createWebHistory('/admin/'), routes })
router.beforeEach((to) => {
  if (to.name !== 'login' && !token.get()) return { name: 'login' }
})
createApp(App).use(router).mount('#app')
