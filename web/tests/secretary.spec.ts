import { test, expect, type Page } from '@playwright/test'
import type { DeskTurn, DeskTurnRequest, Receipt } from '../src/domain/desk'
import type { State } from '../src/domain/types'

// Every API call stays in the browser mock; no backend or model is needed.

type Command = { type: string; id?: string; status?: string; requestId: string; expectedRevision: number }
type Reply = Partial<DeskTurn> | 'abort' | Promise<Partial<DeskTurn> | 'abort'>

function workspace(): State {
  const at = new Date().toISOString()
  return {
    version: 1, revision: 1, budgetUsage: 0,
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00' },
    tasks: [
      { id: 'task', title: '给张三回邮件', notes: '', status: 'todo', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: at, updatedAt: at },
      { id: 'rent', title: '交房租', notes: '', status: 'todo', due: at, dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: at, updatedAt: at },
    ],
    ideas: [], projects: [{ id: 'project', name: 'A 项目', goal: '', status: 'active', progress: '', nextSteps: [], updatedAt: at }],
    agents: [{ id: 'model', name: '模型', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false }],
    memories: [], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {},
  }
}

const created: Receipt = { actionId: 'act-1', op: 'create_task', text: '已建：周五 15:00 给张三回邮件 · A 项目 · 14:30 提醒', thingId: 'task', undoable: true, status: 'done' }

interface Backend {
  commands: Command[]
  turns: DeskTurnRequest[]
  restores: string[]
  errors: string[]
  snapshot: State
}

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/

async function mockBackend(
  page: Page,
  reply: (request: DeskTurnRequest, n: number) => Reply,
  undo?: (command: Command) => { status: number; json: unknown } | undefined,
  conversations = new Map<string, DeskTurn[]>(),
): Promise<Backend> {
  const backend: Backend = { commands: [], turns: [], restores: [], errors: [], snapshot: workspace() }
  page.on('pageerror', (e) => backend.errors.push(e.message))
  await page.route('**/v1/**', (route) => route.fulfill({ status: 500, json: { error: 'unexpected_mock_request' } }))
  // The hall's own reads (phase 3.5): no dates and nothing under 在推进.
  await page.route('**/v1/workspace/schedule?*', (route) => route.fulfill({ json: { days: [], unclear: [], overdue: [] } }))
  await page.route('**/v1/workspace/in-progress', (route) => route.fulfill({ json: { items: [], total: 0, remaining: 0 } }))
  await page.route((url) => url.pathname === '/v1/workspace', (route) => route.fulfill({ json: backend.snapshot }))
  await page.route((url) => url.pathname === '/v1/workspace/commands', async (route) => {
    const body = route.request().postDataJSON() as Command
    backend.commands.push(body)
    const custom = body.type === 'undoAction' ? undo?.(body) : undefined
    if (custom && custom.status !== 200) return route.fulfill(custom)
    if (body.type === 'setTaskStatus') backend.snapshot.tasks = backend.snapshot.tasks.map((t) => (t.id === body.id ? { ...t, status: body.status as 'done' } : t))
    backend.snapshot = { ...backend.snapshot, revision: backend.snapshot.revision + 1 }
    return route.fulfill({ json: backend.snapshot })
  })
  await page.route((url) => url.pathname === '/v1/desk/turn', async (route) => {
    const body = route.request().postDataJSON() as DeskTurnRequest
    backend.turns.push(body)
    const answer = await reply(body, backend.turns.length)
    if (answer === 'abort') return route.abort('failed')
    const conversationId = body.conversationId
    const turn: DeskTurn = { id: `turn-${body.requestId}`, text: body.text, reply: '', cards: [], receipts: [], ask: null, agent: '模型', createdAt: new Date().toISOString(), ...answer }
    const list = conversations.get(conversationId) ?? []
    // The server deduplicates a repeated requestId.
    if (!list.some((t) => t.id === turn.id)) list.push(turn)
    conversations.set(conversationId, list)
    return route.fulfill({ json: { conversationId, turn, state: backend.snapshot } })
  })
  await page.route((url) => url.pathname === '/v1/desk/turns', (route) => {
    const id = new URL(route.request().url()).searchParams.get('conversationId') ?? ''
    backend.restores.push(id)
    return route.fulfill({ json: { conversationId: id, turns: conversations.get(id) ?? [] } })
  })
  return backend
}

