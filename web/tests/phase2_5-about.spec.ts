import { test, expect, type Page } from '@playwright/test'
import type { About, Deadline, StatusCard } from '../src/domain/status'
import type { Memory, State } from '../src/domain/types'

// Everything here is made up: the person, the projects and what was said.
const usedAt = new Date(Date.now() - 2 * 86400_000).toISOString()
let serial = 0
function memory(text: string): Memory {
  return {
    id: `memory-${++serial}`, recordVersion: 1, kind: 'fact', text,
    epistemic: 'sourced', confirmation: 'adopted', acquisition: 'direct', sources: [], versions: [], visibleTo: ['model'], exposure: 1, lastUsedAt: usedAt, pinned: false,
    halfLifeDays: 30, reinforcementLimit: 8, mentions: [], groups: [], category: 'progress', trust: 'stated',
  }
}

const handover = [
  '## 他是谁和现在的处境', '在云岫镇开一家小烘焙坊，今年想把周末市集的摊位固定下来。',
  '## 怎么跟他配合', '先给结论，再给理由；要花钱的事先问。',
  '## 现在手上的事', '阳台菜园改造，还差滴灌和遮阳网。',
  '## 时间和节奏', '周二晚上有课，周末上午出摊。',
  '## 资源和限制', '（暂无依据）',
  '## 口味和标准', '咖啡喝中烘，不喝太酸的。',
  '## 重要的人', '林栖，一起出摊的朋友。',
  '## 他的叫法', '把滴灌定时器叫「小闹钟」。',
  '## 他看重什么', '说到做到，东西要耐用。',
].join('\n')

function fixture() {
  const drip = memory('菜园的滴灌定时器装好了，早晚各十分钟')
  const net = memory('遮阳网周六上午十点前要装完')
  const west = memory('阳台朝西，夏天下午晒得厉害')
  const order = memory('遮阳网还没下单，要先量尺寸')
  const rules = [memory('回答先给结论，再给理由'), memory('要花钱的事先问我'), { ...memory('发出去的东西先给我看'), appliesTo: '起草邮件' }]
  const garden: StatusCard = {
    key: 'entity:garden', kind: 'project', name: '阳台菜园改造', count: 4, builtAt: usedAt, stale: true,
    fields: [
      { field: 'status', items: [drip, west] },
      { field: 'deadline', items: [net] },
      { field: 'next', items: [order] },
      { field: 'blocker', items: [] },
    ],
  }
  const self: StatusCard = { key: 'self:rule', kind: 'self', name: '对助手的要求', count: 3, builtAt: usedAt, stale: false, fields: [{ field: 'preference', items: rules }] }
  const cards: StatusCard[] = [
    { key: 'entity:coffee', kind: 'topic', name: '手冲咖啡', count: 5, builtAt: usedAt, stale: false, fields: [] },
    { key: 'entity:food', kind: 'area', name: '饮食', count: 7, builtAt: usedAt, stale: false, fields: [] },
    { key: 'entity:linqi', kind: 'person', name: '林栖', count: 6, builtAt: usedAt, stale: false, fields: [] },
    garden, self,
  ]
  const deadlines: Deadline[] = [
    { id: 'd1', kind: 'deadline', at: '2031-03-08T02:00:00Z', recurrence: '', title: '装完遮阳网', timeNote: '', memoryId: net.id },
    { id: 'd2', kind: 'recurring', at: null, recurrence: '每周二 7 点', title: '陶艺课', timeNote: '没说上午还是下午', memoryId: drip.id },
  ]
  return { memories: [drip, net, west, order, ...rules], cards, deadlines, garden, net, drip }
}

