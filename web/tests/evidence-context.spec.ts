import { test, expect, type Page } from '@playwright/test'
import type { Memory, State } from '../src/domain/types'

// Fictional fixtures only. Production memories and screenshots never enter git.
const id = 'e4000000-0000-4000-8000-000000000001'
const at = '2024-01-01T10:00:00Z'
async function backend(page: Page, failOnce = false, withConversation = true) {
  const memory: Memory = { id: 'fiction-memory', recordVersion: 1, kind: 'intention', text: '那位同事的演示项目值得联系。', contextDependent: true, epistemic: 'sourced', confirmation: 'candidate', acquisition: 'direct', sources: [{ sourceId: id, version: 1, label: '虚构来源', excerpt: '我想给那位同事发个邮件。', at }], versions: [], visibleTo: [], exposure: 0, lastUsedAt: '', pinned: false }
  const state: State = { version: 1, revision: 1, budgetUsage: 0, settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: false, followUps: false, dailyReviewAt: '09:00', timezone: 'Asia/Shanghai' }, tasks: [], ideas: [], projects: [], memories: [memory], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], notices: [], activity: [], excludedMemories: {}, agents: [] }
  const errors: string[] = []
  page.on('pageerror', e => errors.push(e.message))
  await page.route('**/v1/**', route => route.fulfill({ status: 500, json: { error: 'unexpected_fiction_request' } }))
  await page.route(url => url.pathname === '/v1/workspace', route => route.fulfill({ json: state }))
  await page.route(url => url.pathname === '/v1/workspace/about', route => route.fulfill({ json: { handover: { body: '', builtAt: '', stale: false }, deadlines: [] } }))
  await page.route(url => url.pathname === '/v1/workspace/memory-facets', route => route.fulfill({ json: { people: [], places: [] } }))
  await page.route(url => url.pathname === '/v1/workspace/memories', route => route.fulfill({ json: { items: [memory], next: '', total: 1 } }))
  await page.route(url => url.pathname === '/v1/workspace/memories/fiction-memory', route => route.fulfill({ json: memory }))
  await page.route(url => url.pathname === `/v1/memory/sources/${id}`, route => route.fulfill({ json: { context: withConversation ? { conversation: 'fiction' } : undefined, source: { id, version: 1, title: '虚构对话', text: memory.sources[0].excerpt, recorded_at: at, representation: 'original', has_attachment: false, attachment_missing: false }, derived: [], processing: [] } }))
  const msg = (key: string, role: string, text: string, anchor = false) => ({ id: key, version: 1, text, role, anchor, expressed_at: at, recorded_at: at })
  const current = [msg('before', 'assistant', 'AI 建议交流技术方案。'), msg(id, 'user', memory.sources[0].excerpt!, true), msg('after', 'user', '仅讨论演示方案，尚未决定合作。')]
  await page.route(url => url.pathname === `/v1/memory/sources/${id}/conversation`, route => {
    const url = new URL(route.request().url())
    if (failOnce) { failOnce = false; return route.fulfill({ status: 503, json: { error: 'unavailable' } }) }
    if (url.searchParams.has('before')) return route.fulfill({ json: { messages: [msg('earliest', 'user', '测试人物甲是演示项目的联系人。'), msg('older', 'assistant', '虚构上文。')], gaps: [] } })
    if (url.searchParams.has('after')) return route.fulfill({ json: { messages: [current[2], msg('latest', 'user', '这是一次虚构资料核对。')], gaps: [] } })
    return route.fulfill({ json: { messages: current, before: 'before', after: 'after', gaps: [] } })
  })
  await page.goto('/library?tab=memory')
  await page.getByText(memory.text, { exact: true }).click()
  await expect(page.getByText('这条记忆含有指代', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: /虚构来源/ }).click()
  if (withConversation) await page.getByText('看当时的对话', { exact: true }).click()
  return errors
}

for (const width of [390, 1280]) {
  test(`evidence context keeps roles, limitations and paginated history at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const errors = await backend(page)
    const context = page.locator('.conversation-context')
    await expect(context.locator('.conversation-message')).toHaveCount(3)
    await expect(context.getByLabel('引用原话')).toContainText('我想给那位同事发个邮件。')
    await expect(context).toContainText('尚未决定合作')
    await expect(context).toContainText('AI 的回复用于理解上下文')
    await expect(context).toContainText('说于')
    await context.getByRole('button', { name: '更早的消息' }).click()
    await expect(context).toContainText('测试人物甲')
    await context.getByRole('button', { name: '更晚的消息' }).click()
    await expect(context.locator('.conversation-message')).toHaveCount(6)
    await expect(context.getByRole('button', { name: '更早的消息' })).toHaveCount(0)
    await expect(context.getByRole('button', { name: '更晚的消息' })).toHaveCount(0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
    expect(errors).toEqual([])
  })
}

test('failed context read can retry while the original remains visible', async ({ page }) => {
  await backend(page, true)
  await expect(page.getByRole('alert')).toContainText('当时的对话暂时无法读取')
  await expect(page.locator('.source-words')).toContainText('我想给那位同事发个邮件。')
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.locator('.conversation-message')).toHaveCount(3)
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('a source without conversation keeps the original expansion control', async ({ page }) => {
  const errors = await backend(page, false, false)
  await page.getByText('展开原文', { exact: true }).click()
  await expect(page.getByText('展开原文', { exact: true }).locator('..').locator('pre')).toContainText('我想给那位同事发个邮件。')
  await expect(page.getByText('看当时的对话', { exact: true })).toHaveCount(0)
  expect(errors).toEqual([])
})
