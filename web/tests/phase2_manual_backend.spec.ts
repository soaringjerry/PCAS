import { test, expect, type Page, type Locator, type TestInfo } from '@playwright/test'
import type { ContextRecipient, ManualRunPackage, Run, State } from '../src/domain/types'
import { command, events, evidence, fixture, fixtureURL, login, snapshot } from './support/real'

type SourceRef = { id: string; version: number; kind: 'source' }
type Policy = { id: string; revision: number; recipient: ContextRecipient; purpose: 'knowledge'; scope: { kind: 'unscoped'; include_global_constraints: boolean }; revoked: boolean }
type Package = ManualRunPackage & { attempt: { manifest: { input: { ref: SourceRef }[] } } }
type HTTPRecord = { method: string; path: string; request?: unknown; status?: number; response?: unknown }
const ownerText = 'OWNER 合成独立说明 cobalt-602，继续保留这段原文。'
const target = { id: 'golden', name: '验收假模型' }

function requireSyntheticEnvironment() {
  for (const [name, value] of [['PCAS_TEST_BASE_URL', process.env.PCAS_TEST_BASE_URL], ['PCAS_TEST_FIXTURE_URL', fixtureURL]]) {
    expect(value, `${name} must be explicit; no skip`).toBeTruthy()
    const url = new URL(value!)
    expect(url.protocol).toBe('http:')
    expect(['127.0.0.1', 'localhost', '[::1]']).toContain(url.hostname)
    expect(url.username + url.password).toBe('')
  }
  expect(process.env.PCAS_TEST_API_TOKEN).toMatch(/^pcas-golden-test-only-/)
}

// Read-only browser request/response observation; never intercepts an API or
// records the test token, authorization header, cookie, or private configuration.
function observeHTTP(page: Page) {
  const records: HTTPRecord[] = []
  const pending: Promise<void>[] = []
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  page.on('response', response => {
    const path = new URL(response.url()).pathname
    if (!path.startsWith('/v1/workspace') && !path.startsWith('/v1/memory/sources') && path !== '/v1/models') return
    pending.push((async () => {
      const request = response.request()
      const record: HTTPRecord = { method: request.method(), path, status: response.status() }
      if (request.postData()) record.request = request.postDataJSON() as unknown
      try { record.response = await response.json() }
      catch { record.response = { observation: 'body_unavailable_after_navigation' } }
      records.push(record)
    })())
  })
  return {
    records,
    async attach(info: TestInfo) {
      const settled = await Promise.allSettled(pending)
      expect(settled.filter(result => result.status === 'rejected'), 'passive response capture must complete').toEqual([])
      await info.attach('real-http', { body: JSON.stringify(records, null, 2), contentType: 'application/json' })
      expect(errors).toEqual([])
    },
  }
}

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 1440, height: 1000 } })
test.beforeEach(async ({ page, context }) => {
  requireSyntheticEnvironment()
  await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: process.env.PCAS_TEST_BASE_URL! })
  await login(page)
  await fixture(page)
})

async function setup(page: Page) {
  const suffix = crypto.randomUUID()
  const name = `合成海棠交接资料-${suffix}`
  const marker = `RAW_BACKEND_HANDOFF_${suffix}`
  const prompt = `根据${name}和这件事的独立说明，给出一段建议。`
  const sourceResponse = await page.request.post('/v1/memory/sources', { data: { connector: 'phase2-browser-synthetic', external_id: suffix, external_version: 'v1', title: name, text: `${name}：合成来源 atom ${marker}。`, media_type: 'text/plain' } })
  expect(sourceResponse.status()).toBe(201)
  const ingested = await sourceResponse.json() as SourceRef
  const source: SourceRef = { id: ingested.id, version: ingested.version, kind: ingested.kind }
  expect(source).toMatchObject({ kind: 'source', version: 1 })
  const state = await command(page, { type: 'addTask', title: `独立手动交接-${suffix}` })
  const task = state.tasks.find(item => item.title === `独立手动交接-${suffix}`)!
  expect(task).toBeTruthy()
  await command(page, { type: 'setNotes', id: task.id, text: ownerText })
  const grantResponse = await page.request.post(`/v1/memory/sources/${source.id}/authorization`, { data: { request_id: crypto.randomUUID(), source, expected_policy_revision: 0, recipient: { principal_id: 'manual', role: 'manual', provider: target.id }, purpose: 'knowledge', scope: { kind: 'unscoped', include_global_constraints: false } } })
  expect(grantResponse.status()).toBe(200)
  const grant = await grantResponse.json() as { authorization: Policy }
  expect(grant.authorization).toMatchObject({ revision: 1, revoked: false, recipient: { principal_id: 'manual', role: 'manual', provider: target.id, model: 'golden', protocol: 'openai', channel: 'manual' } })
  expect(grant.authorization.recipient.route_fingerprint).toMatch(/^[a-f0-9]{64}$/)
  // The real worker may extract the synthetic source using the existing golden
  // items=[] response. Wait for those writes; no timer or model response is stubbed.
  await expect.poll(async () => (await snapshot(page)).jobs.filter(job => job.detail.includes(name) && (job.status === 'queued' || job.status === 'running')).length, { timeout: 20000 }).toBe(0)
  await page.goto(`/t/${task.id}`)
  await expect(page.getByText(ownerText, { exact: true })).toBeVisible()
  return { source, policy: grant.authorization, taskId: task.id, marker, prompt }
}

