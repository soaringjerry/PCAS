import { test, expect, type Page } from '@playwright/test'
import type { Memory, MemoryCategory, MemoryGroup, MemoryGroupFacet, MemoryMention, Organize, State } from '../src/domain/types'

// Everything here is made up: the people, the places, the projects and what was said.
type Fixture = Memory & { mentions: MemoryMention[]; groups: MemoryGroup[] }

const garden: MemoryGroup = { entityId: 'project-garden', name: '阳台菜园改造', type: 'project' }
const coffee: MemoryGroup = { entityId: 'topic-coffee', name: '手冲咖啡', type: 'topic' }
const running: MemoryGroup = { entityId: 'topic-running', name: '夜跑', type: 'topic' }
const food: MemoryGroup = { entityId: 'area-food', name: '饮食', type: 'area' }
const health: MemoryGroup = { entityId: 'area-health', name: '健康', type: 'area' }
const linqi: MemoryMention = { entityId: 'person-linqi', name: '林栖', role: 'person' }
const town: MemoryMention = { entityId: 'place-yunxiu', name: '云岫镇', role: 'place' }

const usedAt = new Date(Date.now() - 2 * 86400_000).toISOString()
let serial = 0
function memory(text: string, category: MemoryCategory | undefined, groups: MemoryGroup[], mentions: MemoryMention[] = []): Fixture {
  return {
    id: `memory-${++serial}`, recordVersion: 1, kind: 'preference', text,
    epistemic: 'sourced', confirmation: 'adopted', acquisition: 'direct', sources: [], versions: [], visibleTo: ['model'], exposure: 1, lastUsedAt: usedAt, pinned: false,
    halfLifeDays: 30, reinforcementLimit: 8, mentions, groups,
    ...(category ? { category, durable: true } : {}),
  }
}

const tomato = () => memory('阳台的番茄苗要在四月前移到大盆里', 'goal', [garden, food])
const beans = () => memory('林栖推荐的浅烘豆子太酸，下次换中烘', 'taste', [coffee, food], [linqi])
const brew = () => memory('回答冲煮问题时先给水粉比，再讲原理', 'rule', [coffee])
const laps = () => memory('上周在云岫镇河堤夜跑了三次', 'progress', [running, health], [town])
const unsorted = () => memory('林栖下个月搬到云岫镇', 'unknown', [], [linqi, town])

function facetsOf(all: Fixture[]) {
  const count = <T extends { entityId: string }>(found: T[]) => {
    const seen = new Map<string, T & { count: number }>()
    for (const one of found) seen.set(one.entityId, { ...one, count: (seen.get(one.entityId)?.count ?? 0) + 1 })
    return [...seen.values()].sort((a, b) => b.count - a.count)
  }
  const order = ['project', 'topic', 'area']
  const groups: MemoryGroupFacet[] = count(all.flatMap((m) => m.groups)).sort((a, b) => order.indexOf(a.type) - order.indexOf(b.type))
  const mentions = all.flatMap((m) => m.mentions)
  return { groups, people: count(mentions.filter((m) => m.role === 'person')), places: count(mentions.filter((m) => m.role === 'place')) }
}

async function backend(page: Page, all: Fixture[], organize?: Organize, facets = facetsOf(all)) {
  const state: State = {
    version: 1, revision: 1, budgetUsage: 0,
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: false, followUps: false, dailyReviewAt: '09:00', timezone: 'Asia/Shanghai' },
    tasks: [], ideas: [], projects: [], memories: all.slice(0, 200), memoryTotal: all.length,
    candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], notices: [], activity: [], excludedMemories: {},
    agents: [{ id: 'model', name: '演示模型', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['preference'], includeInferred: false }],
    ...(organize ? { organize } : {}),
  }
  const queries: URL[] = []
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  await page.route('**/v1/**', (route) => route.fulfill({ status: 500, json: { error: 'unexpected_p25_b1_request' } }))
  await page.route((url) => url.pathname === '/v1/workspace', (route) => route.fulfill({ json: state }))
  await page.route((url) => url.pathname === '/v1/workspace/memory-facets', (route) => route.fulfill({ json: facets }))
  await page.route((url) => url.pathname === '/v1/workspace/memories', (route) => {
    const url = new URL(route.request().url())
    queries.push(url)
    const entity = url.searchParams.get('entity'), group = url.searchParams.get('group'), q = url.searchParams.get('q')
    const items = all.filter((m) => (!entity || m.mentions.some((x) => x.entityId === entity)) && (!group || m.groups.some((x) => x.entityId === group)) && (!q || m.text.includes(q)))
    return route.fulfill({ json: { items, next: '', total: items.length } })
  })
  return { state, queries, errors }
}