const input = (page: Page) => page.getByRole('textbox', { name: '跟秘书说' })

async function say(page: Page, text: string) {
  await input(page).fill(text)
  await input(page).press('Enter')
}

test('a sentence comes back as a one-line receipt, and the conversation returns after a reload', async ({ page }) => {
  const backend = await mockBackend(page, () => ({ reply: '好。', receipts: [created] }))
  await page.goto('/')
  // The draft is kept across a reload until it is sent.
  await input(page).fill('周五下午三点给张三回邮件，算在 A 项目里')
  await page.reload()
  await expect(input(page)).toHaveValue('周五下午三点给张三回邮件，算在 A 项目里')
  await input(page).press('Enter')
  const receipt = page.locator('.sec-receipt').filter({ hasText: created.text })
  await expect(receipt).toBeVisible()
  await expect(receipt.getByRole('link', { name: '给张三回邮件' })).toHaveAttribute('href', '/t/task')
  await expect(input(page)).toHaveValue('')
  expect(backend.turns).toHaveLength(1)
  expect(backend.turns[0]).toMatchObject({ thingId: null, text: '周五下午三点给张三回邮件，算在 A 项目里', agentId: 'model' })
  expect(backend.turns[0].conversationId).toMatch(uuid)

  await page.reload()
  await expect(receipt).toBeVisible()
  await expect(page.getByText('好。', { exact: true })).toBeVisible()
  expect(backend.restores).toEqual([backend.turns[0].conversationId])
  expect(backend.turns).toHaveLength(1)
  await expect(input(page)).toHaveValue('')

  // 【改】 puts the thing back in words, into the input, focused.
  await receipt.getByRole('button', { name: '改' }).click()
  await expect(input(page)).toHaveValue('改一下：给张三回邮件，')
  await expect(input(page)).toBeFocused()

  // Esc ends the conversation; the next line starts a new one.
  await input(page).press('Escape')
  await expect(receipt).toHaveCount(0)
  await say(page, '再记一件')
  await expect.poll(() => backend.turns.length).toBe(2)
  expect(backend.turns[1].conversationId).toMatch(uuid)
  expect(backend.turns[1].conversationId).not.toBe(backend.turns[0].conversationId)
  expect(backend.errors).toEqual([])
})

test('lines sent while the first is still thinking go out at once, in the same conversation', async ({ page }) => {
  const held: (() => void)[] = []
  const backend = await mockBackend(page, (request) => new Promise((resolve) => held.push(() => resolve({ reply: `回：${request.text}` }))))
  await page.goto('/')
  await say(page, '第一句')
  await expect(page.getByText('正在想…')).toBeVisible()
  await expect(input(page)).toBeEnabled()
  await say(page, '第二句')
  await say(page, '第三句')
  await expect(page.getByText('正在想…')).toHaveCount(3)
  // Nothing waits for the first answer: all three are with the server together.
  await expect.poll(() => backend.turns.length).toBe(3)
  expect(backend.turns[0].conversationId).toMatch(uuid)
  expect(backend.turns.map((t) => t.conversationId)).toEqual(Array(3).fill(backend.turns[0].conversationId))
  held.reverse().forEach((release) => release())
  await expect(page.locator('.sec-turn .sec-said')).toHaveText(['第一句', '第二句', '第三句'])
  await expect(page.locator('.sec-reply')).toHaveText(['回：第一句', '回：第二句', '回：第三句'])
})

