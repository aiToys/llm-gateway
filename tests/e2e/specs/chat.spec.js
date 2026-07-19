import { test, expect } from '@playwright/test'

// 登录后流式对话应返回内容(用默认选中模型)
test('streaming chat returns content', async ({ page }) => {
  test.setTimeout(60000)
  await page.goto('/login')
  await page.getByRole('textbox').first().fill('demo@demo.com')
  await page.getByRole('textbox').nth(1).fill('demo123')
  await page.getByRole('button', { name: '登录' }).click()
  await page.waitForURL('**/console/chat', { timeout: 10000 })

  // 等待输入框就绪(模型列表加载后)
  const input = page.getByPlaceholder(/输入消息/)
  await expect(input).toBeVisible({ timeout: 10000 })
  await input.fill('你好,这是一条端到端测试')
  await input.press('Enter')

  // 等待 AI 气泡出现文本
  await expect(page.locator('.msg.assistant .bubble pre', { hasText: /mock|收到/ })).toBeVisible({ timeout: 20000 })
})
