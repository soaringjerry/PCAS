import { test, expect } from '@playwright/test'

test('routing failure asks for intent and delegation never starts a model automatically', async ({ page }) => {
  await page.goto('/')
  await page.getByLabel('访问令牌').fill(process.env.PCAS_TEST_API_TOKEN ?? 'pcas-browser-check-secret-123456789012')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByLabel('导办台', { exact: true })).toBeVisible()
  let runs = 0
  page.on('request', req => { if (req.postData()?.includes('requestRun')) runs++ })
  await page.route('**/v1/desk/route', route => route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":"unavailable"}' }))
  const request = `帮我查测试资料${Date.now()}`
  await page.getByLabel('导办台', { exact: true }).fill(request)
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await expect(page.getByRole('group', { name: '这句要怎么处理' })).toBeVisible()
  expect(runs).toBe(0)
  await page.getByRole('button', { name: '放回输入框' }).click()
  await expect(page.getByLabel('导办台', { exact: true })).toHaveValue(request)
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await page.getByRole('group', { name: '这句要怎么处理' }).getByRole('button', { name: '交给副手', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '建好了事项' })).toBeVisible()
  expect(runs).toBe(0)
  await page.reload()
  expect(runs).toBe(0)
})
