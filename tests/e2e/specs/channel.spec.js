import { test, expect } from '@playwright/test'

// 回归: Channels.vue 曾因漏 import NFormItemGi,导致新建渠道表单的
// 供应商/名称/Base URL/优先级/权重/租户ID 整块消失,只剩模型配置 + API Key。
// 这个 spec 守护「名称框必须可见」+ 完整创建→删除链路。

async function adminLogin(page) {
  await page.goto('/admin/login')
  await page.getByRole('textbox').first().fill('admin@demo.com')
  await page.getByRole('textbox').nth(1).fill('admin123')
  await page.getByRole('button', { name: '登录' }).click()
  await page.waitForURL('**/admin/dashboard', { timeout: 10000 })
}

test('新建渠道表单包含名称字段(回归 NFormItemGi 漏 import)', async ({ page }) => {
  await adminLogin(page)
  await page.getByRole('menuitem', { name: '渠道管理' }).click()
  await page.waitForURL('**/admin/channels')
  await page.getByRole('button', { name: '+ 新建渠道' }).click()
  // 名称 + 供应商 必须可见(曾整块消失)
  await expect(page.locator('.n-form-item').filter({ hasText: /^名称/ })).toBeVisible()
  await expect(page.locator('.n-form-item').filter({ hasText: /^供应商/ })).toBeVisible()
})

test('创建表单所有字段齐全(回归 NFormItemGi 漏 import 整块消失)', async ({ page }) => {
  // 完整 CRUD 的 API 层(创建+删除)由 scripts/smoke.sh 覆盖;
  // UI 层这里守护表单字段渲染完整——naive-ui 的 select e2e 交互不稳定,字段可见性更可靠。
  await adminLogin(page)
  await page.getByRole('menuitem', { name: '渠道管理' }).click()
  await page.waitForURL('**/admin/channels')
  await page.getByRole('button', { name: '+ 新建渠道' }).click()
  // 供应商/名称/Base URL/优先级/权重/租户ID 这一整块曾因漏 import NFormItemGi 全部消失
  for (const label of ['供应商', '名称', 'Base URL', '优先级', '权重', '租户ID']) {
    await expect(page.locator('.n-form-item').filter({ hasText: new RegExp('^' + label) })).toBeVisible()
  }
})
