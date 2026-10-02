import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { fixture, input, login, say, snapshot, evidence } from './support/real'

const gold = JSON.parse(readFileSync(new URL('../../testdata/phase2/b1-gold.json', import.meta.url), 'utf8'))
const text: string = gold.fixtures.trip.text
const captureURL = process.env.PCAS_TEST_B1_CAPTURE_URL
if (!captureURL) throw new Error('Run web/tests/support/phase2-backend.sh; full model-request capture is required')

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 390, height: 844 } })
test.afterEach(async ({ page }, info) => { await evidence(page, info, 'phase2-b1') })

test('P1/P8 页面原话→新对话→真实请求与来源卡片，删除后不再供给', async ({ page }) => {
  await login(page)
  // All originals are spoken through the page and keep the online duplicate
  // title shape. No extraction can fabricate a claim for this test.
  for (let n = 0; n < 12; n++) {
    const detail = `合成无关资料${n}，地点${n}安排朋友${n}`
    await fixture(page, [
      { kind: 'secretary', match: detail, content: '{"reply":"收到。","actions":[]}' },
      { kind: 'extraction', match: '', content: '{"items":[]}' },
    ])
    await say(page, detail)
    await input(page).press('Escape')
  }
  await fixture(page, [
    { kind: 'secretary', match: text, content: '{"reply":"收到。","actions":[]}' },
    { kind: 'extraction', match: '', content: '{"items":[]}' },
  ])
  const original = await say(page, text)
  await input(page).press('Escape')
  const reset = await page.request.delete(`${captureURL}/b1/requests`)
  expect(reset.ok()).toBeTruthy()
  await fixture(page, [
    { kind: 'secretary', match: '我去成都想吃什么', content: '{"reply":"春熙路火锅，见老王。","used":["S1"],"actions":[]}' },
    { kind: 'extraction', match: '', content: '{"items":[]}' },
  ])
  const answer = await say(page, '我去成都想吃什么')
  expect(answer.conversationId).not.toBe(original.conversationId)
  const response = await page.request.get(`${captureURL}/b1/requests`)
  expect(response.ok()).toBeTruthy()
  const captured: { requests: { messages: { role: string; content: string }[] }[] } = await response.json()
  const secretaries = captured.requests.filter(r => r.messages.some(m => m.role === 'system' && m.content.includes('前台秘书')))
  expect(secretaries).toHaveLength(1)
  const actualPrompt = secretaries[0].messages.filter(m => m.role !== 'system').map(m => m.content).join('\n')
  expect(actualPrompt).toContain(text)
  expect(actualPrompt).toContain('相关原话')
  const state = await snapshot(page)
  expect(state.memories.filter(m => m.text.includes('春熙路') || m.text.includes('老王'))).toHaveLength(0)
  const sources: { kind: string; sourceId: string; sourceVersion: number; text: string }[] = answer.turn.cards.filter((c: { kind: string }) => c.kind === 'sources').flatMap((c: { items: unknown[] }) => c.items)
  expect(sources).toHaveLength(1)
  expect(sources[0]).toMatchObject({ kind: 'source', text })
  const source = page.getByRole('list', { name: '依据' }).getByRole('button', { name: text })
  await expect(source).toBeVisible()
  await source.click()
  await expect(page.getByRole('dialog').locator('pre').filter({ hasText: text })).toBeVisible()
  await expect(page.getByRole('dialog').locator('pre')).toHaveText(text)
  // Approved R8a exception: when the excerpt is the entire original, no mark.
  await expect(page.getByRole('dialog').locator('mark')).toHaveCount(0)
  await expect(page.getByRole('dialog')).not.toContainText(/第\s*\d+\s*版|摘要|source\.extract|queued/)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  const deleted = await page.request.post('/v1/memory/delete', { data: { targets: [{ id: sources[0].sourceId, version: sources[0].sourceVersion, kind: 'source' }] } })
  expect(deleted.ok(), await deleted.text()).toBeTruthy()
  await page.reload()
  await input(page).press('Escape')
  expect((await page.request.delete(`${captureURL}/b1/requests`)).ok()).toBeTruthy()
  await fixture(page, [
    { kind: 'secretary', match: '我去成都想吃什么', content: '{"reply":"资料已删","used":[],"actions":[]}' },
    { kind: 'extraction', match: '', content: '{"items":[]}' },
  ])
  await say(page, '我去成都想吃什么')
  const control: typeof captured = await (await page.request.get(`${captureURL}/b1/requests`)).json()
  const actualControl = control.requests.filter(r => r.messages.some(m => m.role === 'system' && m.content.includes('前台秘书')))
  expect(actualControl).toHaveLength(1)
  expect(actualControl[0].messages.map(m => m.content).join('\n')).not.toContain(text)
})

