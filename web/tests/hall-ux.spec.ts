import { test, expect, type Page } from '@playwright/test'

function emptyState() {
  return { version: 1, revision: 1, budgetUsage: 0, settings: { dailyBudget: 10, timezone: 'UTC' }, tasks: [], ideas: [], projects: [], memories: [], candidates: [], agents: [{ id: 'model', name: 'Model', enabled: true, available: true, default: true, channel: 'api', memoryKinds: ['fact'], includeInferred: false }], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {} }
}

async function hall(page: Page, snapshot: () => unknown = emptyState) {
  await page.route('**/v1/workspace', route => route.fulfill({ json: snapshot() }))
  await page.goto('/')
  await expect(page.getByLabel('导办台', { exact: true })).toBeVisible()
}

type DelegationBody = { type: string; id: string; title: string; prompt: string; agentId: string; requestId: string; expectedRevision: number }
test('conflict recovery renews the receipt but preserves the unresolved task identity', async ({ page }) => {
  const requests: DelegationBody[] = []
  let snapshot = { ...emptyState(), revision: 2 }
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'delegate', confidence: 0.99 } }))
  await page.route('**/v1/workspace/commands', async route => {
    const body = route.request().postDataJSON() as DelegationBody
    requests.push(body)
    if (requests.length <= 2) { await route.abort('failed'); return }
    if (requests.length === 3) { await route.fulfill({ status: 409, json: { error: 'Version conflict' } }); return }
    snapshot = { ...committedState(body), revision: 3 }
    await route.fulfill({ json: snapshot })
  })
  await hall(page)
  await page.getByRole('textbox').fill('\u5e2e\u6211\u5199\u4e00\u4efd\u9879\u76ee\u8ba1\u5212')
  await page.getByRole('button', { name: '\u4ea4\u7ed9\u5979', exact: true }).click()
  const retry = page.getByRole('button', { name: '\u91cd\u8bd5\u8fd9\u6b21\u63d0\u4ea4' })
  await expect(retry).toBeVisible()
  await expect(page.getByRole('button', { name: '\u4ea4\u7ed9\u5979', exact: true })).toBeEnabled()
  await page.route('**/v1/workspace', route => route.fulfill({ json: snapshot }))
  await page.reload()
  await retry.click()
  await expect.poll(() => requests.length).toBe(3)
  await expect(retry).toBeEnabled()
  await retry.click()
  await expect(retry).toHaveCount(0)
  expect(requests).toHaveLength(4)
  expect(requests[2]).toEqual(requests[0])
  expect(requests[3].id).toBe(requests[0].id)
  expect(requests[3].requestId).not.toBe(requests[0].requestId)
  expect(requests[3].expectedRevision).toBe(2)
})

test('edits typed during a lost response remain in the input while the original submission is retryable', async ({ page }) => {
  const requests: DelegationBody[] = []
  let release!: () => void
  const held = new Promise<void>(resolve => { release = resolve })
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'delegate', confidence: 0.99 } }))
  await page.route('**/v1/workspace/commands', async route => {
    requests.push(route.request().postDataJSON() as DelegationBody)
    await held
    await route.abort('failed')
  })
  await hall(page)
  const input = page.getByRole('textbox')
  await input.fill('\u5e2e\u6211\u5199\u4e00\u4efd\u9879\u76ee\u8ba1\u5212')
  await page.getByRole('button', { name: '\u4ea4\u7ed9\u5979', exact: true }).click()
  await expect.poll(() => requests.length).toBe(1)
  await input.fill('Keep this new draft while the earlier request resolves')
  release()
  await expect(page.getByRole('button', { name: '\u4ea4\u7ed9\u5979', exact: true })).toBeEnabled()
  await expect(input).toHaveValue('Keep this new draft while the earlier request resolves')
  await expect(page.getByRole('button', { name: '\u91cd\u8bd5\u8fd9\u6b21\u63d0\u4ea4' })).toBeVisible()
  expect(requests).toHaveLength(2)
  expect(requests[1]).toEqual(requests[0])
})

