import { test, expect, type Page } from '@playwright/test'
import type { DeskTurn } from '../src/domain/desk'
import type { InProgress, Schedule, ScheduleEntry } from '../src/domain/schedule'
import type { Creation, Idea, Project, State, Task } from '../src/domain/types'

// Phase 3.5, the hall: dates from the table of deadlines on 今天 and 这几天,
// to-dos with no time under 在推进, and what the background made on its own
// under 你不在的时候. Everything here is made up: the person, the shop, what was said.
test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 1440, height: 1000 } })
const now = '2031-03-05T04:00:00Z' // Wednesday noon in Shanghai.
const day = (n: number) => `2031-03-${String(5 + n).padStart(2, '0')}`

function task(id: string, title: string, more: Partial<Task> = {}): Task {
  return { id, title, status: 'todo', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: '2031-03-01T02:00:00Z', updatedAt: '2031-03-04T02:00:00Z', ...more }
}
function entry(id: string, kind: ScheduleEntry['kind'], title: string, more: Partial<ScheduleEntry> = {}): ScheduleEntry {
  return { id, title, kind, at: null, dateOnly: false, timeNote: '', originalText: '', source: { kind: 'deadline', memoryId: `m-${id}`, deadlineId: `d-${id}` }, ...more }
}

function schedule(): Schedule {
  return {
    from: day(0), to: day(3), timezone: 'Asia/Shanghai',
    days: [
      { date: day(0), items: [
        entry('flour', 'deadline', '给面粉供应商回话', { at: '2031-03-05T02:00:00Z', originalText: '周三上午十点前要给面粉那边回话' }),
        entry('class', 'recurring', '裱花课', { at: '2031-03-05T11:00:00Z' }),
        entry('dentist', 'appointment', '看牙', { at: '2031-03-05T07:30:00Z', originalText: '牙医约在周三下午三点半' }),
        entry('permit', 'deadline', '交摊位申请表', { dateOnly: true, timeNote: '没说几点' }),
        // A to-do the workspace already has is placed by the workspace's rules, once.
        { ...entry('t-oven', 'task', '给烤箱除垢', { at: '2031-03-05T09:00:00Z' }), source: { kind: 'task', itemId: 'oven' } },
      ] },
      { date: day(1), items: [
        entry('market', 'appointment', '市集摊位续约面谈', { at: '2031-03-06T06:00:00Z' }),
        entry('class2', 'recurring', '晨跑', { at: '2031-03-05T23:00:00Z' }),
      ] },
      { date: day(2), items: [entry('tax', 'deadline', '报上个月的税', { dateOnly: true })] },
      { date: day(3), items: [] },
    ],
    unclear: [entry('visit', 'appointment', '去看林栖的新店', { timeNote: '没说哪天', originalText: '说好了找个时间去看林栖的新店' })],
    overdue: [
      entry('warranty', 'deadline', '给烤箱续保', { date: '2031-02-10', at: '2031-02-10T02:00:00Z', originalText: '烤箱的保修二月十号到期，要问一下续保' }),
      ...Array.from({ length: 6 }, (_, i) => entry(`old-${i}`, 'deadline', `旧期限 ${i + 1}`, { date: `2031-01-0${i + 1}`, at: `2031-01-0${i + 1}T02:00:00Z` })),
    ],
  }
}

interface Options { tasks?: Task[]; ideas?: Idea[]; projects?: Project[]; schedule?: Schedule; progress?: InProgress; turns?: DeskTurn[] }

