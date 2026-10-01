import { test, expect, type Page } from '@playwright/test'
import type { State } from '../src/domain/types'
import type { DeskTurn } from '../src/domain/desk'

// Melbourne browser against a Shanghai workspace, including the DST change.
test.use({ timezoneId: 'Australia/Melbourne', viewport: { width: 1440, height: 1000 } })
const now = '2026-10-03T14:30:00Z' // Oct 4 00:30 in Melbourne, Oct 3 22:30 in Shanghai.
function workspace(timezone = 'Asia/Shanghai'): State {
  return {
    version: 1, revision: 1, budgetUsage: 0, notices: [],
    settings: { timezone, dailyBudget: 10, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00' },
    tasks: [], ideas: [], projects: [], agents: [], memories: [], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {},
  }
}
async function mock(page: Page, state = workspace(), reject = false) {
  const commands: Record<string, unknown>[] = []
  const errors: string[] = []
  const zones: string[] = []
  page.on('pageerror', e => errors.push(e.message))
  await page.route('**/v1/**', route => route.fulfill({ json: {} }))
  await page.route('**/v1/connectors', route => route.fulfill({ json: [] }))
  await page.route('**/v1/notify/config', route => route.fulfill({ json: { webPush: { publicKey: '', subscriptions: 0 }, telegram: { configured: false, chatId: '' } } }))
  await page.route('**/v1/models/openai', route => route.fulfill({ json: null }))
  await page.route('**/v1/desk/turns?*', route => route.fulfill({ json: { turns: [] } }))
  await page.route('**/v1/workspace', route => {
    zones.push(route.request().headers()['x-pcas-timezone'])
    return route.fulfill({ json: state })
  })
  await page.route('**/v1/workspace/commands', route => {
    const command = route.request().postDataJSON()
    commands.push(command)
    if (reject) return route.fulfill({ status: 400, json: { error: 'invalid_timezone' } })
    Object.assign(state.settings, command.patch)
    state.revision++
    return route.fulfill({ json: state })
  })
  return { state, commands, errors, zones }
}

test('settings searches IANA zones, puts common cities first, and persists through updateSettings', async ({ page }) => {
  const backend = await mock(page)
  await page.goto('/settings')
  await page.getByRole('button', { name: '时区', exact: true }).click()
  const options = page.getByRole('listbox', { name: '时区选项' })
  await expect(options.getByRole('option').first()).toContainText('Australia/Melbourne')
  await page.getByRole('searchbox', { name: '搜索时区' }).fill('墨尔本')
  await expect(options.getByRole('option')).toHaveCount(1)
  await options.getByRole('option').click()
  await expect(page.getByRole('button', { name: '时区', exact: true })).toHaveText('Australia/Melbourne')
  expect(backend.commands[0]).toMatchObject({ type: 'updateSettings', patch: { timezone: 'Australia/Melbourne' }, expectedRevision: 1 })
  expect(backend.zones[0]).toBe('Australia/Melbourne')
  await page.reload()
  await expect(page.getByRole('button', { name: '时区', exact: true })).toHaveText('Australia/Melbourne')
  await page.getByRole('button', { name: '时区', exact: true }).click()
  await page.getByRole('searchbox', { name: '搜索时区' }).fill('Europe/Berlin')
  await expect(options.getByRole('option')).toHaveCount(1)
  expect(backend.errors).toEqual([])
})

test('a timezone save failure explains the fix and keeps the saved zone', async ({ page }) => {
  const backend = await mock(page, workspace(), true)
  await page.goto('/settings')
  await page.getByRole('button', { name: '时区', exact: true }).click()
  await page.getByRole('searchbox', { name: '搜索时区' }).fill('Melbourne')
  await page.getByRole('option').click()
  await expect(page.locator('.connection-banner')).toContainText('请选择有效的 IANA 时区')
  await expect(page.getByRole('button', { name: '时区', exact: true })).toHaveText('Asia/Shanghai')
  expect(backend.state.settings.timezone).toBe('Asia/Shanghai')
})

test('a mismatch prompt is at the top and changes the workspace in one click', async ({ page }) => {
  const backend = await mock(page)
  await page.goto('/')
  const hint = page.locator('.timezone-hint')
  // It says which zone is in use, and names the device's zone once, on the button that switches to it.
  await expect(hint.locator('span')).toHaveText('时间按 Asia/Shanghai 显示，和这台设备不同')
  expect((await hint.innerText()).split('Australia/Melbourne')).toHaveLength(2)
  expect((await hint.boundingBox())!.y).toBeLessThan((await page.locator('.hall-today').boundingBox())!.y)
  await hint.getByRole('button', { name: '改成 Australia/Melbourne' }).click()
  await expect(hint).toHaveCount(0)
  expect(backend.commands[0]).toMatchObject({ type: 'updateSettings', patch: { timezone: 'Australia/Melbourne' } })
  await page.reload()
  await expect(hint).toHaveCount(0)
  expect(backend.errors).toEqual([])
})

test('closing a mismatch persists in this browser across reload and navigation, including mobile', async ({ page }) => {
  const backend = await mock(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  await expect(page.locator('.timezone-hint')).toBeVisible()
  // On a phone the change sits on its own row under the sentence, and × stays beside the sentence.
  const [words, change, close] = await Promise.all([
    page.locator('.timezone-hint > span').boundingBox(),
    page.getByRole('button', { name: '改成 Australia/Melbourne' }).boundingBox(),
    page.getByRole('button', { name: '关闭时区提示' }).boundingBox(),
  ])
  expect(change!.y).toBeGreaterThanOrEqual(words!.y + words!.height - 1)
  expect(close!.y).toBeLessThan(change!.y)
  await page.getByRole('button', { name: '关闭时区提示' }).click()
  await expect(page.locator('.timezone-hint')).toHaveCount(0)
  await page.reload()
  await expect(page.locator('.hall-today')).toBeVisible()
  await expect(page.locator('.timezone-hint')).toHaveCount(0)
  await page.goto('/settings')
  await page.goto('/')
  await expect(page.locator('.timezone-hint')).toHaveCount(0)
  expect(backend.commands).toEqual([])
})

test('matching timezone and IANA aliases do not prompt', async ({ page }) => {
  await mock(page, workspace('Australia/Victoria'))
  await page.goto('/')
  await expect(page.locator('.hall-today')).toBeVisible()
  await expect(page.locator('.timezone-hint')).toHaveCount(0)
})

test('the same due/scheduled/follow-up tasks group by workspace date, including a DST day', async ({ page }) => {
  await page.clock.install({ time: new Date(now) })
  const state = workspace()
  const task = { id: 'due', title: '跨日截止', status: 'todo' as const, notes: '', due: '2026-10-03T15:00:00Z', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: now, updatedAt: now }
  state.tasks = [task, { ...task, id: 'scheduled', title: '跨日安排', due: undefined, scheduled: task.due },
    { ...task, id: 'follow', title: '跨日跟进', status: 'waiting', due: undefined, triggers: [{ id: 'follow-up', kind: 'time', description: '跟进', active: true, nextAt: '2026-10-03T17:00:00Z' }] },
    { ...task, id: 'dst', title: '夏令时后', due: '2026-10-04T12:30:00Z' }]
  const backend = await mock(page, state)
  await page.goto('/')
  const today = page.locator('.hall-today')
  const row = (title: string) => today.locator('.hall-task').filter({ hasText: title })
  await expect(today.locator('.hall-head')).toContainText('10月3日')
  await expect(row(task.title).locator('.hall-time')).toHaveText('23:00')
  await expect(row('跨日安排').locator('.hall-time')).toHaveText('23:00')
  await expect(row('跨日跟进')).toHaveCount(0)
  await expect(row('夏令时后').locator('.h-note')).toHaveText('明天截止')
  await expect(today.getByRole('separator')).toHaveAttribute('aria-label', '现在 22:30')
  await page.getByRole('button', { name: '改成 Australia/Melbourne' }).click()
  await expect(today.locator('.hall-head')).toContainText('10月4日')
  await expect(row(task.title).locator('.hall-time')).toHaveText('01:00')
  await expect(row('跨日安排').locator('.hall-time')).toHaveText('01:00')
  await expect(row('跨日跟进').locator('.h-note')).toContainText('该跟进了')
  await expect(row('夏令时后').locator('.hall-time')).toHaveText('23:30')
  await expect(today.getByRole('separator')).toHaveAttribute('aria-label', '现在 00:30')
  expect(backend.errors).toEqual([])
})

for (const width of [1440, 390]) {
  test(`workspace timezone follows a task across hall, cards, details and project at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 1000 })
    await page.clock.install({ time: new Date(now) })
    const state = workspace()
    const task = {
      id: 'cross-zone', title: '跨时区回信', status: 'todo' as const, notes: '', projectId: 'project',
      due: '2026-10-03T15:00:00Z', dependsOn: [], checklist: [], sources: [], history: [], createdAt: now, updatedAt: now,
      triggers: [{ id: 'due-reminder', kind: 'time' as const, description: '回信', active: true, nextAt: '2026-10-03T14:45:00Z' }],
    }
    state.tasks = [task]
    state.projects = [{ id: 'project', name: '出行项目', goal: '', status: 'active', progress: '', nextSteps: [], updatedAt: now }]
    state.agents = [{ id: 'model', name: '模型', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false }]
    const backend = await mock(page, state)
    const receipt = '已建：今天 23:00 跨时区回信 · 22:45 提醒'
    const turns: DeskTurn[] = []
    await page.route('**/v1/desk/turns?*', route => route.fulfill({ json: { turns } }))
    await page.route('**/v1/desk/turn', route => {
      const request = route.request().postDataJSON()
      const turn: DeskTurn = {
        id: 'turn', text: request.text, reply: '安排好了。', agent: '模型', createdAt: now, ask: null,
        cards: [
          { kind: 'tasks', items: [{ thingId: task.id, title: task.title, due: task.due, status: task.status, project: '出行项目' }] },
          { kind: 'sources', items: [{ memoryId: 'm1', version: 1, text: '回信安排', sourceId: null, sourceVersion: null, at: '2026-10-03T13:00:00Z' }] },
          { kind: 'timeline', title: '安排经过', items: [{ at: '2026-10-03T13:00:00Z', text: '回信安排', status: 'open', memoryId: 'm1', thingId: null }] },
        ],
        receipts: [{ actionId: 'action', op: 'create_task', text: receipt, thingId: task.id, undoable: true, status: 'done' }],
      }
      turns.push(turn)
      return route.fulfill({ json: { conversationId: request.conversationId, turn, state } })
    })
    await page.goto('/')
    const input = page.getByRole('textbox', { name: '跟秘书说' })
    await input.fill('显示安排')
    await input.press('Enter')
    await expect(page.locator('.hall-task').filter({ hasText: task.title }).locator('.hall-time')).toHaveText('23:00')
    await expect.soft(page.locator('.sec-tasks .k-meta')).toContainText('今天 23:00')
    await expect.soft(page.locator('.sec-timeline time')).toHaveText('今天')

    await expect(page.locator('.sec-receipt .r-what')).toHaveCount(1)
    await page.screenshot({ path: test.info().outputPath('shanghai.png'), fullPage: true, animations: 'disabled' })
    await page.goto(`/t/${task.id}`)
    await expect.soft(page.locator('.info-line')).toContainText('今天 23:00 截止')
    await expect.soft(page.locator('.info-line')).toContainText('22:45 提醒')
    await page.goto('/t/project')
    await expect.soft(page.locator('.item-row .reason')).toHaveText('今天 23:00 截止')

    await page.goto('/')
    await page.getByRole('button', { name: '改成 Australia/Melbourne' }).click()
    await expect(page.locator('.hall-task').filter({ hasText: task.title }).locator('.hall-time')).toHaveText('01:00')
    await expect(page.locator('.sec-tasks .k-meta')).toContainText('今天 01:00')
    await expect(page.locator('.sec-timeline time')).toHaveText('昨天')
    await expect(page.locator('.sec-receipt .r-what')).toHaveText(receipt)
    await page.reload()
    await expect(page.locator('.sec-tasks .k-meta')).toContainText('今天 01:00')
    await expect(page.locator('.sec-receipt .r-what')).toHaveText(receipt)
    await expect(page.locator('.sec-receipt .r-what')).toHaveCount(1)
    await page.screenshot({ path: test.info().outputPath('melbourne.png'), fullPage: true, animations: 'disabled' })
    await page.goto(`/t/${task.id}`)
    await expect(page.locator('.info-line')).toContainText('今天 01:00 截止')
    await expect(page.locator('.info-line')).toContainText('00:45 提醒')
    await page.goto('/t/project')
    await expect(page.locator('.item-row .reason')).toHaveText('今天 01:00 截止')
    expect(backend.state.tasks[0].due).toBe('2026-10-03T15:00:00Z')
    expect(backend.state.tasks[0].triggers[0].nextAt).toBe('2026-10-03T14:45:00Z')
    expect(backend.errors).toEqual([])
  })
}

// Literal expected calendar values, independent of the formatting algorithm.
for (const scenario of [
  { name: 'spring DST', now: '2026-10-03T14:30:00Z', iso: '2026-10-04T13:30:00Z', zone: 'Australia/Melbourne', days: 1, text: '明天 00:30' },
  { name: 'autumn DST', now: '2026-04-04T13:30:00Z', iso: '2026-04-05T13:30:00Z', zone: 'Australia/Melbourne', days: 0, text: '今天 23:30' },
  { name: 'Shanghai year boundary', now: '2026-12-31T13:30:00Z', iso: '2027-01-02T13:00:00Z', zone: 'Asia/Shanghai', days: 2, text: '2027年1月2日 周六 21:00' },
  { name: 'Melbourne year boundary', now: '2026-12-31T13:30:00Z', iso: '2027-01-02T13:00:00Z', zone: 'Australia/Melbourne', days: 2, text: '1月3日 周日 00:00' },
  { name: 'past date in previous year', now: '2026-12-31T13:30:00Z', iso: '2026-12-29T13:00:00Z', zone: 'Australia/Melbourne', days: -2, text: '2026年12月30日 周三 00:00' },
]) {
  test(`zoned calendar formatting handles ${scenario.name}`, async ({ page }) => {
    await page.clock.install({ time: new Date(scenario.now) })
    const state = workspace(scenario.zone)
    state.agents = [{ id: 'model', name: '模型', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false }]
    await mock(page, state)
    await page.route('**/v1/desk/turn', route => route.fulfill({ json: {
      conversationId: route.request().postDataJSON().conversationId, state,
      turn: { id: 'calendar', text: '显示日程', reply: '', agent: '模型', createdAt: scenario.now, ask: null, receipts: [],
        cards: [{ kind: 'tasks', items: [{ thingId: 'calendar-task', title: '日历边界', due: scenario.iso, status: 'todo', project: null }] }] },
    } }))
    await page.goto('/')
    await page.getByRole('textbox', { name: '跟秘书说' }).fill('显示日程')
    await page.getByRole('textbox', { name: '跟秘书说' }).press('Enter')
    await expect(page.locator('.sec-tasks .k-meta')).toHaveText(scenario.text)
  })
}

test('project follow-up and upcoming groups use the same calendar as the hall', async ({ page }) => {
  await page.clock.install({ time: new Date(now) })
  const state = workspace()
  const base = { status: 'todo' as const, notes: '', projectId: 'project', dependsOn: [], checklist: [], sources: [], history: [], createdAt: now, updatedAt: now, triggers: [] }
  state.projects = [{ id: 'project', name: '跟进项目', goal: '', status: 'active', progress: '', nextSteps: [], updatedAt: now }]
  state.tasks = [
    { ...base, id: 'follow', title: '跨日跟进', status: 'waiting', triggers: [{ id: 'follow-up', kind: 'time', description: '跟进', active: true, nextAt: '2026-10-03T17:00:00Z' }] },
    { ...base, id: 'soon', title: '临近截止', due: '2026-10-04T12:30:00Z' },
  ]
  const backend = await mock(page, state)
  await page.goto('/t/project')
  const row = (title: string) => page.locator('.item-row').filter({ hasText: title })
  await expect.soft(row('跨日跟进').locator('.reason')).toHaveText('等对方回复 · 明天跟进')
  await expect.soft(row('临近截止').locator('.reason')).toHaveText('明天截止')
  await page.goto('/')
  await expect(page.locator('.hall-task').filter({ hasText: '跨日跟进' })).toHaveCount(0)
  await expect(page.locator('.hall-task').filter({ hasText: '临近截止' })).toContainText('明天截止')
  await page.getByRole('button', { name: '改成 Australia/Melbourne' }).click()
  await expect(page.locator('.hall-task').filter({ hasText: '跨日跟进' })).toContainText('该跟进了')
  await page.goto('/t/project')
  await expect(row('跨日跟进').locator('.reason')).toHaveText('该跟进了：对方还没回')
  await expect(row('临近截止').locator('.reason')).toHaveText('今天 23:30 截止')
  expect(backend.errors).toEqual([])
})

test('source records, background jobs and settings timestamps follow workspace changes', async ({ page }) => {
  await page.clock.install({ time: new Date(now) })
  const state = workspace()
  const due = '2026-10-03T15:00:00Z'
  state.sources = [{ id: 'source', name: '跨日来源', method: '导入', status: 'manual', note: '', itemCount: 1, lastSyncAt: '2026-08-31T15:00:00Z' }]
  state.jobs = [{ id: 'job', title: '跨日同步', trigger: 'time', status: 'waiting', detail: '', createdAt: now, nextRunAt: due }]
  const backend = await mock(page, state)
  await page.route('**/v1/memory/sources/source', route => route.fulfill({ json: { derived: [], processing: [], source: { id: 'source', version: 1, title: '跨日来源', text: '合成原文', recorded_at: due, has_attachment: false, attachment_missing: false, representation: 'original' } } }))
  await page.route('**/v1/memory/summary', route => route.fulfill({ json: { text: '合成摘要', coverage: { gaps: [] }, dependencies: [] } }))
  await page.route('**/v1/connectors', route => route.fulfill({ json: [{ id: 'connector', name: '测试接入', kind: 'webhook', enabled: true, version: 1, interval_seconds: 300, status: 'idle', imported: 1, gaps: [], last_sync: due }] }))
  await page.route('**/v1/models', route => route.fulfill({ json: { chatgptEnabled: true, chatgptDirectEnabled: false } }))
  await page.route('**/v1/chatgpt/account', route => route.fulfill({ json: { account: { type: 'chatgpt', email: 'synthetic@example.test' } } }))
  await page.route('**/v1/chatgpt/limits', route => route.fulfill({ json: { rateLimits: { primary: { usedPercent: 10, resetsAt: Date.parse(due) / 1000 } } } }))
  for (const [zone, day, clock, oldDate] of [
    ['Asia/Shanghai', '10/3/2026', '11:00:00 PM', '8月31日'],
    ['Australia/Melbourne', '10/4/2026', '1:00:00 AM', '9月1日'],
  ]) {
    await page.goto('/library?tab=sources')
    await expect(page.locator('.source-card-foot')).toContainText(`${oldDate}更新`)
    await expect(page.locator('.list')).toContainText(`下次：今天 ${zone === 'Asia/Shanghai' ? '23:00' : '01:00'}`)
    await page.getByRole('button', { name: '查看原文：跨日来源' }).click()
    await expect(page.getByText('记录于', { exact: false })).toContainText(`${day}, ${clock}`)
    await page.goto('/settings')
    await expect(page.getByText('最近同步', { exact: false })).toContainText(`${day}, ${clock}`)
    await page.getByRole('button', { name: /^ChatGPT 订阅（Codex 登录）/ }).click()
    await expect(page.getByText('预计', { exact: false })).toContainText(`${day}, ${clock}`)
    if (zone === 'Asia/Shanghai') {
      await page.getByRole('button', { name: '时区', exact: true }).click()
      await page.getByRole('searchbox', { name: '搜索时区' }).fill('Melbourne')
      await page.getByRole('option').click()
      await expect(page.getByRole('button', { name: '时区', exact: true })).toHaveText('Australia/Melbourne')
    }
  }
  expect(backend.errors).toEqual([])
})

for (const scenario of [
  { zone: 'Asia/Shanghai', due: '明天 00:15 截止', reminder: '今天 23:45 提醒' },
  { zone: 'Australia/Melbourne', due: '今天 03:15 截止', reminder: '01:45 提醒' },
  { zone: undefined, due: '今天 16:15 截止', reminder: '15:45 提醒' },
]) {
  test(`reminder date and DST clock use ${scenario.zone ?? 'UTC fallback'}`, async ({ page }) => {
    await page.clock.install({ time: new Date(now) })
    const state = workspace()
    state.settings.timezone = scenario.zone
    state.tasks = [{ id: 'reminder-date', title: '跨日提醒', status: 'todo', notes: '', due: '2026-10-03T16:15:00Z', dependsOn: [], checklist: [], sources: [], history: [], createdAt: now, updatedAt: now,
      triggers: [{ id: 'due-reminder', kind: 'time', description: '回信', active: true, nextAt: '2026-10-03T15:45:00Z' }] }]
    const backend = await mock(page, state)
    await page.goto('/t/reminder-date')
    await expect(page.locator('.info-line')).toContainText(scenario.due)
    await expect(page.locator('.info-line')).toContainText(scenario.reminder)
    expect(backend.state.tasks[0].triggers[0].nextAt).toBe('2026-10-03T15:45:00Z')
    expect(backend.errors).toEqual([])
  })
}