test('a receipt undone earlier reads 已撤销 when the conversation is restored', async ({ page }) => {
  const at = new Date().toISOString()
  const conversations = new Map([['11111111-1111-4111-8111-111111111111', [
    { id: 'turn-a', text: '周五给张三回邮件', reply: '', cards: [], ask: null, agent: '模型', createdAt: at, receipts: [{ ...created, undone: true }, { ...created, actionId: 'act-9', text: '已建：交房租', thingId: 'rent', undone: false }] },
  ]]])
  await mockBackend(page, () => ({}), undefined, conversations)
  await page.addInitScript(() => localStorage.setItem('pcas.secretary.desk', JSON.stringify({ conversationId: '11111111-1111-4111-8111-111111111111', unanswered: [] })))
  await page.goto('/')
  const undone = page.locator('.sec-receipt').filter({ hasText: created.text })
  await expect(undone).toContainText('已撤销')
  await expect(undone.getByRole('button')).toHaveCount(0)
  await expect(page.locator('.sec-receipt').filter({ hasText: '已建：交房租' }).getByRole('button', { name: '撤销' })).toBeVisible()
})

test('撤销 on a receipt undoes that action and reports why when it cannot', async ({ page }) => {
  const second: Receipt = { ...created, actionId: 'act-2', text: '已改：交房租 → 明天 09:00', thingId: 'rent' }
  const remembered: Receipt = { actionId: null, op: 'remember', text: '记下了，会整理进记忆', thingId: null, undoable: false, status: 'done' }
  const skipped: Receipt = { actionId: null, op: 'update', text: '没改', thingId: null, undoable: false, status: 'skipped', reason: '找不到要改的那件事' }
  const backend = await mockBackend(page, () => ({ receipts: [created, second, remembered, skipped] }), (command) =>
    command.id === 'act-2' ? { status: 409, json: { error: 'changed_since' } } : undefined,
  )
  await page.goto('/')
  await say(page, '周五给张三回邮件，房租改到明天')
  const first = page.locator('.sec-receipt').filter({ hasText: created.text })
  await first.getByRole('button', { name: '撤销' }).click()
  await expect(first).toContainText('已撤销')
  await expect(first.getByRole('button', { name: '撤销' })).toHaveCount(0)
  await expect(first.getByRole('link')).toHaveCount(0)
  expect(backend.commands.at(-1)).toMatchObject({ type: 'undoAction', id: 'act-1' })

  const failing = page.locator('.sec-receipt').filter({ hasText: second.text })
  await failing.getByRole('button', { name: '撤销' }).click()
  await expect(failing.getByRole('alert')).toHaveText('这件事之后又改过，没法直接撤销。')
  expect(backend.commands.at(-1)).toMatchObject({ type: 'undoAction', id: 'act-2' })

  // Nothing to undo or edit on a remembered fact; a skipped action says why.
  const note = page.locator('.sec-receipt').filter({ hasText: remembered.text })
  await expect(note.getByRole('button')).toHaveCount(0)
  await expect(page.locator('.sec-receipt.skipped')).toContainText('找不到要改的那件事')
  await expect(page.locator('.sec-receipt.skipped').getByRole('button')).toHaveCount(0)
})

test('a turn with nothing to show says so instead of staying blank', async ({ page }) => {
  await mockBackend(page, () => ({ cards: [{ kind: 'mystery' }] }))
  await page.goto('/')
  await say(page, '嗯')
  await expect(page.locator('.sec-turn').filter({ hasText: '嗯' })).toContainText('没听出要做什么，换个说法试试？')
})

test('an ask option is sent as the next line', async ({ page }) => {
  const backend = await mockBackend(page, (_request, n) =>
    n === 1 ? { receipts: [created], ask: { question: '张三是指哪一位？', options: ['张三（同事）', '张三（房东）'] } } : { reply: '好，记在同事张三名下。' },
  )
  await page.goto('/')
  await say(page, '给张三回邮件')
  await expect(page.getByText('张三是指哪一位？')).toBeVisible()
  await page.getByRole('button', { name: '张三（同事）' }).click()
  await expect(page.getByText('好，记在同事张三名下。')).toBeVisible()
  expect(backend.turns[1]).toMatchObject({ text: '张三（同事）', conversationId: backend.turns[0].conversationId })
  // The question stays; its options go once it is answered.
  await expect(page.getByRole('button', { name: '张三（房东）' })).toHaveCount(0)
})

