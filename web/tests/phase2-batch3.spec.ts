import { test, expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import type { State } from '../src/domain/types'

const gold = JSON.parse(readFileSync(new URL('../../testdata/phase2/b3-gold.json', import.meta.url), 'utf8'))
const sourceId = 'b3000000-0000-4000-8000-000000000050'
const mentions = [
  { entityId: 'b3000000-0000-4000-8000-000000000051', name: '成都', role: 'place' },
  { entityId: 'b3000000-0000-4000-8000-000000000052', name: '老王', role: 'person' },
]
type Item = { id: string; at: string | null; eventFrom: string; eventTo: string; eventPrecision: string; text: string; status: string; saidLabel: string; eventLabel: string; statusLabel?: string }
const items: Item[] = gold.browser.items

// Check visible calendar parts on the event label itself, independently of order.
async function eventDate(row: ReturnType<Page['locator']>, item: Item) {
  const event = row.locator('.t-event')
  await expect(event).toBeVisible()
  const [year, month, day] = item.eventLabel.split('-')
  const referenceYear = item.at
    ? item.saidLabel.split('-')[0]
    : String(new Intl.DateTimeFormat('en', { year: 'numeric', timeZone: 'Asia/Shanghai' }).format(new Date()))
  if (item.eventPrecision === 'year' || year !== referenceYear) await expect(event).toContainText(`${year}年`)
  if (month) await expect(event).toContainText(`${Number(month)}月`)
  if (day) await expect(event).toContainText(`${Number(day)}日`)
  if (item.eventPrecision === 'range') {
    const [endYear, endMonth, endDay] = gold.rangeDisplayRuling.browserEnd.split('-')
    // A shared month may appear once in "6月12日至14日"; the inclusive final day is required.
    await expect(event).toContainText(new RegExp(`至\\s*(?:${endYear}年)?(?:0?${Number(endMonth)}月)?0?${Number(endDay)}日`))
    if (endYear !== year) await expect(event).toContainText(`${endYear}年`)
    if (endMonth !== month) await expect(event).toContainText(`${Number(endMonth)}月`)
    const excludedDay = Number(gold.rangeDisplayRuling.browserExcludedEnd.split('-')[2])
    await expect(event).not.toContainText(new RegExp(`(?:至\\s*(?:${endYear}年)?(?:0?${Number(endMonth)}月)?|${Number(endMonth)}月)0?${excludedDay}日`))
  }
}
async function backend(page: Page, timelineItems: Item[] = items) {
  const at = '2026-10-02T12:00:00Z'
  const state: State = {
    version: 1, revision: 1, budgetUsage: 0,
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: false, followUps: false, dailyReviewAt: '09:00', timezone: 'Asia/Shanghai' },
    tasks: [], ideas: [], projects: [], memories: [], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], notices: [], excludedMemories: {},
    agents: [{ id: 'model', name: '验收假模型', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['plan'], includeInferred: false }],
  }
  const turn = {
    id: 'b3-turn', text: gold.fixtures.query, reply: gold.browser.reply,
    cards: [
      { kind: 'timeline', items: timelineItems.map(item => ({ at: item.at, eventFrom: item.eventFrom, eventTo: item.eventTo, eventPrecision: item.eventPrecision, mentions, text: item.text, status: item.status, memoryId: item.id, thingId: null })) },
      { kind: 'sources', items: timelineItems.map(item => ({ kind: 'claim', memoryId: item.id, version: 1, sourceId, sourceVersion: 1, text: item.text, at: item.at })) },
    ], receipts: [], ask: null, agent: '验收假模型', createdAt: at,
  }
  const errors: string[] = [], opened: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  let conversation = ''
  await page.route('**/v1/**', route => route.fulfill({ status: 500, json: { error: 'unexpected_b3_request' } }))
  await page.route(url => url.pathname === '/v1/workspace', route => route.fulfill({ json: state }))
  await page.route(url => url.pathname === '/v1/desk/turn', route => {
    conversation = route.request().postDataJSON().conversationId
    return route.fulfill({ json: { conversationId: conversation, turn, state } })
  })
  await page.route(url => url.pathname === '/v1/desk/turns', route => route.fulfill({ json: { conversationId: conversation, turns: [turn] } }))
  await page.route(url => url.pathname === `/v1/memory/sources/${sourceId}`, route => {
    opened.push(route.request().url())
    return route.fulfill({ json: { source: { id: sourceId, version: 1, title: '当时的对话', text: gold.browser.sourceText, recorded_at: at, representation: 'original', has_attachment: false, attachment_missing: false }, derived: [], processing: [] } })
  })
  await page.route(url => url.pathname === '/v1/memory/summary', route => route.fulfill({ json: { text: '', coverage: { gaps: [] }, dependencies: [] } }))
  return { errors, opened }
}
async function ask(page: Page) {
  await page.goto('/')
  const input = page.getByRole('textbox', { name: '跟秘书说' })
  await input.fill(gold.fixtures.query)
  await input.press('Enter')
  await expect(page.locator('.sec-reply')).toHaveText(gold.browser.reply)
}

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 390, height: 844 } })

test('V1 四种状态、说话日期和事件日期、人地点与无日期，390px不溢出', async ({ page }) => {
  const mock = await backend(page)
  await ask(page)
  const timeline = page.locator('.sec-timeline')
  await expect(timeline).toBeVisible()
  await expect(timeline.locator('li')).toHaveCount(4)
  for (const item of items) {
    const row = timeline.locator('li').filter({ hasText: item.text })
    await expect(row).toHaveCount(1)
    await expect(row).toContainText('成都')
    await expect(row).toContainText('老王')
    if (item.at) {
      const said = row.locator('time').first()
      await expect(said).toHaveJSProperty('dateTime', item.at)
      const [year, month, day] = item.saidLabel.split('-')
      await expect(said).toContainText(`${Number(month)}月${Number(day)}日`)
      await expect(said).toContainText(`${year}年`)
    } else await expect(row).toContainText('时间不详')
    await eventDate(row, item)
    if (item.statusLabel) await expect(row).toContainText(item.statusLabel)
    else await expect(row).not.toContainText(/已完成|已取消|后来改过|未完成|未变化|open/)
    await row.scrollIntoViewIfNeeded()
    const bounds = await row.boundingBox()
    expect(bounds).not.toBeNull()
    expect(bounds!.x).toBeGreaterThanOrEqual(0)
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(390)
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  expect(mock.errors).toEqual([])

  // Same-year rows above allow an omitted event year; this row must display its different year.
  const crossYear: Item = gold.secondAcceptanceRuling.browserCrossYearItem
  const crossMock = await backend(page, [crossYear])
  await ask(page)
  const crossRow = page.locator('.sec-timeline li').filter({ hasText: crossYear.text })
  await expect(crossRow).toHaveCount(1)
  await expect(crossRow.locator('time').first()).toHaveJSProperty('dateTime', crossYear.at)
  await eventDate(crossRow, crossYear)
  expect(crossMock.errors).toEqual([])
})

test('V2 点击时间轴条目直接打开当时的原话', async ({ page }) => {
  const mock = await backend(page)
  await ask(page)
  await page.locator('.sec-timeline').getByRole('button', { name: items[0].text }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog.locator('pre').filter({ hasText: gold.browser.sourceText })).toHaveText(gold.browser.sourceText)
  expect(mock.opened).toHaveLength(1)
  expect(new URL(mock.opened[0]).searchParams.get('version')).toBe('1')
  expect(mock.errors).toEqual([])
})
