import { test, expect, type Page } from '@playwright/test'
import type { Deadline, Handover } from '../src/domain/status'
import type { AssistantRequirement, Memory, MemoryFacets, MemoryGroupEntry, State, Task } from '../src/domain/types'

// Phase 2.6, U: the library takes over what the page 「关于你」 showed.
// Everything here is made up: the person, the projects and what was said.
const writtenAt = '2031-03-01T01:30:00Z'
let serial = 0
function memory(text: string, more: Partial<Memory> = {}): Memory {
  return {
    id: `memory-${++serial}`, recordVersion: 1, kind: 'fact', text,
    epistemic: 'sourced', confirmation: 'adopted', acquisition: 'direct', sources: [], versions: [], visibleTo: ['model'], exposure: 1, lastUsedAt: writtenAt, pinned: false,
    halfLifeDays: 30, reinforcementLimit: 8, mentions: [], groups: [], category: 'progress', trust: 'stated', ...more,
  }
}

const handover = [
  '## 他是谁和现在的处境', '在云岫镇开一家小烘焙坊，今年想把周末市集的摊位固定下来。',
  '## 怎么跟他配合', '先给结论，再给理由；要花钱的事先问。',
  '## 现在手上的事', '阳台菜园改造，还差滴灌和遮阳网。',
  '## 时间和节奏', '周二晚上有课，周末上午出摊。',
].join('\n')

const topics = ['手冲咖啡', '面包配方', '市集摊位', 'Sourdough starter', '烤箱保养', '包装设计', '进货渠道', 'Tax filing']
// The directory's keys mean nothing to the page; these are deliberately not shaped like anything.
const directory: MemoryGroupEntry[] = [
  { key: 'k/rule', kind: 'self', name: '对助手的要求', count: 2 },
  { key: 'k/goal', kind: 'self', name: '目标', count: 3 },
  { key: 'k/linqi', kind: 'person', name: '林栖', count: 6 },
  { key: 'k/bo', kind: 'person', name: 'Bo', count: 2 },
  { key: 'k/garden', kind: 'project', name: '阳台菜园改造', count: 1 },
  ...topics.map((name, i) => ({ key: `k/topic ${i}`, kind: 'topic' as const, name, count: 9 - i })),
  { key: 'k/food', kind: 'area', name: '饮食', count: 7 },
]
const facets: MemoryFacets = { groups: [], people: [], places: [{ entityId: 'yunxiu', name: '云岫镇', count: 3 }] }

type Dated = Pick<Deadline, 'id' | 'kind' | 'title' | 'memoryId' | 'dateStatus'> & Partial<Deadline>
const deadline = (d: Dated): Deadline => ({ at: null, recurrence: '', timeNote: '', originalText: '', ...d })

function fixture() {
  const net = memory('遮阳网周六上午十点前要装完', { groups: [{ entityId: 'e-garden', name: '阳台菜园改造', type: 'project' }], mentions: [{ entityId: 'e-linqi', name: '林栖', role: 'person' }, { entityId: 'yunxiu', name: '云岫镇', role: 'place' }] })
  const dough = memory('酸面团每周日喂一次')
  const oven = memory('烤箱的保修上个月到期，要问一下续保')
  const visit = memory('说好了找个时间去看林栖的新店')
  const rule = memory('回答先给结论，再给理由', { category: 'rule' })
  const mail = memory('发出去的东西先给我看', { category: 'rule' })
  const deadlines: Deadline[] = [
    deadline({ id: 'd-far', kind: 'appointment', at: '2031-06-01T06:00:00Z', title: '市集摊位续约面谈', memoryId: net.id, dateStatus: 'upcoming' }),
    deadline({ id: 'd-near', kind: 'deadline', at: '2031-03-08T02:00:00Z', title: '装完遮阳网', memoryId: net.id, dateStatus: 'upcoming' }),
    // The server says which ones have gone by; the page does not work it out from the clock.
    deadline({ id: 'd-gone', kind: 'deadline', at: '2031-02-10T02:00:00Z', title: '给烤箱续保', memoryId: oven.id, dateStatus: 'expired_unknown' }),
    deadline({ id: 'd-weekly', kind: 'recurring', recurrence: '每周日', title: '喂酸面团', memoryId: dough.id, dateStatus: 'recurring' }),
    deadline({ id: 'd-vague', kind: 'appointment', title: '去看林栖的新店', timeNote: '没说哪天', originalText: '说好了找个时间去看林栖的新店', memoryId: visit.id, dateStatus: 'unclear' }),
  ]
  const members: Record<string, Memory[]> = { 'k/rule': [rule, mail], 'k/garden': [net], 'k/linqi': [visit, net], 'k/food': [dough, net, oven] }
  const requirements: AssistantRequirement[] = [
    { memoryId: rule.id, text: rule.text, unrestricted: true, scope: '' },
    { memoryId: mail.id, text: mail.text, unrestricted: false, scope: '起草邮件和对外发的消息' },
  ]
  return { memories: [net, dough, oven, visit, rule, mail], deadlines, members, requirements, net, oven, rule, mail, visit, dough }
}

