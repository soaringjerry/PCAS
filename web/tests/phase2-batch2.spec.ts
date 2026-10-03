import { test, expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import type { Memory, State } from '../src/domain/types'

const gold = JSON.parse(readFileSync(new URL('../../testdata/phase2/b2-gold.json', import.meta.url), 'utf8'))
type Mention = { entityId: string; name: string; role: string }
type MemoryFixture = Memory & { expressedAt?: string; eventFrom?: string; eventTo?: string; eventPrecision?: string; mentions: Mention[] }
const at = new Date(Date.now() - 7 * 86400_000).toISOString()
const people = [{ entityId: 'person-wang', name: '老王', count: 1 }, { entityId: 'person-zhang', name: '张三', count: 1 }]
const places = [{ entityId: 'place-chengdu', name: '成都', count: 1 }]
function memory(n: number, details = true): MemoryFixture {
  return {
    id: `memory-${n}`, recordVersion: 1, kind: n % 2 ? 'preference' : 'plan', text: `合成浏览器记忆${String(n).padStart(3, '0')}`,
    epistemic: 'sourced', confirmation: 'adopted', acquisition: 'direct', sources: [], versions: [], visibleTo: ['model'], exposure: 0, lastUsedAt: '', pinned: false,
    halfLifeDays: 30, reinforcementLimit: 8,
    mentions: details ? [{ entityId: 'person-wang', name: '老王', role: 'person' }, { entityId: 'place-chengdu', name: '成都', role: 'place' }] : [],
    ...(details ? { expressedAt: at, eventFrom: '2025-03-01T00:00:00+08:00', eventTo: '2025-04-01T00:00:00+08:00', eventPrecision: 'month' } : {}),
  }
}
async function backend(page: Page, all: MemoryFixture[], projects: State['projects'] = []) {
  const state: State & { memoryTotal: number } = {
    version: 1, revision: 1, budgetUsage: 0,
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: false, followUps: false, dailyReviewAt: '09:00', timezone: 'Asia/Shanghai' },
    tasks: [], ideas: [], projects, memories: all.slice(0, 200), memoryTotal: all.length,
    candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], notices: [], activity: [], excludedMemories: {},
    agents: [{ id: 'model', name: '验收假模型', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact', 'plan', 'preference'], includeInferred: false }],
  }
  const queries: URL[] = []
  const returned: string[] = []
  const errors: string[] = []
  page.on('pageerror', e => errors.push(e.message))
  await page.route('**/v1/**', route => route.fulfill({ status: 500, json: { error: 'unexpected_b2_request' } }))
  await page.route(url => url.pathname === '/v1/workspace', route => route.fulfill({ json: state }))
  await page.route(url => url.pathname === '/v1/workspace/memory-facets', route => route.fulfill({ json: { people, places } }))
  await page.route(url => url.pathname === '/v1/workspace/memories', route => {
    const url = new URL(route.request().url())
    queries.push(url)
    const entity = url.searchParams.get('entity'), nature = url.searchParams.get('nature'), q = url.searchParams.get('q')
    const project = url.searchParams.get('project'), epistemic = url.searchParams.get('epistemic'), agent = url.searchParams.get('agent')
    const filtered = all.filter(m => (!entity || m.mentions.some(x => x.entityId === entity)) && (!nature || nature === m.kind) && (!q || m.text.includes(q)) && (!project || m.projectId === project) && (!epistemic || m.epistemic === epistemic) && (!agent || m.visibleTo.includes(agent)))
    const start = Number(url.searchParams.get('cursor') ?? '0'), limit = Math.min(Number(url.searchParams.get('limit') ?? '50'), 100)
    const items = filtered.slice(start, start + limit)
    returned.push(...items.map(m => m.id))
    return route.fulfill({ json: { items, next: start + limit < filtered.length ? String(start + limit) : '', total: filtered.length } })
  })
  await page.route(url => /^\/v1\/workspace\/memories\/[^/]+$/.test(url.pathname), route => {
    const id = new URL(route.request().url()).pathname.split('/').at(-1)
    const found = all.find(m => m.id === id)
    return route.fulfill({ status: found ? 200 : 404, json: found ?? { error: 'not_found' } })
  })
  return { queries, returned, errors }
}
async function card(page: Page, text: string) {
  const line = page.getByText(text, { exact: true })
  await expect(line).toBeVisible()
  // Existing card/list semantics only. No data-test hook is required of U2.
  return line.locator('xpath=ancestor::*[self::article or self::li or contains(@class,"mem-row") or contains(@class,"memory-card") or contains(@class,"mem-card") or contains(concat(" ",normalize-space(@class)," ")," card ")][1]')
}
async function more(page: Page) {
  const button = page.getByRole('button', { name: /加载更多|查看更多|更多记忆|下一页/ })
  if (await button.count()) await button.click()
  else { await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight)); await page.mouse.wheel(0, 1600) }
}

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 390, height: 844 } })

