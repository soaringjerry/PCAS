import { test, expect, type Page } from '@playwright/test'
import type { Agent, Run, State } from '../src/domain/types'
import type { Action } from '../src/store/actions'
import type { RunRequest } from '../src/store/context'

type Command = (Action | (RunRequest & { type: 'requestRun'; id: string })) & { requestId: string; expectedRevision: number }

function agent(id = 'original', name = '原副手', overrides: Partial<Agent> = {}): Agent {
  return { id, name, enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false, ...overrides }
}

function workspace(): State {
  const at = new Date().toISOString()
  return {
    version: 1, revision: 1, budgetUsage: 0,
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00' },
    tasks: [{ id: 'task', title: '测试任务', notes: '', status: 'todo', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: at, updatedAt: at }],
    ideas: [{ id: 'idea', title: '测试想法', body: '', status: 'active', conditions: [], remindersOn: true, evolution: [], sources: [], createdAt: at, updatedAt: at }],
    projects: [{ id: 'project', name: '测试项目', goal: '', status: 'active', progress: '', nextSteps: [], updatedAt: at }],
    agents: [agent()], memories: [], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {},
  }
}

function failedRun(error = 'daily_budget_exceeded'): Run {
  return { id: 'failed-run', thingId: 'task', agentId: 'original', kind: 'draft', prompt: '保留这条原始指令', brief: '', status: 'failed', error, contextMemoryIds: [], staleContext: false, cost: 0, createdAt: new Date().toISOString() }
}

async function mockWorkspace(page: Page, snapshot = workspace()) {
  const commands: Command[] = []
  // Every API request stays in the browser mock; no backend or model is needed.
  await page.route('**/v1/**', route => route.fulfill({ status: 500, json: { error: 'unexpected_mock_request' } }))
  await page.route('**/v1/workspace', route => route.fulfill({ json: snapshot }))
  await page.route('**/v1/workspace/commands', async route => {
    const body = route.request().postDataJSON() as Command
    commands.push(body)
    expect(body.expectedRevision).toBe(snapshot.revision)
    switch (body.type) {
      case 'setNotes':
        snapshot.tasks = snapshot.tasks.map(task => task.id === body.id ? { ...task, notes: body.text } : task)
        snapshot.ideas = snapshot.ideas.map(idea => idea.id === body.id ? { ...idea, body: body.text } : idea)
        break
      case 'updateProject':
        snapshot.projects = snapshot.projects.map(project => project.id === body.id ? { ...project, ...body.patch } : project)
        break
      case 'requestRun':
        snapshot.runs.push({ ...failedRun(), id: body.id, thingId: body.thingId, agentId: body.agentId, kind: body.kind, prompt: body.prompt, status: 'running', error: undefined })
        break
      case 'capture':
        break
      case 'delegateTask':
        snapshot.tasks.push({ ...snapshot.tasks[0], id: body.id, title: body.title, notes: body.prompt })
        break
      default:
        throw new Error(`Unexpected command: ${body.type}`)
    }
    snapshot = { ...snapshot, revision: snapshot.revision + 1 }
    await route.fulfill({ json: snapshot })
  })
  return commands
}

for (const kind of ['task', 'idea', 'project'] as const) {
  test(`${kind} notes save on blur and survive reload`, async ({ page }) => {
    const commands = await mockWorkspace(page)
    await page.goto(`/t/${kind}`)
    const notes = page.getByRole('textbox', { name: '说明', exact: true })
    await notes.fill(`${kind} 的说明，刷新后也应保留`)
    await page.getByRole('textbox', { name: '标题', exact: true }).click()
    await expect.poll(() => commands.length).toBe(1)
    expect(commands[0]).toMatchObject(kind === 'project'
      ? { type: 'updateProject', id: kind, patch: { goal: `${kind} 的说明，刷新后也应保留` } }
      : { type: 'setNotes', id: kind, text: `${kind} 的说明，刷新后也应保留` })
    await page.reload()
    await expect(notes).toHaveValue(`${kind} 的说明，刷新后也应保留`)
    expect(commands).toHaveLength(1)
  })
}

test('desk draft survives reload and stays empty after a successful submission', async ({ page }) => {
  const commands = await mockWorkspace(page)
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'record', confidence: 0.99 } }))
  await page.goto('/')
  const input = page.getByLabel('导办台', { exact: true })
  await input.fill('测试草稿')
  await page.reload()
  await expect(input).toHaveValue('测试草稿')
  expect(commands).toHaveLength(0)
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '记下了，后台会整理' })).toBeVisible()
  expect(commands).toHaveLength(1)
  expect(commands[0]).toMatchObject({ type: 'capture', text: '测试草稿' })
  await expect(input).toHaveValue('')
  await page.reload()
  await expect(input).toHaveValue('')
  expect(commands).toHaveLength(1)
})