interface Options { handover?: Handover; deadlines?: Deadline[]; memories?: Memory[]; members?: Record<string, Memory[]>; requirements?: AssistantRequirement[]; tasks?: Task[] }

async function backend(page: Page, { handover = { body: '', builtAt: '', stale: false }, deadlines = [], memories = [], members = {}, requirements = [], tasks = [] }: Options = {}) {
  const mock = {
    state: {
      version: 1, revision: 1, budgetUsage: 0,
      settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: false, followUps: false, dailyReviewAt: '09:00', timezone: 'Asia/Shanghai' },
      tasks, ideas: [], projects: [], memories: [], memoryTotal: memories.length,
      candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], notices: [], activity: [], excludedMemories: {},
      agents: [{ id: 'model', name: '演示模型', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false }],
    } as State,
    /** Every read, by path and query, in the order it was asked. */
    asked: [] as string[],
    lists: [] as URLSearchParams[],
    commands: [] as Record<string, unknown>[],
    errors: [] as string[],
    failing: new Set<string>(),
    /** Called for each command; returns a refusal, or nothing to accept it. */
    refuse: undefined as undefined | ((command: Record<string, unknown>) => string | undefined),
    reads: (path: string) => mock.asked.filter((a) => a.split('?')[0] === path).length,
  }
  page.on('pageerror', (e) => mock.errors.push(e.message))
  await page.route('**/v1/**', (route) => route.fulfill({ status: 500, json: { error: 'unexpected_request' } }))
  await page.route((url) => url.pathname === '/v1/workspace', (route) => route.fulfill({ json: mock.state }))
  await page.route((url) => url.pathname === '/v1/desk/turns', (route) => route.fulfill({ json: { conversationId: '', turns: [] } }))
  const read = async (path: string, answer: (query: URLSearchParams) => unknown) => page.route((url) => url.pathname === path, (route) => {
    const url = new URL(route.request().url())
    mock.asked.push(url.pathname + url.search)
    return mock.failing.has(path) ? route.fulfill({ status: 503, json: { error: 'storage_unavailable' } }) : route.fulfill({ json: answer(url.searchParams) })
  })
  await read('/v1/workspace/handover', () => handover)
  await read('/v1/workspace/deadlines', () => ({ items: deadlines }))
  await read('/v1/workspace/assistant-requirements', () => ({ items: requirements }))
  await read('/v1/workspace/memory-groups', () => ({ items: directory }))
  await read('/v1/workspace/memory-facets', () => facets)
  await read('/v1/workspace/memories', (query) => { mock.lists.push(query); return { items: memories, next: '', total: memories.length } })
  for (const group of directory) {
    const items = members[group.key] ?? []
    // Two to a page, so reading on is exercised.
    await read(`/v1/workspace/memory-groups/${encodeURIComponent(group.key)}/memories`, (query) => {
      const from = Number(query.get('cursor') ?? 0)
      return { items: items.slice(from, from + 1), next: from + 1 < items.length ? String(from + 1) : '', total: items.length }
    })
  }
  await page.route((url) => /^\/v1\/workspace\/memories\/[^/]+$/.test(url.pathname), (route) => {
    const found = memories.find((m) => m.id === new URL(route.request().url()).pathname.split('/').at(-1))
    return route.fulfill({ status: found ? 200 : 404, json: found ?? { error: 'not_found' } })
  })
  await page.route((url) => url.pathname === '/v1/workspace/commands', (route) => {
    const command = route.request().postDataJSON()
    mock.commands.push(command)
    const refusal = mock.refuse?.(command)
    if (refusal) return route.fulfill({ status: 409, json: { error: refusal } })
    if (command.type === 'toggleCheck') {
      mock.state = { ...mock.state, tasks: mock.state.tasks.map((t) => t.id === command.taskId ? { ...t, checklist: t.checklist.map((c) => c.id === command.itemId ? { ...c, done: !c.done } : c) } : t) }
    }
    mock.state = { ...mock.state, revision: mock.state.revision + 1 }
    return route.fulfill({ json: mock.state })
  })
  return mock
}

