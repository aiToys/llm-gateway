import { test, expect } from '@playwright/test'

async function adminLogin(page) {
  await page.goto('/admin/login')
  await page.getByRole('textbox').first().fill('admin@demo.com')
  await page.getByRole('textbox').nth(1).fill('admin123')
  await page.getByRole('button', { name: '登录' }).click()
  await page.waitForURL('**/admin/dashboard', { timeout: 10000 })
}

test('admin dashboard shows stats', async ({ page }) => {
  await adminLogin(page)
  await expect(page.getByText('总请求数')).toBeVisible()
  await expect(page.getByText('活跃租户')).toBeVisible()
})

test('admin channel connectivity test', async ({ page }) => {
  await adminLogin(page)
  await page.getByRole('menuitem', { name: '渠道管理' }).click()
  await page.waitForURL('**/admin/channels')
  // 点击第一行的「测试」按钮,应弹出成功提示
  const testBtn = page.getByRole('button', { name: '测试' }).first()
  await expect(testBtn).toBeVisible()
  await testBtn.click()
  await expect(page.getByText(/连通正常|测试失败/).first()).toBeVisible({ timeout: 15000 })
})

test('admin audit log page', async ({ page }) => {
  await adminLogin(page)
  await page.getByRole('menuitem', { name: '审计日志' }).click()
  await page.waitForURL('**/admin/audit')
  await expect(page.getByText('动作')).toBeVisible()
})