test('reload preserves an edited desk draft while the older delegation is retried', async ({ page }) => {
  const commands = await mockWorkspace(page)
  await page.goto('/')
  await expect(page.getByLabel('导办台', { exact: true })).toBeVisible()
  await page.evaluate(() => {
    localStorage.setItem('pcas.shell', JSON.stringify({ drafts: { desk: '另一个未提交的草稿' }, agents: {} }))
    sessionStorage.setItem('pcas.pending-delegations.v1', JSON.stringify([
      { question: '之前的委派', action: { type: 'delegateTask', id: 'pending-task', title: '之前的委派', prompt: '之前的委派', agentId: 'original' } },
    ]))
  })
  await page.reload()
  const input = page.getByLabel('导办台', { exact: true })
  await expect(input).toHaveValue('另一个未提交的草稿')
  await page.getByRole('button', { name: '重试这次提交' }).click()
  await expect(page.getByText('副手开始做了', { exact: true })).toBeVisible()
  expect(commands).toHaveLength(1)
  expect(commands[0]).toMatchObject({ type: 'delegateTask', id: 'pending-task', prompt: '之前的委派' })
  await expect(input).toHaveValue('另一个未提交的草稿')
  await page.reload()
  await expect(input).toHaveValue('另一个未提交的草稿')
  await expect(page.getByRole('button', { name: '重试这次提交' })).toHaveCount(0)
  expect(commands).toHaveLength(1)
})

test('reconciling a committed delegation clears its persisted desk draft without resubmitting', async ({ page }) => {
  const commands = await mockWorkspace(page)
  await page.goto('/')
  await expect(page.getByLabel('导办台', { exact: true })).toBeVisible()
  await page.evaluate(() => {
    localStorage.setItem('pcas.shell', JSON.stringify({ drafts: { desk: '之前的委派' }, agents: {} }))
    sessionStorage.setItem('pcas.pending-delegations.v1', JSON.stringify([
      { question: '之前的委派', action: { type: 'delegateTask', id: 'task', title: '之前的委派', prompt: '之前的委派', agentId: 'original' } },
    ]))
  })
  await page.reload()
  const input = page.getByLabel('导办台', { exact: true })
  await expect(page.getByRole('status').filter({ hasText: '已找到之前提交的事项' })).toBeVisible()
  await expect(input).toHaveValue('')
  await page.reload()
  await expect(input).toHaveValue('')
  expect(commands).toHaveLength(0)
})

const failures = [
  ['DAILY_BUDGET_EXCEEDED', '超过今天的额度'],
  ['provider timeout', '等太久没回应'],
  ['context deadline exceeded', '等太久没回应'],
  ['model unavailable', '这个副手现在连不上'],
  ['provider not configured', '这个副手现在连不上'],
  ['unexpected provider error', '出了点问题'],
] as const

for (const [error, reason] of failures) {
  test(`failed run explains ${error} and retries the original instruction`, async ({ page }) => {
    const run = failedRun(error)
    const snapshot = { ...workspace(), runs: [run] }
    const commands = await mockWorkspace(page, snapshot)
    await page.goto('/t/task')
    const failure = page.getByText(`没做成：${reason}`, { exact: true })
    await expect(failure).toBeVisible()
    await expect(failure).toHaveAttribute('title', error)
    await expect(page.getByRole('button', { name: /^换 .* 重试$/ })).toHaveCount(0)
    await page.getByRole('button', { name: '重试', exact: true }).click()
    await expect.poll(() => commands.length).toBe(1)
    expect(commands[0]).toMatchObject({ type: 'requestRun', thingId: run.thingId, agentId: run.agentId, kind: run.kind, prompt: run.prompt })
    expect(commands[0]).toHaveProperty('id', expect.any(String))
    expect(commands[0]).not.toMatchObject({ id: run.id })
    await expect(page.locator('.record > .card')).toHaveCount(2)
    await expect(failure).toBeVisible()
    await page.reload()
    await expect(page.locator('.record > .card')).toHaveCount(2)
    await expect(failure).toBeVisible()
    expect(commands).toHaveLength(1)
  })
}

test('failed run falls back to the output error and a generic reason when none is available', async ({ page }) => {
  const run = { ...failedRun(), error: undefined, output: 'upstream TIMEOUT' }
  const emptyError = { ...failedRun(), id: 'empty-error', error: undefined }
  await mockWorkspace(page, { ...workspace(), runs: [run, emptyError] })
  await page.goto('/t/task')
  await expect(page.getByText('没做成：等太久没回应', { exact: true })).toHaveAttribute('title', run.output)
  await expect(page.getByText('没做成：出了点问题', { exact: true })).toBeVisible()
})

test('retry can switch to the enabled assistant selected for this thing', async ({ page }) => {
  const run = failedRun('unavailable')
  const snapshot = { ...workspace(), agents: [agent(), agent('alternate', '备用副手', { default: false })], runs: [run] }
  await page.addInitScript(() => {
    localStorage.setItem('pcas.shell', JSON.stringify({ drafts: {}, agents: { task: 'alternate' } }))
  })
  const commands = await mockWorkspace(page, snapshot)
  await page.goto('/t/task')
  await expect(page.getByRole('button', { name: '重试', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '换 备用副手 重试', exact: true }).click()
  await expect.poll(() => commands.length).toBe(1)
  expect(commands[0]).toMatchObject({ type: 'requestRun', thingId: run.thingId, agentId: 'alternate', kind: run.kind, prompt: run.prompt })
  await expect(page.locator('.record > .card')).toHaveCount(2)
  await expect(page.getByText('没做成：这个副手现在连不上', { exact: true })).toBeVisible()
})

test('a disabled alternative assistant does not get a retry button', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('pcas.shell', JSON.stringify({ drafts: {}, agents: { task: 'alternate' } }))
  })
  await mockWorkspace(page, { ...workspace(), agents: [agent(), agent('alternate', '已停用副手', { enabled: false })], runs: [failedRun()] })
  await page.goto('/t/task')
  await expect(page.getByRole('button', { name: '重试', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /^换 .* 重试$/ })).toHaveCount(0)
})