const open = (page: Page) => page.goto('/library?tab=memory')
const row = (page: Page, name: string) => page.getByRole('group', { name, exact: true })
const line = (page: Page, text: string) => page.getByText(text, { exact: true })
const card = (page: Page, text: string) => page.locator('.mem-entry').filter({ hasText: text })
const lastQuery = (mock: { queries: URL[] }, name: string) => mock.queries.at(-1)?.searchParams.get(name) ?? null
const fits = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)
/** Words for how the sorting works inside; none of them belongs on this page. */
const internals = /规则版本|落后|批|实体/

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 1280, height: 900 } })
test.afterEach(async ({ page }, info) => {
  if (info.status === 'passed') await expect(page.locator('body')).not.toContainText(internals)
})

test('R17 项目、主题、领域各一行，带名字和条数；点一个只看它下面的，再点一次取消', async ({ page }) => {
  const all = [tomato(), beans(), brew(), laps(), unsorted()]
  const mock = await backend(page, all)
  await open(page)
  await expect(row(page, '项目').getByRole('button')).toHaveText(['阳台菜园改造1'])
  await expect(row(page, '主题').getByRole('button')).toHaveText(['手冲咖啡2', '夜跑1'])
  await expect(row(page, '领域').getByRole('button')).toHaveText(['饮食2', '健康1'])
  await expect(page.locator('.mem-entry')).toHaveCount(5)

  const chip = row(page, '主题').getByRole('button', { name: /手冲咖啡/ })
  await chip.click()
  await expect.poll(() => lastQuery(mock, 'group')).toBe(coffee.entityId)
  await expect(chip).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('.mem-entry')).toHaveCount(2)
  await expect(line(page, all[1].text)).toBeVisible()
  await expect(line(page, all[2].text)).toBeVisible()
  await expect(page.getByText('「手冲咖啡」下面的记忆有 2 条')).toBeVisible()
  expect(new URL(page.url()).searchParams.get('group')).toBe(coffee.entityId)

  await chip.click()
  await expect(page.locator('.mem-entry')).toHaveCount(5)
  await expect(chip).toHaveAttribute('aria-pressed', 'false')
  expect(lastQuery(mock, 'group')).toBeNull()
  expect(mock.errors).toEqual([])
})

test('R17 分组和按人、搜索同时生效；清掉筛选一起清', async ({ page }) => {
  const all = [tomato(), beans(), brew(), laps(), unsorted()]
  const mock = await backend(page, all)
  await open(page)
  await row(page, '领域').getByRole('button', { name: /饮食/ }).click()
  await expect(page.locator('.mem-entry')).toHaveCount(2)
  await row(page, '提到的人').getByRole('button', { name: /林栖/ }).click()
  await expect.poll(() => lastQuery(mock, 'entity')).toBe(linqi.entityId)
  expect(lastQuery(mock, 'group')).toBe(food.entityId)
  await expect(page.locator('.mem-entry')).toHaveCount(1)
  await expect(line(page, all[1].text)).toBeVisible()
  await expect(page.getByText('「饮食」下面提到「林栖」的记忆有 1 条')).toBeVisible()

  await page.getByRole('textbox', { name: '搜索记忆' }).fill('中烘')
  await expect.poll(() => lastQuery(mock, 'q')).toBe('中烘')
  expect(lastQuery(mock, 'group')).toBe(food.entityId)
  expect(lastQuery(mock, 'entity')).toBe(linqi.entityId)
  await expect(page.locator('.mem-entry')).toHaveCount(1)

  await page.getByRole('button', { name: '清掉筛选' }).click()
  await expect(page.locator('.mem-entry')).toHaveCount(5)
  for (const name of ['group', 'entity', 'q']) expect(lastQuery(mock, name)).toBeNull()
  expect(mock.errors).toEqual([])
})

