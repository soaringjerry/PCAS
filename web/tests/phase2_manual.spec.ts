import { expect, test, type Page } from '@playwright/test'
import type { Agent, ContextRecipient, ManualRunPackage, State } from '../src/domain/types'

// Independent contract fixtures: every identifier and text is synthetic.
const at = '2026-10-01T08:00:00Z'
const thingId = 'phase2-synthetic-thing'
const runId = 'phase2-synthetic-run'
const prompt = '原用户请求：请写一份自然的合成邀请'
const ownerText = '独立 owner 原文：先保留我的原话，再整理邀请'
const notes = '原事项说明：合成读书会，只用于验收'
const recipient: ContextRecipient = { provider: 'writer', principal_id: 'manual', role: 'manual', channel: 'manual', protocol: 'openai', model: 'synthetic-model', route_fingerprint: 'synthetic-route-A' }
const makeAgent = (id: string, patch: Partial<Agent> = {}): Agent => ({ id, name: `友好接收者 ${id}`, channel: 'api', enabled: true, available: true, note: '', memoryKinds: ['fact'], includeInferred: false, inputPrice: 0, outputPrice: 0, maxOutput: 100, ...patch })
function workspace(): State {
  return {
    version: 1, revision: 10, budgetUsage: 0, settings: { timezone: 'UTC', dailyBudget: 10, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00' },
    tasks: [{ id: thingId, title: '合成读书会验收', notes, status: 'todo', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: at, updatedAt: at }],
    agents: [makeAgent('writer'), makeAgent('other'), makeAgent('disabled', { enabled: false }), makeAgent('unavailable', { available: false }), makeAgent('embed'), makeAgent('transcribe'), makeAgent('unsupported'), makeAgent('empty-model'), makeAgent('catalog-unavailable'), makeAgent('manual', { channel: 'manual', name: '手动交接' })],
    runs: [{ id: runId, thingId, agentId: 'manual', kind: 'draft', prompt, brief: 'BRIEF_MUST_NEVER_BECOME_HANDOFF', contextMemoryIds: [], manualRecipient: { ...recipient }, contextTask: { recipient: { ...recipient } }, status: 'waiting', staleContext: false, cost: 0, createdAt: at }],
    ideas: [], projects: [], memories: [], candidates: [], sources: [], jobs: [], activity: [], docs: [], samples: [], notices: [], excludedMemories: {},
  }
}
const providers = [
  { id: 'writer' }, { id: 'other', protocol: 'responses' }, { id: 'disabled' }, { id: 'unavailable' }, { id: 'embed', embedding: true }, { id: 'transcribe', transcription: true }, { id: 'unsupported', protocol: 'synthetic-unsupported' }, { id: 'empty-model', model: '' }, { id: 'catalog-only' }, { id: 'catalog-unavailable', available: false },
].map(p => ({ name: `友好接收者 ${p.id}`, protocol: 'openai', model: 'synthetic-model', available: true, ...p }))

type Command = { type: string; id: string; thingId?: string; agentId?: string; kind?: string; prompt?: string; manualRecipient?: object; expectedRevision: number; requestId: string; output?: string }
async function mock(page: Page, initial = workspace()) {
  const backend = {
    state: structuredClone(initial), commands: [] as Command[], packages: [] as string[], errors: [] as string[], unexpected: [] as string[], workspaceReads: 0,
    rejectPackage: false, rejectPaste: false, rejectWorkspace: false,
    responsePatch: undefined as ((pkg: ManualRunPackage) => void) | undefined,
    waitPackage: undefined as Promise<void> | undefined,
    hold() { let release!: () => void; this.waitPackage = new Promise<void>(resolve => { release = resolve }); return release },
  }
  page.on('pageerror', e => backend.errors.push(e.message))
  await page.addInitScript(({ thingId }) => {
    localStorage.setItem(`pcas.secretary.${thingId}`, JSON.stringify({ conversationId: 'synthetic-owner-conversation', unanswered: [] }))
    const calls: { path: string; cache?: string; method?: string }[] = [], copied: string[] = []
    Object.assign(window, { phase2Fetches: calls, phase2Copied: copied })
    const original = window.fetch.bind(window)
    window.fetch = (input, init) => { calls.push({ path: String(input), cache: init?.cache, method: init?.method }); return original(input, init) }
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (text: string) => { copied.push(text) } } })
  }, { thingId })
  await page.route('**/v1/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (path === '/v1/workspace') {
      backend.workspaceReads++
      return backend.rejectWorkspace ? route.fulfill({ status: 401, json: { error: 'unauthorized' } }) : route.fulfill({ json: backend.state })
    }
    if (path === '/v1/models') return route.fulfill({ json: { providers } })
    if (path === '/v1/desk/turns') return route.fulfill({ json: { conversationId: 'synthetic-owner-conversation', turns: [{ id: 'synthetic-owner-turn', text: ownerText, reply: '已保留合成原文', cards: [], receipts: [], ask: null, agent: '合成秘书', createdAt: at }] } })
    if (path === `/v1/workspace/runs/${runId}/package`) {
      const number = backend.packages.length + 1
      const pkg: ManualRunPackage = { run_id: runId, package: `FRESH_PACKAGE_${number}\n${prompt}\n合成资料内容`, attempt: { id: `synthetic-attempt-${number}`, delivered_at: at, external_receipt: 'unknown', manifest: { recipient: structuredClone(recipient) } } }
      backend.responsePatch?.(pkg)
      backend.packages.push(pkg.package)
      const wait = backend.waitPackage; backend.waitPackage = undefined
      if (wait) await wait
      if (backend.rejectPackage) return route.fulfill({ status: 409, json: { error: 'context_changed' } })
      return route.fulfill({ json: pkg })
    }
    if (path === '/v1/workspace/commands') {
      const body = request.postDataJSON() as Command
      backend.commands.push(body)
      expect(body.expectedRevision).toBe(backend.state.revision)
      if (body.type === 'pasteRunResult' && backend.rejectPaste) return route.fulfill({ status: 400, json: { error: 'context_changed' } })
      expect(body.type).toBe('requestRun')
      backend.state = { ...backend.state, revision: backend.state.revision + 1 }
      return route.fulfill({ json: backend.state })
    }
    backend.unexpected.push(`${request.method()} ${path}`)
    return route.fulfill({ status: 500, json: { error: 'unexpected_synthetic_request' } })
  })
  return backend
}
const preview = (page: Page) => page.getByRole('button', { name: '预览交接内容', exact: true })
const copy = (page: Page) => page.getByRole('button', { name: '复制给它的内容', exact: true })
const handoff = (page: Page) => page.locator('li.act-run').filter({ has: page.getByText(`等你转交：${prompt}`, { exact: true }) })
const shownPackage = (page: Page) => page.locator('pre.act-brief')
async function copied(page: Page): Promise<string[]> { return page.evaluate(() => (window as unknown as { phase2Copied: string[] }).phase2Copied) }
async function open(page: Page) { await page.goto(`/t/${thingId}`); await expect(preview(page)).toBeVisible(); await expect(page.getByText(ownerText, { exact: true })).toBeVisible() }
async function assertClean(page: Page, backend: Awaited<ReturnType<typeof mock>>) {
  expect(backend.errors).toEqual([]); expect(backend.unexpected).toEqual([])
  await expect(page.getByText(ownerText, { exact: true })).toBeVisible()
  expect(backend.state.tasks[0].notes).toBe(notes)
  await expect(page.getByText('BRIEF_MUST_NEVER_BECOME_HANDOFF', { exact: false })).toHaveCount(0)
}