async function backend(page: Page, options: Options = {}) {
  const mock = {
    state: {
      version: 1, revision: 1, memoryRevision: 1, budgetUsage: 0, notices: [],
      settings: { timezone: 'Asia/Shanghai', dailyBudget: 10, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00' },
      tasks: options.tasks ?? [], ideas: options.ideas ?? [], projects: options.projects ?? [],
      agents: [], memories: [], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {},
    } as State,
    schedule: options.schedule ?? schedule(),
    progress: options.progress ?? { items: [], total: 0, remaining: 0 },
    asked: [] as string[],
    commands: [] as Record<string, unknown>[],
    errors: [] as string[],
    failing: new Set<string>(),
  }
  page.on('pageerror', (e) => mock.errors.push(e.message))
  await page.clock.install({ time: new Date(now) })
  await page.route('**/v1/**', (route) => route.fulfill({ status: 500, json: { error: 'unexpected_request' } }))
  await page.route((url) => url.pathname === '/v1/workspace', (route) => route.fulfill({ json: mock.state }))
  await page.route((url) => url.pathname === '/v1/desk/turns', (route) => route.fulfill({ json: { conversationId: '', turns: options.turns ?? [] } }))
  const read = (path: string, answer: () => unknown) => page.route((url) => url.pathname === path, (route) => {
    const url = new URL(route.request().url())
    mock.asked.push(url.pathname + url.search)
    return mock.failing.has(path) ? route.fulfill({ status: 503, json: { error: 'storage_unavailable' } }) : route.fulfill({ json: answer() })
  })
  await read('/v1/workspace/schedule', () => mock.schedule)
  await read('/v1/workspace/in-progress', () => mock.progress)
  await page.route((url) => url.pathname === '/v1/workspace/commands', (route) => {
    const command = route.request().postDataJSON() as Record<string, unknown>
    mock.commands.push(command)
    const { state } = mock
    if (command.type === 'completeDeadline') mock.schedule.overdue = mock.schedule.overdue.filter((e) => e.source.deadlineId !== command.id)
    if (command.type === 'setTaskStatus') {
      state.tasks = state.tasks.map((t) => (t.id === command.id ? { ...t, status: 'done' } : t))
      mock.progress.items = mock.progress.items.filter((t) => t.id !== command.id)
    }
    if (command.type === 'undoAction') {
      // Undoing what made a thing takes the thing away; a project's to-dos go back to having none.
      const made = (c?: Creation) => c?.actionId === command.id
      const gone = state.projects.filter((p) => made(p.creation)).map((p) => p.id)
      state.projects = state.projects.filter((p) => !made(p.creation))
      state.tasks = state.tasks.filter((t) => !made(t.creation)).map((t) => (t.projectId && gone.includes(t.projectId) ? { ...t, projectId: undefined } : t))
      state.ideas = state.ideas.filter((i) => !made(i.creation))
    }
    state.revision++
    return route.fulfill({ json: state })
  })
  return mock
}

const today = (page: Page) => page.getByRole('region', { name: '今天', exact: true })
const titles = (page: Page, group: string) => today(page).locator('.hall-group').filter({ has: page.getByRole('heading', { name: group, exact: true }) }).first().locator('.h-title')

test('the day reads the schedule: appointments, deadlines, fixed arrangements and to-dos in one line of time', async ({ page }) => {
  const mock = await backend(page, { tasks: [task('oven', '给烤箱除垢', { scheduled: '2031-03-05T09:00:00Z' })] })
  await page.goto('/')
  // One read, for today and the three days after it, in the workspace's dates.
  await expect.poll(() => mock.asked.filter((a) => a.startsWith('/v1/workspace/schedule'))).toEqual([`/v1/workspace/schedule?from=${day(0)}&to=${day(3)}`])
  // What has passed, then what is for today with no hour, then the hours; the to-do the workspace already has is there once.
  await expect(titles(page, '按时间')).toHaveText(['给面粉供应商回话', '交摊位申请表', '看牙', '给烤箱除垢', '裱花课'])
  const line = today(page).locator('.hall-group').filter({ has: page.getByRole('heading', { name: '按时间' }) })
  await expect(line.locator('.hall-time')).toHaveText(['10:00', '今天', '15:30', '17:00', '19:00'])
  // What is past is above 「现在」 and dimmed; a deadline gone by this morning can be said to be met.
  const flour = line.locator('.hall-task').filter({ hasText: '给面粉供应商回话' })
  await expect(flour).toHaveClass(/past/)
  await expect(flour.locator('.h-note')).toHaveText('过了截止时间')
  await expect(flour.getByRole('button', { name: '做完了：给面粉供应商回话' })).toBeVisible()
  await expect(line.locator('.hall-task').filter({ hasText: '看牙' }).locator('.h-note')).toHaveText('预约')
  await expect(line.locator('.hall-task').filter({ hasText: '裱花课' }).locator('.h-note')).toHaveText('固定安排')
  await expect(line.locator('.hall-task').filter({ hasText: '交摊位申请表' }).locator('.h-note')).toHaveText('截止 · 没说几点')
  // Only what can be finished has a circle.
  await expect(line.locator('.hall-task').filter({ hasText: '看牙' }).getByRole('button', { name: /做完了/ })).toHaveCount(0)
  expect(mock.errors).toEqual([])
})

test('the next three days list deadlines and appointments, and end with the dates that were never pinned down', async ({ page }) => {
  const mock = await backend(page, { tasks: [task('rent', '交房租', { due: '2031-03-06T10:00:00Z' })] })
  await page.goto('/')
  const soon = today(page).locator('.hall-group').filter({ has: page.getByRole('heading', { name: '这几天', exact: true }) })
  // A fixed arrangement shows on its own day, not ahead of it.
  await expect(soon.locator('.h-title')).toHaveText(['市集摊位续约面谈', '交房租', '报上个月的税', '去看林栖的新店'])
  await expect(soon.locator('.hall-task').filter({ hasText: '市集摊位续约面谈' }).locator('.h-note')).toHaveText('明天 14:00 预约')
  await expect(soon.locator('.hall-task').filter({ hasText: '报上个月的税' }).locator('.h-note')).toHaveText('3月7日截止')
  const unclear = soon.getByRole('group', { name: '日期没说清的' })
  await expect(unclear.locator('.h-title')).toHaveText(['去看林栖的新店'])
  await expect(unclear.locator('.h-note')).toHaveText('原话：说好了找个时间去看林栖的新店')
  // It opens onto what was said and the memory under it.
  await unclear.getByRole('button', { name: /去看林栖的新店/ }).click()
  const sheet = page.getByRole('dialog')
  await expect(sheet.getByRole('heading', { name: '去看林栖的新店' })).toBeVisible()
  await expect(sheet).toContainText('预约 · 日期没说清')
  await expect(sheet.locator('.source-text')).toHaveText('说好了找个时间去看林栖的新店')
  await expect(sheet.getByRole('link', { name: '看这条记忆和它的来源' })).toHaveAttribute('href', '/library?m=m-visit')
  await expect(sheet.getByRole('button', { name: /做完了/ })).toHaveCount(0)
  expect(mock.errors).toEqual([])
})

test('a deadline that went by unmet looks like 已过截止 and can be said to be done, from the row or once opened', async ({ page }) => {
  const mock = await backend(page)
  await page.goto('/')
  const late = today(page).locator('.hall-group').filter({ has: page.getByRole('heading', { name: '在等你，或已经晚了' }) })
  // The latest first; years of them do not flood the column.
  await expect(late.locator('.h-title')).toHaveText(['给烤箱续保', '旧期限 6', '旧期限 5', '旧期限 4', '旧期限 3'])
  await expect(late.locator('.hall-task').first().locator('.h-note')).toHaveText('已过截止 · 2月10日')
  await late.getByRole('button', { name: '还有 2 条' }).click()
  await expect(late.locator('.h-title')).toHaveCount(7)

  await late.getByRole('button', { name: /^给烤箱续保/ }).click()
  const sheet = page.getByRole('dialog')
  await expect(sheet.locator('.source-text')).toHaveText('烤箱的保修二月十号到期，要问一下续保')
  await sheet.getByRole('button', { name: '做完了：给烤箱续保' }).click()
  // It is marked on the memory's deadline; no to-do is made.
  await expect.poll(() => mock.commands.map((c) => [c.type, c.id])).toEqual([['completeDeadline', 'd-warranty']])
  // Said, the sheet closes and the row is gone.
  await expect(sheet).toHaveCount(0)
  await expect(late.locator('.h-title').filter({ hasText: '给烤箱续保' })).toHaveCount(0)
  // The circle on the row does the same, and it can be taken back.
  await late.getByRole('button', { name: '做完了：旧期限 6' }).click()
  await expect.poll(() => mock.commands.at(-1)).toMatchObject({ type: 'completeDeadline', id: 'd-old-5' })
  const done = mock.commands.at(-1)!.requestId
  await page.getByRole('button', { name: '撤销' }).click()
  await expect.poll(() => mock.commands.at(-1)).toMatchObject({ type: 'undoAction', id: done })
  expect(mock.state.tasks).toEqual([])
  expect(mock.errors).toEqual([])
})

test('when the schedule cannot be read the to-dos still show, and the column says what is missing', async ({ page }) => {
  const mock = await backend(page, { tasks: [task('oven', '给烤箱除垢', { scheduled: '2031-03-05T09:00:00Z' })] })
  mock.failing.add('/v1/workspace/schedule')
  await page.goto('/')
  await expect(titles(page, '按时间')).toHaveText(['给烤箱除垢'])
  const problem = today(page).getByRole('alert')
  await expect(problem).toContainText('期限和固定安排没读出来，上面只有待办：服务器出错了（错误 503，storage_unavailable）')
  mock.failing.clear()
  await problem.getByRole('button', { name: '重试' }).click()
  await expect(titles(page, '按时间')).toHaveText(['给面粉供应商回话', '交摊位申请表', '看牙', '给烤箱除垢', '裱花课'])
  await expect(today(page).getByRole('alert')).toHaveCount(0)
})

test('to-dos with no time are under 在推进, in the order and number the server gives', async ({ page }) => {
  const menu = task('menu', '重写春季菜单', { projectId: 'spring', updatedAt: '2031-03-05T01:00:00Z' })
  const sign = task('sign', '换门口的招牌', { updatedAt: '2031-03-02T04:00:00Z' })
  const mock = await backend(page, {
    tasks: [sign, menu],
    projects: [{ id: 'spring', name: '春季上新', goal: '', status: 'active', updatedAt: '2031-03-01T00:00:00Z' }],
    progress: { items: [menu, sign], total: 9, remaining: 7 },
  })
  await page.goto('/')
  const wall = page.getByRole('region', { name: '在推进' })
  await expect(wall.locator('.h-title')).toHaveText(['重写春季菜单', '换门口的招牌'])
  await expect(wall.locator('.h-note')).toHaveText(['春季上新 · 3 小时前', '3 天前'])
  await expect(wall).toContainText('还有 7 件没列出来，在各自的项目里，搜索也能找到。')
  // They are not on the line of time.
  await expect(today(page).getByText('重写春季菜单')).toHaveCount(0)
  await wall.getByRole('button', { name: '做完了：换门口的招牌' }).click()
  await expect.poll(() => mock.commands.at(-1)).toMatchObject({ type: 'setTaskStatus', id: 'sign', status: 'done' })
  await expect(wall.locator('.h-title')).toHaveText(['重写春季菜单'])
  await expect(wall.getByRole('link', { name: /重写春季菜单/ })).toHaveAttribute('href', '/t/menu')
  expect(mock.errors).toEqual([])
})

test('在推进 takes no room while there is nothing in it, and says why when it cannot be read', async ({ page }) => {
  const mock = await backend(page)
  await page.goto('/')
  await expect(page.getByRole('region', { name: '项目' })).toBeVisible()
  await expect.poll(() => mock.asked).toContain('/v1/workspace/in-progress')
  await expect(page.getByRole('region', { name: '在推进' })).toHaveCount(0)
  mock.failing.add('/v1/workspace/in-progress')
  await page.reload()
  await expect(page.getByRole('region', { name: '在推进' }).getByRole('alert')).toContainText('没读出来：服务器出错了（错误 503，storage_unavailable）')
})

test('what the background made on its own is under 你不在的时候, one line each with who, on what, and 撤销', async ({ page }) => {
  const at = '2031-03-05T02:30:00Z'
  const said = { sourceId: 's-1', label: '你说的话', excerpt: '周五前把发票寄给老周', at }
  const mock = await backend(page, {
    tasks: [
      task('invoice', '把发票寄给老周', { createdAt: at, creation: { by: 'background_extraction', source: said, actionId: 'act-task' } }),
      task('order', '订下个月的黄油', { createdAt: at, projectId: 'fair', creation: { by: 'secretary', actionId: 'act-sec' } }),
    ],
    ideas: [{ id: 'cold', title: '夏天试试冷萃', body: '', status: 'active', conditions: [], remindersOn: true, evolution: [], sources: [], createdAt: '2031-03-05T03:00:00Z', updatedAt: at, creation: { by: 'background_extraction', source: { sourceId: 's-2', label: '三月进货单', at }, actionId: 'act-idea' } }],
    projects: [{ id: 'fair', name: '春季市集', goal: '', status: 'active', createdAt: '2031-03-05T03:30:00Z', updatedAt: at, creation: { by: 'background_topic', memoryIds: Array.from({ length: 11 }, (_, i) => `m${i}`), groupKey: 'k/fair', actionId: 'act-project' } }],
  })
  await page.goto('/')
  const away = page.getByRole('region', { name: '你不在的时候' })
  // What the secretary made has its receipt in the conversation, not here.
  await away.getByRole('button', { name: /你不在的时候：3 件/ }).click()
  await expect(away.locator('.h-text')).toHaveText([
    '后台看这个主题攒了 11 条记忆，建了项目「春季市集」',
    '后台整理时建了想法「夏天试试冷萃」 · 依据「三月进货单」',
    '后台整理时建了待办「把发票寄给老周」 · 依据原话「周五前把发票寄给老周」',
  ])
  await expect(away.getByRole('link', { name: /建了待办「把发票寄给老周」/ })).toHaveAttribute('href', '/t/invoice')
  await expect(away.getByRole('button', { name: '撤销' })).toHaveCount(3)

  // Taking the project back leaves its to-do, with no project.
  await away.getByRole('listitem').filter({ hasText: '春季市集' }).getByRole('button', { name: '撤销' }).click()
  await expect.poll(() => mock.commands.at(-1)).toMatchObject({ type: 'undoAction', id: 'act-project' })
  await expect(away.locator('.h-text')).toHaveCount(2)
  await expect(page.getByText('撤销了', { exact: true })).toBeVisible()
  expect(mock.state.tasks.find((t) => t.id === 'order')?.projectId).toBeUndefined()
  await away.getByRole('listitem').filter({ hasText: '把发票寄给老周' }).getByRole('button', { name: '撤销' }).click()
  await expect.poll(() => mock.commands.at(-1)).toMatchObject({ type: 'undoAction', id: 'act-task' })
  await expect(away.locator('.h-text')).toHaveText(['后台整理时建了想法「夏天试试冷萃」 · 依据「三月进货单」'])
  expect(mock.errors).toEqual([])
})

test('the secretary says it made a project, and that can be taken back from the receipt', async ({ page }) => {
  const turn: DeskTurn = {
    id: 'turn-1', text: '下个月要办一场春季市集，得订摊位、做菜单、印海报', reply: '', cards: [], ask: null, agent: '模型', createdAt: now,
    receipts: [{ actionId: 'act-fair', op: 'create_project', text: '建了项目「春季市集」', thingId: 'fair', undoable: true, status: 'done' }],
  }
  const mock = await backend(page, {
    turns: [turn],
    projects: [{ id: 'fair', name: '春季市集', goal: '', status: 'active', updatedAt: now, creation: { by: 'secretary', actionId: 'act-fair' } }],
    tasks: [task('stall', '订摊位', { projectId: 'fair', due: '2031-03-20T02:00:00Z' })],
  })
  await page.addInitScript(() => localStorage.setItem('pcas.secretary.desk', JSON.stringify({ conversationId: '11111111-1111-4111-8111-111111111111', unanswered: [] })))
  await page.goto('/')
  const receipt = page.locator('.sec-receipts li').filter({ hasText: '建了项目「春季市集」' })
  await receipt.getByRole('button', { name: '撤销' }).click()
  await expect.poll(() => mock.commands.at(-1)).toMatchObject({ type: 'undoAction', id: 'act-fair' })
  await expect(receipt).toContainText('已撤销')
  await expect(page.getByRole('region', { name: '项目' }).getByText('春季市集')).toHaveCount(0)
  expect(mock.state.tasks[0].projectId).toBeUndefined()
})

test('settings no longer has a switch for making to-dos from material', async ({ page }) => {
  const mock = await backend(page)
  await page.route((url) => /^\/v1\/(connectors|notify\/config|models\/openai)/.test(url.pathname), (route) => route.fulfill({ json: url(route.request().url()) }))
  await page.goto('/settings')
  await expect(page.getByRole('switch', { name: '事项提醒', exact: true })).toBeVisible()
  await expect(page.getByRole('switch', { name: /直接创建/ })).toHaveCount(0)
  await expect(page.getByText(/自动采纳|明确的待办/)).toHaveCount(0)
  expect(mock.commands).toEqual([])
})

/** What the settings page reads besides the workspace, empty. */
function url(href: string): unknown {
  const path = new URL(href).pathname
  if (path === '/v1/connectors') return []
  if (path === '/v1/notify/config') return { webPush: { publicKey: '', subscriptions: 0 }, telegram: { configured: false, chatId: '' } }
  return null
}

test('on a phone the short column keeps its five rows, dates and to-dos together', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const mock = await backend(page)
  await page.goto('/')
  // Today comes before the deadlines of earlier days, however many of those there are.
  await expect(today(page).locator('.hall-task .h-title')).toHaveText(['给烤箱续保', '交摊位申请表', '看牙', '给烤箱除垢', '裱花课'])
  await today(page).getByRole('button', { name: /^还有 \d+ 件$/ }).click()
  await expect(today(page).getByRole('group', { name: '日期没说清的' })).toBeVisible()
  await expect(page.getByRole('region', { name: '今天', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  expect(mock.errors).toEqual([])
})
