import { test, expect, request } from '@playwright/test'

test('generic connector persists, scoped webhook cannot read memory, archive upload queues parsing', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', e => errors.push(e.message))
  await page.goto('/settings')
  await page.getByLabel('访问令牌').fill(process.env.PCAS_TEST_API_TOKEN ?? 'pcas-browser-check-secret-123456789012')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: '资料接入' })).toBeVisible()
  const name = `聊天连接-${Date.now()}`
  await page.getByLabel('接入名称').fill(name)
  const response = page.waitForResponse(r => r.url().endsWith('/v1/connectors') && r.request().method() === 'POST')
  await page.getByRole('button', { name: '添加接入' }).click()
  const configured = await (await response).json() as { connection: { id: string }; webhook_token: string }
  await expect(page.getByText(name, { exact: true })).toBeVisible()
  const external = await request.newContext({ baseURL: process.env.PCAS_TEST_BASE_URL ?? 'http://127.0.0.1:18090', extraHTTPHeaders: { Authorization: `Bearer ${configured.webhook_token}` } })
  const imported = await external.post(`/v1/connectors/${configured.connection.id}/records`, { data: { records: [{ id: 'chat-1', text: '我只是考虑去书店，还没有决定时间。', role: 'user', conversation_id: 'visit' }] } })
  expect(imported.status()).toBe(200)
  expect((await external.get('/v1/workspace')).status()).toBe(401)
  await external.dispose()
  await page.getByRole('button', { name: '已保存，隐藏密钥' }).click()
  await page.reload()
  await expect(page.getByText(name, { exact: true })).toBeVisible()
  const archiveResponse = page.waitForResponse(r => r.url().endsWith('/v1/connectors/archive') && r.request().method() === 'POST')
  await page.locator('input[type="file"]').setInputFiles({ name: 'chat.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify({ records: [{ id: 'archive-1', text: '保留我的原始表达。', role: 'user' }] })) })
  expect((await archiveResponse).status()).toBe(202)
  await expect(page.getByRole('status').filter({ hasText: '原始归档已保留' })).toBeVisible()
  expect(errors).toEqual([])
})
