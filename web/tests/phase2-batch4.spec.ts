import { test, expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import type { State } from '../src/domain/types'

// Independent UI acceptance. Mock payloads follow batch4 section 6; no
// product import component is read to construct these expectations.
const gold = JSON.parse(readFileSync(new URL('../../testdata/phase2/b4-gold.json', import.meta.url), 'utf8'))
const preview = gold.browser.preview
const at = '2025-05-14T03:10:00Z'
type Batch = {
  id: string; name: string; state: 'importing' | 'paused' | 'done' | 'failed'
  archiveId: string; archiveVersion: number
  total: number; stored: number; organized: number; leftOut: number
  earliest: string; latest: string; errorCode: string; error: string; createdAt: string; updatedAt: string
}
function batch(state: Batch['state']): Batch {
  return { id: 'b4-synthetic-batch', archiveId: '11111111-1111-4111-8111-111111111111', archiveVersion: 1, name: 'chatgpt-export.zip', state, total: 500, stored: state === 'done' ? 500 : 250,
    organized: state === 'done' ? 300 : 25, leftOut: 2, earliest: preview.earliest, latest: preview.latest,
    errorCode: state === 'failed' ? 'invalid_json' : '', error: state === 'failed' ? '聊天文件没有读完，请继续导入。' : '', createdAt: at, updatedAt: at }
}
async function mock(page: Page, initial: Batch[] = [], failure?: { error: string; message: string; status: number }, importPreview = { ...preview, blocked: 0 }) {
  const state: State = {
    version: 1, revision: 1, budgetUsage: 0, notices: [],
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: false, followUps: false, dailyReviewAt: '09:00', timezone: 'Asia/Shanghai' },
    tasks: [], ideas: [], projects: [], agents: [], memories: [], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {},
  }
  const items = initial.map(item => ({ ...item }))
  const requests: { path: string; method: string }[] = []
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    const method = route.request().method()
    requests.push({ path, method })
    if (path === '/v1/workspace') return route.fulfill({ json: state })
    if (path === '/v1/desk/turns') return route.fulfill({ json: { turns: [] } })
    if (path === '/v1/connectors') return route.fulfill({ json: [] })
    if (path === '/v1/connectors/imports') return route.fulfill({ json: { items } })
    if (path === '/v1/notify/config') return route.fulfill({ json: { webPush: { publicKey: '', subscriptions: 0 }, telegram: { configured: false, chatId: '' } } })
    if (path === '/v1/models') return route.fulfill({ json: { chatgptEnabled: false, chatgptDirectEnabled: false } })
    if (path === '/v1/chatgpt/account') return route.fulfill({ json: { account: null } })
    if (path === '/v1/chatgpt/direct/account') return route.fulfill({ json: { accounts: [], active_client_id: '', pending: false, default_ready: false } })
    if (path === '/v1/models/openai') {
      const model = { base_url: '', model: '', input_cny_per_million: 2, output_cny_per_million: 4, default: false, key_configured: false }
      return route.fulfill({ json: { editable: true, text: model, embedding: model, decision: { key_configured: false, saved: false } } })
    }
    if (path === '/v1/connectors/archive/preview' && method === 'POST') {
      if (failure) return route.fulfill({ status: failure.status, json: { error: failure.error, message: failure.message } })
      return route.fulfill({ json: importPreview })
    }
    if (path === '/v1/connectors/archive' && method === 'POST') {
      items.unshift({ ...batch('importing'), total: importPreview.messages - importPreview.alreadyImported - importPreview.leftOut - importPreview.blocked, stored: 0, organized: 0 })
      return route.fulfill({ status: 202, json: { batchId: items[0].id, imported: 0, refs: [], gaps: preview.gaps } })
    }
    const action = path.match(/^\/v1\/connectors\/imports\/([^/]+)\/(pause|resume)$/)
    if (action && method === 'POST') {
      const item = items.find(item => item.id === action[1])
      if (!item) return route.fulfill({ status: 404, json: { error: 'not_found' } })
      item.state = action[2] === 'pause' ? 'paused' : 'importing'
      return route.fulfill({ json: item })
    }
    return route.fulfill({ status: 500, json: { error: 'unexpected_b4_request' } })
  })
  return { requests, errors }
}
async function open(page: Page) {
  await page.goto('/settings')
  await expect(page.locator('input[type="file"]').last()).toBeAttached()
}
async function choose(page: Page) {
  // A synthetic official mapping file; preview numbers are controlled by the
  // mock, while production decoding is exercised by I1 and real-backend W4.
  const id = 'b4-ui-conversation'
  const conversation = [{ id, current_node: 'user', mapping: { user: { id: 'user', parent: null, children: [], message: { id: 'user', author: { role: 'user' }, create_time: 1718161800, content: { content_type: 'text', parts: ['纯合成历史资料。'] } } } } }]
  await page.locator('input[type="file"]').last().setInputFiles({ name: 'conversations.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(conversation)) })
}
async function noOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
}
async function noInternals(page: Page) {
  const visible = await page.locator('body').innerText()
  for (const word of gold.browser.forbiddenTerms as string[]) expect(visible).not.toContain(word)
}
test.use({ viewport: { width: 390, height: 844 }, timezoneId: 'Asia/Shanghai' })
test.afterEach(async ({ page }, info) => {
  await info.attach('b4-ui', { body: await page.screenshot({ fullPage: true, animations: 'disabled' }), contentType: 'image/png' })
})

test('W1 被禁止重新导入的消息单列，显示这次实际导入数量', async ({ page }) => {
  const supplement = gold.coordinator_amendment_2e2b8f9
  const p = supplement.blockedPreview
  const m = await mock(page, [], undefined, p)
  await open(page)
  await choose(page)
  await expect.poll(() => m.requests.filter(r => r.path === '/v1/connectors/archive/preview' && r.method === 'POST').length).toBe(1)
  const body = page.locator('body')
  await expect(body).toContainText(/不再导入|禁止.*导入|禁止.*重新|跳过|已删除/)
  await expect(page.getByText(/(?:本次|这次|将|可).*8.*(?:条|消息)|(?:本次|这次|将|可).*导入.*8/).first()).toBeVisible()
  expect(p.messages - p.alreadyImported - p.leftOut - p.blocked).toBe(supplement.blockedWillImport)
  expect(m.requests.filter(r => r.path === '/v1/connectors/archive' && r.method === 'POST')).toHaveLength(0)
  await noOverflow(page)
  await noInternals(page)
  expect(m.errors).toEqual([])
})

test('W1 文件选择先预览：数量、时间、已导过和放不下；确认前无导入', async ({ page }) => {
  const m = await mock(page)
  await open(page)
  await choose(page)
  await expect.poll(() => m.requests.filter(r => r.path === '/v1/connectors/archive/preview' && r.method === 'POST').length).toBe(1)
  for (const count of [3, 12, 6, 4, 2]) await expect(page.getByText(new RegExp(`\\b${count}\\b`)).first()).toBeVisible()
  const body = page.locator('body')
  await expect(body).toContainText(/2024[年/.-]\s*0?6[月/.-]\s*12/)
  await expect(body).toContainText(/2025[年/.-]\s*0?5[月/.-]\s*14/)
  await expect(body).toContainText(/对话/)
  await expect(body).toContainText(/消息|原话/)
  await expect(body).toContainText(/已.*导|重复/)
  await expect(body).toContainText(/放不下|未导入|超过|超出|跳过/)
  expect(m.requests.filter(r => r.path === '/v1/connectors/archive' && r.method === 'POST')).toHaveLength(0)
  await noOverflow(page)
  await noInternals(page)
  const response = page.waitForResponse(r => new URL(r.url()).pathname === '/v1/connectors/archive' && r.request().method() === 'POST')
  await page.getByRole('button', { name: /确认导入|开始导入|^确认$/ }).click()
  expect((await response).ok()).toBeTruthy()
  expect(m.requests.filter(r => r.path === '/v1/connectors/archive' && r.method === 'POST')).toHaveLength(1)
  expect(m.errors).toEqual([])
})

for (const kind of ['importing', 'paused', 'done', 'failed'] as const) {
  test(`W2 ${kind} 的存好/整理进度和操作正确，390px不溢出`, async ({ page }) => {
    const item = batch(kind)
    const m = await mock(page, [item])
    await open(page)
    await expect(page.getByText(item.name, { exact: true })).toBeVisible()
    await expect(page.getByText(new RegExp(`\\b${item.stored}\\b`)).first()).toBeVisible()
    await expect(page.getByText(new RegExp(`\\b${item.organized}\\b`)).first()).toBeVisible()
    await expect(page.locator('body')).toContainText(/整理/)
    await noOverflow(page)
    await noInternals(page)
    if (kind === 'importing') {
      const response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith(`/${item.id}/pause`))
      await page.getByRole('button', { name: /^暂停/ }).click()
      expect((await response).ok()).toBeTruthy()
      await expect(page.getByRole('button', { name: /继续|恢复/ })).toBeVisible()
    } else if (kind === 'paused' || kind === 'failed') {
      if (kind === 'failed') await expect(page.locator('body')).toContainText(/没有读完|继续导入/)
      const response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith(`/${item.id}/resume`))
      await page.getByRole('button', { name: /继续|恢复|重试/ }).click()
      expect((await response).ok()).toBeTruthy()
      await expect(page.getByRole('button', { name: /^暂停/ })).toBeVisible()
    } else {
      await expect(page.locator('body')).toContainText(/完成|已导入/)
      await expect(page.getByRole('button', { name: /^暂停|继续|恢复/ })).toHaveCount(0)
    }
    expect(m.errors).toEqual([])
  })
}

