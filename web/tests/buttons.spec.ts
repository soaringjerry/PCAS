import { test, expect, type Page } from '@playwright/test'
import type { Run, State } from '../src/domain/types'
import type { Action } from '../src/store/actions'

// Every API call stays in the browser mock; no backend or model is needed.

type Command = Action & { requestId: string; expectedRevision: number }

const HOUR = 60 * 60 * 1000

function workspace(): State {
  const at = new Date(Date.now() - 3 * 24 * HOUR).toISOString()
  const due = new Date(Date.now() + 2 * 24 * HOUR).toISOString()
  return {
    version: 1, revision: 1, budgetUsage: 0, notices: [],
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00' },
    tasks: [
      {
        id: 'task', title: '给张三回邮件', notes: '', status: 'todo', projectId: 'project', due, owedTo: { who: '张三', since: at },
        dependsOn: [], checklist: [], sources: [], createdAt: at, updatedAt: at,
        triggers: [{ id: 'due-reminder', kind: 'time', description: '给张三回邮件', nextAt: new Date(new Date(due).getTime() - 30 * 60 * 1000).toISOString(), active: true, offset: '-30m' }],
        history: [{ at, by: 'user', summary: '新建：给张三回邮件' }],
      },
      { id: 'rent', title: '交房租', notes: '', status: 'todo', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: at, updatedAt: at },
    ],
    ideas: [],
    projects: [{ id: 'project', name: 'A 项目', goal: '', status: 'active', progress: '', nextSteps: [], updatedAt: at }],
    agents: [{ id: 'model', name: 'GPT', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false }],
    memories: [], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {},
  }
}

function adoptedRun(): Run {
  const at = new Date(Date.now() - HOUR).toISOString()
  return {
    id: 'run', thingId: 'task', agentId: 'model', kind: 'breakdown', prompt: '把这件事拆成具体的子任务', brief: '', contextMemoryIds: [],
    status: 'done', output: '- [ ] 查一下报价\n- [ ] 写正文\n- [ ] 发出去', staleContext: false, cost: 0, createdAt: at, finishedAt: at,
    adopted: { as: 'subtasks', at, edited: false, actionId: 'adopt-1', auto: true },
  }
}

interface Backend {
  commands: Command[]
  dismissed: string[]
  errors: string[]
  snapshot: State
}

async function mockBackend(page: Page, snapshot = workspace()): Promise<Backend> {
  const backend: Backend = { commands: [], dismissed: [], errors: [], snapshot }
  const bump = () => (backend.snapshot = { ...backend.snapshot, revision: backend.snapshot.revision + 1 })
  page.on('pageerror', (e) => backend.errors.push(e.message))
  await page.route('**/v1/**', (route) => route.fulfill({ status: 500, json: { error: 'unexpected_mock_request' } }))
  await page.route((url) => url.pathname === '/v1/workspace', (route) => route.fulfill({ json: backend.snapshot }))
  await page.route((url) => url.pathname === '/v1/desk/turns', (route) => route.fulfill({ json: { turns: [] } }))
  await page.route((url) => url.pathname.startsWith('/v1/notify/notices/'), (route) => {
    const id = decodeURIComponent(new URL(route.request().url()).pathname.split('/')[4])
    backend.dismissed.push(id)
    backend.snapshot.notices = backend.snapshot.notices.map((n) => (n.id === id ? { ...n, dismissedAt: new Date().toISOString() } : n))
    return route.fulfill({ json: bump() })
  })
  await page.route((url) => url.pathname === '/v1/workspace/commands', (route) => {
    const body = route.request().postDataJSON() as Command
    backend.commands.push(body)
    const s = backend.snapshot
    switch (body.type) {
      case 'setTaskStatus':
        s.tasks = s.tasks.map((t) => (t.id === body.id ? { ...t, status: body.status } : t))
        break
      case 'updateDoc':
        s.docs = s.docs.map((d) => (d.id === body.id ? { ...d, ...body.patch } : d))
        break
      case 'undoAction': {
        // The adoption goes back; everything else here is a status change.
        if (body.id === 'adopt-1') s.runs = s.runs.map((r) => ({ ...r, adopted: undefined }))
        const done = backend.commands.find((c) => c.requestId === body.id)
        if (done?.type === 'setTaskStatus') s.tasks = s.tasks.map((t) => (t.id === done.id ? { ...t, status: 'todo' } : t))
        break
      }
    }
    return route.fulfill({ json: bump() })
  })
  return backend
}

const secretaryInput = (page: Page) => page.getByRole('textbox', { name: '跟秘书说' })