const now = (page: Page) => page.getByRole('region', { name: '眼下', exact: true })
const part = (page: Page, name: string) => now(page).getByRole('region', { name, exact: true })
const row = (page: Page, name: string) => page.getByRole('group', { name, exact: true })
const chips = (page: Page, name: string) => row(page, name).locator('.mem-facet-name')
const fits = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 1280, height: 900 } })

test('U1 资料库顶部是「眼下」：交接说明写明写于何时，期限分成未到、已过期、固定安排、日期没说清', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, { handover: { body: handover, builtAt: writtenAt, stale: false }, deadlines: f.deadlines, memories: f.memories })
  await page.goto('/library')
  await expect(now(page)).toBeVisible()

  const note = part(page, '交接说明')
  await expect(note).toContainText('写于 2031年3月1日 周六 09:30')
  await expect(note).not.toContainText('正在更新')
  await expect(note).not.toContainText('#')
  // The first parts are in sight; the rest is counted on the button, not dropped.
  await expect(note.getByRole('heading', { level: 4 })).toHaveText(['他是谁和现在的处境', '怎么跟他配合'])
  await note.getByRole('button', { name: '看全文（还有 2 节）' }).click()
  await expect(note.getByRole('heading', { level: 4 })).toHaveText(['他是谁和现在的处境', '怎么跟他配合', '现在手上的事', '时间和节奏'])
  await expect(note).toContainText('周二晚上有课，周末上午出摊。')

  const dates = part(page, '期限和固定安排')
  await expect(dates.getByRole('group')).toHaveCount(4)
  const upcoming = dates.getByRole('group', { name: '还没到的' }).getByRole('listitem')
  // The nearest first, whatever order they arrived in.
  await expect(upcoming).toHaveCount(2)
  await expect(upcoming.nth(0)).toContainText('2031年3月8日 周六 10:00')
  await expect(upcoming.nth(0)).toContainText('装完遮阳网')
  await expect(upcoming.nth(0)).toContainText('截止')
  await expect(upcoming.nth(1)).toContainText('市集摊位续约面谈')
  const overdue = dates.getByRole('group', { name: '已过期，不知是否完成' }).getByRole('listitem')
  await expect(overdue).toHaveCount(1)
  await expect(overdue).toContainText('2031年2月10日')
  await expect(overdue).toContainText('给烤箱续保')
  const fixed = dates.getByRole('group', { name: '固定安排' }).getByRole('listitem')
  await expect(fixed).toHaveText(['每周日喂酸面团'])
  const vague = dates.getByRole('group', { name: '日期没说清的' }).getByRole('listitem')
  await expect(vague).toContainText('日期没说清')
  await expect(vague).toContainText('去看林栖的新店')
  await expect(vague).toContainText('没说哪天')
  await expect(vague).toContainText('原话：说好了找个时间去看林栖的新店')
  // Every date there is was asked for, the ones gone by included.
  expect(mock.asked.filter((a) => a.startsWith('/v1/workspace/deadlines'))).toEqual(['/v1/workspace/deadlines'])

  // A date opens the memory it was taken from, in the usual sheet.
  await overdue.getByRole('button').click()
  await expect(page.getByRole('dialog').getByRole('textbox', { name: '内容' })).toHaveValue(f.oven.text)
  expect(new URL(page.url()).searchParams.get('m')).toBe(f.oven.id)
  expect(await fits(page)).toBe(true)
  expect(mock.errors).toEqual([])
})

test('U1 交接说明过期时写明「正在更新，下面是上一份」，正文照常显示', async ({ page }) => {
  const mock = await backend(page, { handover: { body: handover, builtAt: writtenAt, stale: true } })
  await page.goto('/library')
  const note = part(page, '交接说明')
  await expect(note).toContainText('正在更新，下面是上一份，写于 2031年3月1日 周六 09:30')
  await expect(note).toContainText('在云岫镇开一家小烘焙坊')
  expect(mock.errors).toEqual([])
})