test('filters recipients and sends only provider with a new natural request', async ({ page }) => {
  const backend = await mock(page); await open(page)
  await page.getByRole('button', { name: '手动转交', exact: true }).click()
  const form = page.locator('form').filter({ has: page.getByRole('textbox', { name: '本次转交请求' }) })
  await form.getByRole('textbox', { name: '本次转交请求' }).fill('新的自然请求：请写亲切一点')
  await expect(form.getByRole('button', { name: '建立转交请求' })).toBeDisabled()
  await form.getByRole('button', { name: '转交接收者' }).click()
  await expect(page.getByRole('option')).toHaveCount(2)
  await expect(page.getByRole('option', { name: /友好接收者 writer/ })).toBeVisible()
  await page.getByRole('option', { name: /友好接收者 other/ }).click()
  await form.getByRole('button', { name: '建立转交请求' }).click()
  await expect.poll(() => backend.commands.length).toBe(1)
  expect(backend.commands[0]).toMatchObject({ type: 'requestRun', agentId: 'manual', thingId, kind: 'ask', prompt: '新的自然请求：请写亲切一点', manualRecipient: { provider: 'other' } })
  expect(Object.keys(backend.commands[0].manualRecipient!)).toEqual(['provider'])
  expect(backend.commands[0].id).not.toBe(runId)
  expect(JSON.stringify(backend.commands[0])).not.toMatch(/fingerprint|scope/)
  expect(backend.state.runs[0].prompt).toBe(prompt)
  await assertClean(page, backend)
})

