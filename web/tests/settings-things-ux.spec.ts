import { test, expect, type Page } from '@playwright/test'
import type { Agent, Candidate, Run, State, Task } from '../src/domain/types'
import type { Action } from '../src/store/actions'

// The settings page, the pending-content sheet and the thing page as someone
// uses them. Every API call is answered in the browser; no backend or model.

type Command = Action & { requestId: string; expectedRevision: number }
const at = '2026-09-30T02:00:00Z'

function agent(over: Partial<Agent>): Agent {
  return { id: 'api', name: 'gpt-fake', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 2, outputPrice: 8, maxOutput: 100, memoryKinds: ['fact', 'preference'], includeInferred: false, ...over }
}
function task(over: Partial<Task>): Task {
  return { id: 'task', title: '交房租', notes: '', status: 'todo', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: at, updatedAt: at, ...over }
}
function run(over: Partial<Run>): Run {
  return { id: 'run', thingId: 'task', agentId: 'api', kind: 'breakdown', prompt: '拆成几步', brief: '', contextMemoryIds: [], status: 'done', output: '', staleContext: false, cost: 0, createdAt: at, finishedAt: at, ...over }
}
function candidate(over: Partial<Candidate>): Candidate {
  return { id: 'cand', kind: 'task', text: '周五前把报销单交给财务', confidence: 0.8, source: { sourceId: 's', label: '和李四的聊天记录' }, state: 'pending', createdAt: at, ...over }
}
function workspace(over: Partial<State> = {}): State {
  return {
    version: 1, revision: 1, budgetUsage: 3.4, notices: [],
    settings: { timezone: 'Asia/Shanghai', city: '上海', dailyBudget: 10, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00' },
    tasks: [task({})], ideas: [], projects: [], agents: [agent({}), agent({ id: 'manual', name: '手动转交', channel: 'manual', default: false, memoryKinds: [] })],
    memories: [], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {},
    ...over,
  }
}

interface Mock {
  state: State
  commands: Command[]
  posts: { path: string; method: string; body: unknown }[]
  errors: string[]
  /** Command types the server refuses, until removed. */
  refuse: Set<string>
  /** Refuses the commands it returns true for. */
  reject?: (command: Command) => boolean
  /** What the secretary changes when it is told something. */
  onTurn?: (state: State) => void
  /** Milliseconds each command waits before it is answered. */
  delay: number
  telegram: boolean
  chatgpt: boolean
}

async function mock(page: Page, state = workspace(), options: Partial<Pick<Mock, 'telegram' | 'chatgpt'>> = {}): Promise<Mock> {
  const m: Mock = { state, commands: [], posts: [], errors: [], refuse: new Set(), delay: 0, telegram: false, chatgpt: false, ...options }
  const text = { base_url: 'https://models.example.test/v1', model: 'gpt-fake', input_cny_per_million: 2, output_cny_per_million: 8, default: true, key_configured: true }
  page.on('pageerror', (e) => m.errors.push(e.message))
  await page.route('**/v1/**', (route) => route.fulfill({ status: 500, json: { error: 'unexpected_mock_request' } }))
  const on = (path: string, handler: Parameters<Page['route']>[1]) => page.route((url) => url.pathname === path, handler)
  await on('/v1/workspace', (route) => route.fulfill({ json: m.state }))
  await on('/v1/desk/turns', (route) => route.fulfill({ json: { turns: [] } }))
  await on('/v1/memory/summary', (route) => route.fulfill({ json: { text: '', coverage: { gaps: [] }, dependencies: [] } }))
  await on('/v1/notify/config', (route) => route.fulfill({ json: { webPush: { publicKey: '', subscriptions: 0 }, telegram: { configured: m.telegram, chatId: m.telegram ? '123' : '' } } }))
  await on('/v1/models', (route) => route.fulfill({ json: { chatgptEnabled: m.chatgpt, chatgptDirectEnabled: m.chatgpt } }))
  await on('/v1/chatgpt/account', (route) => route.fulfill({ json: { account: null } }))
  await on('/v1/chatgpt/direct/account', (route) => route.fulfill({ json: { accounts: [], active_client_id: '', pending: false, default_ready: false } }))
  await on('/v1/models/openai', (route) => route.fulfill({ json: { editable: true, text, embedding: { ...text, model: 'embed-fake', default: false, key_configured: false }, decision: { key_configured: false, saved: false } } }))
  await on('/v1/models/openai/text', (route) => {
    const body = route.request().postDataJSON()
    m.posts.push({ path: '/v1/models/openai/text', method: route.request().method(), body })
    return route.fulfill({ json: { ...text, base_url: body.base_url, model: body.model, default: body.default } })
  })
  const connectors: unknown[] = []
  await on('/v1/connectors', (route) => {
    if (route.request().method() === 'GET') return route.fulfill({ json: connectors })
    const body = route.request().postDataJSON()
    m.posts.push({ path: '/v1/connectors', method: 'POST', body })
    const connection = { id: 'conn', name: body.name, kind: body.kind, enabled: true, version: 1, interval_seconds: body.interval_seconds, status: 'idle', imported: 0, gaps: [] }
    connectors.push(connection)
    return route.fulfill({ json: { connection, webhook_token: 'synthetic-token' } })
  })
  await on('/v1/desk/turn', (route) => {
    const body = route.request().postDataJSON()
    m.onTurn?.(m.state)
    m.state = { ...m.state, revision: m.state.revision + 1 }
    const turn = { id: `turn-${body.requestId}`, text: body.text, reply: '改好了。', cards: [], receipts: [], ask: null, agent: '模型', createdAt: at }
    return route.fulfill({ json: { conversationId: body.conversationId, turn, state: m.state } })
  })
  await on('/v1/workspace/commands', async (route) => {
    const body = route.request().postDataJSON() as Command
    m.commands.push(body)
    if (m.delay) await new Promise((done) => setTimeout(done, m.delay))
    if (m.refuse.has(body.type) || m.reject?.(body)) return route.fulfill({ status: 400, json: { error: 'invalid_input' } })
    const s = m.state
    switch (body.type) {
      case 'updateSettings': Object.assign(s.settings, body.patch); break
      case 'updateAgent': s.agents = s.agents.map((a) => (a.id === body.id ? { ...a, ...body.patch } : a)); break
      case 'renameThing': s.tasks = s.tasks.map((t) => (t.id === body.id ? { ...t, title: body.title } : t)); break
      case 'setNotes': s.tasks = s.tasks.map((t) => (t.id === body.id ? { ...t, notes: body.text } : t)); break
      case 'updateTask': s.tasks = s.tasks.map((t) => (t.id === body.id ? { ...t, ...body.patch } : t)); break
      case 'setTaskStatus': s.tasks = s.tasks.map((t) => (t.id === body.id ? { ...t, status: body.status } : t)); break
      case 'deleteThing': s.tasks = s.tasks.filter((t) => t.id !== body.id); break
      case 'acceptCandidate':
        s.candidates = s.candidates.map((c) => (c.id === body.id ? { ...c, state: 'accepted', resolvedInto: body.kind === 'task' ? `made-${c.id}` : undefined } : c))
        if (body.kind === 'task') s.tasks = [...s.tasks, task({ id: `made-${body.id}`, title: body.text })]
        break
      case 'ignoreCandidate': s.candidates = s.candidates.map((c) => (c.id === body.id ? { ...c, state: 'ignored' } : c)); break
    }
    m.state = { ...s, revision: s.revision + 1 }
    return route.fulfill({ json: m.state })
  })
  return m
}

const noSidewaysScroll = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)
const secretary = (page: Page) => page.getByRole('textbox', { name: '跟秘书说' })

/* ---------- 设置 ---------- */