test('U1 一组期限多于五条时写明还有几条，点开全部看得到', async ({ page }) => {
  const net = memory('遮阳网周六上午十点前要装完')
  const deadlines = Array.from({ length: 8 }, (_, i) => deadline({ id: `d-${i}`, kind: 'deadline', at: new Date(Date.UTC(2031, 2, 8 + i, 2)).toISOString(), title: `第 ${i + 1} 件要交的事`, memoryId: net.id, dateStatus: 'upcoming' }))
  const mock = await backend(page, { deadlines, memories: [net] })
  await page.goto('/library')
  const upcoming = part(page, '期限和固定安排').getByRole('group', { name: '还没到的' })
  await expect(upcoming.getByRole('heading', { level: 4 })).toHaveText('还没到的8')
  await expect(upcoming.getByRole('listitem')).toHaveCount(5)
  await upcoming.getByRole('button', { name: '还有 3 条' }).click()
  await expect(upcoming.getByRole('listitem')).toHaveCount(8)
  expect(mock.errors).toEqual([])
})

test('U1 「关于你」入口没有了：首页、资料库、顶栏查找里都没有', async ({ page }) => {
  const mock = await backend(page)
  await page.goto('/')
  await expect(page.locator('.hall-desk')).toBeVisible()
  await expect(page.getByRole('link', { name: '关于你' })).toHaveCount(0)
  await page.goto('/library')
  await expect(now(page)).toBeVisible()
  await expect(page.getByRole('link', { name: '关于你' })).toHaveCount(0)
  await page.keyboard.press('Control+k')
  const palette = page.getByRole('dialog')
  await expect(palette.getByText('资料库')).toBeVisible()
  await expect(palette.getByText('关于你')).toHaveCount(0)
  await palette.getByRole('combobox').or(palette.getByRole('textbox')).first().fill('期限')
  await expect(palette.getByText('资料库')).toBeVisible()
  expect(mock.errors).toEqual([])
})

test('U1 「眼下」可以收起，下次打开还是收起的；看已被替代或合并的记忆时不显示', async ({ page }) => {
  const mock = await backend(page, { handover: { body: handover, builtAt: writtenAt, stale: false } })
  await page.goto('/library')
  const toggle = now(page).getByRole('button', { name: '收起' }).first()
  await expect(part(page, '交接说明')).toBeVisible()
  await toggle.click()
  await expect(part(page, '交接说明')).toHaveCount(0)
  await page.reload()
  await expect(now(page).getByRole('button', { name: '展开' })).toHaveAttribute('aria-expanded', 'false')
  await expect(part(page, '交接说明')).toHaveCount(0)
  await now(page).getByRole('button', { name: '展开' }).click()
  await expect(part(page, '交接说明')).toBeVisible()
  await page.goto('/library?retired=1')
  await expect(page.getByRole('status', { name: '已被替代或合并的' })).toBeVisible()
  await expect(now(page)).toHaveCount(0)
  expect(mock.errors).toEqual([])
})

test('U3 没有内容时如实说没有；一样读不出来时说明原因并能重试，另一样和记忆列表照常', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, { handover: { body: '', builtAt: '', stale: true }, memories: f.memories })
  await page.goto('/library')
  await expect(part(page, '交接说明')).toContainText('还没有交接说明。')
  // Nothing is promised about when one will be there.
  await expect(now(page)).not.toContainText(/稍后|过一会儿|正在更新|正在整理|刚有变动/)
  await expect(part(page, '期限和固定安排')).toContainText('没有记下期限或固定安排。')
  await expect(part(page, '期限和固定安排').getByRole('group')).toHaveCount(0)

  mock.failing.add('/v1/workspace/handover')
  await page.reload()
  const alert = now(page).getByRole('alert')
  await expect(alert).toContainText('交接说明没读出来：服务器出错了（错误 503，storage_unavailable）')
  await expect(part(page, '期限和固定安排')).toContainText('没有记下期限或固定安排。')
  await expect(page.getByRole('button', { name: f.net.text })).toBeVisible()
  mock.failing.clear()
  await alert.getByRole('button', { name: '重试' }).click()
  await expect(part(page, '交接说明')).toContainText('还没有交接说明。')
  expect(mock.errors).toEqual([])
})