function committedState(command: DelegationBody) {
  const at = new Date().toISOString()
  return { ...emptyState(), revision: 2,
    tasks: [{ id: command.id, title: command.title, notes: command.prompt, status: 'todo', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: at, updatedAt: at }],
    runs: [{ id: 'paid-run', thingId: command.id, agentId: command.agentId, kind: 'draft', prompt: command.prompt, status: 'running', contextMemoryIds: [], contextVersions: [], staleContext: false, cost: 1, createdAt: at }],
  }
}

test('committed delegation with both responses lost reconciles after reload without a new paid run', async ({ page }) => {
  const requests: DelegationBody[] = []
  let serverState: unknown = emptyState()
  let starts = 0
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'delegate', confidence: 0.99 } }))
  await page.route('**/v1/workspace/commands', async route => {
    const body = route.request().postDataJSON() as DelegationBody
    requests.push(body)
    if (requests.length === 1) { serverState = committedState(body); starts++ }
    await route.abort('failed') // Commit succeeded; both response deliveries fail.
  })
  await hall(page)
  // Even the immediate reconciliation fetch fails while the connection is down.
  await page.route('**/v1/workspace', route => route.abort('failed'))
  const question = '帮我起草项目计划'
  await page.getByLabel('导办台', { exact: true }).fill(question)
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await expect(page.getByLabel('导办台', { exact: true })).toHaveValue(question)
  await expect(page.getByRole('button', { name: '重试这次提交' })).toBeVisible()
  expect(requests).toHaveLength(2)
  expect(requests[1]).toEqual(requests[0])
  await page.route('**/v1/workspace', route => route.fulfill({ json: serverState }))
  await page.reload()
  await expect(page.getByRole('status').filter({ hasText: '已找到之前提交的事项' })).toBeVisible()
  await expect(page.getByLabel('导办台', { exact: true })).toHaveValue('')
  await expect(page.getByRole('button', { name: '重试这次提交' })).toHaveCount(0)
  expect(requests).toHaveLength(2)
  expect(starts).toBe(1)
})

test('reload retry keeps original task and receipt while an earlier transaction is still committing', async ({ page }) => {
  const requests: DelegationBody[] = []
  let starts = 0
  let serverState: unknown = emptyState()
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'delegate', confidence: 0.99 } }))
  await page.route('**/v1/workspace/commands', async route => {
    const body = route.request().postDataJSON() as DelegationBody
    requests.push(body)
    if (requests.length <= 2) { await route.abort('failed'); return }
    // The first transaction commits before this retry obtains the owner lock.
    // The existing receipt returns its snapshot; it never starts a second run.
    expect(body).toEqual(requests[0])
    starts++
    serverState = committedState(requests[0])
    await route.fulfill({ json: serverState })
  })
  await hall(page, () => serverState)
  await page.getByLabel('导办台', { exact: true }).fill('帮我起草项目计划')
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await expect(page.getByRole('button', { name: '重试这次提交' })).toBeVisible()
  await expect(page.getByRole('button', { name: '交给她', exact: true })).toBeEnabled()
  expect(requests).toHaveLength(2)
  await page.reload()
  await expect(page.getByLabel('导办台', { exact: true })).toHaveValue('帮我起草项目计划')
  await page.getByRole('button', { name: '重试这次提交' }).click()
  await expect(page.getByText('副手开始做了', { exact: true })).toBeVisible()
  expect(requests).toHaveLength(3)
  expect(requests[2]).toEqual(requests[0])
  expect(starts).toBe(1)
})