test('the info line is read-only; clicking it starts a sentence to the secretary', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/task')
  const info = page.locator('.info-line')
  await expect(info).toContainText('待办')
  await expect(info).toContainText('截止')
  await expect(info).toContainText('A 项目')
  await expect(info).toContainText('提醒')
  await expect(info).toContainText('张三在等你')
  // No pickers are left for status, due time or project.
  await expect(page.getByRole('button', { name: /^(状态|截止|项目)/ })).toHaveCount(0)
  // With no documents there is no 文档 heading, only the faint line that starts one.
  await expect(page.getByRole('button', { name: '写点什么…' })).toBeVisible()
  await expect(page.locator('.section-label').filter({ hasText: '文档' })).toHaveCount(0)
  await info.click()
  await expect(secretaryInput(page)).toHaveValue('改一下这件事：')
  await expect(secretaryInput(page)).toBeFocused()
  expect(backend.commands).toEqual([])
  expect(backend.errors).toEqual([])
})

test('the done circle ticks the task with a toast that undoes it', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/task')
  await page.getByRole('button', { name: '做完了', exact: true }).click()
  const toast = page.locator('.toast')
  await expect(toast).toContainText('做完了')
  await expect(page.getByRole('button', { name: '改回没做完' })).toHaveAttribute('aria-pressed', 'true')
  const done = backend.commands.find((c) => c.type === 'setTaskStatus')!
  expect(done).toMatchObject({ id: 'task', status: 'done' })
  await toast.getByRole('button', { name: '撤销' }).click()
  await expect.poll(() => backend.commands.at(-1)).toMatchObject({ type: 'undoAction', id: done.requestId })
  await expect(toast).toContainText('撤销了')
  await expect(page.getByRole('button', { name: '做完了', exact: true })).toHaveAttribute('aria-pressed', 'false')
})

test('an auto-adopted result is one line with 撤销; undone, it offers 放回去', async ({ page }) => {
  const backend = await mockBackend(page, { ...workspace(), runs: [adoptedRun()] })
  await page.goto('/t/task')
  const row = page.locator('.activity > li').filter({ hasText: '已加入 3 个子任务' })
  await expect(row).toHaveCount(1)
  // The assistant reads 副手 like 你 and 秘书; the model's name is only a tooltip.
  await expect(row.locator('.act-who')).toHaveText('副手')
  await expect(row.locator('.act-who')).toHaveAttribute('title', 'GPT')
  // 看看 opens the original text in place.
  await row.getByRole('button', { name: '看看' }).click()
  await expect(row.getByText('写正文')).toBeVisible()
  await row.getByRole('button', { name: '撤销' }).click()
  await expect.poll(() => backend.commands.at(-1)).toMatchObject({ type: 'undoAction', id: 'adopt-1' })
  const back = page.locator('.activity > li').filter({ hasText: '把这件事拆成具体的子任务' })
  await expect(back.getByRole('button', { name: '放回去' })).toBeVisible()
  await expect(back.getByRole('button', { name: '撤销' })).toHaveCount(0)
  // 看看 sits next to 放回去 and opens the original text.
  await expect(back.getByRole('button', { name: /^(看看|收起)$/ })).toBeVisible()
  // The old choices are gone.
  for (const name of ['改一下', '不要', /依据/]) await expect(page.getByRole('button', { name })).toHaveCount(0)
  await back.getByRole('button', { name: '放回去' }).click()
  await expect.poll(() => backend.commands.at(-1)).toMatchObject({ type: 'adoptRun', id: 'run', as: 'subtasks' })
  expect(backend.errors).toEqual([])
})