test('R17 只按人翻时和原来一样，请求不带分组', async ({ page }) => {
  const all = [tomato(), beans(), laps(), unsorted()]
  const mock = await backend(page, all)
  await open(page)
  await row(page, '地点').getByRole('button', { name: /云岫镇/ }).click()
  await expect.poll(() => lastQuery(mock, 'entity')).toBe(town.entityId)
  expect(lastQuery(mock, 'group')).toBeNull()
  await expect(page.locator('.mem-entry')).toHaveCount(2)
  await expect(page.getByText('提到「云岫镇」的记忆有 2 条')).toBeVisible()
  expect(mock.errors).toEqual([])
})

test('R17/R19 卡片上有分组和类型的中文叫法；点卡片上的分组等于按它筛选；没整理过的不显示类型', async ({ page }) => {
  const all = [tomato(), beans(), brew(), laps(), unsorted(), memory('这条还没有任何标签', undefined, [])]
  const mock = await backend(page, all)
  await open(page)
  const labels = ['目标', '口味', '对助手的要求', '进展']
  for (const [i, label] of labels.entries()) await expect(card(page, all[i].text).locator('.tag').filter({ hasText: label })).toHaveText(label)
  for (const bare of [all[4], all[5]]) {
    await expect(card(page, bare.text)).not.toContainText(/身份|口味|对助手的要求|目标|进展|一次性的事|看法|关于别人|unknown|未知/)
  }
  // A sorted memory shows its type in place of its nature; one not sorted yet keeps the nature.
  for (const sorted of all.slice(0, 4)) await expect(card(page, sorted.text).locator('.meta')).not.toContainText(/事实|偏好|决定|意向|计划/)
  for (const bare of [all[4], all[5]]) await expect(card(page, bare.text).locator('.meta')).toContainText('偏好')
  // Whether it will still hold in half a year is not shown in this round.
  await expect(page.locator('.list')).not.toContainText(/长期|durable/)
  await expect(card(page, all[5].text).locator('.mem-marks')).toHaveCount(0)

  const beansCard = card(page, all[1].text)
  await expect(beansCard.getByRole('button', { name: '手冲咖啡', exact: true })).toBeVisible()
  await expect(beansCard.getByRole('button', { name: '饮食', exact: true })).toBeVisible()
  await expect(beansCard.getByRole('button', { name: '林栖', exact: true })).toBeVisible()

  await beansCard.getByRole('button', { name: '手冲咖啡', exact: true }).click()
  await expect.poll(() => lastQuery(mock, 'group')).toBe(coffee.entityId)
  await expect(page.locator('.mem-entry')).toHaveCount(2)
  await expect(row(page, '主题').getByRole('button', { name: /手冲咖啡/ })).toHaveAttribute('aria-pressed', 'true')
  // Narrowing from a card does not open it.
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await card(page, all[1].text).getByRole('button', { name: '手冲咖啡', exact: true }).click()
  await expect(page.locator('.mem-entry')).toHaveCount(6)

  await line(page, all[2].text).click()
  const sheet = page.getByRole('dialog')
  await expect(sheet.getByRole('heading', { name: '对助手的要求' })).toBeVisible()
  await expect(sheet.locator('.side-sheet-head')).not.toContainText('偏好')
  await expect(sheet.getByRole('button', { name: '手冲咖啡', exact: true })).toBeVisible()
  await sheet.getByRole('button', { name: '关闭' }).click()
  await line(page, all[4].text).click()
  await expect(sheet.getByRole('heading', { name: '偏好', exact: true })).toBeVisible()
  expect(mock.errors).toEqual([])
})

test('R18 还在整理时有一行「已整理 N / M」，整理完就没有', async ({ page }) => {
  const all = [tomato(), beans(), unsorted()]
  const mock = await backend(page, all, { done: 2, total: 3, version: 7 })
  await open(page)
  const progress = page.getByRole('status', { name: '整理进度' })
  await expect(progress).toHaveText('已整理 2 / 3')
  await expect(page.locator('.mem-entry')).toHaveCount(3)

  mock.state.organize = { done: 3, total: 3, version: 7 }
  await page.reload()
  await expect(page.locator('.mem-entry')).toHaveCount(3)
  await expect(progress).toHaveCount(0)
  await expect(page.locator('body')).not.toContainText('已整理')
  expect(mock.errors).toEqual([])
})