test('a lost connection keeps the words and retries with the same requestId, even after a reload', async ({ page }) => {
  let online = false
  const backend = await mockBackend(page, () => (online ? { receipts: [created] } : 'abort'))
  await page.goto('/')
  await say(page, '周五给张三回邮件')
  const line = page.locator('.sec-turn').filter({ hasText: '周五给张三回邮件' })
  await expect(line.getByRole('button', { name: '重试' })).toBeVisible()
  await expect(line.locator('.sec-said')).toHaveText('周五给张三回邮件')
  await expect(input(page)).toBeEnabled()

  await page.reload()
  await expect(line.getByText('上次没等到回复，原话还在')).toBeVisible()
  online = true
  await line.getByRole('button', { name: '重试' }).click()
  await expect(page.locator('.sec-receipt').filter({ hasText: created.text })).toBeVisible()
  expect(backend.turns).toHaveLength(2)
  expect(backend.turns[1]).toEqual(backend.turns[0])

  // Once answered, a reload shows the turn, not a retry.
  await page.reload()
  await expect(page.locator('.sec-receipt').filter({ hasText: created.text })).toBeVisible()
  await expect(page.getByRole('button', { name: '重试' })).toHaveCount(0)
})

test('the secretary on a thing page sends that thing along', async ({ page }) => {
  const backend = await mockBackend(page, () => ({ receipts: [{ ...created, op: 'update', text: '已改：给张三回邮件 → 下周一 10:00' }] }))
  await page.goto('/t/task')
  await say(page, '改到下周一上午十点')
  await expect(page.getByText('已改：给张三回邮件 → 下周一 10:00')).toBeVisible()
  expect(backend.turns[0]).toMatchObject({ thingId: 'task' })
  // Each thing keeps its own conversation; the hall's is separate.
  await page.goto('/')
  await expect(page.getByText('已改：给张三回邮件 → 下周一 10:00')).toHaveCount(0)
})

test('all four cards render, and an unknown kind is skipped quietly', async ({ page }) => {
  const at = new Date(Date.now() - 400 * 24 * 60 * 60 * 1000).toISOString()
  const backend = await mockBackend(page, () => ({
    reply: '去年去过两次成都。',
    cards: [
      { kind: 'sources', items: [
        { memoryId: 'm1', version: 1, text: '三月和同事去成都出差', sourceId: 'src-1', sourceVersion: 1, at },
        { memoryId: 'm9', version: 1, text: '小林说成都的火锅最好吃', sourceId: 'src-1', sourceVersion: 1, at: null },
        { memoryId: 'm8', version: 1, text: '第一次听说青城山', sourceId: 'src-1', sourceVersion: 1, at: new Date(Date.now() - 800 * 24 * 60 * 60 * 1000).toISOString() },
      ] },
      { kind: 'links', items: [{ url: 'https://example.com/chengdu', host: 'example.com' }] },
      { kind: 'timeline', title: '去年关于成都', items: [
        { at, text: '订了去成都的票', status: 'done', memoryId: 'm1', thingId: null },
        { at, text: '想去青城山', status: 'dropped', memoryId: null, thingId: null },
        { at: null, text: '再去一次', status: 'open', memoryId: null, thingId: 'task' },
      ] },
      { kind: 'tasks', items: [{ thingId: 'rent', title: '交房租', due: new Date().toISOString(), project: 'A 项目', status: 'todo' }] },
      { kind: 'chart', items: [{ x: 1 }] },
      { kind: 'mystery' },
    ],
  }))
  await page.route((url) => url.pathname === '/v1/memory/sources/src-1', (route) =>
    route.fulfill({ json: { derived: [], source: { id: 'src-1', version: 1, title: '出差记录', text: '三月和同事去成都出差', recorded_at: at, has_attachment: false, attachment_missing: false, representation: 'original' }, processing: [] } }),
  )
  await page.goto('/')
  await say(page, '我去年去过成都吗')
  await expect(page.getByText('去年去过两次成都。')).toBeVisible()
  // With a timeline the quotes are not listed a second time; they live on the timeline.
  await expect(page.getByRole('list', { name: '依据' })).toHaveCount(0)
  const link = page.getByRole('link', { name: 'example.com' })
  await expect(link).toHaveAttribute('href', 'https://example.com/chengdu')
  await expect(link).toHaveAttribute('target', '_blank')
  const timeline = page.locator('.sec-timeline')
  await expect(timeline.locator('figcaption')).toHaveText('去年关于成都')
  await expect(timeline.locator('li.done')).toContainText('订了去成都的票')
  await expect(timeline.locator('li.dropped')).toContainText('想去青城山')
  await expect(timeline.getByRole('link', { name: '再去一次' })).toHaveAttribute('href', '/t/task')
  // Quotes the timeline did not place join it in time order, undated ones last, each still opening its source.
  await expect(timeline.locator('li')).toHaveCount(5)
  await expect(timeline.locator('li').first()).toContainText('第一次听说青城山')
  await expect(timeline.locator('li').last()).toContainText('小林说成都的火锅最好吃')
  await expect(timeline.getByRole('button', { name: '小林说成都的火锅最好吃' })).toBeVisible()
  await expect(page.getByRole('list', { name: '事项' }).getByRole('link', { name: '交房租' })).toBeVisible()
  await timeline.getByRole('button', { name: '订了去成都的票' }).click()
  await expect(page.getByRole('dialog')).toContainText('出差记录')
  expect(backend.errors).toEqual([])
})

