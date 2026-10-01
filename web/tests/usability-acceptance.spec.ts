import { expect, test, type Locator, type Page } from '@playwright/test'
import type { Candidate, State } from '../src/domain/types'
import type { Action } from '../src/store/actions'

type Command = Action & { requestId: string; expectedRevision: number }
const at = '2026-10-01T09:00:00Z'
const source = { sourceId: 'source-original', label: '验收原资料', at }

// Business/request expectations are fixed by U2 and S1, independently of U2's tests.
function workspace(candidates: Candidate[] = []): State {
  return {
    version: 1, revision: 7, budgetUsage: 2.5, notices: [],
    settings: { timezone: 'UTC', city: '原城市', dailyBudget: 37, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '08:40' },
    tasks: [{ id: 'task-original', title: '原任务：长标题与原说明必须保留', notes: '原说明', status: 'todo', dependsOn: [], checklist: [], triggers: [], sources: [source], history: [], createdAt: at, updatedAt: at }],
    ideas: [], projects: [], memories: [], candidates,
    agents: [{ id: 'agent-limited', name: '独立验收模型', enabled: false, available: true, default: true, channel: 'api', note: '', inputPrice: 3, outputPrice: 7, maxOutput: 100, memoryKinds: ['fact', 'plan'], includeInferred: false }],
    docs: [], runs: [], samples: [], sources: [{ id: source.sourceId, name: source.label, method: 'manual', status: 'manual', note: '不随忽略候选删除', itemCount: 1 }], jobs: [], activity: [], excludedMemories: {},
  }
}
function candidate(kind: Candidate['kind'], id = `candidate-${kind}`): Candidate {
  return { id, kind, text: `${id} 的原文`, memoryKind: kind === 'memory' ? 'preference' : undefined, confidence: 0.5, source, state: 'pending', createdAt: at }
}
async function mock(page: Page, initial = workspace()) {
  const backend = {
    state: structuredClone(initial), commands: [] as Command[], writes: [] as { path: string; method: string }[], errors: [] as string[],
    deskCalls: [] as { thingId?: string; text: string }[], turnUpdate: undefined as { title?: string; notes?: string } | undefined,
    resolutionId: 'server-result-a73', resolutionMissing: false, rejectNext: false, waitNext: undefined as Promise<void> | undefined,
    holdNext() {
      let release!: () => void
      this.waitNext = new Promise<void>(resolve => { release = resolve })
      return release
    },
  }
  page.on('pageerror', error => backend.errors.push(error.message))
  await page.route('**/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname, method = request.method()
    if (method !== 'GET') backend.writes.push({ path, method })
    if (path === '/v1/workspace' && method === 'GET') return route.fulfill({ json: backend.state })
    if (path === '/v1/desk/turns' && method === 'GET') return route.fulfill({ json: { turns: [] } })
    if (path === '/v1/desk/turn' && method === 'POST') {
      const body = request.postDataJSON()
      backend.deskCalls.push(body)
      expect(body.thingId).toBe('task-original')
      backend.state = { ...backend.state, revision: backend.state.revision + 1, tasks: backend.state.tasks.map(task => task.id === body.thingId ? { ...task, ...backend.turnUpdate } : task) }
      return route.fulfill({ json: { conversationId: body.conversationId, state: backend.state, turn: {
        id: 'server-turn', text: body.text, reply: '改好了', agent: '独立验收模型', createdAt: at, ask: null, cards: [],
        receipts: [{ actionId: 'server-update-action', op: 'update', text: '已更新标题和说明', thingId: body.thingId, status: 'done', undoable: true }],
      } } })
    }
    if (path === '/v1/connectors' && method === 'GET') return route.fulfill({ json: [] })
    if (path === '/v1/notify/config' && method === 'GET') return route.fulfill({ json: { webPush: { publicKey: '', subscriptions: 0 }, telegram: { configured: false, chatId: '' } } })
    if (path === '/v1/models/openai' && method === 'GET') return route.fulfill({ json: { editable: true, text: { base_url: '', model: '', input_cny_per_million: 3, output_cny_per_million: 7, default: false, key_configured: false }, embedding: { base_url: '', model: '', input_cny_per_million: 0.2, output_cny_per_million: 0, default: false, key_configured: false }, decision: { key_configured: false, saved: false } } })
    if (path === '/v1/chatgpt/account' && method === 'GET') return route.fulfill({ json: { enabled: false, authenticated: false, models: [] } })
    if (path === '/v1/chatgpt/direct/account' && method === 'GET') return route.fulfill({ json: { accounts: [], default_ready: false, pending: false } })
    if (path !== '/v1/workspace/commands') return route.fulfill({ status: method === 'GET' ? 200 : 500, json: method === 'GET' ? {} : { error: 'unexpected_acceptance_write' } })
    const command = request.postDataJSON() as Command
    backend.commands.push(command)
    expect(command.requestId).toEqual(expect.any(String))
    expect(command.expectedRevision).toBe(backend.state.revision)
    const pause = backend.waitNext
    const reject = backend.rejectNext
    backend.waitNext = undefined
    backend.rejectNext = false
    if (pause) await pause
    if (reject) return route.fulfill({ status: 409, json: { error: 'version_conflict' } })
    const state = backend.state
    switch (command.type) {
      case 'updateSettings': Object.assign(state.settings, command.patch); break
      case 'updateAgent': state.agents = state.agents.map(agent => agent.id === command.id ? { ...agent, ...command.patch } : agent); break
      case 'renameThing': state.tasks = state.tasks.map(task => task.id === command.id ? { ...task, title: command.title } : task); break
      case 'setNotes': state.tasks = state.tasks.map(task => task.id === command.id ? { ...task, notes: command.text } : task); break
      case 'ignoreCandidate': state.candidates = state.candidates.map(item => item.id === command.id ? { ...item, state: 'ignored' } : item); break
      case 'acceptCandidate': {
        state.candidates = state.candidates.map(item => item.id === command.id ? { ...item, state: 'accepted', resolvedInto: backend.resolutionId } : item)
        if (command.kind === 'task' && !backend.resolutionMissing) state.tasks.push({ id: backend.resolutionId, title: command.text, notes: '', status: 'todo', dependsOn: [], checklist: [], triggers: [], sources: [source], history: [], createdAt: at, updatedAt: at })
        else if (command.kind === 'idea' && !backend.resolutionMissing) state.ideas.push({ id: backend.resolutionId, title: command.text, body: '', status: 'active', conditions: [], remindersOn: true, evolution: [], sources: [source], createdAt: at, updatedAt: at })
        else if (command.kind === 'memory' && !backend.resolutionMissing) state.memories.push({ id: backend.resolutionId, kind: command.memoryKind ?? 'fact', text: command.text, epistemic: 'confirmed', recordVersion: 1, pinned: false, sources: [source], versions: [], visibleTo: [], exposure: 1, lastUsedAt: at })
        else if (command.kind === 'unknown') throw new Error('unknown must never be submitted')
        break
      }
      default: throw new Error(`Unexpected acceptance command: ${command.type}`)
    }
    backend.state = { ...state, revision: state.revision + 1 }
    return route.fulfill({ json: backend.state })
  })
  return backend
}
async function assertNoOverflow(page: Page, width: number) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(width + 1)
}
async function clickWithoutWaitingForEnabled(page: Page, button: Locator) {
  const box = await button.boundingBox()
  expect(box).not.toBeNull()
  await page.mouse.click(box!.x + box!.width / 2, box!.y + box!.height / 2)
}
async function pendingEntry(page: Page) {
  await page.goto('/library')
  const entry = page.getByRole('button', { name: /逐条处理|待确认内容/ }).or(page.getByRole('link', { name: /逐条处理|待确认内容/ }))
  await expect(entry).toBeVisible()
  await entry.click()
  await expect(page.getByRole('heading', { name: /待确认/ })).toBeVisible()
}
const candidateAction = (page: Page, kind: 'task' | 'idea' | 'memory') => page.getByRole('button', { name: { task: /创建待办/, idea: /保存为想法/, memory: /保存为记忆/ }[kind] })

