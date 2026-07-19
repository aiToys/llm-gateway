import { defineConfig } from 'vitepress'

// LLM Gateway 文档站配置。
// - markdown 与 .vitepress 同处 docs-site/(单一真源)。
// - base 可经 DOCS_BASE 覆盖(GitHub Pages 子路径 / 云主机子目录),默认 '/'。
// - cleanUrls: false → 生成 /xxx.html,nginx 无需 SPA fallback。
export default defineConfig({
  title: 'LLM Gateway',
  description: '多租户多供应商 LLM 网关 —— 统一 OpenAI/Anthropic 协议,负载均衡与计费内建',
  lang: 'zh-CN',
  lastUpdated: true,
  cleanUrls: false,
  base: process.env.DOCS_BASE ?? '/',
  ignoreDeadLinks: false,
  head: [['meta', { name: 'theme-color', content: '#3D6EFF' }]],
  markdown: {
    lineNumbers: false,
  },
  themeConfig: {
    logo: '/logo.svg',
    search: { provider: 'local' },
    nav: [
      { text: '首页', link: '/' },
      { text: '快速开始', link: '/quickstart' },
      { text: '核心概念', link: '/concepts' },
      { text: 'GitHub', link: 'https://github.com/aitoys/llm-gateway' },
    ],
    sidebar: [
      {
        text: '开始',
        items: [
          { text: '快速开始', link: '/quickstart' },
          { text: '核心概念', link: '/concepts' },
        ],
      },
      {
        text: '使用',
        items: [
          { text: '模型与定价', link: '/models-pricing' },
          { text: '多供应商负载均衡', link: '/load-balancing' },
          { text: '租户模型启停', link: '/tenant-models' },
          { text: 'API 密钥与限流', link: '/api-keys' },
          { text: '双协议接入', link: '/api-compat' },
        ],
      },
      {
        text: '运维',
        items: [
          { text: '部署', link: '/deployment' },
          { text: '配置参考', link: '/configuration' },
          { text: '计费与账本', link: '/billing' },
          { text: '可观测性', link: '/observability' },
        ],
      },
      {
        text: '深入',
        items: [
          { text: '架构', link: '/architecture' },
          { text: '数据模型', link: '/data-model' },
        ],
      },
    ],
    socialLinks: [
      { icon: 'github', link: 'https://github.com/aitoys/llm-gateway' },
    ],
    footer: {
      message: '基于 MIT 协议发布',
      copyright: 'Copyright © 2026 LLM Gateway',
    },
    outline: { level: [2, 3] },
    docFooter: { prev: '上一页', next: '下一页' },
    lastUpdatedText: '最后更新',
  },
})