test('settings open with the current state and the everyday groups, and keep keys and URLs folded away', async ({ page }) => {
  const m = await mock(page, workspace(), { telegram: true })
  await page.goto('/settings')
  const overview = page.getByRole('group', { name: '当前状态' })
  await expect(overview.getByRole('button', { name: /^时间/ })).toContainText('Asia/Shanghai')
  await expect(overview.getByRole('button', { name: /^提醒/ })).toContainText('发到 Telegram')
  // A saved key is reported as configured, never as connected or verified.
  await expect(overview.getByRole('button', { name: /^模型/ })).toContainText('1 个已配置')
  await expect(overview.getByRole('button', { name: /^今天的 API 预算/ })).toContainText('¥3.40 / ¥10')
  await expect(page.getByRole('heading', { level: 2 })).toHaveText(['时间和地点', '提醒', '模型和预算', '资料', '数据', '高级'])
  // What used to be spread across the page: no key, URL, token or price field until a row is opened.
  expect(await page.getByRole('textbox').evaluateAll((els) => els.map((el) => el.getAttribute('aria-label')))).toEqual(['所在城市', '每日额度'])
  await expect(page.locator('input[type=password], input[type=url]')).toHaveCount(0)
  await expect(page.getByRole('spinbutton')).toHaveCount(0)
  for (const gone of ['Chat Completions', 'Base URL', 'Webhook', 'Jev', '由 Jev 判断去向', '曝光']) await expect(page.getByText(gone)).toHaveCount(0)
  // The rows that hold them still say where each one stands.
  await expect(page.getByRole('button', { name: /^按量计费接口/ })).toContainText('已填密钥 · gpt-fake · 向量还没填')
  await expect(page.getByRole('button', { name: /^Telegram/ })).toContainText('已连接')
  await expect(page.getByRole('switch')).toHaveCount(5)
  expect(m.commands).toEqual([])
  expect(m.errors).toEqual([])
})

test('first use says what is missing and takes you there', async ({ page }) => {
  const m = await mock(page, workspace({ agents: [agent({ available: false })] }))
  await page.goto('/settings')
  const overview = page.getByRole('group', { name: '当前状态' })
  await expect(overview.getByRole('button', { name: /^提醒/ })).toContainText('还没有接收方式')
  const models = overview.getByRole('button', { name: /^模型/ })
  await expect(models).toContainText('还没有配置好的模型')
  await models.click()
  await expect(page.getByRole('heading', { name: '模型和预算' })).toBeFocused()
  await expect(page.getByRole('heading', { name: '模型和预算' })).toBeInViewport()
  await expect(page.getByText('还没有配置好的模型，秘书没法回答。在下面「连接」里连一个。')).toBeVisible()
  await expect(page.getByText('已启用，但它的连接还没配置好，看下面的「连接」')).toBeVisible()
  expect(m.commands).toEqual([])
})

test('each switch says what it does now, saves only its own field, and shows the save beside it', async ({ page }) => {
  const m = await mock(page)
  m.delay = 300
  await page.goto('/settings')
  const cases = [
    { label: '事项提醒', field: 'followUps', from: true, after: '现在：已停。到时间不再提醒，也不往设备和 Telegram 发；定好的时间都还留着。' },
    { label: '带回搁置的想法', field: 'wakeIdeas', from: true, after: '现在：已停。搁置的想法一直放着，直到你自己去翻。' },
  ] as const
  for (const c of cases) {
    const toggle = page.getByRole('switch', { name: c.label, exact: true })
    const row = page.locator('.setting').filter({ has: toggle })
    await expect(toggle).toHaveAttribute('aria-checked', String(c.from))
    await expect(row.getByText(c.after)).toHaveCount(0)
    await toggle.click()
    // While the server has not answered, the switch cannot be flipped again and nothing claims to be saved.
    await expect(toggle).toBeDisabled()
    await expect(row.getByRole('status')).toHaveText('正在保存')
    await expect(row.getByRole('status')).toHaveText('已保存')
    await expect(toggle).toHaveAttribute('aria-checked', String(!c.from))
    await expect(row.getByText(c.after)).toBeVisible()
    expect(m.commands.at(-1)).toMatchObject({ type: 'updateSettings', patch: { [c.field]: !c.from } })
    expect(Object.keys((m.commands.at(-1) as Extract<Command, { type: 'updateSettings' }>).patch)).toEqual([c.field])
  }
  expect(m.commands).toHaveLength(2)
  // Nothing else moved: budget, review time, city and timezone are what they were.
  expect(m.state.settings).toMatchObject({ dailyBudget: 10, dailyReviewAt: '09:00', city: '上海', timezone: 'Asia/Shanghai' })
  // With task reminders off, the overview and the channel list both say reminders will not arrive.
  await expect(page.getByRole('group', { name: '当前状态' }).getByRole('button', { name: /^提醒/ })).toContainText('事项提醒已停')
  await expect(page.getByText('事项提醒已停，下面开着也收不到事项提醒')).toBeVisible()
  expect(m.errors).toEqual([])
})

test('a refused setting stays as it was and says so beside the switch', async ({ page }) => {
  const m = await mock(page)
  m.refuse.add('updateSettings')
  await page.goto('/settings')
  const toggle = page.getByRole('switch', { name: '带回搁置的想法' })
  const row = page.locator('.setting').filter({ has: toggle })
  await toggle.click()
  await expect(row.getByRole('alert')).toHaveText('没保存上，还是原来的设置')
  await expect(row.getByRole('status')).toHaveCount(0)
  await expect(toggle).toHaveAttribute('aria-checked', 'true')
  await expect(toggle).toBeEnabled()
  await expect(row.getByText('现在：到了它等的时间，或新资料里提到它等的事，就回到眼前让你核对。')).toBeVisible()
  // The reason is the server's, shown once at the top.
  await expect(page.locator('.connection-banner')).toContainText('内容或状态不符合要求')
  expect(m.state.settings.wakeIdeas).toBe(true)
  // The city field reports a refused save the same way.
  await page.getByRole('textbox', { name: '所在城市' }).fill('墨尔本')
  await page.getByRole('textbox', { name: '所在城市' }).press('Enter')
  await expect(page.locator('.setting').filter({ has: page.getByRole('textbox', { name: '所在城市' }) }).getByRole('alert')).toHaveText('没保存上，还是原来的设置')
  expect(m.state.settings.city).toBe('上海')
})

test('the budget and the daily count save where they are changed', async ({ page }) => {
  const m = await mock(page)
  await page.goto('/settings')
  const budget = page.locator('.budget')
  await expect(budget).toContainText('今天已用和已预留 ¥3.40，按你填的单价估算，不是供应商账单。')
  await budget.getByRole('button', { name: '增加' }).click()
  await expect(budget.getByRole('status')).toHaveText('已保存')
  expect(m.commands.at(-1)).toMatchObject({ type: 'updateSettings', patch: { dailyBudget: 11 } })
  await expect(page.getByRole('group', { name: '当前状态' }).getByRole('button', { name: /^今天的 API 预算/ })).toContainText('¥3.40 / ¥11')
  await page.getByRole('button', { name: '每天汇总时间' }).click()
  await page.getByRole('option', { name: '21:30' }).click()
  expect(m.commands.at(-1)).toMatchObject({ type: 'updateSettings', patch: { dailyReviewAt: '21:30' } })
  expect(m.commands).toHaveLength(2)
})