test('edited pending instruction and a deliberate repeat after success receive new task identities', async ({ page }) => {
  const requests: DelegationBody[] = []
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'delegate', confidence: 0.99 } }))
  await page.route('**/v1/workspace/commands', async route => {
    requests.push(route.request().postDataJSON() as DelegationBody)
    if (requests.length <= 2) { await route.abort('failed'); return }
    await route.fulfill({ json: { ...emptyState(), revision: requests.length } })
  })
  await hall(page)
  await page.getByLabel('导办台', { exact: true }).fill('帮我起草项目计划')
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await expect(page.getByRole('button', { name: '交给她', exact: true })).toBeEnabled()
  await page.getByLabel('导办台', { exact: true }).fill('帮我起草另一个项目计划')
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await expect(page.getByText('副手开始做了', { exact: true })).toBeVisible()
  expect(requests[2].id).not.toBe(requests[0].id)
  await page.getByLabel('导办台', { exact: true }).fill('帮我起草另一个项目计划')
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await expect.poll(() => requests.length).toBe(4)
  expect(requests[3].id).not.toBe(requests[2].id)
  // The original unresolved submission remains available for its own retry.
  await expect(page.getByRole('button', { name: '重试这次提交' })).toBeVisible()
})

test('receipt persistence failure prevents submitting a paid delegation', async ({ page }) => {
  let commands = 0
  await page.addInitScript(() => {
    const original = Storage.prototype.setItem
    let writes = 0
    Storage.prototype.setItem = function (key, value) {
      if (key === 'pcas.pending-delegations.v1' && ++writes === 2) throw new DOMException('Storage unavailable', 'QuotaExceededError')
      return original.call(this, key, value)
    }
  })
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'delegate', confidence: 0.99 } }))
  await page.route('**/v1/workspace/commands', route => { commands++; return route.fulfill({ json: emptyState() }) })
  await hall(page)
  await page.getByLabel('导办台', { exact: true }).fill('帮我起草项目计划')
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await expect(page.getByRole('alert').filter({ hasText: '浏览器无法保存提交状态' })).toBeVisible()
  await expect(page.getByRole('button', { name: '交给她', exact: true })).toBeEnabled()
  expect(commands).toBe(0)
})

test('clear delegation starts once and reload does not submit again', async ({ page }) => {
  let starts = 0
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'delegate', confidence: 0.99 } }))
  await page.route('**/v1/workspace/commands', async route => {
    expect(route.request().postDataJSON().type).toBe('delegateTask')
    starts++
    await route.fulfill({ json: { ...emptyState(), revision: 2 } })
  })
  await hall(page)
  await page.getByLabel('导办台', { exact: true }).fill('帮我起草项目计划')
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await expect(page.getByText('副手开始做了', { exact: true })).toBeVisible()
  expect(starts).toBe(1)
  await expect(page.getByRole('group', { name: '这句要怎么处理' })).toHaveCount(0)
  await page.reload()
  await expect(page.getByLabel('导办台', { exact: true })).toBeVisible()
  expect(starts).toBe(1)
})

test('ambiguous discussion does not become a task even if routing overstates certainty', async ({ page }) => {
  let commands = 0
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'delegate', confidence: 0.99 } }))
  await page.route('**/v1/workspace/commands', route => { commands++; return route.fulfill({ json: emptyState() }) })
  await hall(page)
  await page.getByLabel('导办台', { exact: true }).fill('帮我考虑一下是否要换工作')
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await expect(page.getByRole('group', { name: '这句要怎么处理' })).toBeVisible()
  expect(commands).toBe(0)
})

test('consecutive ask edit reuse submits server turn IDs', async ({ page }) => {
  const questions: { question: string; history: { id: string; a: string }[] }[] = []
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'ask', confidence: 0.99 } }))
  await page.route('**/v1/desk/answer', route => {
    questions.push(route.request().postDataJSON())
    return route.fulfill({ json: { id: `turn-${questions.length}`, answer: `Plan revision ${questions.length}`, agent: 'Model', used: [], links: [], searches: [] } })
  })
  await hall(page)
  for (const [i, text] of ['Write a plan?', 'Edit paragraph two', 'Use the previous plan'].entries()) {
    await page.getByLabel('导办台', { exact: true }).fill(text)
    await page.getByRole('button', { name: i === 0 ? '交给她' : '接着问', exact: true }).click()
    await expect(page.getByText(`Plan revision ${i + 1}`, { exact: true })).toBeVisible()
  }
  expect(questions[1].history).toEqual([{ id: 'turn-1', q: 'Write a plan?', a: '' }])
  expect(questions[2].history.map(t => t.id)).toEqual(['turn-1', 'turn-2'])
})
