import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 生产部署在 /admin 子路径,资源与路由都带该前缀。
// 手动分包: echarts/zrender 独立(体积大、仅用量页懒加载);
// vue 生态(naive-ui/vue-router/vue-echarts/@vue/*)与其它依赖统一进 vendor。
// 注意: 不要把 vue 运行时或 naive-ui 单独拆出 -- 二者互相引用,
// 跨 chunk 会形成 ES 模块循环求值(TDZ),挂载即抛
// "Cannot access 'X' before initialization" 导致白屏。
function manualChunks(id) {
  if (id.includes('node_modules')) {
    if (id.includes('echarts') || id.includes('zrender')) return 'echarts'
    return 'vendor'
  }
}

export default defineConfig({
  base: '/admin/',
  plugins: [vue()],
  server: {
    port: 5174,
    proxy: { '/api': 'http://localhost:8088' }
  },
  build: { rollupOptions: { output: { manualChunks } } }
})
