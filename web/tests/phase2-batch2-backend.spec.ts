import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { command, fixture, input, login, say, evidence } from './support/real'

const gold = JSON.parse(readFileSync(new URL('../../testdata/phase2/b2-gold.json', import.meta.url), 'utf8'))
const captureURL = process.env.PCAS_TEST_B1_CAPTURE_URL
if (!captureURL) throw new Error('Run support/real-backend.sh with full local model request capture')
test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 390, height: 844 } })
test.afterEach(async ({ page }, info) => { await evidence(page, info, 'phase2-b2') })

test('U4 真实秘书原话经后台变成带成都老王日期的记忆，点老王可筛出', async ({ page }) => {
  await login(page)
  await command(page, { type: 'updateSettings', patch: { timezone: 'Asia/Shanghai' } })
  const today = new Date(Date.now() + 8 * 3600_000)
  const next = new Date(today)
  next.setUTCDate(today.getUTCDate() + gold.time_oracle.next_week_monday_days_by_weekday_sun0[today.getUTCDay()])
  const end = new Date(next)
  end.setUTCDate(next.getUTCDate() + gold.time_oracle.next_week_duration_calendar_days)
  const item = { kind: 'memory', nature: 'plan', text: gold.fixtures.trip.memory, subject: '我', predicate: '旅行计划', quote: gold.fixtures.trip.text, acquisition: 'direct', confidence: 1, explicit: true,
    people: ['老王'], places: ['成都', '春熙路'], organizations: [], when: { from: next.toISOString().slice(0, 10), to: end.toISOString().slice(0, 10), precision: 'range', quote: '下周' } }
  await fixture(page, [
    { kind: 'secretary', match: gold.fixtures.trip.text, content: '{"reply":"收到旅行安排","actions":[]}' },
    { kind: 'extraction', match: gold.fixtures.trip.text, content: JSON.stringify({ items: [item] }) },
    { kind: 'extraction', match: '', content: '{"items":[]}' },
  ])
  expect((await page.request.delete(`${captureURL}/b1/requests`)).ok()).toBeTruthy()
  await say(page, gold.fixtures.trip.text)
  await input(page).press('Escape')
  type WireMemory = { id: string; text: string; mentions: { entityId: string; name: string; role: string }[]; eventFrom?: string; eventTo?: string; expressedAt?: string; confirmation: string }
  let found: WireMemory | undefined
  await expect.poll(async () => {
    const response = await page.request.get(`/v1/workspace/memories?q=${encodeURIComponent(gold.fixtures.trip.memory)}`)
    expect(response.ok()).toBeTruthy()
    const body: { items: WireMemory[] } = await response.json()
    found = body.items.find(m => m.text === gold.fixtures.trip.memory)
    return found?.mentions.map(x => x.name).sort()
  }, { timeout: 30_000 }).toEqual(['成都', '春熙路', '老王'].sort())
  expect(found?.confirmation).toBe('adopted')
  expect(found?.expressedAt).toBeTruthy()
  expect(new Date(found!.eventFrom!).getTime()).toBe(new Date(`${item.when.from}T00:00:00+08:00`).getTime())
  expect(new Date(found!.eventTo!).getTime()).toBe(new Date(`${item.when.to}T00:00:00+08:00`).getTime())
  const captured = await (await page.request.get(`${captureURL}/b1/requests`)).json() as { requests: { messages: { role: string; content: string }[] }[] }
  const actual = captured.requests.filter(r => r.messages.some(m => m.content.includes('从原文提取独立线索')))
  // Match the actual source in a non-system message; no fabricated prompt.
  expect(actual.some(r => r.messages.some(m => m.role !== 'system' && m.content.includes(gold.fixtures.trip.text)))).toBeTruthy()
  await page.goto('/library?tab=memory')
  await expect(page.getByText(gold.fixtures.trip.memory, { exact: true })).toBeVisible()
  await expect(page.getByText('成都', { exact: true }).first()).toBeVisible()
  // Since phase 2.6 a person is a group in the directory, read through the group's own list.
  const request = page.waitForRequest(r => /^\/v1\/workspace\/memory-groups\/[^/]+\/memories$/.test(new URL(r.url()).pathname))
  await page.getByRole('button', { name: /老王/ }).first().click()
  await request
  const person = found!.mentions.find(x => x.name === '老王')!
  const filtered = await page.request.get(`/v1/workspace/memories?entity=${person.entityId}`)
  expect(filtered.ok()).toBeTruthy()
  const body: { items: WireMemory[] } = await filtered.json()
  expect(body.items.length).toBeGreaterThan(0)
  expect(body.items.every(m => m.mentions.some(x => x.entityId === person.entityId))).toBeTruthy()
  await expect(page.getByText(gold.fixtures.trip.memory, { exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
})
