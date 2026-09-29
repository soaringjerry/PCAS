import { test, expect } from '@playwright/test'

test('API connection keeps separate keys and queues vector backfill', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', e => errors.push(e.message))
  const config = {
    editable: true,
    text: { base_url: 'https://api.openai.com/v1', model: 'gpt-6.1-sol', input_cny_per_million: 1, output_cny_per_million: 4, default: false, key_configured: false },
    embedding: { base_url: 'https://api.openai.com/v1', model: 'text-embedding-3-small', input_cny_per_million: 0.2, output_cny_per_million: 0, default: false, key_configured: false },
  }
  const saved: Record<string, unknown>[] = []
  await page.route('**/v1/models/openai**', async route => {
    if (route.request().method() === 'GET') {
      await route.fulfill({ json: config })
      return
    }
    const role = route.request().url().endsWith('/text') ? 'text' : 'embedding'
    const body = route.request().postDataJSON()
    saved.push(body)
    const publicBody = { ...body }
    delete publicBody.api_key
    config[role] = { ...config[role], ...publicBody, key_configured: true }
    await route.fulfill({ json: config[role] })
  })
  await page.route('**/v1/models/embeddings/rebuild', route => route.fulfill({ json: { queued: 3 } }))
  await page.goto('/settings')
  await page.getByLabel('访问令牌').fill(process.env.PCAS_TEST_API_TOKEN ?? 'pcas-browser-check-secret-123456789012')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'API 与向量接入' })).toBeVisible()
  await page.getByLabel('文本 API Base URL', { exact: true }).fill('https://compatible.example/v1')
  await page.getByLabel('文本 API API Key', { exact: true }).fill('fake-text-key')
  await page.getByRole('button', { name: '保存文本 API接入', exact: true }).click()
  await expect(page.getByText('API 接入已保存，可在副手中使用。')).toBeVisible()
  await expect(page.getByLabel('文本 API API Key', { exact: true })).toHaveValue('')
  await expect(page.getByLabel('向量 Base URL', { exact: true })).toHaveValue('https://api.openai.com/v1')
  await page.getByLabel('向量 API Key', { exact: true }).fill('fake-vector-key')
  await page.getByRole('button', { name: '保存向量接入', exact: true }).click()
  await expect(page.getByText('向量接入已保存，可补建现有资料的向量。')).toBeVisible()
  await page.getByRole('button', { name: '补建现有资料向量', exact: true }).click()
  await expect(page.getByText('已排队 3 项资料，后台按每日预算补建缺失向量。')).toBeVisible()
  expect(saved).toEqual([
    { base_url: 'https://compatible.example/v1', model: 'gpt-6.1-sol', api_key: 'fake-text-key', input_cny_per_million: 1, output_cny_per_million: 4, default: false },
    { base_url: 'https://api.openai.com/v1', model: 'text-embedding-3-small', api_key: 'fake-vector-key', input_cny_per_million: 0.2, output_cny_per_million: 0, default: false },
  ])
  expect(errors).toEqual([])
})