function handoffRows(page: Page, prompt: string) {
  return page.locator('.activity > li').filter({ has: page.getByRole('textbox', { name: '贴回回答' }) }).filter({ hasText: prompt })
}

function runRow(page: Page, prompt: string) {
  return handoffRows(page, prompt).filter({ has: page.getByRole('button', { name: '复制给它的内容', exact: true, disabled: false }) })
}

async function createThroughUI(page: Page, taskId: string, prompt: string) {
  const catalogPromise = page.waitForResponse(response => new URL(response.url()).pathname === '/v1/models' && response.request().method() === 'GET')
  await page.getByRole('button', { name: '手动转交', exact: true }).click()
  const form = page.locator('form').filter({ has: page.getByRole('textbox', { name: '本次转交请求' }) })
  await form.getByRole('textbox', { name: '本次转交请求' }).fill(prompt)
  await expect(form.getByRole('button', { name: '建立转交请求', exact: true })).toBeDisabled()
  const catalog = await catalogPromise
  expect(catalog.status()).toBe(200)
  const model = (await catalog.json() as { providers: { id: string; name: string; available: boolean }[] }).providers.find(provider => provider.id === target.id)
  expect(model).toMatchObject({ id: target.id, name: target.name, available: true })
  await form.getByRole('button', { name: '转交接收者：选择接收者', exact: true }).click()
  await page.getByRole('option', { name: new RegExp(target.name) }).click()
  const responsePromise = page.waitForResponse(response => new URL(response.url()).pathname === '/v1/workspace/commands' && response.request().postDataJSON()?.type === 'requestRun')
  await form.getByRole('button', { name: '建立转交请求', exact: true }).click()
  const response = await responsePromise
  expect(response.status()).toBe(200)
  const body = response.request().postDataJSON() as { id: string; type: string; thingId: string; agentId: string; prompt: string; kind: string; manualRecipient: Record<string, unknown> }
  expect(body).toMatchObject({ type: 'requestRun', thingId: taskId, agentId: 'manual', kind: 'ask', prompt, manualRecipient: { provider: target.id } })
  expect(Object.keys(body.manualRecipient)).toEqual(['provider'])
  expect(body).not.toHaveProperty('scope')
  expect(body).not.toHaveProperty('fingerprint')
  const state = await response.json() as State
  const run = state.runs.find(candidate => candidate.id === body.id)!
  expect(run).toMatchObject({ status: 'waiting', staleContext: false, prompt, contextTask: { recipient: { role: 'manual', provider: target.id } } })
  expect(run.contextTask!.recipient.route_fingerprint).toMatch(/^[a-f0-9]{64}$/)
  await expect(runRow(page, prompt).getByRole('button', { name: '预览交接内容', exact: true })).toBeVisible()
  return run
}

function assertPackage(pkg: Package, run: Run, source: SourceRef, marker: string, allowed: boolean) {
  expect(pkg.run_id).toBe(run.id)
  expect(pkg.attempt.id).toMatch(/^[a-f0-9-]{36}$/)
  expect(pkg.attempt.delivered_at).toBeTruthy()
  expect(pkg.attempt.external_receipt).toBe('unknown')
  expect(pkg.attempt.manifest.recipient).toEqual(run.contextTask!.recipient)
  expect(pkg.package).toContain(ownerText)
  expect(pkg.package.includes(marker)).toBe(allowed)
  expect(pkg.attempt.manifest.input.some(input => input.ref.id === source.id && input.ref.version === source.version && input.ref.kind === source.kind)).toBe(allowed)
  expect(run.brief).not.toContain(marker)
}