test('a document saves itself when it loses focus, with no edit or save buttons', async ({ page }) => {
  const at = new Date().toISOString()
  const backend = await mockBackend(page, { ...workspace(), docs: [{ id: 'doc', thingId: 'task', title: '回信草稿', body: '# 回信草稿\n\n张三你好', by: 'ai', createdAt: at, updatedAt: at }] })
  await page.goto('/t/task')
  for (const name of ['编辑', '存好', '取消', '写文档']) await expect(page.getByRole('button', { name, exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: /回信草稿/ }).first().click()
  await page.getByRole('textbox', { name: '回信草稿 的内容' }).click()
  const editor = page.getByRole('textbox', { name: '回信草稿 的内容' })
  await expect(editor).toBeFocused()
  await editor.fill('# 回信草稿\n\n张三你好，报价见附件。')
  await page.getByRole('textbox', { name: '说明', exact: true }).click()
  await expect.poll(() => backend.commands.find((c) => c.type === 'updateDoc')).toMatchObject({
    type: 'updateDoc',
    id: 'doc',
    patch: { body: '# 回信草稿\n\n张三你好，报价见附件。', title: '回信草稿' },
  })
  // Deleting sits in a small menu and comes back with 撤销 instead of a confirmation.
  await page.getByRole('button', { name: '文档「回信草稿」的更多操作' }).click()
  await page.getByRole('menuitem', { name: '删除' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.locator('.toast')).toContainText('删掉了')
  await expect(page.locator('.toast').getByRole('button', { name: '撤销' })).toBeVisible()
  expect(backend.commands.at(-1)).toMatchObject({ type: 'deleteDoc', id: 'doc' })
})

test('reminders that rang are pinned on top of 今天, and × closes one', async ({ page }) => {
  const now = Date.now()
  const snapshot = workspace()
  snapshot.notices = [
    { id: 'n-late', thingId: 'rent', title: '交房租', reason: '交房租', dueAt: new Date(now - 2 * HOUR).toISOString(), createdAt: new Date(now - 2 * HOUR).toISOString() },
    { id: 'n-early', thingId: 'task', title: '给张三回邮件', reason: '给张三回邮件', dueAt: new Date(now - 3 * HOUR).toISOString(), createdAt: new Date(now - 3 * HOUR).toISOString() },
    // Closed, or about something already done: neither is pinned.
    { id: 'n-closed', thingId: 'task', title: '早就关掉的', reason: '', dueAt: new Date(now - 30 * HOUR).toISOString(), createdAt: new Date(now - 30 * HOUR).toISOString(), dismissedAt: new Date(now - 29 * HOUR).toISOString() },
    { id: 'n-done', thingId: 'paid', title: '已经交了电费', reason: '', dueAt: new Date(now - HOUR).toISOString(), createdAt: new Date(now - HOUR).toISOString() },
  ]
  snapshot.tasks.push({ ...snapshot.tasks[1], id: 'paid', title: '已经交了电费', status: 'done' })
  const backend = await mockBackend(page, snapshot)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/')
  const today = page.locator('.hall-today')
  const rang = today.locator('.hall-rang-group')
  await expect(today.locator('.hall-group').first()).toHaveClass(/hall-rang-group/)
  await expect(rang.locator('.h-title')).toHaveText(['给张三回邮件', '交房租'])
  // A pinned task is not listed a second time below.
  await expect(today.getByText('交房租', { exact: true })).toHaveCount(1)
  await rang.getByRole('button', { name: '关掉提醒：交房租' }).click()
  await expect.poll(() => backend.dismissed).toEqual(['n-late'])
  await expect(rang.locator('.h-title')).toHaveText(['给张三回邮件'])
  expect(backend.commands).toEqual([])
  expect(backend.errors).toEqual([])
})

test('the hall has no 拿不准 entry, and the decision strip hides when nothing needs a hand', async ({ page }) => {
  const at = new Date().toISOString()
  const snapshot = workspace()
  // Pending captures and guesses belong to the observatory now.
  snapshot.candidates = [{ id: 'cand', kind: 'unknown', text: '周三的会', confidence: 0.4, source: { sourceId: 's', label: '邮件', at }, state: 'pending', createdAt: at }]
  snapshot.runs = [{ ...adoptedRun(), id: 'fresh', adopted: undefined }]
  await mockBackend(page, snapshot)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/')
  await expect(page.locator('.hall-today')).toBeVisible()
  await expect(page.getByText('拿不准')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '逐条确认' })).toHaveCount(0)
  await expect(page.locator('.hall-queue')).toHaveCount(0)
  await expect(page.getByText('没有要你拍板的事')).toHaveCount(0)

  // A result whose basis changed does need a hand, and shows up.
  await page.unrouteAll({ behavior: 'ignoreErrors' })
  snapshot.runs = [{ ...adoptedRun(), id: 'stale', adopted: undefined, staleContext: true }]
  await mockBackend(page, snapshot)
  await page.reload()
  await expect(page.locator('.hall-queue')).toContainText('1 件要你动手')
})

test("a task due earlier today stays on today's timeline, greyed before the now line", async ({ page }) => {
  const snapshot = workspace()
  const earlier = new Date()
  earlier.setHours(0, 1, 0, 0)
  // Only meaningful after the first minute of the day.
  test.skip(Date.now() - earlier.getTime() < 60_000)
  snapshot.tasks[1] = { ...snapshot.tasks[1], due: earlier.toISOString() }
  await mockBackend(page, snapshot)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/')
  const today = page.locator('.hall-today')
  const row = today.locator('.hall-task.past').filter({ hasText: '交房租' })
  await expect(row).toBeVisible()
  await expect(today.getByText('今天没有定了时间的事。')).toHaveCount(0)
  // It sits before the now line.
  const [rowBox, nowBox] = [await row.boundingBox(), await today.locator('.hall-now').boundingBox()]
  expect(rowBox!.y).toBeLessThan(nowBox!.y)
})

test('on a phone 今天 is short: at most five rows, then 还有 N 件', async ({ page }) => {
  const snapshot = workspace()
  const due = new Date(Date.now() - 26 * HOUR).toISOString()
  for (let i = 0; i < 7; i++) snapshot.tasks.push({ ...snapshot.tasks[1], id: `late-${i}`, title: `晚了的事 ${i}`, due })
  await mockBackend(page, snapshot)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  const today = page.locator('.hall-today')
  await expect(today.locator('.hall-task')).toHaveCount(5)
  const more = today.getByRole('button', { name: /^还有 \d+ 件$/ })
  await expect(more).toBeVisible()
  await more.click()
  await expect(today.locator('.hall-task')).toHaveCount(8)
})