async function backend(page: Page, about: About, memories: Memory[] = []) {
  const state: State = {
    version: 1, revision: 1, budgetUsage: 0,
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: false, followUps: false, dailyReviewAt: '09:00', timezone: 'Asia/Shanghai' },
    tasks: [], ideas: [], projects: [], memories: [], memoryTotal: memories.length,
    candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], notices: [], activity: [], excludedMemories: {},
    agents: [{ id: 'model', name: '演示模型', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false }],
  }
  const asked: string[] = []
  const commands: Record<string, unknown>[] = []
  const errors: string[] = []
  const fail = { card: false }
  page.on('pageerror', (e) => errors.push(e.message))
  await page.route('**/v1/**', (route) => route.fulfill({ status: 500, json: { error: 'unexpected_about_request' } }))
  await page.route((url) => url.pathname === '/v1/workspace', (route) => route.fulfill({ json: state }))
  await page.route((url) => url.pathname === '/v1/desk/turns', (route) => route.fulfill({ json: { conversationId: '', turns: [] } }))
  await page.route((url) => url.pathname === '/v1/workspace/memory-facets', (route) => route.fulfill({ json: { groups: [], people: [], places: [] } }))
  await page.route((url) => url.pathname === '/v1/workspace/memories', (route) => route.fulfill({ json: { items: memories, next: '', total: memories.length } }))
  await page.route((url) => /^\/v1\/workspace\/memories\/[^/]+$/.test(url.pathname), (route) => {
    const found = memories.find((m) => m.id === new URL(route.request().url()).pathname.split('/').at(-1))
    return route.fulfill({ status: found ? 200 : 404, json: found ?? { error: 'not_found' } })
  })
  await page.route((url) => url.pathname === '/v1/workspace/about', (route) => {
    const key = new URL(route.request().url()).searchParams.get('key')
    asked.push(key ?? '')
    if (key && fail.card) return route.fulfill({ status: 503, json: { error: 'storage_unavailable' } })
    // The index carries no contents; one card is filled in when asked for by key.
    return route.fulfill({ json: { ...about, cards: about.cards.map((c) => (c.key === key ? c : { ...c, fields: [] })) } })
  })
  await page.route((url) => url.pathname === '/v1/workspace/commands', (route) => {
    const command = route.request().postDataJSON()
    commands.push(command)
    if (command.type === 'editMemory') {
      const target = memories.find((m) => m.id === command.id)!
      target.text = command.text
      state.revision++
    }
    return route.fulfill({ json: state })
  })
  return { state, about, asked, commands, errors, fail }
}

const section = (page: Page, name: string) => page.getByRole('region', { name, exact: true })
const fits = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)
/** Words for how this is kept up to date inside; none of them belongs on the page. */
const internals = /规则版本|过期|重建|实体/

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 1280, height: 900 } })
test.afterEach(async ({ page }, info) => {
  if (info.status === 'passed') await expect(page.locator('body')).not.toContainText(internals)
})

test('R3-14 从上到下：交接说明分节、期限和固定安排、按五类分组的卡片（名字和条数）', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, { handover: { body: handover, builtAt: usedAt, stale: true }, cards: f.cards, deadlines: f.deadlines, building: { done: 5, total: 5 } }, f.memories)
  await page.goto('/about')
  await expect(page.getByRole('heading', { level: 1, name: '关于你' })).toBeVisible()
  await expect(page.locator('.bar-title')).toHaveText('关于你')

  const note = section(page, '交接说明')
  await expect(note.getByRole('heading', { level: 3 })).toHaveText(['他是谁和现在的处境', '怎么跟他配合', '现在手上的事', '时间和节奏', '资源和限制', '口味和标准', '重要的人', '他的叫法', '他看重什么'])
  await expect(note).toContainText('先给结论，再给理由；要花钱的事先问。')
  await expect(note).toContainText('（暂无依据）')
  await expect(note).not.toContainText('#')

  const dates = section(page, '期限和固定安排').getByRole('listitem')
  await expect(dates).toHaveCount(2)
  await expect(dates.nth(0)).toContainText('2031年3月8日 周六 10:00')
  await expect(dates.nth(0)).toContainText('装完遮阳网')
  await expect(dates.nth(0)).toContainText('截止')
  await expect(dates.nth(1)).toContainText('每周二 7 点')
  await expect(dates.nth(1)).toContainText('陶艺课')
  await expect(dates.nth(1)).toContainText('没说上午还是下午')
  await expect(dates.nth(1)).toContainText('固定安排')

  await expect(page.locator('.about-title')).toHaveText(['交接说明', '期限和固定安排', '关于你', '项目', '人', '主题', '领域'])
  await expect(section(page, '关于你').getByRole('button')).toHaveText(['对助手的要求3 条'])
  await expect(section(page, '项目').getByRole('button')).toHaveText(['阳台菜园改造4 条'])
  await expect(section(page, '人').getByRole('button')).toHaveText(['林栖6 条'])
  await expect(section(page, '主题').getByRole('button')).toHaveText(['手冲咖啡5 条'])
  await expect(section(page, '领域').getByRole('button')).toHaveText(['饮食7 条'])
  await expect(page.getByRole('status', { name: '整理进度' })).toHaveCount(0)
  expect(mock.asked).toEqual([''])
  expect(mock.errors).toEqual([])
})