async function obtain(page: Page, row: Locator, run: Run, copy: boolean) {
  const responsePromise = page.waitForResponse(response => new URL(response.url()).pathname === `/v1/workspace/runs/${run.id}/package` && response.request().method() === 'GET')
  await row.getByRole('button', { name: copy ? '复制给它的内容' : '预览交接内容', exact: true }).click()
  const response = await responsePromise
  expect(response.status()).toBe(200)
  const pkg = await response.json() as Package
  if (copy) {
    await expect(row.getByRole('status')).toContainText('已复制，可以粘贴给')
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(pkg.package)
  } else await expect(row.locator('pre.act-brief')).toHaveText(pkg.package)
  await expect(row).toContainText('PCAS 无法确认对方是否收到')
  return pkg
}

async function submit(page: Page, row: Locator, run: Run, text: string) {
  await row.getByRole('textbox', { name: '贴回回答' }).fill(text)
  const responsePromise = page.waitForResponse(response => new URL(response.url()).pathname === '/v1/workspace/commands' && response.request().postDataJSON()?.type === 'pasteRunResult' && response.request().postDataJSON()?.id === run.id)
  await row.getByRole('button', { name: '保存并加入这件事', exact: true }).click()
  const response = await responsePromise
  expect(response.status()).toBe(200)
  expect(response.request().postDataJSON()).toMatchObject({ type: 'pasteRunResult', id: run.id, output: text })
  const state = await snapshot(page)
  expect(state.runs.find(candidate => candidate.id === run.id)).toMatchObject({ status: 'done', output: text, adopted: { auto: true, as: 'doc' } })
  expect(state.docs.filter(doc => doc.runId === run.id)).toHaveLength(1)
  expect(state.docs.find(doc => doc.runId === run.id)!.body).toBe(text)
  expect(state.tasks.find(task => task.id === run.thingId)!.notes).toBe(ownerText)
  await expect(page.getByText(ownerText, { exact: true })).toBeVisible()
  return state
}

test('real_target_package_clipboard_submit', async ({ page }, info) => {
  const http = observeHTTP(page)
  try {
    const data = await setup(page)
    const run = await createThroughUI(page, data.taskId, data.prompt)
    const row = runRow(page, data.prompt)
    const preview = await obtain(page, row, run, false)
    assertPackage(preview, run, data.source, data.marker, true)
    const copied = await obtain(page, row, run, true)
    assertPackage(copied, run, data.source, data.marker, true)
    expect(copied.attempt.id).not.toBe(preview.attempt.id)
    await submit(page, row, run, '合成贴回回答：请保留独立原文，按一个普通步骤继续。')
    expect((await events(page)).filter(event => event.kind === 'model' && event.role === 'assistant')).toEqual([])
    await info.attach('two-real-packages', { body: JSON.stringify({ syntheticData: data, preview, copied, originalRun: run }, null, 2), contentType: 'application/json' })
    await evidence(page, info, 'manual-real-completed')
  } finally { await http.attach(info) }
})