for (const choice of [
  { name: /事项提醒/, field: 'followUps', before: true },
  { name: /带回搁置的想法/, field: 'wakeIdeas', before: true },
  { name: /资料里的明确待办直接创建/, field: 'autoAccept', before: false },
] as const) {
  test(`only ${choice.field} is saved without changing budget or memory permissions`, async ({ page }) => {
    const backend = await mock(page)
    const saved = structuredClone(backend.state)
    await page.goto('/settings')
    const control = page.getByRole('switch', { name: choice.name })
    await expect(control).toHaveAttribute('aria-checked', String(choice.before))
    expect(backend.writes).toEqual([])
    await control.click()
    await expect.poll(() => backend.commands.length).toBe(1)
    expect(backend.commands[0]).toMatchObject({ type: 'updateSettings', patch: { [choice.field]: !choice.before } })
    if (backend.commands[0].type !== 'updateSettings') throw new Error('wrong setting command')
    expect(Object.keys(backend.commands[0].patch)).toEqual([choice.field])
    expect(backend.state.settings).toEqual({ ...saved.settings, [choice.field]: !choice.before })
    expect(backend.state.agents).toEqual(saved.agents)
    expect(backend.state.budgetUsage).toBe(saved.budgetUsage)
    await page.reload()
    await expect(control).toHaveAttribute('aria-checked', String(!choice.before))
    expect(backend.commands).toHaveLength(1)
    expect(backend.writes).toEqual([{ path: '/v1/workspace/commands', method: 'POST' }])
    expect(backend.errors).toEqual([])
  })
}

