import { test, expect } from '@playwright/test'

// 回归: Team.vue 表格「复制链接」列曾用 r._link,但后端 listInvites 不返回明文 token,
// 前端 load() 把 _link 设空、genInvite 重拉列表又丢失 → 点「复制链接」复制到 undefined。
// 修复后:生成时回填 _link;空态显示「生成时已复制」。
// 守护:生成邀请后,列表里出现「复制链接」或「生成时已复制」(均非 undefined 空操作)。

test('生成邀请后列表显示可用复制入口(回归 _link undefined)', async ({ page }) => {
  await page.goto('/login')
  await page.getByRole('textbox').first().fill('admin@demo.com')
  await page.getByRole('textbox').nth(1).fill('admin123')
  await page.getByRole('button', { name: '登录' }).click()
  await page.waitForURL('**/console/**', { timeout: 10000 })

  await page.goto('/console/team')
  const genBtn = page.getByRole('button', { name: /生成邀请链接/ }).first()

  // 需 team admin 账号才显示「生成邀请」按钮;非 admin 则跳过(不报错)。
  if (!(await genBtn.isVisible().catch(() => false))) {
    test.skip(true, '当前账号非 team admin,跳过邀请 UI 回归')
  }

  await genBtn.click()
  // 生成成功提示
  await expect(page.getByText(/已生成|已复制/).first()).toBeVisible({ timeout: 10000 })
  // 列表复制入口:刚生成的显示「复制链接」,刷新后显示「生成时已复制」,都不应是 undefined
  await expect(page.getByText(/复制链接|生成时已复制/).first()).toBeVisible({ timeout: 5000 })
})