test('U2 分组来自分组目录：按人、项目、主题、领域和「你本人」的某一类筛选，条数和目录一致，能接着往后读', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, f)
  await page.goto('/library')
  await expect(page.locator('.mem-facet-label')).toHaveText(['你本人', '人', '项目', '主题', '领域', '地点'])
  await expect(row(page, '你本人').getByRole('button')).toHaveText(['对助手的要求2', '目标3'])
  await expect(row(page, '人').getByRole('button')).toHaveText(['林栖6', 'Bo2'])
  await expect(chips(page, '项目')).toHaveText(['阳台菜园改造'])
  await expect(chips(page, '主题')).toHaveCount(6)
  await expect(chips(page, '领域')).toHaveText(['饮食'])
  await expect(chips(page, '地点')).toHaveText(['云岫镇'])

  await row(page, '你本人').getByRole('button', { name: /对助手的要求/ }).click()
  expect(new URL(page.url()).searchParams.get('in')).toBe('k/rule')
  await expect(page.locator('.mem-summary')).toContainText('「对助手的要求」名下的记忆有 2 条')
  // Both pages of the group, read from the group's own address with the key passed whole.
  // The next page is read as the end of the list comes into sight.
  await page.locator('.list > .mem-state').scrollIntoViewIfNeeded()
  await expect(page.locator('.mem-entry')).toHaveCount(2)
  expect(mock.asked.filter((a) => a.includes('/memory-groups/k%2Frule/memories'))).toEqual(['/v1/workspace/memory-groups/k%2Frule/memories?limit=50', '/v1/workspace/memory-groups/k%2Frule/memories?limit=50&cursor=1'])
  // Each one says when it holds.
  await expect(page.locator('.mem-entry').filter({ hasText: f.rule.text })).toContainText('不限范围，每一轮都带')
  await expect(page.locator('.mem-entry').filter({ hasText: f.mail.text })).toContainText('范围：起草邮件和对外发的消息')
  await expect(page.getByRole('button', { name: f.net.text })).toHaveCount(0)
  await expect(row(page, '你本人').getByRole('button', { name: /对助手的要求/ })).toHaveAttribute('aria-pressed', 'true')

  await page.getByRole('button', { name: '清掉筛选' }).click()
  expect(new URL(page.url()).searchParams.has('in')).toBe(false)
  await expect(page.locator('.mem-entry')).toHaveCount(f.memories.length)
  // Outside that group, nothing is said about scope.
  await expect(page.locator('.mem-applies')).toHaveCount(0)

  // A group named on a memory leads to the directory's group of that name; a place is still narrowed by mention.
  await page.locator('.mem-entry').filter({ hasText: f.net.text }).getByRole('button', { name: '阳台菜园改造' }).click()
  expect(new URL(page.url()).searchParams.get('in')).toBe('k/garden')
  await expect(page.locator('.mem-summary')).toContainText('「阳台菜园改造」名下的记忆有 1 条')
  // A person named on it is a group too; with one already picked, the two are taken together.
  await page.locator('.mem-entry').getByRole('button', { name: '林栖' }).click()
  expect(new URL(page.url()).searchParams.getAll('in')).toEqual(['k/garden', 'k/linqi'])
  await expect(page.locator('.mem-summary')).toContainText('同时在「阳台菜园改造」「林栖」名下的记忆有 1 条')
  await page.getByRole('button', { name: '清掉筛选' }).click()
  await page.locator('.mem-entry').filter({ hasText: f.net.text }).getByRole('button', { name: '云岫镇' }).click()
  expect(Object.fromEntries(new URL(page.url()).searchParams)).toEqual({ entity: 'yunxiu' })
  await expect.poll(() => mock.lists.at(-1)?.get('entity')).toBe('yunxiu')
  expect(mock.errors).toEqual([])
})