for (const width of [1440, 390]) {
  test(`folding settings is read-only and permission defaults survive at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const backend = await mock(page), before = structuredClone(backend.state)
    await page.goto('/settings')
    const folds = [
      { name: /Codex 登录/, content: page.getByRole('heading', { name: 'Codex App Server', exact: true }) },
      { name: /按量计费接口/, content: page.getByLabel('文本 API Base URL', { exact: true }) },
      { name: /接入其他应用/, content: page.getByRole('heading', { name: '资料接入', exact: true }) },
      { name: /记忆范围/, content: page.getByText('事实', { exact: true }) },
    ]
    for (const {name, content} of folds) {
      const fold = page.getByRole('button', { name })
      await fold.scrollIntoViewIfNeeded()
      await fold.focus()
      await expect(fold).toBeFocused()
      await fold.press('Enter')
      await expect(content).toBeVisible()
      await fold.press('Enter')
      await expect(content).toBeHidden()
    }
    await assertNoOverflow(page, width)
    expect(backend.writes).toEqual([])
    expect(backend.state).toEqual(before)
    expect(backend.errors).toEqual([])
  })

  test(`rejected switch saves can retry and rapid pending clicks send once at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const backend = await mock(page), before = structuredClone(backend.state)
    backend.rejectNext = true
    const release = backend.holdNext()
    await page.goto('/settings')
    const control = page.getByRole('switch', { name: /事项提醒/ })
    try {
      await control.click()
      await expect.poll(() => backend.commands.length).toBe(1)
      await expect(control).toBeDisabled()
      await clickWithoutWaitingForEnabled(page, control)
      expect(backend.commands).toHaveLength(1)
    } finally { release() }
    await expect(page.getByText(/没保存上/)).toBeVisible()
    await expect(control).toHaveAttribute('aria-checked', 'true')
    expect(backend.state).toEqual(before)
    await control.click()
    await expect(control).toHaveAttribute('aria-checked', 'false')
    await expect(page.getByText('已保存', { exact: true })).toBeVisible()
    expect(backend.commands).toHaveLength(2)
    for (const command of backend.commands) expect(command).toMatchObject({ type: 'updateSettings', patch: { followUps: false } })
    expect(backend.state.agents).toEqual(before.agents)
    expect(backend.state.settings).toEqual({ ...before.settings, followUps: false })
    await assertNoOverflow(page, width)
    expect(backend.errors).toEqual([])
  })

  test(`task notes keep the draft after rejection and retry the same target at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const backend = await mock(page)
    backend.rejectNext = true
    const draft = '这是等待保存的长说明。'.repeat(12)
    await page.goto('/t/task-original')
    const notes = page.getByRole('textbox', { name: '说明', exact: true })
    await notes.fill(draft)
    await expect(notes).toBeFocused()
    await notes.press('Tab')
    await expect(page.getByText(/没保存上/)).toBeVisible()
    await expect(notes).toHaveValue(draft)
    expect(backend.state.tasks[0].notes).toBe('原说明')
    expect(backend.commands).toHaveLength(1)
    await page.getByRole('button', { name: '再存一次', exact: true }).click()
    await expect(page.getByText('已保存', { exact: true })).toBeVisible()
    expect(backend.commands).toHaveLength(2)
    for (const command of backend.commands) expect(command).toMatchObject({ type: 'setNotes', id: 'task-original', text: draft })
    expect(backend.state.tasks[0].title).toBe('原任务：长标题与原说明必须保留')
    await page.reload()
    await expect(notes).toHaveValue(draft)
    expect(backend.commands).toHaveLength(2)
    await assertNoOverflow(page, width)
    expect(backend.errors).toEqual([])
  })

  test(`unknown stays unsubmitted without an explicit target and ignore keeps source data at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    const item = candidate('unknown')
    const backend = await mock(page, workspace([item])), before = structuredClone(backend.state)
    await pendingEntry(page)
    for (const kind of ['task', 'idea', 'memory'] as const) await expect(candidateAction(page,kind)).toBeEnabled()
    // Viewing a suggestion or moving focus is not a classification/submission.
    await candidateAction(page,'task').focus()
    await expect(candidateAction(page,'task')).toBeFocused()
    expect(backend.writes).toEqual([])
    const ignore = page.getByRole('button', { name: '忽略这条', exact: true })
    await expect(ignore).toBeVisible()
    await expect(page.getByRole('dialog').getByRole('button', { name: /删除/ })).toHaveCount(0)
    await ignore.click()
    await expect.poll(() => backend.commands.length).toBe(1)
    expect(backend.commands[0]).toMatchObject({ type: 'ignoreCandidate', id: item.id })
    expect(backend.state.candidates[0].state).toBe('ignored')
    expect(backend.state.sources).toEqual(before.sources)
    expect(backend.state.tasks).toEqual(before.tasks)
    expect(backend.state.memories).toEqual(before.memories)
    await expect(page.getByRole('button', { name: '撤销', exact: true })).toHaveCount(0)
    await assertNoOverflow(page, width)
    expect(backend.errors).toEqual([])
  })
}

