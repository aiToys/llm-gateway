import { defineConfig } from '@playwright/test'

// 端到端测试: 假设网关已在 BASE_URL(默认 http://localhost:8088)运行,
// 且已执行 `./bin/gateway -seed` 灌入演示数据。
export default defineConfig({
  testDir: './specs',
  timeout: 60000,
  retries: 1,
  workers: 1, // 单实例网关,串行执行更稳定
  use: {
    baseURL: process.env.BASE_URL || 'http://localhost:8088',
    headless: true,
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { browserName: 'chromium' } }],
})