test('real_revoke_clear_refuse_regenerate_submit', async ({ page }, info) => {
  const http = observeHTTP(page)
  try {
    const data = await setup(page)
    const run = await createThroughUI(page, data.taskId, data.prompt)
    await expect(handoffRows(page, data.prompt)).toHaveCount(1)
    const oldRow = handoffRows(page, data.prompt).first()
    const lawful = await obtain(page, oldRow, run, false)
    assertPackage(lawful, run, data.source, data.marker, true)
    const clipboardSentinel = 'OWNER synthetic clipboard sentinel before revocation'
    await page.evaluate(text => navigator.clipboard.writeText(text), clipboardSentinel)
    await oldRow.getByRole('textbox', { name: '贴回回答' }).fill('这份旧回答不得保存')
    const revoke = await page.request.delete(`/v1/memory/sources/${data.source.id}/authorization`, { data: { request_id: crypto.randomUUID(), source: data.source, policy_id: data.policy.id, expected_policy_revision: data.policy.revision, recipient: data.policy.recipient, purpose: data.policy.purpose, scope: data.policy.scope, revoke: true } })
    expect(revoke.status()).toBe(200)
    expect((await revoke.json() as { authorization: Policy }).authorization).toMatchObject({ id: data.policy.id, revoked: true, revision: data.policy.revision + 1 })
    await expect(oldRow.getByRole('alert')).toContainText('资料、授权或接收者已经变化', { timeout: 12000 })
    await expect(oldRow.locator('pre.act-brief')).toHaveCount(0)
    await expect(oldRow.getByRole('button', { name: '复制给它的内容', exact: true })).toBeDisabled()
    await expect(oldRow.getByRole('button', { name: '保存并加入这件事', exact: true })).toBeDisabled()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(clipboardSentinel)
    const oldPackage = await page.request.get(`/v1/workspace/runs/${run.id}/package`)
    expect(oldPackage.status()).toBe(409)
    expect(await oldPackage.json()).toEqual({ error: 'version_conflict' })
    const beforeRejectedPaste = await snapshot(page)
    const oldPaste = await page.request.post('/v1/workspace/commands', { data: { type: 'pasteRunResult', id: run.id, output: '这份旧回答不得保存', requestId: crypto.randomUUID(), expectedRevision: beforeRejectedPaste.revision } })
    expect(oldPaste.status()).toBe(409)
    expect(await oldPaste.json()).toEqual({ error: 'version_conflict' })
    const refused = await snapshot(page)
    expect(refused.runs.find(candidate => candidate.id === run.id)).toMatchObject({ status: 'waiting', staleContext: true })
    expect(refused.runs.find(candidate => candidate.id === run.id)!.output).toBeFalsy()
    expect(refused.runs.find(candidate => candidate.id === run.id)!.adopted).toBeFalsy()
    expect(refused.docs.filter(doc => doc.runId === run.id)).toEqual([])
    const regeneratePromise = page.waitForResponse(response => new URL(response.url()).pathname === '/v1/workspace/commands' && response.request().postDataJSON()?.type === 'requestRun')
    await oldRow.getByRole('button', { name: '向原接收者重新生成', exact: true }).click()
    const regenerated = await regeneratePromise
    expect(regenerated.status()).toBe(200)
    const regenerationBody = regenerated.request().postDataJSON() as { id: string; manualRecipient: unknown; prompt: string }
    expect(regenerationBody.manualRecipient).toEqual(run.manualRecipient)
    expect(regenerationBody.prompt).toBe(run.prompt)
    expect(regenerationBody.id).not.toBe(run.id)
    const state = await regenerated.json() as State
    const next = state.runs.find(candidate => candidate.id === regenerationBody.id)!
    expect(next).toMatchObject({ status: 'waiting', staleContext: false, prompt: run.prompt })
    expect(next.contextTask!.recipient).toEqual(run.contextTask!.recipient)
    expect(state.runs.some(candidate => candidate.id === run.id && candidate.staleContext)).toBe(true)
    await expect(handoffRows(page, data.prompt)).toHaveCount(2)
    await expect(handoffRows(page, data.prompt).filter({ has: page.getByRole('alert') })).toHaveCount(1)
    const nextRow = runRow(page, data.prompt)
    await expect(nextRow).toHaveCount(1)
    await expect(nextRow.getByRole('alert')).toHaveCount(0)
    const safePreview = await obtain(page, nextRow, next, false)
    assertPackage(safePreview, next, data.source, data.marker, false)
    const safeCopied = await obtain(page, nextRow, next, true)
    assertPackage(safeCopied, next, data.source, data.marker, false)
    expect(safeCopied.attempt.id).not.toBe(safePreview.attempt.id)
    await submit(page, nextRow, next, '合成恢复回答：仅据独立说明继续，不使用已撤资料。')
    expect((await events(page)).filter(event => event.kind === 'model' && event.role === 'assistant')).toEqual([])
    await info.attach('revoke-refuse-recover', { body: JSON.stringify({ syntheticData: data, lawful, originalRun: run, refused, next, safePreview, safeCopied }, null, 2), contentType: 'application/json' })
    await evidence(page, info, 'manual-real-recovered')
  } finally { await http.attach(info) }
})
