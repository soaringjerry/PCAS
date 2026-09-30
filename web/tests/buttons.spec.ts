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
  const row = page.locator('.activity > li').filter({ hasText: 'GPT' })
  await expect(row).toHaveCount(1)
  await expect(row).toContainText('已加入 3 个子任务')
  // 看看 opens the original text in place.
  await row.getByRole('button', { name: '看看' }).click()
  await expect(row.getByText('写正文')).toBeVisible()
  await row.getByRole('button', { name: '撤销' }).click()
  await expect.poll(() => backend.commands.at(-1)).toMatchObject({ type: 'undoAction', id: 'adopt-1' })
  await expect(row.getByRole('button', { name: '放回去' })).toBeVisible()
  await expect(row.getByRole('button', { name: '撤销' })).toHaveCount(0)
  // The old choices are gone.
  for (const name of ['改一下', '不要', /依据/]) await expect(page.getByRole('button', { name })).toHaveCount(0)
  await row.getByRole('button', { name: '放回去' }).click()
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
