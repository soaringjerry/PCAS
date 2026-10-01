import { test, expect } from '@playwright/test'
import { evidence } from './support/real'

test('official plan connection explains usage and keeps the Codex channel', async ({ page }, info) => {
  await page.goto('/settings')
  await page.getByLabel('访问令牌').fill(process.env.PCAS_TEST_API_TOKEN ?? 'pcas-browser-check-secret-123456789012')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  // Connection details open from their rows on the settings page.
  await page.getByRole('button', { name: /^ChatGPT 订阅（官方授权）/ }).click()
  await page.getByRole('button', { name: /^ChatGPT 订阅（Codex 登录）/ }).click()
  await expect(page.getByRole('heading', { name: 'ChatGPT 订阅', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Continue with ChatGPT', exact: true })).toBeEnabled()
  await expect(page.getByText('通过 OpenAI 官方授权直接运行副手和资料整理，消耗你的 ChatGPT 套餐及账户允许的额度。')).toBeVisible()
  await expect(page.getByRole('link', { name: '管理 ChatGPT 用量与应用权限' })).toHaveAttribute('href', 'https://chatgpt.com/settings/usage')
  await expect(page.getByRole('heading', { name: 'Codex App Server', exact: true })).toBeVisible()
  const account = await page.request.get('/v1/chatgpt/direct/account')
  expect(account.status()).toBe(200)
  expect(await account.json()).toMatchObject({ accounts: [], default_ready: false, pending: false })
  const accountBody = await account.text()
  for (const field of ['access_token', 'refresh_token', 'id_token']) expect(accountBody).not.toContain(field)
  await evidence(page, info)
})