test('R3-14 点开一张卡看各栏目的记忆；点记忆是现有的详情，能改；再点一次收起', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, { handover: { body: handover, builtAt: usedAt, stale: false }, cards: f.cards, deadlines: f.deadlines, building: { done: 5, total: 5 } }, f.memories)
  await page.goto('/about')
  const bar = section(page, '项目').getByRole('button', { name: /阳台菜园改造/ })
  await expect(bar).toHaveAttribute('aria-expanded', 'false')
  await bar.click()
  await expect(bar).toHaveAttribute('aria-expanded', 'true')
  await expect.poll(() => mock.asked.at(-1)).toBe('entity:garden')
  const card = section(page, '项目')
  await expect(card.getByRole('group', { name: '现状' }).getByRole('listitem')).toHaveText(['菜园的滴灌定时器装好了，早晚各十分钟', '阳台朝西，夏天下午晒得厉害'])
  await expect(card.getByRole('group', { name: '期限' }).getByRole('listitem')).toHaveText(['遮阳网周六上午十点前要装完'])
  await expect(card.getByRole('group', { name: '下一步' }).getByRole('listitem')).toHaveText(['遮阳网还没下单，要先量尺寸'])
  // A heading with nothing under it is left out.
  await expect(card.getByRole('group', { name: '卡点' })).toHaveCount(0)
  expect(new URL(page.url()).searchParams.get('card')).toBe('entity:garden')

  await card.getByRole('button', { name: '遮阳网周六上午十点前要装完' }).click()
  const sheet = page.getByRole('dialog')
  await expect(sheet.getByRole('textbox', { name: '内容' })).toHaveValue('遮阳网周六上午十点前要装完')
  await expect(sheet.getByRole('button', { name: '删掉这条' })).toBeVisible()
  await sheet.getByRole('textbox', { name: '内容' }).fill('遮阳网周日上午十点前要装完')
  await sheet.getByRole('button', { name: '存为新版本' }).click()
  await expect.poll(() => mock.commands.at(-1)?.type).toBe('editMemory')
  expect(mock.commands.at(-1)).toMatchObject({ id: f.net.id, text: '遮阳网周日上午十点前要装完' })
  // The page reads again after the change.
  await expect.poll(() => mock.asked.filter((k) => k === '').length).toBeGreaterThan(1)
  await sheet.getByRole('button', { name: '关闭' }).click()
  await expect(sheet).toHaveCount(0)

  await bar.click()
  await expect(bar).toHaveAttribute('aria-expanded', 'false')
  await expect(card.getByRole('group')).toHaveCount(0)

  // What is asked of the assistant says which kind of task each one is for, when it is for one.
  await section(page, '关于你').getByRole('button', { name: /对助手的要求/ }).click()
  const asked = section(page, '关于你').getByRole('group', { name: '偏好' }).getByRole('listitem')
  await expect(asked).toHaveText(['回答先给结论，再给理由', '要花钱的事先问我', '发出去的东西先给我看适用于：起草邮件'])
  expect(mock.errors).toEqual([])
})

test('R3-14 期限那一行点开是它出自的那条记忆', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, { handover: { body: '', builtAt: '', stale: true }, cards: [], deadlines: f.deadlines, building: { done: 0, total: 0 } }, f.memories)
  await page.goto('/about')
  await expect(section(page, '交接说明')).toHaveCount(0)
  await section(page, '期限和固定安排').getByRole('button', { name: /装完遮阳网/ }).click()
  await expect(page.getByRole('dialog').getByRole('textbox', { name: '内容' })).toHaveValue(f.net.text)
  expect(mock.errors).toEqual([])
})

test('R3-15 没有任何编辑卡片和交接说明的控件，有一句怎么改的说明', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, { handover: { body: handover, builtAt: usedAt, stale: false }, cards: f.cards, deadlines: f.deadlines, building: { done: 5, total: 5 } }, f.memories)
  await page.goto('/about?card=entity%3Agarden')
  await expect(section(page, '项目').getByRole('group', { name: '现状' })).toBeVisible()
  await expect(page.getByText('这些是从你说过的话整理出来的，哪里不对就改那条记忆，或者直接告诉我。')).toBeVisible()
  const main = page.locator('main.page')
  for (const role of ['textbox', 'checkbox', 'switch', 'combobox', 'radio'] as const) await expect(main.getByRole(role)).toHaveCount(0)
  await expect(main.locator('[contenteditable]')).toHaveCount(0)
  await expect(main.getByRole('button', { name: /编辑|修改|保存|删|改名|刷新|更新/ })).toHaveCount(0)
  expect(mock.errors).toEqual([])
})