test('U2 分组可以叠加，也能和搜字、类型、地点一起用：对整组记忆筛，不只筛读到的那一页', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, f)
  await page.goto('/library?in=k%2Ffood')
  await expect(page.locator('.mem-summary')).toContainText('「饮食」名下的记忆有 3 条')
  // Only the first page is on screen (the fixture serves one a page); the match is on the last.
  await expect(page.locator('.mem-entry')).toHaveCount(1)
  await page.getByRole('textbox', { name: '搜索记忆' }).fill('续保')
  await expect(page.locator('.mem-summary')).toContainText('「饮食」名下、符合其余条件的记忆有 1 条')
  await expect(page.getByRole('button', { name: f.oven.text })).toBeVisible()
  // The whole group was read to the end, a hundred at a time, and the search was not sent to the general list.
  expect(mock.asked.filter((a) => a.includes('/memory-groups/k%2Ffood/memories?limit=100'))).toHaveLength(3)
  expect(mock.lists.some((query) => query.has('q'))).toBe(false)
  await page.getByRole('textbox', { name: '搜索记忆' }).fill('')
  await expect(page.locator('.mem-summary')).toContainText('「饮食」名下的记忆有 3 条')

  // Two groups: what is under both.
  await row(page, '人').getByRole('button', { name: /林栖/ }).click()
  await expect(page.locator('.mem-summary')).toContainText('同时在「饮食」「林栖」名下的记忆有 1 条')
  await expect(page.getByRole('button', { name: f.net.text })).toBeVisible()
  await expect(row(page, '人').getByRole('button', { name: /林栖/ })).toHaveAttribute('aria-pressed', 'true')
  await expect(row(page, '领域').getByRole('button', { name: /饮食/ })).toHaveAttribute('aria-pressed', 'true')
  // The group read a moment ago is not read again.
  expect(mock.asked.filter((a) => a.includes('/memory-groups/k%2Ffood/memories?limit=100'))).toHaveLength(3)
  // And a place on top of them.
  await row(page, '地点').getByRole('button', { name: /云岫镇/ }).click()
  await expect(page.locator('.mem-summary')).toContainText('同时在「饮食」「林栖」名下、提到「云岫镇」的记忆有 1 条')
  await page.getByRole('radiogroup', { name: '类型' }).getByRole('radio', { name: '偏好' }).click()
  await expect(page.getByText('没找到符合的记忆。')).toBeVisible()
  await expect(page.locator('.mem-summary')).toContainText('有 0 条')
  await page.getByRole('radiogroup', { name: '类型' }).getByRole('radio', { name: '全部' }).click()

  // Taking one group off leaves the other.
  await row(page, '领域').getByRole('button', { name: /饮食/ }).click()
  expect(new URL(page.url()).searchParams.getAll('in')).toEqual(['k/linqi'])
  await expect(page.locator('.mem-summary')).toContainText('「林栖」名下、提到「云岫镇」的记忆有 1 条')
  await page.getByRole('button', { name: '清掉筛选' }).click()
  expect(new URL(page.url()).search).toBe('')
  await expect(page.locator('.mem-entry')).toHaveCount(f.memories.length)
  expect(mock.errors).toEqual([])
})

test('U2 读整组时中途读不出来：说明原因，能重试，不把读到一半的当成全部', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, f)
  mock.failing.add('/v1/workspace/memory-groups/k%2Ffood/memories')
  await page.goto('/library?in=k%2Ffood&q=%E7%BB%AD%E4%BF%9D')
  const alert = page.locator('.sheet .mem-state.failed')
  await expect(alert).toContainText('记忆没读出来：服务器出错了（错误 503，storage_unavailable）')
  await expect(page.locator('.mem-entry')).toHaveCount(0)
  mock.failing.clear()
  await alert.getByRole('button', { name: '重试' }).click()
  await expect(page.getByRole('button', { name: f.oven.text })).toBeVisible()
  await expect(page.locator('.mem-summary')).toContainText('有 1 条')
  expect(mock.errors).toEqual([])
})

test('U2 能搜分组名，中英文不分大小写', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, f)
  await page.goto('/library')
  const find = page.getByRole('searchbox', { name: '搜索分组' })
  // Past the first six of a row, a name is found by typing part of it.
  await expect(chips(page, '主题').filter({ hasText: 'Tax filing' })).toHaveCount(0)
  await find.fill('tax')
  await expect(chips(page, '主题')).toHaveText(['Tax filing'])
  await expect(row(page, '项目')).toHaveCount(0)
  await expect(row(page, '你本人')).toHaveCount(0)
  await find.fill('BO')
  await expect(chips(page, '人')).toHaveText(['Bo'])
  await find.fill('对助手')
  await expect(chips(page, '你本人')).toHaveText(['对助手的要求'])
  await find.fill('云岫')
  await expect(chips(page, '地点')).toHaveText(['云岫镇'])
  await find.fill('菜园')
  await row(page, '项目').getByRole('button', { name: /阳台菜园改造/ }).click()
  expect(new URL(page.url()).searchParams.get('in')).toBe('k/garden')
  await find.fill('没有这个名字')
  await expect(page.getByText('没有名字里带「没有这个名字」的分组。')).toBeVisible()
  await expect(page.locator('.mem-facet')).toHaveCount(0)
  expect(mock.errors).toEqual([])
})

