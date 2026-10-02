import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { fixture, login, evidence } from './support/real'

const gold = JSON.parse(readFileSync(new URL('../../testdata/phase2/b4-gold.json', import.meta.url), 'utf8'))
type Batch = { id: string; name: string; state: string; total: number; stored: number; organized: number; leftOut: number }
test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 390, height: 844 } })
test.afterEach(async ({ page }, info) => { await evidence(page, info, 'phase2-b4-real') })

test('W4 真实后端：历史导入、暂停、继续至完成，在资料库找到原话', async ({ page }) => {
  test.setTimeout(120_000)
  // This is part of the frozen coordinator seam. A missing environment must
  // fail rather than skip or add sleeps to make pause accidentally possible.
  expect(process.env.PCAS_IMPORT_CHUNK_SIZE, 'Start owned backend with PCAS_IMPORT_CHUNK_SIZE=1').toBe('1')
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await fixture(page, [{ kind: 'extraction', match: '', content: '{"items":[]}' }])
  await login(page, '/settings')
  const prefix = `b4-browser-${crypto.randomUUID()}`
  const count: number = gold.coordinator_amendment_5865411.W4.messages
  const text = `合成 ChatGPT 历史 ${prefix} 青色灯塔7319寄存柜密码8624。`
  const conversations = Array.from({ length: count }, (_, n) => {
    const id = `${prefix}-${n}`
    const at = Date.parse('2024-06-12T03:10:00Z') / 1000 + n * 60
    return { id, conversation_id: id, title: `合成历史${n}`, create_time: at, update_time: at, current_node: `${id}-u`,
      mapping: { [`${id}-root`]: { id: `${id}-root`, parent: null, children: [`${id}-u`], message: null },
        [`${id}-u`]: { id: `${id}-u`, parent: `${id}-root`, children: [], message: { id: `${id}-u`, author: { role: 'user' }, create_time: at,
          content: { content_type: 'text', parts: [n === 0 ? text : `合成导入消息 ${prefix} 第${n}条。`] } } } } }
  })
  const previewResponse = page.waitForResponse(r => new URL(r.url()).pathname === '/v1/connectors/archive/preview')
  await page.locator('input[type="file"]').last().setInputFiles({ name: `${prefix}.json`, mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(conversations)) })
  const preview = await previewResponse
  expect(preview.ok(), await preview.text()).toBeTruthy()
  expect(await preview.json()).toMatchObject({ conversations: count, messages: count, fromUser: count, alreadyImported: 0, leftOut: 0 })
  await expect(page.getByText(/2[,]?000/).first()).toBeVisible()
  const importResponse = page.waitForResponse(r => new URL(r.url()).pathname === '/v1/connectors/archive' && r.request().method() === 'POST')
  await page.getByRole('button', { name: /确认导入|开始导入|^确认$/ }).click()
  const imported = await importResponse
  expect(imported.ok(), await imported.text()).toBeTruthy()
  const result: { batchId: string } = await imported.json()
  expect(result.batchId).toBeTruthy()
  const progress = async (): Promise<Batch> => {
    const response = await page.request.get('/v1/connectors/imports')
    expect(response.ok(), await response.text()).toBeTruthy()
    const items: { items: Batch[] } = await response.json()
    const item = items.items.find(item => item.id === result.batchId)
    expect(item).toBeDefined()
    return item!
  }
  await expect.poll(async () => (await progress()).stored, { timeout: 30_000 }).toBeGreaterThan(0)
  const partial = await progress()
  expect(partial.stored).toBeLessThan(count)
  const pauseResponse = page.waitForResponse(r => new URL(r.url()).pathname.endsWith(`/${result.batchId}/pause`))
  await page.getByRole('button', { name: /^暂停/ }).click()
  expect((await pauseResponse).ok()).toBeTruthy()
  await expect(page.getByRole('button', { name: /继续|恢复/ })).toBeVisible()
  // Observe multiple actual backend reads, without introducing a worker delay.
  const paused = await progress()
  expect(paused.state).toBe('paused')
  for (let n = 0; n < 5; n++) {
    const current = await progress()
    expect(current.state).toBe('paused')
    expect(current.stored).toBeLessThanOrEqual(paused.stored + 1)
  }
  const resumeResponse = page.waitForResponse(r => new URL(r.url()).pathname.endsWith(`/${result.batchId}/resume`))
  await page.getByRole('button', { name: /继续|恢复/ }).click()
  expect((await resumeResponse).ok()).toBeTruthy()
  await expect.poll(async () => (await progress()).state, { timeout: 90_000 }).toBe('done')
  const done = await progress()
  expect(done).toMatchObject({ total: count, stored: count, leftOut: 0 })
  await expect(page.getByText(/2[,]?000/).first()).toBeVisible()
  await expect(page.locator('body')).toContainText(/完成|已导入/)
  await page.goto('/library?tab=sources')
  await expect(page.getByRole('button', { name: /查看原文：.*合成历史0/ })).toBeVisible()
  await page.getByRole('button', { name: /查看原文：.*合成历史0/ }).click()
  await expect(page.getByRole('dialog')).toContainText(text)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  expect(errors).toEqual([])
})