test('every preview and copy obtains a fresh no-store GET; external receipt stays unknown', async ({ page }) => {
  const backend = await mock(page); await open(page)
  await preview(page).click(); await expect(shownPackage(page)).toContainText('FRESH_PACKAGE_1'); await expect(shownPackage(page)).toHaveText(backend.packages[0])
  await expect(handoff(page)).toContainText(/无法确认.*(?:收到|接收)|外部接收.*未知/)
  await page.getByRole('button', { name: '收起', exact: true }).click()
  await preview(page).click(); await expect(shownPackage(page)).toContainText('FRESH_PACKAGE_2'); await expect(shownPackage(page)).toHaveText(backend.packages[1])
  await copy(page).click(); await expect.poll(() => backend.packages.length).toBe(3); await expect.poll(() => copied(page)).toEqual([backend.packages[2]])
  await copy(page).click(); await expect.poll(() => backend.packages.length).toBe(4); await expect.poll(() => copied(page)).toEqual([backend.packages[2], backend.packages[3]])
  expect(backend.packages).toHaveLength(4)
  const requests = await page.evaluate(() => (window as unknown as { phase2Fetches: { path: string; cache?: string; method?: string }[] }).phase2Fetches.filter(f => f.path.endsWith('/package')))
  expect(requests).toHaveLength(4)
  for (const request of requests) expect(request).toMatchObject({ method: 'GET', cache: 'no-store' })
  await expect(handoff(page)).not.toContainText(/对方已收到|外部已收到|接收者已收到/)
  await assertClean(page, backend)
})

const changes = ['revision', 'stale', 'route', 'status', 'authorization'] as const
function change(backend: Awaited<ReturnType<typeof mock>>, mode: typeof changes[number]) {
  backend.state = structuredClone(backend.state)
  if (mode === 'revision' || mode === 'authorization') backend.state.revision++
  if (mode === 'stale') backend.state.runs[0].staleContext = true
  if (mode === 'route') backend.state.runs[0].contextTask!.recipient.route_fingerprint = 'synthetic-route-B'
  if (mode === 'status') backend.state.runs[0].status = 'done'
  if (mode === 'authorization') backend.state.agents[0].enabled = false
  backend.state.tasks[0].title = `已同步变化 ${mode}`
}
async function waitChange(page: Page, mode: string) { await expect(page.getByRole('textbox', { name: '标题', exact: true })).toHaveValue(`已同步变化 ${mode}`, { timeout: 7000 }) }
for (const mode of changes) {
  test(`existing preview clears after ${mode} changes`, async ({ page }) => {
    const backend = await mock(page); await open(page); await preview(page).click(); await expect(shownPackage(page)).toBeVisible()
    change(backend, mode); await waitChange(page, mode)
    await expect(shownPackage(page)).toHaveCount(0); expect(await copied(page)).toEqual([])
    await assertClean(page, backend)
  })
  for (const operation of ['preview', 'copy'] as const) test(`late ${operation} package is rejected after ${mode} changes`, async ({ page }) => {
    const backend = await mock(page); await open(page); const release = backend.hold()
    const response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/package'))
    try {
      await (operation === 'preview' ? preview(page) : copy(page)).click(); await expect.poll(() => backend.packages.length).toBe(1)
      change(backend, mode); await waitChange(page, mode)
    } finally { release() }
    await (await response).finished()
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
    if (mode !== 'stale' && mode !== 'status') await expect(copy(page)).toBeEnabled()
    await expect(shownPackage(page)).toHaveCount(0); expect(await copied(page)).toEqual([])
    await assertClean(page, backend)
  })
}

for (const malformed of ['wrong run', 'undelivered', 'wrong manifest provider', 'wrong manifest route'] as const) test(`rejects ${malformed} package`, async ({ page }) => {
  const backend = await mock(page)
  backend.responsePatch = pkg => {
    if (malformed === 'wrong run') pkg.run_id = 'another-run'
    if (malformed === 'undelivered') delete pkg.attempt.delivered_at
    if (malformed === 'wrong manifest provider') pkg.attempt.manifest.recipient.provider = 'other'
    if (malformed === 'wrong manifest route') pkg.attempt.manifest.recipient.route_fingerprint = 'synthetic-mismatched-route'
  }
  await open(page); await preview(page).click(); await expect(copy(page)).toBeEnabled()
  await expect(shownPackage(page)).toHaveCount(0)
  await expect(page.getByRole('button', { name: '向原接收者重新生成' })).toBeVisible()
  expect(await copied(page)).toEqual([]); await assertClean(page, backend)
})