test('U2 原来卡片页的链接跳到资料库里同一个分组；分组不在了如实说', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, f)
  const lands = async (from: string, query: Record<string, string>) => {
    await page.goto(from)
    await expect(page.getByRole('heading', { level: 1, name: '资料库' })).toBeVisible()
    const url = new URL(page.url())
    expect(url.pathname).toBe('/library')
    expect(Object.fromEntries(url.searchParams)).toEqual(query)
  }
  await lands('/about', {})
  // The card's key is the group's key, passed on as it is.
  await lands('/about?card=k%2Fgarden', { in: 'k/garden' })
  await expect(row(page, '项目').getByRole('button', { name: /阳台菜园改造/ })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('.mem-summary')).toContainText('「阳台菜园改造」名下的记忆有 1 条')
  await lands('/about?card=k%2Ftopic%207', { in: 'k/topic 7' })
  // The one in use is in sight although it is past the first six.
  await expect(row(page, '主题').getByRole('button', { name: /Tax filing/ })).toHaveAttribute('aria-pressed', 'true')
  await lands(`/about?card=k%2Fgarden&m=${f.net.id}`, { in: 'k/garden', m: f.net.id })
  await expect(page.getByRole('dialog').getByRole('textbox', { name: '内容' })).toHaveValue(f.net.text)
  await lands('/about?card=entity%3Agone', { in: 'entity:gone' })
  await expect(page.getByText('没有这个分组，或者它名下已经没有记忆了。')).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await page.getByRole('button', { name: '清掉筛选' }).click()
  await expect(page.locator('.mem-entry')).toHaveCount(f.memories.length)
  expect(mock.errors).toEqual([])
})

test('U4 后台只动了版本号时不重读列表；记忆真的变了才重读', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, { handover: { body: handover, builtAt: writtenAt, stale: false }, deadlines: f.deadlines, memories: f.memories })
  await page.goto('/library')
  await expect(page.getByRole('button', { name: f.net.text })).toBeVisible()
  await expect(part(page, '期限和固定安排').getByRole('group')).toHaveCount(4)
  await expect(chips(page, '项目')).toHaveText(['阳台菜园改造'])
  const paths = ['/v1/workspace/memories', '/v1/workspace/handover', '/v1/workspace/deadlines', '/v1/workspace/memory-groups', '/v1/workspace/memory-facets']
  const count = () => paths.map((path) => mock.reads(path))
  const before = count()
  expect(before).toEqual([1, 1, 1, 1, 1])
  // The background writes something that is not a memory, three polls in a row.
  for (let i = 0; i < 3; i++) {
    const seen = page.waitForResponse((r) => new URL(r.url()).pathname === '/v1/workspace')
    mock.state = { ...mock.state, revision: mock.state.revision + 5 }
    await seen
  }
  await page.waitForTimeout(300)
  expect(count()).toEqual(before)

  // A memory changed: the snapshot carries it, and the list is read again.
  mock.state = { ...mock.state, revision: mock.state.revision + 1, memories: [{ ...f.net, recordVersion: 2, text: '遮阳网改到周日装' }] }
  await expect.poll(count).toEqual([2, 2, 2, 2, 2])
  expect(mock.errors).toEqual([])
})

test('U4 工作区没变时轮询不取正文；记忆的版本号变了才重读列表', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, { handover: { body: handover, builtAt: writtenAt, stale: false }, deadlines: f.deadlines, memories: f.memories })
  mock.state = { ...mock.state, memoryRevision: 7 }
  let tag = '"w1"'
  const asked: (string | undefined)[] = []
  let unchanged = 0
  await page.route((url) => url.pathname === '/v1/workspace', (route) => {
    const sent = route.request().headers()['if-none-match']
    asked.push(sent)
    if (sent === tag) { unchanged++; return route.fulfill({ status: 304, headers: { ETag: tag } }) }
    return route.fulfill({ json: mock.state, headers: { ETag: tag } })
  })
  await page.goto('/library')
  await expect(page.getByRole('button', { name: f.net.text })).toBeVisible()
  expect(asked[0]).toBeUndefined()
  await expect.poll(() => unchanged, { timeout: 10000 }).toBeGreaterThanOrEqual(2)
  // Nothing was taken away by an answer with no body.
  await expect(page.getByRole('button', { name: f.net.text })).toBeVisible()
  expect(mock.reads('/v1/workspace/memories')).toBe(1)

  // The background wrote something that is not a memory: a new snapshot, the same memory count.
  tag = '"w2"'
  mock.state = { ...mock.state, memories: [{ ...f.net, recordVersion: 2 }] }
  await expect.poll(() => asked.filter((a) => a === '"w2"').length, { timeout: 10000 }).toBeGreaterThanOrEqual(1)
  expect(mock.reads('/v1/workspace/memories')).toBe(1)

  // The server says a memory changed.
  tag = '"w3"'
  mock.state = { ...mock.state, memoryRevision: 8 }
  await expect.poll(() => mock.reads('/v1/workspace/memories'), { timeout: 10000 }).toBe(2)
  expect(mock.errors).toEqual([])
})