test('a task card is ticked with dispatchUndoable, and its toast undoes that exact command', async ({ page }) => {
  const backend = await mockBackend(page, () => ({ cards: [{ kind: 'tasks', items: [{ thingId: 'rent', title: '交房租', due: null, project: null, status: 'todo' }] }] }))
  await page.goto('/')
  await say(page, '我今天有什么事')
  const tick = page.getByRole('list', { name: '事项' }).getByRole('button', { name: '做完了：交房租' })
  await tick.click()
  const toast = page.locator('.toast')
  await expect(toast).toContainText('做完了')
  await expect(tick).toHaveAttribute('aria-pressed', 'true')
  const done = backend.commands.find((c) => c.type === 'setTaskStatus')!
  expect(done).toMatchObject({ id: 'rent', status: 'done' })
  // Pointing at the toast keeps it; 8 seconds is long enough to reach it.
  await toast.hover()
  await page.waitForTimeout(8500)
  await toast.getByRole('button', { name: '撤销' }).click()
  await expect.poll(() => backend.commands.length).toBe(2)
  expect(backend.commands[1]).toMatchObject({ type: 'undoAction', id: done.requestId })
  await expect(toast).toContainText('撤销了')
})

test('on a phone the pinned input sits on a solid dock, so nothing shows through around it', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockBackend(page, () => ({}))
  await page.goto('/')
  await expect(input(page)).toBeVisible()
  // Scroll so the today panel passes under the input, then probe the gaps around it.
  await page.locator('.hall-today').scrollIntoViewIfNeeded()
  await page.evaluate(() => document.querySelector('.main')?.scrollBy(0, 200))
  const box = (await page.locator('.sec-input').boundingBox())!
  const probes = [
    [box.x + box.width / 2, box.y + box.height + 6], // below the input
    [4, box.y + box.height / 2], // left of it
    [box.x + box.width / 2, box.y - 4], // just above it
  ]
  for (const [x, y] of probes) {
    expect(await page.evaluate(([x, y]) => !!document.elementFromPoint(x, y)?.closest('.sec-dock'), [x, y])).toBe(true)
  }
})