for (const exactQuery of [false, true]) {
test(`P6/R8a 带换行的长资料${exactQuery ? '唯一中段查询对照' : ''}：实际中段摘录能标记并定位在可见区域`, async ({ page }, info) => {
  const spec = gold.integration_supplement_2026_10_02.long_browser
  const fullText = [
    ...Array<string>(spec.prefix_repetitions).fill(spec.prefix_line),
    ...spec.middle_lines,
    ...Array<string>(spec.suffix_repetitions).fill(spec.suffix_line),
  ].join('\n')
  const question: string = exactQuery ? gold.integration_supplement_2026_10_02.long_browser_exact_query_control.question : spec.question
  expect(Array.from(fullText).length).toBeGreaterThan(600)
  expect(fullText).toContain('\n')
  await login(page, '/library?tab=sources')
  await fixture(page, [{ kind: 'extraction', match: '', content: '{"items":[]}' }])
  await page.getByRole('button', { name: '导入资料', exact: true }).click()
  const importing = page.getByRole('dialog')
  await importing.getByLabel('标题', { exact: true }).fill(spec.title)
  await importing.getByLabel('原文', { exact: true }).fill(fullText)
  await importing.getByRole('button', { name: '导入', exact: true }).click()
  await expect(importing).toHaveCount(0)
  await page.goto('/')
  await input(page).press('Escape')
  expect((await page.request.delete(`${captureURL}/b1/requests`)).ok()).toBeTruthy()
  await fixture(page, [
    { kind: 'secretary', match: question, content: JSON.stringify({ reply: spec.reply, used: ['S1'], actions: [] }) },
    { kind: 'extraction', match: '', content: '{"items":[]}' },
  ])
  const answer = await say(page, question)
  const captured = await (await page.request.get(`${captureURL}/b1/requests`)).json()
  const requests = captured.requests.filter((r: { messages: { role: string; content: string }[] }) => r.messages.some(m => m.role === 'system' && m.content.includes('前台秘书')))
  expect(requests).toHaveLength(1)
  expect.soft(requests[0].messages.map((m: { content: string }) => m.content).join('\n')).toContain('青色灯塔7319')
  const sources: { kind: string; sourceId: string; sourceVersion: number; text: string }[] = answer.turn.cards.filter((c: { kind: string }) => c.kind === 'sources').flatMap((c: { items: unknown[] }) => c.items)
  expect(sources).toHaveLength(1)
  expect(sources[0].kind).toBe('source')
  expect.soft(sources[0].text).toContain('青色灯塔7319')
  const source = await (await page.request.get(`/v1/memory/sources/${sources[0].sourceId}?version=${sources[0].sourceVersion}`)).json()
  expect(source.source.text).toBe(fullText)
  // Keep the exact backend text, including whitespace, to diagnose matching.
  await info.attach('long-source-excerpt', { body: JSON.stringify({ excerpt: sources[0].text, original: source.source.text, exactMatch: fullText.includes(sources[0].text), whitespaceCollapsedMatch: fullText.replace(/\s+/g, ' ').includes(sources[0].text.replace(/…$/, '')) }, null, 2), contentType: 'application/json' })
  const quoted = page.getByRole('list', { name: '依据' }).getByRole('button')
  await expect(quoted).toHaveCount(1)
  await quoted.click()
  const dialog = page.getByRole('dialog')
  const original = dialog.locator('pre')
  await expect(original).toBeVisible()
  expect(await original.textContent()).toBe(fullText)
  const mark = original.locator('mark')
  await expect(mark).toHaveCount(1)
  await expect.soft(mark).toContainText('青色灯塔7319')
  await expect(mark).toBeInViewport()
  await expect(dialog.getByText('展开原文', { exact: true })).toHaveCount(0)
  await expect(dialog).not.toContainText(/第\s*\d+\s*版|摘要/)
})
}