test('a model row keeps its switch in sight and opens its memory scope without changing it', async ({ page }) => {
  const m = await mock(page)
  await page.goto('/settings')
  const row = page.locator('.agent-row').filter({ hasText: 'gpt-fake' })
  await expect(row).toContainText('已启用，秘书和副手可以把事交给它')
  const scope = row.getByRole('button', { name: /^记忆范围/ })
  await expect(scope).toContainText('能看 事实、偏好')
  await expect(row.getByRole('switch', { name: '包含推测' })).toHaveCount(0)
  // Keyboard: Enter opens it and focus stays on the row that was opened.
  await scope.focus()
  await page.keyboard.press('Enter')
  await expect(scope).toHaveAttribute('aria-expanded', 'true')
  await expect(scope).toBeFocused()
  const kinds = row.getByRole('group', { name: 'gpt-fake 能看到的记忆' })
  await expect(kinds.getByRole('button', { pressed: true })).toHaveText(['事实', '偏好'])
  await expect(row.getByRole('switch', { name: '包含推测' })).toHaveAttribute('aria-checked', 'false')
  await expect(row).toContainText('现在：只给你确认过或亲口说过的')
  // Opening changed nothing on the server; each choice then sends exactly itself.
  expect(m.commands).toEqual([])
  await kinds.getByRole('button', { name: '决定' }).click()
  await expect.poll(() => m.commands.at(-1)).toMatchObject({ type: 'updateAgent', id: 'api', patch: { memoryKinds: ['fact', 'preference', 'decision'] } })
  expect(Object.keys((m.commands.at(-1) as Extract<Command, { type: 'updateAgent' }>).patch)).toEqual(['memoryKinds'])
  await expect(scope).toContainText('能看 事实、偏好、决定')
  await row.getByRole('switch', { name: '启用 gpt-fake' }).click()
  await expect.poll(() => m.commands.at(-1)).toMatchObject({ type: 'updateAgent', id: 'api', patch: { enabled: false } })
  await expect(row).toContainText('已停用，秘书和副手不会用它')
  expect(m.state.agents[0].includeInferred).toBe(false)
  expect(m.commands).toHaveLength(2)
})

test('the pay-per-use API opens from its row, saves what was typed, and no longer promises the old routing', async ({ page }) => {
  const m = await mock(page)
  await page.goto('/settings')
  // The same path the real-backend model-api spec takes.
  await page.getByRole('button', { name: /^按量计费接口/ }).click()
  await expect(page.getByRole('heading', { name: 'API 与向量接入' })).toBeVisible()
  await expect(page.getByText('保存时不会试连，能不能用要到第一次调用才知道。')).toBeVisible()
  await page.getByLabel('文本 API Base URL', { exact: true }).fill('https://other.example.test/v1')
  await page.getByLabel('文本 API API Key', { exact: true }).fill('fake-text-key')
  // The default switch is part of the form: it reaches the server only with 保存.
  await page.getByRole('switch', { name: 'API 作为默认入口' }).click()
  expect(m.posts).toEqual([])
  await page.getByRole('button', { name: '保存文本 API接入', exact: true }).click()
  await expect(page.getByText('API 接入已保存，可在副手中使用。')).toBeVisible()
  expect(m.posts[0]).toMatchObject({ path: '/v1/models/openai/text', body: { base_url: 'https://other.example.test/v1', api_key: 'fake-text-key', model: 'gpt-fake', default: false } })
  await expect(page.getByLabel('文本 API API Key', { exact: true })).toHaveValue('')
  await expect(page.getByLabel('向量 Base URL', { exact: true })).toBeVisible()
  // The legacy key is one more level down and says the secretary does not use it.
  await expect(page.getByLabel('Jev API Key')).toHaveCount(0)
  const legacy = page.getByRole('button', { name: /^旧版分流接口的密钥/ })
  await expect(legacy).toContainText('现在的秘书不使用')
  await legacy.click()
  await expect(page.getByText('现在的秘书和 Telegram 都不经过它，只有仍在调用旧接口的外部程序会用到。')).toBeVisible()
  await expect(page.getByText('由 Jev 判断去向')).toHaveCount(0)
  expect(m.errors).toEqual([])
})