test('quotes without a timeline are a list of their own, each opening its source', async ({ page }) => {
  await mockBackend(page, () => ({ reply: '你说过不吃香菜。', cards: [{ kind: 'sources', items: [{ memoryId: 'm1', version: 1, text: '我不吃香菜', sourceId: 'src-1', sourceVersion: 1, at: null }] }] }))
  await page.route((url) => url.pathname === '/v1/memory/sources/src-1', (route) =>
    route.fulfill({ json: { derived: [], source: { id: 'src-1', version: 1, title: '饮食偏好', text: '我不吃香菜', recorded_at: new Date().toISOString(), has_attachment: false, attachment_missing: false, representation: 'original' }, processing: [] } }),
  )
  await page.goto('/')
  await say(page, '我吃香菜吗')
  await page.getByRole('list', { name: '依据' }).getByRole('button', { name: '我不吃香菜' }).click()
  await expect(page.getByRole('dialog')).toContainText('饮食偏好')
})

for (const [width, height] of [[1440, 900], [390, 844]]) {
  test(`on a long thing page the receipt lands above the pinned input, where 撤销 can be reached, at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height })
    const backend = await mockBackend(page, () => ({ receipts: [{ ...created, op: 'update', text: '已改：给张三回邮件 → 下周一 10:00' }] }))
    // Enough steps that the page scrolls under the pinned input.
    backend.snapshot.tasks[0].checklist = Array.from({ length: 12 }, (_, i) => ({ id: `step-${i}`, text: `第 ${i + 1} 步`, done: false }))
    await page.goto('/t/task')
    await say(page, '改到下周一上午十点')
    const undo = page.locator('.sec-receipt').getByRole('button', { name: '撤销' })
    await expect(undo).toBeVisible()
    // The point a finger would press is the button itself, not the dock over it.
    await expect.poll(() => undo.evaluate((el) => {
      const box = el.getBoundingClientRect()
      return document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2) === el
    })).toBe(true)
    // Once the scroll settles the whole receipt is clear of the dock.
    await expect.poll(async () => {
      const [receipt, dock] = [await page.locator('.sec-receipt').boundingBox(), await page.locator('.sec-dock').boundingBox()]
      return receipt!.y + receipt!.height <= dock!.y
    }).toBe(true)
    expect(backend.errors).toEqual([])
  })
}

test('on a phone a receipt wraps, so what changed and why an undo failed can be read in full', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const long: Receipt = { ...created, text: '已建：周五 15:00 给张三回邮件确认下周三的季度复盘会议议程和参会名单 · A 项目 · 14:30 提醒' }
  const second: Receipt = { ...created, actionId: 'act-2', text: '已改：交房租 → 明天 09:00', thingId: 'rent' }
  const skipped: Receipt = { actionId: null, op: 'update', text: '没改：牙医预约', thingId: null, undoable: false, status: 'skipped', reason: '有两件事都叫「预约牙医」，不确定是哪一件' }
  const backend = await mockBackend(page, () => ({ receipts: [long, second, skipped] }), (command) =>
    command.id === 'act-2' ? { status: 409, json: { error: 'changed_since' } } : undefined,
  )
  await page.goto('/')
  await say(page, '周五给张三回邮件，房租改到明天，牙医也改一下')
  const failing = page.locator('.sec-receipt').filter({ hasText: second.text })
  await failing.getByRole('button', { name: '撤销' }).click()
  await expect(failing.getByRole('alert')).toHaveText('这件事之后又改过，没法直接撤销。')
  await expect(page.locator('.sec-receipt .r-text')).toHaveCount(3)
  // Nothing is cut off behind an ellipsis.
  const cut = (selector: string) => page.locator(selector).evaluateAll((els) => els.filter((el) => el.scrollWidth > el.clientWidth + 1).length)
  expect(await cut('.sec-receipt .r-text')).toBe(0)
  expect(await cut('.sec-receipt .r-error')).toBe(0)
  // The reason sits under its receipt, and 改 / 撤销 stay on the receipt's first line.
  const [text, reason, actions] = [await failing.locator('.r-text').boundingBox(), await failing.getByRole('alert').boundingBox(), await failing.locator('.r-actions').boundingBox()]
  expect(reason!.y).toBeGreaterThanOrEqual(text!.y + text!.height - 1)
  expect(actions!.y).toBeLessThan(reason!.y)
  expect(backend.errors).toEqual([])
})

/** No page picks 'latest' yet (D2 decides where), so flip the component's default in the served bundle. */
async function preferLatest(page: Page) {
  let flipped = false
  await page.route(/\/assets\/index-[^/]*\.js$/, async (route) => {
    const response = await route.fetch()
    const body = (await response.text()).replace(/variant:([\w$]+)="full"/, (_m, name) => ((flipped = true), `variant:${name}="latest"`))
    await route.fulfill({ response, body })
  })
  return () => flipped
}

test("variant 'latest' shows the latest turn and opens the rest on demand", async ({ page }) => {
  const flipped = await preferLatest(page)
  const backend = await mockBackend(page, (request, n) => (n === 4 ? 'abort' : { reply: `回：${request.text}` }))
  await page.goto('/')
  expect(flipped()).toBe(true)
  for (const words of ['一', '二', '三']) {
    await say(page, words)
    await expect(page.locator('.sec-reply')).toHaveText([`回：${words}`])
  }
  const toggle = page.getByRole('button', { name: '展开对话（3 轮）' })
  await expect(page.locator('.sec-turn')).toHaveCount(1)
  await toggle.click()
  await expect(page.locator('.sec-reply')).toHaveText(['回：一', '回：二', '回：三'])
  await page.getByRole('button', { name: '收起' }).click()
  await expect(page.locator('.sec-turn')).toHaveCount(1)

  // An unsent line stays in sight while folded, so its words are never hidden.
  await say(page, '四')
  await expect(page.getByRole('button', { name: '重试' })).toBeVisible()
  await say(page, '五')
  await expect(page.locator('.sec-turn .sec-said')).toHaveText(['四', '五'])
  await expect(page.getByRole('button', { name: '展开对话（5 轮）' })).toBeVisible()
  expect(backend.turns).toHaveLength(5)
})

test('folded, a line still being thought about stays in sight when another is sent', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const held: (() => void)[] = []
  await mockBackend(page, (request, n) => (n === 1 ? new Promise((resolve) => held.push(() => resolve({ reply: `回：${request.text}` }))) : { reply: `回：${request.text}` }))
  await page.goto('/')
  await say(page, '第一句')
  await say(page, '第二句')
  await expect(page.locator('.sec-reply')).toHaveText(['回：第二句'])
  // The first line has no answer yet: it is neither dropped nor hidden behind 展开.
  await expect(page.locator('.sec-turn .sec-said')).toHaveText(['第一句', '第二句'])
  await expect(page.locator('.sec-turn.waiting')).toContainText('正在想…')
  // Once answered it folds away like any earlier turn, one tap from 展开.
  held.forEach((release) => release())
  await expect(page.locator('.sec-turn .sec-said')).toHaveText(['第二句'])
  await page.getByRole('button', { name: '展开对话（2 轮）' }).click()
  await expect(page.locator('.sec-reply')).toHaveText(['回：第一句', '回：第二句'])
})

test('a long conversation fades at its top edge instead of cutting a line in half', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const at = new Date().toISOString()
  const id = '22222222-2222-4222-8222-222222222222'
  const turns = Array.from({ length: 8 }, (_, i) => ({ id: `t${i}`, text: `第 ${i + 1} 句`, reply: '这是一段两三行长的回答，用来把对话撑到需要滚动的高度。'.repeat(3), cards: [], receipts: [], ask: null, agent: '模型', createdAt: at }))
  await mockBackend(page, () => ({}), undefined, new Map([[id, turns]]))
  await page.addInitScript((id) => localStorage.setItem('pcas.secretary.desk', JSON.stringify({ conversationId: id, unanswered: [] })), id)
  await page.goto('/')
  const list = page.locator('.sec-thread > ol')
  await expect(page.locator('.sec-turn')).toHaveCount(8)
  // Restored at the latest turn, the earlier ones fade out upwards.
  await expect(list).toHaveClass(/faded/)
  await list.evaluate((el) => el.scrollTo(0, 0))
  await expect(list).not.toHaveClass(/faded/)
})