test('R3-16 还在建时显示「正在整理 N / M」，建完不显示；什么都没有时说清楚', async ({ page }) => {
  const f = fixture()
  const about: About = { handover: { body: '', builtAt: '', stale: true }, cards: f.cards.slice(3), deadlines: [], building: { done: 2, total: 9 } }
  const mock = await backend(page, about, f.memories)
  await page.goto('/about')
  const progress = page.getByRole('status', { name: '整理进度' })
  await expect(progress).toHaveText('正在整理 2 / 9')
  await expect(section(page, '项目').getByRole('button')).toHaveCount(1)

  about.building = { done: 9, total: 9 }
  await page.reload()
  await expect(section(page, '项目').getByRole('button')).toHaveCount(1)
  await expect(progress).toHaveCount(0)

  about.cards = []
  about.building = { done: 0, total: 6 }
  await page.reload()
  await expect(progress).toHaveText('正在整理 0 / 6')
  await expect(page.getByText('还在整理，过一会儿这里就有内容了。')).toBeVisible()

  about.building = { done: 0, total: 0 }
  await page.reload()
  await expect(page.getByText('还没有整理出什么。多跟秘书说说你的事，这里会慢慢长出来。')).toBeVisible()
  await expect(progress).toHaveCount(0)
  expect(mock.errors).toEqual([])
})

test('读不出来时说明原因并能重试；一张卡读不出来不影响其余', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, { handover: { body: handover, builtAt: usedAt, stale: false }, cards: f.cards, deadlines: f.deadlines, building: { done: 5, total: 5 } }, f.memories)
  mock.fail.card = true
  await page.goto('/about')
  await section(page, '项目').getByRole('button', { name: /阳台菜园改造/ }).click()
  const alert = section(page, '项目').getByRole('alert')
  await expect(alert).toContainText('这张卡没读出来：服务器出错了（错误 503，storage_unavailable）')
  await expect(section(page, '交接说明')).toBeVisible()
  mock.fail.card = false
  await alert.getByRole('button', { name: '重试' }).click()
  await expect(section(page, '项目').getByRole('group', { name: '现状' })).toBeVisible()
  expect(mock.errors).toEqual([])
})

test('R3-14 从首页和资料库都能进', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, { handover: { body: handover, builtAt: usedAt, stale: false }, cards: f.cards, deadlines: [], building: { done: 5, total: 5 } }, f.memories)
  await page.goto('/')
  await page.getByRole('link', { name: '关于你' }).click()
  await expect(page).toHaveURL(/\/about$/)
  await expect(section(page, '交接说明')).toBeVisible()
  await page.goto('/library')
  await page.getByRole('link', { name: '关于你' }).click()
  await expect(page).toHaveURL(/\/about$/)
  await expect(section(page, '交接说明')).toBeVisible()
  expect(mock.errors).toEqual([])
})

test('手机宽度下不撑破页面', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const f = fixture()
  f.garden.name = '阳台菜园改造和整个露台的重新规划以及明年春天的播种安排'
  f.deadlines[0].title = '把遮阳网、滴灌定时器和所有花盆的托盘都装完并且检查一遍'
  const mock = await backend(page, { handover: { body: handover, builtAt: usedAt, stale: false }, cards: f.cards, deadlines: f.deadlines, building: { done: 3, total: 5 } }, f.memories)
  await page.goto('/about?card=entity%3Agarden')
  await expect(section(page, '项目').getByRole('group', { name: '现状' })).toBeVisible()
  expect(await fits(page)).toBeTruthy()
  expect(mock.errors).toEqual([])
})

// Not a check: writes the two pictures handed over with the work. Run with PCAS_SHOT_DIR set.
test('截图', async ({ page }) => {
  const dir = process.env.PCAS_SHOT_DIR
  test.skip(!dir, 'PCAS_SHOT_DIR 没设')
  const f = fixture()
  await backend(page, { handover: { body: handover, builtAt: usedAt, stale: false }, cards: f.cards, deadlines: f.deadlines, building: { done: 4, total: 5 } }, f.memories)
  for (const [name, width, height] of [['desktop', 1280, 1500], ['phone', 390, 2100]] as const) {
    await page.setViewportSize({ width, height })
    await page.goto('/about?card=entity%3Agarden')
    await expect(section(page, '项目').getByRole('group', { name: '现状' })).toBeVisible()
    await page.screenshot({ path: `${dir}/about-${name}.png`, fullPage: true })
  }
})