const at = '2031-03-01T01:30:00Z'
const task = (): Task => ({
  id: 'task-net', title: '装遮阳网', notes: '', status: 'todo', dependsOn: [], triggers: [], sources: [], history: [], createdAt: at, updatedAt: at,
  checklist: [{ id: 'c1', text: '量尺寸', done: false }, { id: 'c2', text: '下单', done: false }],
})

test('U4 打勾时撞上后台写了别的东西：自动再发一次，不报错', async ({ page }) => {
  const mock = await backend(page, { tasks: [task()] })
  await page.goto('/t/task-net')
  const first = page.locator('.check-row').filter({ hasText: '量尺寸' }).getByRole('button', { name: '完成' })
  await expect(first).toBeVisible()
  // The server holds a newer revision than the page has read; nothing about this task changed.
  let refused = 0
  mock.refuse = (command) => {
    if (command.expectedRevision === mock.state.revision) return undefined
    refused++
    return 'version_conflict'
  }
  await page.route((url) => url.pathname === '/v1/workspace', (route) => route.fulfill({ json: mock.commands.length === 0 ? { ...mock.state, revision: 1 } : mock.state }))
  mock.state = { ...mock.state, revision: mock.state.revision + 3 }
  await first.click()
  await expect(page.locator('.check-row').filter({ hasText: '量尺寸' }).getByRole('button', { name: '标为未完成' })).toBeVisible()
  expect(refused).toBe(1)
  const sent = mock.commands.filter((c) => c.type === 'toggleCheck')
  // The same request, so the server would not apply it twice.
  expect(sent).toHaveLength(2)
  expect(new Set(sent.map((c) => c.requestId)).size).toBe(1)
  await expect(page.locator('.connection-banner')).toHaveCount(0)
  expect(mock.errors).toEqual([])
})

test('U4 打勾的那件事本身被改了：不再发，照旧提示检查后重试', async ({ page }) => {
  const mock = await backend(page, { tasks: [task()] })
  await page.goto('/t/task-net')
  const first = page.locator('.check-row').filter({ hasText: '量尺寸' }).getByRole('button', { name: '完成' })
  await expect(first).toBeVisible()
  mock.refuse = (command) => (command.expectedRevision === mock.state.revision ? undefined : 'version_conflict')
  // Elsewhere, the same task's list was changed.
  let held = true
  await page.route((url) => url.pathname === '/v1/workspace', async (route) => {
    if (held && route.request().method() === 'GET' && mock.commands.length === 0) return route.fulfill({ json: { ...mock.state, revision: 1, tasks: [task()] } })
    return route.fulfill({ json: mock.state })
  })
  mock.state = { ...mock.state, revision: mock.state.revision + 1, tasks: [{ ...task(), checklist: [{ id: 'c1', text: '量尺寸（朝西那面）', done: false }, { id: 'c2', text: '下单', done: false }] }] }
  await first.click()
  held = false
  await expect(page.locator('.connection-banner')).toContainText('数据已在其他窗口或后台更新。已刷新，请检查后重试。')
  expect(mock.commands.filter((c) => c.type === 'toggleCheck')).toHaveLength(1)
  await expect(page.locator('.check-row').filter({ hasText: '量尺寸（朝西那面）' }).getByRole('button', { name: '完成' })).toBeVisible()
  expect(mock.errors).toEqual([])
})

test('手机宽度下「眼下」和筛选不撑破页面', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, { handover: { body: handover, builtAt: writtenAt, stale: true }, deadlines: f.deadlines, memories: f.memories })
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/library')
  await expect(part(page, '期限和固定安排').getByRole('group')).toHaveCount(4)
  expect(await fits(page)).toBe(true)
  await page.getByRole('searchbox', { name: '搜索分组' }).fill('a')
  expect(await fits(page)).toBe(true)
  expect(mock.errors).toEqual([])
})