test('connecting another app and the ChatGPT rows open in place, as the real-backend specs use them', async ({ page }) => {
  const m = await mock(page, workspace(), { chatgpt: true })
  await page.goto('/settings')
  await page.getByRole('button', { name: /^ChatGPT 订阅（官方授权）/ }).click()
  await page.getByRole('button', { name: /^ChatGPT 订阅（Codex 登录）/ }).click()
  await expect(page.getByRole('heading', { name: 'ChatGPT 订阅', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Continue with ChatGPT', exact: true })).toBeEnabled()
  await expect(page.getByRole('link', { name: '管理 ChatGPT 用量与应用权限' })).toHaveAttribute('href', 'https://chatgpt.com/settings/usage')
  await expect(page.getByRole('heading', { name: 'Codex App Server', exact: true })).toBeVisible()

  await expect(page.getByLabel('接入名称')).toHaveCount(0)
  const row = page.getByRole('button', { name: /^接入其他应用/ })
  await row.click()
  await expect(page.getByRole('heading', { name: '资料接入' })).toBeVisible()
  await page.getByLabel('接入名称').fill('我的笔记')
  await page.getByRole('button', { name: '添加接入' }).click()
  expect(m.posts.at(-1)).toMatchObject({ path: '/v1/connectors', body: { name: '我的笔记', kind: 'webhook', enabled: true, expected_version: 0 } })
  await expect(page.getByText('Bearer synthetic-token')).toBeVisible()
  await page.getByRole('button', { name: '已保存，隐藏密钥' }).click()
  // Closed again, the connection and its state are still on the page.
  await row.click()
  await expect(page.getByText('我的笔记', { exact: true })).toBeVisible()
  await expect(row).toContainText('已有 1 个')
  await expect(page.locator('input[type="file"]')).toHaveCount(1)
  expect(m.errors).toEqual([])
})

test('on a phone the settings fit the screen and the rows still open', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const m = await mock(page, workspace(), { telegram: true })
  await page.goto('/settings')
  await expect(page.getByRole('group', { name: '当前状态' }).getByRole('button')).toHaveCount(4)
  expect(await noSidewaysScroll(page)).toBe(true)
  const toggle = page.getByRole('switch', { name: '事项提醒' })
  const [text, control] = await Promise.all([page.getByText('事项到时间提醒我').boundingBox(), toggle.boundingBox()])
  // The switch sits beside its sentence, inside the screen.
  expect(control!.x + control!.width).toBeLessThanOrEqual(390)
  expect(control!.x).toBeGreaterThan(text!.x)
  await page.getByRole('button', { name: /^Telegram/ }).click()
  await expect(page.getByLabel('Telegram bot token')).toBeVisible()
  await page.getByRole('button', { name: /^按量计费接口/ }).click()
  await expect(page.getByLabel('文本 API Base URL', { exact: true })).toBeVisible()
  expect(await noSidewaysScroll(page)).toBe(true)
  expect(m.errors).toEqual([])
})

/* ---------- 待确认内容 ---------- */

test('pending content is reached from settings, and each button does the one thing it names', async ({ page }) => {
  const m = await mock(page, workspace({ candidates: [candidate({}), candidate({ id: 'unknown', kind: 'unknown', text: '也许把晨会挪到十点' }), candidate({ id: 'mem', kind: 'memory', text: '李四喜欢周三开会', memoryKind: 'preference' })] }))
  await page.goto('/settings')
  await expect(page.getByText('3 条读到了但拿不准')).toBeVisible()
  await page.getByRole('link', { name: '去资料库处理' }).click()
  await expect(page).toHaveURL(/\/library\?pending=1$/)
  const sheet = page.getByRole('dialog')
  await expect(sheet.getByRole('heading', { name: '待确认内容' })).toBeVisible()
  const item = (text: string) => sheet.locator('.item').filter({ hasText: text })
  // The guess is offered first; an unrecognised one has no default, and nothing is disabled waiting for a category.
  await expect(item('周五前把报销单交给财务').locator('.btn-primary')).toHaveText('创建待办')
  await expect(item('也许把晨会挪到十点')).toContainText('没看出这是什么，你来定')
  await expect(item('也许把晨会挪到十点').locator('.btn-primary')).toHaveCount(0)
  for (const name of ['创建待办', '保存为想法', '保存为记忆', '忽略这条']) await expect(item('也许把晨会挪到十点').getByRole('button', { name })).toBeEnabled()
  await expect(sheet.getByRole('button', { name: /收下|不要|记进|加进/ })).toHaveCount(0)

  // A refusal leaves the item in place and says so; nothing reports it as done.
  m.refuse.add('acceptCandidate')
  await item('周五前把报销单交给财务').getByRole('button', { name: '创建待办' }).click()
  await expect(item('周五前把报销单交给财务').getByRole('alert')).toHaveText('没保存上，这条还在这里。')
  await expect(sheet.getByRole('status')).toHaveCount(0)
  expect(m.state.candidates.find((c) => c.id === 'cand')!.state).toBe('pending')
  m.refuse.clear()

  // Two quick clicks while the server is thinking send one request.
  m.delay = 300
  const before = m.commands.length
  const accept = item('周五前把报销单交给财务').getByRole('button', { name: '创建待办' })
  await accept.click()
  await accept.click({ force: true }).catch(() => {})
  await expect(sheet.getByRole('status')).toContainText('已创建待办')
  expect(m.commands.slice(before)).toHaveLength(1)
  expect(m.commands.at(-1)).toMatchObject({ type: 'acceptCandidate', id: 'cand', kind: 'task', text: '周五前把报销单交给财务' })
  await expect(item('周五前把报销单交给财务')).toHaveCount(0)
  m.delay = 0

  // Choosing another kind than the guess sends that kind.
  await item('李四喜欢周三开会').getByRole('button', { name: '保存为想法' }).click()
  await expect.poll(() => m.commands.at(-1)).toMatchObject({ type: 'acceptCandidate', id: 'mem', kind: 'idea', memoryKind: 'preference' })
  await item('也许把晨会挪到十点').getByRole('button', { name: '忽略这条' }).click()
  await expect.poll(() => m.commands.at(-1)).toMatchObject({ type: 'ignoreCandidate', id: 'unknown' })
  await expect(sheet.getByRole('status')).toContainText('已忽略，原资料还在')
  // A candidate saved as an idea or a memory names where it went; only a recorded id gets a link to the thing itself.
  await expect(sheet.getByRole('status')).toContainText('已保存为想法')
  await expect(sheet.getByText('都处理完了。')).toBeVisible()
  expect(m.commands.filter((c) => c.type === 'acceptCandidate' || c.type === 'ignoreCandidate')).toHaveLength(4)
  expect(m.commands.some((c) => c.type === 'deleteMemory' || c.type === 'undoAction')).toBe(false)

  // The task that was made opens by the id the server recorded, not by looking for its title.
  await sheet.getByRole('link', { name: '打开' }).click()
  await expect(page).toHaveURL(/\/t\/made-cand$/)
  await expect(page.getByRole('textbox', { name: '标题', exact: true })).toHaveValue('周五前把报销单交给财务')
  expect(m.errors).toEqual([])
})

test('the library shows the pending count only when there is some, and closing the sheet clears the address', async ({ page }) => {
  await mock(page, workspace({ candidates: [candidate({})] }))
  await page.goto('/library')
  await expect(page.getByText('待确认内容 · 1 条')).toBeVisible()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.getByRole('button', { name: '逐条处理' }).click()
  await expect(page).toHaveURL(/pending=1/)
  await page.getByRole('dialog').getByRole('button', { name: '关闭' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page).not.toHaveURL(/pending/)
  // The hall still has no entry for it.
  await page.goto('/')
  await expect(page.getByText(/待确认内容|拿不准/)).toHaveCount(0)
  // Nothing pending: no entry in the library or in settings.
  await mock(page, workspace())
  await page.goto('/library')
  await expect(page.getByText(/待确认内容/)).toHaveCount(0)
  await page.goto('/settings')
  await expect(page.getByRole('link', { name: '去资料库处理' })).toHaveCount(0)
})

/* ---------- 事项 ---------- */

test('the title saves when left and says so; a refused save keeps the draft with a way back', async ({ page }) => {
  const m = await mock(page)
  await page.goto('/t/task')
  const title = page.getByRole('textbox', { name: '标题', exact: true })
  const saved = page.locator('.head-row').getByRole('status')
  // Tab reaches it: back link, done circle, then the title.
  await page.getByRole('link', { name: '大厅', exact: true }).focus()
  await page.keyboard.press('Tab')
  await page.keyboard.press('Tab')
  await expect(title).toBeFocused()
  await page.keyboard.press('End')
  await page.keyboard.type('和物业费')
  await page.keyboard.press('Enter')
  await expect(saved).toHaveText('已保存')
  expect(m.commands).toHaveLength(1)
  expect(m.commands[0]).toMatchObject({ type: 'renameThing', id: 'task', title: '交房租和物业费' })

  // Leaving it unchanged, or blank, sends nothing; a blank title goes back to the saved one.
  await title.fill('')
  await page.getByRole('textbox', { name: '说明', exact: true }).focus()
  await expect(title).toHaveValue('交房租和物业费')
  expect(m.commands).toHaveLength(1)

  m.refuse.add('renameThing')
  await title.fill('交房租、物业费和水电')
  await page.getByRole('textbox', { name: '说明', exact: true }).focus()
  const unsaved = page.getByRole('alert').filter({ hasText: '标题没保存上' })
  await expect(unsaved).toBeVisible()
  await expect(title).toHaveValue('交房租、物业费和水电')
  await expect(saved).toHaveCount(0)
  expect(m.state.tasks[0].title).toBe('交房租和物业费')
  // 再存一次 sends the draft again; once it goes through the warning is gone.
  m.refuse.clear()
  await unsaved.getByRole('button', { name: '再存一次' }).click()
  await expect(saved).toHaveText('已保存')
  await expect(unsaved).toHaveCount(0)
  expect(m.state.tasks[0].title).toBe('交房租、物业费和水电')
  expect(m.commands.filter((c) => c.type === 'renameThing')).toHaveLength(3)
  expect(m.errors).toEqual([])
})

test('refused notes can be put back, and a draft never follows you to another thing', async ({ page }) => {
  const m = await mock(page, workspace({ tasks: [task({ notes: '月底前' }), task({ id: 'other', title: '预约牙医' })] }))
  m.refuse.add('setNotes')
  m.refuse.add('renameThing')
  await page.goto('/t/task')
  const notes = page.getByRole('textbox', { name: '说明', exact: true })
  const title = page.getByRole('textbox', { name: '标题', exact: true })
  await notes.fill('月底前，转账备注写门牌号')
  await title.focus()
  const unsaved = page.getByRole('alert').filter({ hasText: '说明没保存上' })
  await expect(unsaved).toBeVisible()
  await expect(notes).toHaveValue('月底前，转账备注写门牌号')
  await unsaved.getByRole('button', { name: '改回原来的' }).click()
  await expect(notes).toHaveValue('月底前')
  await expect(unsaved).toHaveCount(0)
  expect(m.commands.filter((c) => c.type === 'setNotes')).toHaveLength(1)

  await title.fill('没存上的标题')
  await notes.focus()
  await expect(page.getByRole('alert').filter({ hasText: '标题没保存上' })).toBeVisible()
  await page.locator('.connection-banner').getByRole('button', { name: '知道了' }).click()
  // Go to another thing through the app, without a reload.
  await page.getByRole('link', { name: '大厅', exact: true }).click()
  await expect(page.locator('.hall-today')).toBeVisible()
  await page.evaluate(() => {
    history.pushState({}, '', '/t/other')
    dispatchEvent(new PopStateEvent('popstate'))
  })
  await expect(page).toHaveURL(/\/t\/other$/)
  await expect(title).toHaveValue('预约牙医')
  await expect(notes).toHaveValue('')
  await expect(page.locator('.unsaved')).toHaveCount(0)
  expect(m.state.tasks.map((t) => t.title)).toEqual(['交房租', '预约牙医'])
  expect(m.errors).toEqual([])
})

test('a change made by the secretary shows in the title and notes, but never over a draft that failed to save', async ({ page }) => {
  const m = await mock(page, workspace({ tasks: [task({ notes: '月底前' })] }))
  const rename = (title: string, notes: string) => (m.onTurn = (s) => (s.tasks = s.tasks.map((t) => ({ ...t, title, notes }))))
  await page.goto('/t/task')
  const title = page.getByRole('textbox', { name: '标题', exact: true })
  const notes = page.getByRole('textbox', { name: '说明', exact: true })
  // The user's own saved edits first, so both boxes hold text that was typed into them.
  await title.fill('我先改的标题')
  await notes.fill('我先写的说明')
  await secretary(page).focus()
  await expect.poll(() => m.commands.map((c) => c.type)).toEqual(['renameThing', 'setNotes'])
  rename('交房租和物业费', '月底前，秘书补了一句')
  await secretary(page).fill('标题加上物业费')
  await secretary(page).press('Enter')
  await expect(title).toHaveValue('交房租和物业费')
  await expect(notes).toHaveValue('月底前，秘书补了一句')

  // A draft the server refused stays on screen when something else changes the thing.
  m.refuse.add('renameThing')
  await title.fill('我自己改的标题')
  await notes.focus()
  await expect(page.getByRole('alert').filter({ hasText: '标题没保存上' })).toBeVisible()
  await page.locator('.connection-banner').getByRole('button', { name: '知道了' }).click()
  rename('秘书又改了一次', '月底前，秘书补了一句')
  await secretary(page).fill('再改一次')
  await secretary(page).press('Enter')
  // The page has taken the secretary's change in (the bar shows it) before the draft is checked.
  await expect(page.locator('.bar-title')).toHaveText('秘书又改了一次')
  await expect(title).toHaveValue('我自己改的标题')
  // 改回原来的 goes to what is saved now, which is the secretary's title.
  await page.getByRole('alert').filter({ hasText: '标题没保存上' }).getByRole('button', { name: '改回原来的' }).click()
  await expect(title).toHaveValue('秘书又改了一次')
  expect(m.errors).toEqual([])
})

test('entering a field and leaving it untouched never writes an old value over a change made elsewhere', async ({ page }) => {
  const m = await mock(page, workspace({ tasks: [task({ notes: '月底前' })] }))
  await page.goto('/t/task')
  const title = page.getByRole('textbox', { name: '标题', exact: true })
  const notes = page.getByRole('textbox', { name: '说明', exact: true })
  // The caret is in the title, nothing typed, when another window renames the thing; the page picks it up on its next refresh.
  await title.click()
  m.state = { ...m.state, revision: m.state.revision + 1, tasks: m.state.tasks.map((t) => ({ ...t, title: '别处改的标题', notes: '别处改的说明' })) }
  await expect(page.locator('.bar-title')).toHaveText('别处改的标题', { timeout: 10000 })
  await expect(title).toHaveValue('交房租')
  await notes.click()
  // Leaving sends nothing and the box now shows what is saved.
  await expect(title).toHaveValue('别处改的标题')
  await expect(notes).toHaveValue('别处改的说明')
  await page.getByRole('heading', { name: '动态' }).or(page.locator('.section-label').filter({ hasText: '动态' })).first().click()
  await expect(notes).toHaveValue('别处改的说明')
  expect(m.commands).toEqual([])
  expect(m.state.tasks[0]).toMatchObject({ title: '别处改的标题', notes: '别处改的说明' })
  // Typing still saves.
  await title.fill('我接着改')
  await notes.focus()
  await expect.poll(() => m.commands.at(-1)).toMatchObject({ type: 'renameThing', title: '我接着改' })
  expect(m.commands).toHaveLength(1)
  expect(m.errors).toEqual([])
})

test('editing again while a save is on its way ends on the newer text, and an old answer cannot call it saved', async ({ page }) => {
  const m = await mock(page)
  m.delay = 400
  m.reject = (c) => c.type === 'renameThing' && c.title.includes('第二次')
  await page.goto('/t/task')
  const title = page.getByRole('textbox', { name: '标题', exact: true })
  const notes = page.getByRole('textbox', { name: '说明', exact: true })
  const saved = page.locator('.head-row').getByRole('status')
  // Watch the whole exchange: 已保存 must not appear at any moment while the newer text is unsaved.
  await page.evaluate(() => {
    const seen = window as unknown as { claimedSaved: boolean }
    seen.claimedSaved = false
    new MutationObserver(() => {
      if (document.querySelector('.head-row')?.textContent?.includes('已保存')) seen.claimedSaved = true
    }).observe(document.body, { subtree: true, childList: true, characterData: true })
  })
  await title.fill('第一次改')
  await notes.focus()
  await expect(saved).toHaveText('正在保存')
  await title.fill('第二次改')
  await notes.focus()
  // The first answer arrives and succeeds, but the newer text is still unsaved: no 已保存, and the box keeps it.
  await expect.poll(() => m.state.tasks[0].title).toBe('第一次改')
  await expect(title).toHaveValue('第二次改')
  await expect(page.getByRole('alert').filter({ hasText: '标题没保存上' })).toBeVisible()
  await expect(saved).toHaveCount(0)
  expect(await page.evaluate(() => (window as unknown as { claimedSaved: boolean }).claimedSaved)).toBe(false)
  await expect(title).toHaveValue('第二次改')
  expect(m.commands.filter((c) => c.type === 'renameThing').map((c) => (c as Extract<Command, { type: 'renameThing' }>).title)).toEqual(['第一次改', '第二次改'])
  // What is saved is the first edit, and that is what 改回原来的 returns to.
  await page.getByRole('alert').filter({ hasText: '标题没保存上' }).getByRole('button', { name: '改回原来的' }).click()
  await expect(title).toHaveValue('第一次改')
  expect(m.errors).toEqual([])
})

test('a task with nothing set offers what can be set, and each fact starts its own sentence', async ({ page }) => {
  const due = new Date(Date.now() + 26 * 3600_000).toISOString()
  const state = workspace({
    projects: [{ id: 'project', name: 'A 项目', goal: '', status: 'active', progress: '', nextSteps: [], updatedAt: at }],
    tasks: [task({}), task({ id: 'full', title: '回邮件', due, projectId: 'project', triggers: [{ id: 'due-reminder', kind: 'time', description: '回邮件', nextAt: new Date(Date.parse(due) - 1800_000).toISOString(), active: true }] })],
  })
  const m = await mock(page, state)
  await page.goto('/t/task')
  const info = page.locator('.info-line')
  await expect(info.getByRole('button')).toHaveText(['待办', '定个截止时间', '加个提醒', '归到项目', '标成尽快'])
  for (const [name, sentence] of [['定个截止时间', '截止时间定在：'], ['加个提醒', '到这个时间提醒我：'], ['归到项目', '把这件事归到项目：'], ['标成尽快', '这件事要尽快'], ['待办', '把状态改成：']]) {
    await info.getByRole('button', { name, exact: true }).click()
    await expect(secretary(page)).toHaveValue(sentence)
    await expect(secretary(page)).toBeFocused()
  }
  await page.goto('/t/full')
  await expect(info.getByRole('button', { name: /^(定个截止时间|加个提醒|归到项目)$/ })).toHaveCount(0)
  await info.getByRole('button', { name: /提醒$/ }).click()
  await expect(secretary(page)).toHaveValue('把提醒改到：')
  await info.getByRole('button', { name: 'A 项目' }).click()
  await expect(secretary(page)).toHaveValue('把这件事挪到项目：')
  // Nothing was sent: the sentence is the user's to finish.
  expect(m.commands).toEqual([])

  // With reminders switched off in settings the time is not shown as if it would fire; the line says why.
  m.state.settings.followUps = false
  await page.reload()
  await expect(info).not.toContainText(/\d 提醒/)
  await expect(info.getByRole('link', { name: '提醒已在设置里停了' })).toHaveAttribute('href', '/settings')
  expect(m.errors).toEqual([])
})

test('a result waiting to be used is offered by where it will go', async ({ page }) => {
  const state = workspace({
    projects: [{ id: 'project', name: 'A 项目', goal: '', status: 'active', progress: '', nextSteps: [], updatedAt: at }],
    runs: [
      run({ id: 'steps', output: '- [ ] 查余额\n- [ ] 转账' }),
      run({ id: 'summary', kind: 'summary', prompt: '总结进展', output: '房东已确认金额。' }),
      run({ id: 'draft', kind: 'draft', prompt: '起草给房东的消息', output: '房东你好，房租今天转。' }),
      run({ id: 'p-steps', thingId: 'project', output: '- [ ] 订会议室\n- [ ] 发邀请\n- [ ] 印议程' }),
      run({ id: 'p-summary', thingId: 'project', kind: 'summary', prompt: '总结项目', output: '已完成一半。' }),
    ],
  })
  const m = await mock(page, state)
  await page.goto('/t/task')
  const row = (text: string) => page.locator('.activity > li').filter({ hasText: text })
  await expect(page.getByRole('button', { name: /放回去|放回来|看看|重做|存成|写进/ })).toHaveCount(0)
  await expect(row('拆成几步').getByRole('button')).toHaveText(['加入 2 个子任务', '看结果'])
  await expect(row('起草给房东的消息').getByRole('button')).toHaveText(['保存为文档', '看结果'])
  // A summary on a task is added to its notes; the button no longer calls that 进度.
  await expect(row('总结进展').getByRole('button')).toHaveText(['追加到说明', '看结果'])
  await row('总结进展').getByRole('button', { name: '看结果' }).click()
  await expect(row('总结进展').getByText('房东已确认金额。')).toBeVisible()
  expect(m.commands).toEqual([])
  await row('总结进展').getByRole('button', { name: '追加到说明' }).click()
  await expect(page.locator('.toast')).toContainText('已追加到说明')
  await expect(page.locator('.toast').getByRole('button', { name: '撤销' })).toBeVisible()
  expect(m.commands.at(-1)).toMatchObject({ type: 'adoptRun', id: 'summary', as: 'progress', text: '房东已确认金额。' })
  await row('起草给房东的消息').getByRole('button', { name: '保存为文档' }).click()
  await expect.poll(() => m.commands.at(-1)).toMatchObject({ type: 'adoptRun', id: 'draft', as: 'doc' })

  await page.goto('/t/project')
  await expect(row('拆成几步').getByRole('button')).toHaveText(['创建 3 件待办', '看结果'])
  await expect(row('总结项目').getByRole('button')).toHaveText(['记进项目记忆', '看结果'])
  await row('拆成几步').getByRole('button', { name: '创建 3 件待办' }).click()
  await expect.poll(() => m.commands.at(-1)).toMatchObject({ type: 'adoptRun', id: 'p-steps', as: 'subtasks' })
  expect(m.commands).toHaveLength(3)
  expect(m.errors).toEqual([])
})

test('a long title and long notes stay readable and editable on a phone', async ({ browser }) => {
  // A touch screen: nothing can depend on hovering.
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true })
  const page = await context.newPage()
  const long = '给张三回邮件确认下周三在墨尔本办公室的季度复盘会议议程、参会名单和需要提前准备的三份材料'
  const due = new Date(Date.now() + 5 * 3600_000).toISOString()
  const m = await mock(page, workspace({
    projects: [{ id: 'project', name: '季度复盘', goal: '', status: 'active', progress: '', nextSteps: [], updatedAt: at }],
    tasks: [task({ title: long, notes: '对方周一已经催过一次。\n附件用第二版议程，不要发第一版。'.repeat(3), due, projectId: 'project', owedTo: { who: '张三', since: at }, checklist: [{ id: 'c1', text: '确认参会名单', done: false }] })],
    runs: [run({ output: '- [ ] 订会议室\n- [ ] 发会议邀请\n- [ ] 打印议程', prompt: '把会前准备拆成几步' })],
  }))
  await page.goto('/t/task')
  const title = page.getByRole('textbox', { name: '标题', exact: true })
  await expect(title).toHaveValue(long)
  expect(await noSidewaysScroll(page)).toBe(true)
  // The whole title and the whole of the notes are shown, not cut or scrolled inside their boxes.
  for (const box of [title, page.getByRole('textbox', { name: '说明', exact: true })]) expect(await box.evaluate((el) => el.scrollHeight <= el.clientHeight + 1)).toBe(true)
  // The facts wrap inside the screen, and the one that removes a subtask is there without hovering.
  for (const fact of await page.locator('.info-line .fact').all()) {
    const box = (await fact.boundingBox())!
    expect(box.x + box.width).toBeLessThanOrEqual(390)
  }
  await expect(page.getByRole('button', { name: '删掉：确认参会名单' })).toBeVisible()
  expect(await page.getByRole('button', { name: '删掉：确认参会名单' }).evaluate((el) => Number(getComputedStyle(el).opacity))).toBeGreaterThan(0)
  // The result's own line keeps its words; its action is whole and inside the screen.
  const action = page.getByRole('button', { name: '加入 3 个子任务' })
  await action.scrollIntoViewIfNeeded()
  const box = (await action.boundingBox())!
  expect(box.x + box.width).toBeLessThanOrEqual(390)
  await expect(page.locator('.activity > li').filter({ hasText: '把会前准备拆成几步' })).toBeVisible()
  // Editing still works with the pinned secretary on screen.
  await title.focus()
  await page.keyboard.press('Control+End')
  await page.keyboard.type('（周三前）')
  await page.getByRole('textbox', { name: '说明', exact: true }).focus()
  await expect(page.locator('.head-row').getByRole('status')).toHaveText('已保存')
  expect(m.commands[0]).toMatchObject({ type: 'renameThing', title: `${long}（周三前）` })
  expect(m.errors).toEqual([])
  await context.close()
})

test('a ChatGPT sign-in waiting on a remote server is finished by pasting the address the browser ended on', async ({ page }) => {
  const m = await mock(page, workspace(), { chatgpt: true })
  let connected = false
  await page.route((url) => url.pathname === '/v1/chatgpt/direct/account', (route) => route.fulfill({
    json: connected
      ? { accounts: [{ client_id: 'oaiapp_test', email: 'me@example.test', connected: true, plan_enabled: true, verified: false, paused: false }], active_client_id: 'oaiapp_test', pending: false, default_ready: false }
      : { accounts: [], active_client_id: '', pending: true, default_ready: false },
  }))
  await page.route((url) => url.pathname === '/v1/chatgpt/direct/models', (route) => route.fulfill({ json: { models: [] } }))
  await page.route((url) => url.pathname === '/v1/chatgpt/direct/callback', (route) => {
    m.posts.push({ path: '/v1/chatgpt/direct/callback', method: route.request().method(), body: route.request().postDataJSON() })
    connected = true
    return route.fulfill({ json: { connected: true } })
  })
  await page.goto('/settings')
  await page.getByRole('button', { name: /ChatGPT 订阅（官方授权）/ }).click()
  const address = page.getByRole('textbox', { name: '授权后的浏览器地址' })
  const finish = page.getByRole('button', { name: '完成登录' })
  await expect(finish).toBeDisabled()
  const pasted = 'http://127.0.0.1:1455/auth/callback?code=test-code&state=test-state'
  await address.fill(`  ${pasted}  `)
  await finish.click()
  await expect(page.getByText('已连接 me@example.test').first()).toBeVisible()
  await expect(address).toHaveCount(0)
  expect(m.posts.filter((p) => p.path === '/v1/chatgpt/direct/callback')).toEqual([{ path: '/v1/chatgpt/direct/callback', method: 'POST', body: { url: pasted } }])
  expect(m.errors).toEqual([])
})

test('a ChatGPT model missing from the list can be typed in, and an unreadable list does not block the page', async ({ page }) => {
  const m = await mock(page, workspace(), { chatgpt: true })
  let model = ''
  await page.route((url) => url.pathname === '/v1/chatgpt/direct/account', (route) => route.fulfill({
    json: { accounts: [{ client_id: 'oaiapp_test', email: 'me@example.test', connected: true, plan_enabled: true, verified: false, paused: false, model, model_manual: model !== '' }], active_client_id: 'oaiapp_test', pending: false, default_ready: false },
  }))
  await page.route((url) => url.pathname === '/v1/chatgpt/direct/models', (route) => route.fulfill({ status: 502, json: { error: 'chatgpt_provider_error', message: '模型列表暂时读不到' } }))
  await page.route((url) => url.pathname === '/v1/chatgpt/direct/select', (route) => {
    const body = route.request().postDataJSON()
    m.posts.push({ path: '/v1/chatgpt/direct/select', method: 'POST', body })
    model = body.model
    return route.fulfill({ json: { selected: true } })
  })
  await page.goto('/settings')
  await page.getByRole('button', { name: /ChatGPT 订阅（官方授权）/ }).click()
  await expect(page.getByText('已连接 me@example.test').first()).toBeVisible()
  const name = page.getByRole('textbox', { name: '手动填写模型名' })
  const use = page.getByRole('button', { name: '用这个模型' })
  await expect(use).toBeDisabled()
  await name.fill('bad name')
  await expect(use).toBeDisabled()
  await name.fill(' gpt-6.1-sol ')
  await use.click()
  await expect(page.getByText('现在用的是手动填写的 gpt-6.1-sol。')).toBeVisible()
  expect(m.posts.filter((p) => p.path === '/v1/chatgpt/direct/select')).toEqual([{ path: '/v1/chatgpt/direct/select', method: 'POST', body: { client_id: 'oaiapp_test', model: 'gpt-6.1-sol', manual: true } }])
  expect(m.errors).toEqual([])
})


/* ---------- 说了「尽快」的事（H1） ---------- */

test('a to-do the user said cannot wait leads today\'s timeline until it is done or gets a time', async ({ page }) => {
  // Midday, so that times set for this evening are still ahead whenever the test runs.
  const noon = new Date(); noon.setHours(12, 0, 0, 0)
  await page.clock.setFixedTime(noon)
  const today = (hour: number) => { const d = new Date(); d.setHours(hour, 0, 0, 0); return d.toISOString() }
  const m = await mock(page, workspace({
    settings: { timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, city: '上海', dailyBudget: 10, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00' },
    tasks: [
      task({ id: 'later', title: '晚上对账', due: today(23) }),
      task({ id: 'second', title: '赶紧回客户邮件', urgent: true, createdAt: '2026-09-30T03:00:00Z' }),
      task({ id: 'first', title: '尽快开始推广', urgent: true, createdAt: '2026-09-29T03:00:00Z' }),
      task({ id: 'done', title: '马上交表', urgent: true, status: 'done' }),
      task({ id: 'plain', title: '整理发票' }),
    ],
  }))
  await page.goto('/')
  const today_ = page.getByRole('region', { name: '今天' })
  const rows = today_.locator('.hall-task')
  // Urgent ones first, oldest first, with 尽快 where the clock time goes; done and unmarked ones stay out.
  await expect(rows).toHaveCount(3)
  await expect(rows.nth(0)).toContainText('尽快开始推广')
  await expect(rows.nth(0).locator('.hall-time')).toHaveText('尽快')
  await expect(rows.nth(1)).toContainText('赶紧回客户邮件')
  await expect(rows.nth(2)).toContainText('晚上对账')
  await expect(today_).not.toContainText('马上交表')
  await expect(today_).not.toContainText('整理发票')

  // On the thing page 尽快 starts a sentence for the secretary, like the other facts; the mark itself is a plain patch.
  await page.goto('/t/first')
  await page.getByRole('button', { name: '尽快', exact: true }).click()
  await expect(secretary(page)).toHaveValue('这件事不急了')
  m.state = { ...m.state, tasks: m.state.tasks.map((t) => (t.id === 'first' ? { ...t, urgent: false } : t)) }
  await page.goto('/t/first')
  await expect(page.getByRole('button', { name: '标成尽快' })).toBeVisible()
  await page.goto('/')
  await expect(rows.nth(0)).toContainText('赶紧回客户邮件')
  await expect(today_).not.toContainText('尽快开始推广')

  // Given a time today, it sits by that time and no longer says 尽快; without it, it is back on top.
  m.state = { ...m.state, tasks: m.state.tasks.map((t) => (t.id === 'second' ? { ...t, due: today(22) } : t)) }
  await page.goto('/')
  await expect(rows.nth(0)).toContainText('赶紧回客户邮件')
  await expect(today_.locator('.hall-time', { hasText: '尽快' })).toHaveCount(0)
  m.state = { ...m.state, tasks: m.state.tasks.map((t) => (t.id === 'second' ? { ...t, due: undefined } : t)) }
  await page.goto('/')
  await expect(rows.nth(0).locator('.hall-time')).toHaveText('尽快')
  expect(m.errors).toEqual([])
})

test('only urgent to-dos: the timeline shows them and does not say nothing is timed; ideas are not marked', async ({ page }) => {
  await mock(page, workspace({ tasks: [task({ id: 'only', title: '抓紧续签合同', urgent: true })], ideas: [{ id: 'idea', title: '学游泳', body: '', status: 'active', conditions: [], evolution: [], sources: [], remindersOn: false, createdAt: at, updatedAt: at }] as State['ideas'] }))
  await page.goto('/')
  const today_ = page.getByRole('region', { name: '今天' })
  await expect(today_.locator('.hall-task').first()).toContainText('抓紧续签合同')
  await expect(today_).not.toContainText('今天没有定了时间的事')
  await page.goto('/t/idea')
  await expect(page.getByRole('group', { name: /这件事的情况/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /尽快/ })).toHaveCount(0)
})

test('on a phone the five rows are: what rang, who waits, what cannot wait, then what is ahead', async ({ page }) => {
  // Midday, so that times set for this evening are still ahead whenever the test runs.
  const noon = new Date(); noon.setHours(12, 0, 0, 0)
  await page.clock.setFixedTime(noon)
  await page.setViewportSize({ width: 390, height: 844 })
  const today = (hour: number, minute = 0) => { const d = new Date(); d.setHours(hour, minute, 0, 0); return d.toISOString() }
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone
  await mock(page, workspace({
    settings: { timezone: zone, city: '上海', dailyBudget: 10, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00' },
    notices: [{ id: 'n1', thingId: 'rang', triggerId: 'due-reminder', title: '到点的事', reason: '', dueAt: '2026-09-30T01:00:00Z', createdAt: at }] as State['notices'],
    tasks: [
      task({ id: 'rang', title: '到点的事' }),
      task({ id: 'owed', title: '回李四', owedTo: { who: '李四', since: at } }),
      task({ id: 'u1', title: '尽快甲', urgent: true, createdAt: '2026-09-29T03:00:00Z' }),
      task({ id: 'u2', title: '尽快乙', urgent: true, createdAt: '2026-09-29T04:00:00Z' }),
      task({ id: 'a1', title: '今晚一', due: today(23, 40) }),
      task({ id: 'a2', title: '今晚二', due: today(23, 50) }),
      task({ id: 'a3', title: '今晚三', due: today(23, 55) }),
    ],
  }))
  await page.goto('/')
  const rows = page.getByRole('region', { name: '今天' }).locator('.hall-task')
  await expect(rows).toHaveCount(5)
  await expect(rows).toContainText(['到点的事', '回李四', '尽快甲', '尽快乙', '今晚一'])
  expect(await noSidewaysScroll(page)).toBe(true)
})

/* ---------- 资料库：来源按出处合并 ---------- */

test('the library lists each origin once; what was said opens as a list that can be searched and paged', async ({ page }) => {
  const m = await mock(page, workspace({
    sources: [
      { id: 'said', name: '跟秘书说的话', kind: 'said', single: false, status: 'connected', note: '', itemCount: 61, lastSyncAt: at },
      { id: 'import:11111111-1111-4111-8111-111111111111', name: 'chatgpt-export.zip', kind: 'import', single: false, status: 'connected', note: '', itemCount: 23110, lastSyncAt: at },
      { id: 'doc', name: '租房合同', kind: 'file', single: true, status: 'failed', note: '原文可读，部分处理未完成；展开后台任务查看缺口', itemCount: 1, lastSyncAt: at },
    ],
    jobs: [
      { id: 'j-done', title: '建立检索索引', trigger: 'event', status: 'done', detail: '「租房合同」', createdAt: at },
      { id: 'j-failed', title: '从资料里读出要点', trigger: 'event', status: 'failed', detail: '「租房合同」', createdAt: at },
    ] as State['jobs'],
  }))
  const asked: string[] = []
  await page.route((url) => url.pathname === '/v1/workspace/source-groups/said/items', (route) => {
    const url = new URL(route.request().url())
    asked.push(url.search)
    const q = url.searchParams.get('q')
    if (q) return route.fulfill({ json: { items: [{ id: 's-hit', title: '', excerpt: '周末想去爬山', at }], next: '' } })
    if (url.searchParams.get('cursor')) return route.fulfill({ json: { items: [{ id: 's-51', title: '', excerpt: '最早的一句', at }], next: '' } })
    return route.fulfill({ json: { items: Array.from({ length: 50 }, (_, n) => ({ id: `s-${n}`, title: '', excerpt: `第 ${n + 1} 句话`, at })), next: `${at}|00000000-0000-4000-8000-000000000050` } })
  })
  await page.route((url) => url.pathname === '/v1/memory/sources/s-hit', (route) => route.fulfill({ json: { derived: [], processing: [], source: { id: 's-hit', version: 1, title: '秘书原话', text: '周末想去爬山', recorded_at: at, has_attachment: false, representation: 'original' } } }))
  await page.goto('/library?tab=sources')
  const cards = page.locator('.source-card')
  await expect(cards).toHaveCount(3)
  await expect(cards.nth(0)).toContainText('跟秘书说的话')
  await expect(cards.nth(0)).toContainText('61 条')
  await expect(cards.nth(1)).toContainText('23110 条')
  // No internal words, and nothing is tagged unless something is wrong.
  await expect(page.locator('.sources-grid')).not.toContainText(/desk|actions|已连接|原文已保存/)
  await expect(cards.nth(2)).toContainText('部分处理未完成')
  // Finished background work is counted, not listed, until asked for.
  const work = page.locator('.list').last()
  await expect(work).toContainText('从资料里读出要点')
  await expect(work).not.toContainText('建立检索索引')
  await page.getByRole('button', { name: '已完成 1 项，点开看' }).click()
  await expect(work).toContainText('建立检索索引')
  await page.getByRole('button', { name: '收起已完成的' }).click()

  await page.getByRole('button', { name: '查看：跟秘书说的话，共 61 条' }).click()
  const sheet = page.getByRole('dialog')
  await expect(sheet.locator('.source-item')).toHaveCount(50)
  await sheet.getByRole('button', { name: '再看 50 条' }).click()
  await expect(sheet.locator('.source-item')).toHaveCount(51)
  await expect(sheet.getByRole('button', { name: '再看 50 条' })).toHaveCount(0)
  await sheet.getByRole('searchbox', { name: '在这里面搜' }).fill('爬山')
  await expect(sheet.locator('.source-item')).toHaveCount(1)
  await sheet.getByRole('button', { name: '查看原文：周末想去爬山' }).click()
  await expect(page.getByRole('dialog').last()).toContainText('周末想去爬山')
  expect(asked.some((s) => s.includes('q=%E7%88%AC%E5%B1%B1'))).toBe(true)
  expect(m.errors).toEqual([])
})

/* ---------- 删除一件事 ---------- */

test('a to-do is deleted only after confirming, and the page returns home without it', async ({ page }) => {
  const m = await mock(page, workspace({ tasks: [task({ id: 'gone', title: '验证用的待办', status: 'cancelled' }), task({ id: 'kept', title: '交房租' })] }))
  await page.goto('/t/gone')
  await page.getByRole('button', { name: '删除：验证用的待办' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toContainText('删了找不回来')
  await dialog.getByRole('button', { name: '留着' }).click()
  expect(m.commands.some((c) => c.type === 'deleteThing')).toBe(false)
  await page.getByRole('button', { name: '删除：验证用的待办' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '删掉' }).click()
  await expect(page).toHaveURL(/\/$/)
  expect(m.commands.filter((c) => c.type === 'deleteThing')).toMatchObject([{ type: 'deleteThing', id: 'gone' }])
  expect(m.state.tasks.map((t) => t.id)).toEqual(['kept'])
  expect(m.errors).toEqual([])
})

/* ---------- 副手做完了 ---------- */

test('finished agent work is pinned on the home page as a result, not as a reminder that came due', async ({ page }) => {
  await mock(page, workspace({
    tasks: [task({ id: 'research', title: '研究 PCAS' })],
    notices: [{ id: 'n-run', thingId: 'research', title: '研究 PCAS', reason: '副手做完了：\n研究结论：三层记忆已经落地。', result: true, dueAt: at, createdAt: at }] as State['notices'],
  }))
  await page.goto('/')
  const today = page.getByRole('region', { name: '今天' })
  await expect(today.getByRole('heading', { name: '做完了，等你看' })).toBeVisible()
  const row = today.locator('.hall-rang')
  await expect(row).toContainText('研究 PCAS')
  await expect(row).toContainText('副手做完了')
  await expect(row).not.toContainText('到点')
})

/* ---------- 要你动手 ---------- */

test('a stale result asks to be redone only while its thing is open and it is the newest result', async ({ page }) => {
  const stale = (over: Partial<Run>) => run({ status: 'done', output: '旧结果', staleContext: true, prompt: '今天天气怎么样', ...over })
  await mock(page, workspace({
    tasks: [task({ id: 'closed', title: '今天天气怎么样', status: 'done' }), task({ id: 'open', title: '写周报' }), task({ id: 'redone', title: '订机票' })],
    runs: [
      stale({ id: 'r-closed', thingId: 'closed' }),
      stale({ id: 'r-open', thingId: 'open', prompt: '列提纲' }),
      stale({ id: 'r-old', thingId: 'redone', prompt: '查航班', createdAt: '2026-09-29T02:00:00Z' }),
      run({ id: 'r-new', thingId: 'redone', status: 'done', output: '新结果', prompt: '查航班', createdAt: '2026-09-30T02:00:00Z' }),
    ],
  }))
  await page.goto('/')
  await page.getByRole('button', { name: /1 件要你动手/ }).click()
  const queue = page.locator('.hall-tag.redo')
  await expect(queue).toHaveCount(1)
  await expect(page.getByText('「列提纲」用到的记忆后来改过，要重做')).toBeVisible()
  await expect(page.getByText('「今天天气怎么样」用到的记忆后来改过，要重做')).toHaveCount(0)
})

/* ---------- 文档：保存失败不丢字 ---------- */

test('a document keeps what was typed when saving fails, and can be saved again', async ({ page }) => {
  const m = await mock(page, workspace({ docs: [{ id: 'doc', thingId: 'task', title: '方案', body: '旧正文', by: 'user', createdAt: at, updatedAt: at }] as State['docs'] }))
  m.refuse.add('updateDoc')
  await page.goto('/t/task')
  await page.getByRole('button', { name: /方案/ }).first().click()
  await page.getByRole('textbox', { name: '方案 的内容' }).click()
  const editor = page.locator('textarea.doc-editor')
  await editor.fill('刚写的新正文，不能丢')
  await editor.blur()
  // The save was refused: the editor stays open with the text, and says so.
  await expect(page.getByRole('alert').filter({ hasText: '没保存上' })).toBeVisible()
  await expect(editor).toHaveValue('刚写的新正文，不能丢')
  m.refuse.delete('updateDoc')
  await page.getByRole('button', { name: '再保存一次' }).click()
  await expect(page.getByRole('alert').filter({ hasText: '没保存上' })).toHaveCount(0)
  expect(m.commands.filter((c) => c.type === 'updateDoc').at(-1)).toMatchObject({ patch: { body: '刚写的新正文，不能丢' } })

})
