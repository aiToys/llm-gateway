import { test, expect } from '@playwright/test'

// 公开展示页可匿名访问
test('public marketplace loads', async ({ page }) => {
  await page.goto('/models')
  await expect(page).toHaveTitle(/模型广场/)
  // 至少有一个模型卡片
  await expect(page.getByText('试用').first()).toBeVisible({ timeout: 10000 })
})

// 登录失败提示
test('login with wrong password fails', async ({ page }) => {
  await page.goto('/login')
  await page.getByRole('textbox').first().fill('demo@demo.com')
  await page.getByRole('textbox').nth(1).fill('wrong-password')
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page.getByText(/失败|无效|invalid/i).first()).toBeVisible({ timeout: 8000 })
})