for (const kind of ['task', 'idea', 'memory'] as const) {
  test(`Library pending entry submits the ${kind} target and offers no fake undo`, async ({ page }) => {
    const item = candidate(kind)
    const backend = await mock(page, workspace([item]))
    await pendingEntry(page)
    expect(backend.writes).toEqual([])
    const action = candidateAction(page, kind)
    await expect(action).toBeEnabled()
    await action.click()
    await expect.poll(() => backend.commands.length).toBe(1)
    expect(backend.commands[0]).toMatchObject({ type: 'acceptCandidate', id: item.id, kind, text: item.text })
    if (kind === 'memory') expect(backend.commands[0]).toMatchObject({ memoryKind: 'preference' })
    expect(backend.state.candidates[0].state).toBe('accepted')
    if (kind !== 'memory') await expect(page.locator(`a[href="/t/${backend.resolutionId}"]`)).toBeVisible()
    await expect(page.getByRole('button', { name: '撤销', exact: true })).toHaveCount(0)
    expect(backend.errors).toEqual([])
  })
}

test('unknown explicit idea target survives rejection and retry without a duplicate request', async ({ page }) => {
  const item = candidate('unknown', 'retry-candidate')
  const backend = await mock(page, workspace([item]))
  await pendingEntry(page)
  backend.rejectNext = true
  const release = backend.holdNext()
  const action = candidateAction(page, 'idea')
  try {
    await action.click()
    await expect.poll(() => backend.commands.length).toBe(1)
    await expect(action).toBeDisabled()
    await clickWithoutWaitingForEnabled(page, action)
    expect(backend.commands).toHaveLength(1)
  } finally { release() }
  await expect(page.getByText(/没保存上|没加进去|未保存/)).toBeVisible()
  await expect(action).toBeEnabled()
  expect(backend.state.candidates[0].state).toBe('pending')
  await action.click()
  await expect.poll(() => backend.commands.length).toBe(2)
  for (const command of backend.commands) expect(command).toMatchObject({ type: 'acceptCandidate', id: item.id, kind: 'idea', text: item.text })
  await expect(page.getByRole('button', { name: '撤销', exact: true })).toHaveCount(0)
  expect(backend.errors).toEqual([])
})


test('accepted candidate without a matching server target cannot navigate to a guessed item', async ({ page }) => {
 const item=candidate('task','no-reliable-link')
 const backend=await mock(page,workspace([item]))
 backend.resolutionMissing=true
 await pendingEntry(page)
 await candidateAction(page,'task').click()
 await expect.poll(()=>backend.commands.length).toBe(1)
 expect(backend.commands[0]).toMatchObject({type:'acceptCandidate',id:item.id,kind:'task',text:item.text})
 expect(backend.state.candidates[0].state).toBe('accepted')
 await expect(page.locator(`a[href="/t/${backend.resolutionId}"]`)).toHaveCount(0)
 await expect(page.locator(`a[href="/t/${item.id}"]`)).toHaveCount(0)
 expect(backend.errors).toEqual([])
})