test('package failure clears old preview and regenerates with the complete original recipient', async ({ page }) => {
  const backend = await mock(page); await open(page); await preview(page).click(); await expect(shownPackage(page)).toBeVisible()
  backend.rejectPackage = true; await copy(page).click()
  await expect(shownPackage(page)).toHaveCount(0)
  await page.getByRole('button', { name: '向原接收者重新生成' }).click()
  await expect.poll(() => backend.commands.length).toBe(1)
  expect(backend.commands[0]).toMatchObject({ type: 'requestRun', prompt, kind: 'draft', manualRecipient: recipient })
  expect(backend.commands[0].id).not.toBe(runId); expect(await copied(page)).toEqual([])
  await assertClean(page, backend)
})

test('paste rejection clears preview and offers regeneration without claiming save', async ({ page }) => {
  const backend = await mock(page); backend.rejectPaste = true; await open(page)
  await preview(page).click(); await expect(shownPackage(page)).toBeVisible()
  await page.getByRole('textbox', { name: '贴回回答' }).fill('合成外部回答')
  await page.getByRole('button', { name: '保存并加入这件事' }).click()
  await expect(shownPackage(page)).toHaveCount(0)
  await expect(handoff(page)).toContainText('回答没有保存或加入这件事')
  await expect(page.getByRole('button', { name: '向原接收者重新生成' })).toBeVisible()
  expect(backend.commands).toHaveLength(1); expect(backend.commands[0]).toMatchObject({ type: 'pasteRunResult', id: runId, output: '合成外部回答' })
  await assertClean(page, backend)
})

test('failed manual run retries with complete original recipient and unchanged prompt', async ({ page }) => {
  const state = workspace(); state.runs[0].status = 'failed'; state.runs[0].error = 'synthetic-failure'
  const backend = await mock(page, state); await page.goto(`/t/${thingId}`)
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect.poll(() => backend.commands.length).toBe(1)
  expect(backend.commands[0]).toMatchObject({ type: 'requestRun', agentId: 'manual', prompt, kind: 'draft', manualRecipient: recipient })
  expect(backend.commands[0].id).not.toBe(runId); await assertClean(page, backend)
})

test('changing recipient creates a new run and preserves the old run and owner input', async ({ page }) => {
  const backend = await mock(page); const old = structuredClone(backend.state.runs[0]); await open(page)
  await page.getByRole('button', { name: '换接收者，建立新请求' }).click()
  await expect(page.getByRole('textbox', { name: '本次转交请求' })).toHaveValue(prompt)
  await page.getByRole('button', { name: '转交接收者' }).click(); await page.getByRole('option', { name: /友好接收者 other/ }).click()
  await page.getByRole('button', { name: '建立转交请求' }).click(); await expect.poll(() => backend.commands.length).toBe(1)
  expect(backend.commands[0]).toMatchObject({ type: 'requestRun', prompt, kind: 'draft', manualRecipient: { provider: 'other' } })
  expect(backend.commands[0].id).not.toBe(runId); expect(backend.state.runs[0]).toEqual(old); await assertClean(page, backend)
})

test('desktop and 390px browser views preserve content without horizontal overflow', async ({ page }, info) => {
  const backend = await mock(page); await page.setViewportSize({ width: 1280, height: 1000 }); await open(page)
  await preview(page).click(); await expect(shownPackage(page)).toBeVisible()
  await page.screenshot({ path: info.outputPath('phase2-manual-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 390, height: 1000 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)
  await page.screenshot({ path: info.outputPath('phase2-manual-390px.png'), fullPage: true })
  await page.getByRole('button', { name: '换接收者，建立新请求' }).click()
  await page.getByRole('button', { name: '转交接收者' }).click(); await page.getByRole('option', { name: /友好接收者 other/ }).click()
  await expect(page.getByRole('textbox', { name: '本次转交请求' })).toHaveValue(prompt)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)
  await page.screenshot({ path: info.outputPath('phase2-manual-selection-390px.png'), fullPage: true })
  await expect(page.getByRole('button', { name: '建立转交请求' })).toBeEnabled(); await assertClean(page, backend)
})
