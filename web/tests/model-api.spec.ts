import { test, expect } from '@playwright/test'
import { evidence, fixtureURL, login } from './support/real'

test('real API settings persist separate keys, redact secrets and queue vector backfill', async ({ page }, info) => {
  const errors: string[] = []
  page.on('pageerror', e => errors.push(e.message))
  await login(page, '/settings')
  await expect(page.getByRole('heading', { name: 'API 与向量接入' })).toBeVisible()
  const before = await (await page.request.get('/v1/models/openai')).json()
  await page.getByLabel('文本 API Base URL', { exact: true }).fill(`${fixtureURL}/v1`)
  await page.getByLabel('文本 API API Key', { exact: true }).fill('fake-text-key')
  await page.getByRole('button', { name: '保存文本 API接入', exact: true }).click()
  await expect(page.getByText('API 接入已保存，可在副手中使用。')).toBeVisible()
  await expect(page.getByLabel('文本 API API Key', { exact: true })).toHaveValue('')
  await expect(page.getByLabel('向量 Base URL', { exact: true })).toHaveValue(before.embedding.base_url)
  await page.getByLabel('向量 Base URL', { exact: true }).fill(`${fixtureURL}/v1`)
  await page.getByLabel('向量 API Key', { exact: true }).fill('fake-vector-key')
  await page.getByRole('button', { name: '保存向量接入', exact: true }).click()
  await expect(page.getByText('向量接入已保存，可补建现有资料的向量。')).toBeVisible()
  await page.reload()
  await expect(page.getByLabel('文本 API Base URL', { exact: true })).toHaveValue(`${fixtureURL}/v1`)
  await expect(page.getByLabel('向量 Base URL', { exact: true })).toHaveValue(`${fixtureURL}/v1`)
  const configResponse = await page.request.get('/v1/models/openai')
  const config = await configResponse.json()
  expect(config.text.key_configured).toBe(true)
  expect(config.embedding.key_configured).toBe(true)
  for (const secret of ['fake-text-key', 'fake-vector-key', 'api_key']) expect(await configResponse.text()).not.toContain(secret)
  const queued = page.waitForResponse(r => r.url().endsWith('/v1/models/embeddings/rebuild') && r.request().method() === 'POST')
  await page.getByRole('button', { name: '补建现有资料向量', exact: true }).click()
  const response = await queued
  expect(response.ok()).toBeTruthy()
  const count = (await response.json()).queued
  expect(count).toBeGreaterThanOrEqual(0)
  await expect(page.getByText(`已排队 ${count} 项资料，后台按每日预算补建缺失向量。`)).toBeVisible()
  expect(errors).toEqual([])
  await evidence(page, info)
})