test('U1 记忆卡片显示人地点说话和事件日期，没有内容的卡片无空位及内部说法', async ({ page }) => {
  const rich = memory(0), bare = memory(1, false)
  const mock = await backend(page, [rich, bare])
  await page.goto('/library?tab=memory')
  const richCard = await card(page, rich.text)
  await expect(richCard).toContainText(gold.sequences.U1.people[0])
  await expect(richCard).toContainText(gold.sequences.U1.places[0])
  await expect(richCard).toContainText(/2025.*3|3.*月/)
  const parts = new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', month: 'numeric', day: 'numeric' }).formatToParts(new Date(at))
  for (const key of ['month', 'day']) await expect(richCard).toContainText(parts.find(p => p.type === key)!.value)
  const bareCard = await card(page, bare.text)
  await expect(bareCard).not.toContainText(/老王|成都|未知时间|暂无人物|暂无地点|无事件时间|—|--/)
  for (const forbidden of gold.sequences.U1.forbidden) { await expect(richCard).not.toContainText(forbidden); await expect(bareCard).not.toContainText(forbidden) }
  expect(mock.queries.length).toBeGreaterThan(0)
  expect(mock.errors).toEqual([])
})

test('U2 点人后仅保留提到他的记忆，清掉筛选后恢复；地点和性质筛选传给接口', async ({ page }) => {
  const wang = memory(0), zhang = memory(1)
  zhang.mentions = [{ entityId: 'person-zhang', name: '张三', role: 'person' }]
  const mock = await backend(page, [wang, zhang])
  await page.goto('/library?tab=memory')
  await expect(page.getByText(zhang.text, { exact: true })).toBeVisible()
  await page.getByRole('button', { name: /老王/ }).first().click()
  await expect.poll(() => mock.queries.at(-1)?.searchParams.get('entity')).toBe('person-wang')
  await expect(page.getByText(wang.text, { exact: true })).toBeVisible()
  await expect(page.getByText(zhang.text, { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: /清除筛选|清空筛选|全部记忆|所有记忆|全部/ }).first().click()
  await expect(page.getByText(zhang.text, { exact: true })).toBeVisible()
  await page.getByRole('button', { name: /成都/ }).first().click()
  await expect.poll(() => mock.queries.at(-1)?.searchParams.get('entity')).toBe('place-chengdu')
  await expect(page.getByText(zhang.text, { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: /清除筛选|清空筛选|全部记忆|所有记忆|全部/ }).first().click()
  const nature = page.getByRole('combobox', { name: /性质/ })
  if (await nature.count()) await nature.selectOption('plan')
  else await page.getByRole('button', { name: '计划', exact: true }).click()
  await expect.poll(() => mock.queries.at(-1)?.searchParams.get('nature')).toBe('plan')
  await expect(page.getByText(wang.text, { exact: true })).toBeVisible()
  await expect(page.getByText(zhang.text, { exact: true })).toHaveCount(0)
  expect(mock.errors).toEqual([])
})

test('U3 300条记忆从接口翻至末尾不重复，390px无横向溢出', async ({ page }) => {
  const all = Array.from({ length: gold.sequences.U3.total }, (_, n) => memory(n, false))
  const mock = await backend(page, all)
  await page.goto('/library?tab=memory')
  await expect(page.getByText(all[0].text, { exact: true })).toBeVisible()
  for (let pageNo = 1; pageNo <= 10 && !mock.returned.includes(all.at(-1)!.id); pageNo++) {
    const before = mock.returned.length
    await more(page)
    await expect.poll(() => mock.returned.length).toBeGreaterThan(before)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  }
  await expect(page.getByText(all.at(-1)!.text, { exact: true })).toBeVisible()
  await expect(page.getByText(/^合成浏览器记忆\d{3}$/, { exact: true })).toHaveCount(gold.sequences.U3.total)
  for (const m of all) await expect(page.getByText(m.text, { exact: true })).toHaveCount(1)
  expect(mock.returned).toHaveLength(gold.sequences.U3.total)
  expect(new Set(mock.returned).size).toBe(gold.sequences.U3.unique)
  expect(mock.queries.length).toBeGreaterThan(1)
  expect(mock.queries.slice(1).every(q => q.searchParams.get('cursor'))).toBeTruthy()
  expect(mock.errors).toEqual([])
})

test('U1 事件区间卡片显示实际最后一天', async ({ page }) => {
  const spec = gold.supplement_0f8e759.U1
  const ranged = memory(0)
  delete ranged.expressedAt
  ranged.eventFrom = spec.event_from
  ranged.eventTo = spec.event_to
  ranged.eventPrecision = 'range'
  await backend(page, [ranged])
  await page.goto('/library?tab=memory')
  const rangeCard = await card(page, ranged.text)
  await expect(rangeCard).toContainText(spec.first_display_day)
  await expect(rangeCard).toContainText(spec.last_display_day)
  await expect(rangeCard).not.toContainText(spec.excluded_display_day)
})