const failures = [
  { error: 'invalid_zip', status: 400, message: '这不是有效的压缩包，请重新选择 ChatGPT 导出文件。' },
  { error: 'no_supported_records', status: 400, message: '压缩包里没有找到聊天记录，请检查导出内容。' },
  { error: 'invalid_json', status: 400, message: '聊天记录文件没有读完，请重新下载后再导入。' },
  { error: 'archive_too_large', status: 413, message: '文件太大，请拆成小一些的文件后再导入。' },
]
test('W3 四种导入失败各有不同的人话，无内部错误码和确认动作', async ({ page }) => {
  const descriptions = new Set<string>()
  for (const failure of failures) {
    await page.unrouteAll({ behavior: 'wait' })
    const m = await mock(page, [], failure)
    await open(page)
    await choose(page)
    const alert = page.getByRole('alert')
    await expect(alert).toBeVisible()
    const description = (await alert.innerText()).trim()
    expect(description).not.toBe('')
    expect(description).not.toContain(failure.error)
    expect(description).toMatch(/文件|压缩包|聊天|记录/)
    expect(description).toMatch(/重新|选择|检查|下载|拆|导出|再/)
    expect(descriptions.has(description)).toBeFalsy()
    descriptions.add(description)
    expect(m.requests.filter(r => r.path === '/v1/connectors/archive' && r.method === 'POST')).toHaveLength(0)
    await noOverflow(page)
    expect(m.errors).toEqual([])
  }
  expect(descriptions.size).toBe(4)
})