for (const permission of [
 { field:'enabled', name:/启用 独立验收模型/, open:false },
 { field:'includeInferred', name:/推测/, open:true },
] as const) {
 test(`explicit ${permission.field} changes only that model field`,async({page})=>{
  const initial=workspace()
  // Edit permissions for an enabled model; the separate fold test keeps disabled defaults untouched.
  if(permission.open) initial.agents[0].enabled=true
  const backend=await mock(page,initial),before=structuredClone(backend.state)
  await page.goto('/settings')
  if(permission.open) await page.getByRole('button',{name:/记忆范围/}).click()
  const control=page.getByRole('switch',{name:permission.name})
  await expect(control).toHaveAttribute('aria-checked','false')
  expect(backend.writes).toEqual([])
  await control.click()
  await expect.poll(()=>backend.commands.length).toBe(1)
  const command=backend.commands[0]
  expect(command).toMatchObject({type:'updateAgent',id:'agent-limited',patch:{[permission.field]:true}})
  if(command.type!=='updateAgent') throw new Error('wrong model command')
  expect(Object.keys(command.patch)).toEqual([permission.field])
  expect(backend.state.agents).toEqual([{...before.agents[0],[permission.field]:true}])
  expect(backend.state.settings).toEqual(before.settings)
  expect(backend.state.budgetUsage).toBe(before.budgetUsage)
  expect(backend.writes).toEqual([{path:'/v1/workspace/commands',method:'POST'}])
  expect(backend.errors).toEqual([])
 })
}


test('mounted saved title and notes show secretary updates while full reload agrees', async ({ page },info) => {
 const initial=workspace();initial.agents[0].enabled=true
 const backend=await mock(page,initial)
 await page.goto('/t/task-original')
 const title=page.getByRole('textbox',{name:'标题',exact:true})
 const notes=page.getByRole('textbox',{name:'说明',exact:true})
 await title.fill('人工保存标题')
 await title.press('Tab')
 await expect.poll(()=>backend.commands.length).toBe(1)
 expect(backend.state.tasks[0].title).toBe('人工保存标题')
 await notes.fill('人工保存说明')
 await title.click()
 await expect.poll(()=>backend.commands.length).toBe(2)
 expect(backend.state.tasks[0].notes).toBe('人工保存说明')
 backend.turnUpdate={title:'秘书更新后的标题',notes:'秘书更新后的说明'}
 const input=page.getByRole('textbox',{name:'跟秘书说'})
 await input.fill('改标题和说明')
 await input.press('Enter')
 await expect.poll(()=>backend.deskCalls.length).toBe(1)
 await expect(page.getByText('已更新标题和说明',{exact:true})).toBeVisible()
 expect(backend.state.tasks[0]).toMatchObject(backend.turnUpdate)
 await info.attach('server-state-and-mounted-inputs.json',{body:JSON.stringify({commands:backend.commands,deskCalls:backend.deskCalls,saved:backend.state.tasks[0],mounted:{title:await title.inputValue(),notes:await notes.inputValue()}},null,2),contentType:'application/json'})
 await page.screenshot({path:info.outputPath('mounted-after-secretary.png'),fullPage:true})
 await expect.soft(title).toHaveValue('秘书更新后的标题')
 await expect.soft(notes).toHaveValue('秘书更新后的说明')
 await page.reload()
 await expect(title).toHaveValue('秘书更新后的标题')
 await expect(notes).toHaveValue('秘书更新后的说明')
 expect(backend.commands).toHaveLength(2)
 expect(backend.deskCalls).toHaveLength(1)
 expect(backend.errors).toEqual([])
})

test('a rejected unsaved notes draft survives a secretary state refresh',async({page})=>{
 const initial=workspace();initial.agents[0].enabled=true
 const backend=await mock(page,initial)
 backend.rejectNext=true
 await page.goto('/t/task-original')
 const notes=page.getByRole('textbox',{name:'说明',exact:true})
 await notes.fill('保存失败的草稿，不能被静默覆盖')
 await notes.press('Tab')
 await expect(page.getByText(/没保存上/)).toBeVisible()
 backend.turnUpdate={notes:'服务端更新的已保存说明'}
 const input=page.getByRole('textbox',{name:'跟秘书说'})
 await input.fill('更新已保存说明')
 await input.press('Enter')
 await expect(page.getByText('已更新标题和说明',{exact:true})).toBeVisible()
 expect(backend.state.tasks[0].notes).toBe('服务端更新的已保存说明')
 await expect(notes).toHaveValue('保存失败的草稿，不能被静默覆盖')
 expect(backend.commands).toHaveLength(1)
 expect(backend.deskCalls).toHaveLength(1)
 expect(backend.errors).toEqual([])
})