test('一个分组都没有时不出现分组入口，人和地点照旧', async ({ page }) => {
  const all = [unsorted(), memory('云岫镇的集市每周六开', undefined, [], [town])]
  const mock = await backend(page, all, { done: 0, total: 0, version: 1 })
  await open(page)
  await expect(page.locator('.mem-entry')).toHaveCount(2)
  for (const name of ['项目', '主题', '领域']) await expect(row(page, name)).toHaveCount(0)
  await expect(page.locator('.mem-facet')).toHaveCount(2)
  await expect(row(page, '提到的人').getByRole('button')).toHaveText(['林栖1'])
  await expect(row(page, '地点').getByRole('button')).toHaveText(['云岫镇2'])
  await expect(page.locator('body')).not.toContainText('已整理')
  await expect(page.locator('.mem-entry .tag')).toHaveText(['原话有据', '原话有据'])
  expect(mock.queries.every((q) => !q.searchParams.has('group'))).toBeTruthy()
  expect(mock.errors).toEqual([])
})

test('分组很多时每行先显示条数最多的 6 个，其余收起；手机宽度不撑破页面', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const names = ['手冲咖啡', '夜跑', '旧书修补和重新装订的各种小技巧', '观鸟', '家庭收纳', '胶片摄影', '周末短途徒步路线', '发酵面包', '桌面游戏', '盆栽养护', '城市骑行', '语言交换']
  const topics: MemoryGroup[] = names.map((name, i) => ({ entityId: `topic-${i}`, name, type: 'topic' }))
  // The first topic is under twelve memories, the last under one.
  const all = topics.flatMap((topic, i) => Array.from({ length: names.length - i }, (_, n) => memory(`关于${topic.name}的第 ${n + 1} 条`, 'opinion', [topic, garden, food])))
  const mock = await backend(page, all, { done: 40, total: all.length, version: 1 })
  await open(page)
  const chips = row(page, '主题').locator('.chip')
  await expect(chips).toHaveCount(6)
  await expect(chips.first()).toHaveText('手冲咖啡12')
  expect(await fits(page)).toBeTruthy()

  await row(page, '主题').getByRole('button', { name: '更多 6' }).click()
  await expect(chips).toHaveCount(12)
  expect(await fits(page)).toBeTruthy()
  await chips.last().click()
  await expect.poll(() => lastQuery(mock, 'group')).toBe('topic-11')
  await row(page, '主题').getByRole('button', { name: '收起' }).click()
  // The one in use stays in sight.
  await expect(chips).toHaveCount(7)
  await expect(chips.last()).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('.mem-entry')).toHaveCount(1)
  expect(await fits(page)).toBeTruthy()
  expect(mock.errors).toEqual([])
})

// Not a check: writes the two pictures handed over with the work. Run with PCAS_SHOT_DIR set.
test('截图', async ({ page }) => {
  const dir = process.env.PCAS_SHOT_DIR
  test.skip(!dir, 'PCAS_SHOT_DIR 没设')
  const all = [
    tomato(), beans(), brew(), laps(), unsorted(),
    memory('菜园的滴灌定时器装好了，早晚各十分钟', 'progress', [garden]),
    memory('林栖觉得阳台朝西，夏天得加遮阳网', 'other_person', [garden], [linqi]),
    memory('河堤那段路晚上九点后灯太暗，不想一个人跑', 'opinion', [running, health], [town]),
    memory('我是左撇子，工具把手要选对称的', 'identity', []),
    memory('上个月参加了云岫镇的咖啡市集', 'event', [coffee], [town]),
  ]
  await backend(page, all, { done: 9, total: 10, version: 1 })
  for (const [name, width, height] of [['desktop', 1280, 900], ['phone', 390, 844]] as const) {
    await page.setViewportSize({ width, height })
    await page.goto(`/library?tab=memory&group=${coffee.entityId}`)
    await expect(page.locator('.mem-entry')).toHaveCount(3)
    await row(page, '主题').getByRole('button', { name: /手冲咖啡/ }).click()
    await expect(page.locator('.mem-entry')).toHaveCount(10)
    await row(page, '主题').getByRole('button', { name: /手冲咖啡/ }).click()
    await expect(page.locator('.mem-entry')).toHaveCount(3)
    await page.screenshot({ path: `${dir}/U1-${name}.png`, fullPage: true })
  }
})
