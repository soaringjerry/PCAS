# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: phase2_manual_backend.spec.ts >> real_target_package_clipboard_submit
- Location: tests/phase2_manual_backend.spec.ts:167:1

# Error details

```
Error: expect(received).toEqual(expected) // deep equality

- Expected  - 1
+ Received  + 3

- Array []
+ Array [
+   "Cannot read properties of undefined (reading 'route_fingerprint')",
+ ]
```

# Test source

```ts
  1   | import { test, expect, type Page, type Locator, type TestInfo } from '@playwright/test'
  2   | import type { ContextRecipient, ManualRunPackage, Run, State } from '../src/domain/types'
  3   | import { command, events, evidence, fixture, fixtureURL, login, snapshot } from './support/real'
  4   | 
  5   | type SourceRef = { id: string; version: number; kind: 'source' }
  6   | type Policy = { id: string; revision: number; recipient: ContextRecipient; purpose: 'knowledge'; scope: { kind: 'unscoped'; include_global_constraints: boolean }; revoked: boolean }
  7   | type Package = ManualRunPackage & { attempt: { manifest: { input: { ref: SourceRef }[] } } }
  8   | type HTTPRecord = { method: string; path: string; request?: unknown; status?: number; response?: unknown }
  9   | const ownerText = 'OWNER 合成独立说明 cobalt-602，继续保留这段原文。'
  10  | const target = { id: 'golden', name: '验收假模型' }
  11  | 
  12  | function requireSyntheticEnvironment() {
  13  |   for (const [name, value] of [['PCAS_TEST_BASE_URL', process.env.PCAS_TEST_BASE_URL], ['PCAS_TEST_FIXTURE_URL', fixtureURL]]) {
  14  |     expect(value, `${name} must be explicit; no skip`).toBeTruthy()
  15  |     const url = new URL(value!)
  16  |     expect(url.protocol).toBe('http:')
  17  |     expect(['127.0.0.1', 'localhost', '[::1]']).toContain(url.hostname)
  18  |     expect(url.username + url.password).toBe('')
  19  |   }
  20  |   expect(process.env.PCAS_TEST_API_TOKEN).toMatch(/^pcas-golden-test-only-/)
  21  | }
  22  | 
  23  | // Read-only browser request/response observation; never intercepts an API or
  24  | // records the test token, authorization header, cookie, or private configuration.
  25  | function observeHTTP(page: Page) {
  26  |   const records: HTTPRecord[] = []
  27  |   const pending: Promise<void>[] = []
  28  |   const errors: string[] = []
  29  |   page.on('pageerror', error => errors.push(error.message))
  30  |   page.on('response', response => {
  31  |     const path = new URL(response.url()).pathname
  32  |     if (!path.startsWith('/v1/workspace') && !path.startsWith('/v1/memory/sources') && path !== '/v1/models') return
  33  |     pending.push((async () => {
  34  |       const request = response.request()
  35  |       const record: HTTPRecord = { method: request.method(), path, status: response.status() }
  36  |       if (request.postData()) record.request = request.postDataJSON() as unknown
  37  |       try { record.response = await response.json() }
  38  |       catch { record.response = { observation: 'body_unavailable_after_navigation' } }
  39  |       records.push(record)
  40  |     })())
  41  |   })
  42  |   return {
  43  |     records,
  44  |     async attach(info: TestInfo) {
  45  |       const settled = await Promise.allSettled(pending)
  46  |       expect(settled.filter(result => result.status === 'rejected'), 'passive response capture must complete').toEqual([])
  47  |       await info.attach('real-http', { body: JSON.stringify(records, null, 2), contentType: 'application/json' })
> 48  |       expect(errors).toEqual([])
      |                      ^ Error: expect(received).toEqual(expected) // deep equality
  49  |     },
  50  |   }
  51  | }
  52  | 
  53  | test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 1440, height: 1000 } })
  54  | test.beforeEach(async ({ page, context }) => {
  55  |   requireSyntheticEnvironment()
  56  |   await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: process.env.PCAS_TEST_BASE_URL! })
  57  |   await login(page)
  58  |   await fixture(page)
  59  | })
  60  | 
  61  | async function setup(page: Page) {
  62  |   const suffix = crypto.randomUUID()
  63  |   const name = `合成海棠交接资料-${suffix}`
  64  |   const marker = `RAW_BACKEND_HANDOFF_${suffix}`
  65  |   const prompt = `根据${name}和这件事的独立说明，给出一段建议。`
  66  |   const sourceResponse = await page.request.post('/v1/memory/sources', { data: { connector: 'phase2-browser-synthetic', external_id: suffix, external_version: 'v1', title: name, text: `${name}：合成来源 atom ${marker}。`, media_type: 'text/plain' } })
  67  |   expect(sourceResponse.status()).toBe(201)
  68  |   const ingested = await sourceResponse.json() as SourceRef
  69  |   const source: SourceRef = { id: ingested.id, version: ingested.version, kind: ingested.kind }
  70  |   expect(source).toMatchObject({ kind: 'source', version: 1 })
  71  |   const state = await command(page, { type: 'addTask', title: `独立手动交接-${suffix}` })
  72  |   const task = state.tasks.find(item => item.title === `独立手动交接-${suffix}`)!
  73  |   expect(task).toBeTruthy()
  74  |   await command(page, { type: 'setNotes', id: task.id, text: ownerText })
  75  |   const grantResponse = await page.request.post(`/v1/memory/sources/${source.id}/authorization`, { data: { request_id: crypto.randomUUID(), source, expected_policy_revision: 0, recipient: { principal_id: 'manual', role: 'manual', provider: target.id }, purpose: 'knowledge', scope: { kind: 'unscoped', include_global_constraints: false } } })
  76  |   expect(grantResponse.status()).toBe(200)
  77  |   const grant = await grantResponse.json() as { authorization: Policy }
  78  |   expect(grant.authorization).toMatchObject({ revision: 1, revoked: false, recipient: { principal_id: 'manual', role: 'manual', provider: target.id, model: 'golden', protocol: 'openai', channel: 'manual' } })
  79  |   expect(grant.authorization.recipient.route_fingerprint).toMatch(/^[a-f0-9]{64}$/)
  80  |   // The real worker may extract the synthetic source using the existing golden
  81  |   // items=[] response. Wait for those writes; no timer or model response is stubbed.
  82  |   await expect.poll(async () => (await snapshot(page)).jobs.filter(job => job.detail.includes(name) && (job.status === 'queued' || job.status === 'running')).length, { timeout: 20000 }).toBe(0)
  83  |   await page.goto(`/t/${task.id}`)
  84  |   await expect(page.getByText(ownerText, { exact: true })).toBeVisible()
  85  |   return { source, policy: grant.authorization, taskId: task.id, marker, prompt }
  86  | }
  87  | 
  88  | function handoffRows(page: Page, prompt: string) {
  89  |   return page.locator('.activity > li').filter({ has: page.getByRole('textbox', { name: '贴回回答' }) }).filter({ hasText: prompt })
  90  | }
  91  | 
  92  | function runRow(page: Page, prompt: string) {
  93  |   return handoffRows(page, prompt).filter({ has: page.getByRole('button', { name: '复制给它的内容', exact: true, disabled: false }) })
  94  | }
  95  | 
  96  | async function createThroughUI(page: Page, taskId: string, prompt: string) {
  97  |   const catalogPromise = page.waitForResponse(response => new URL(response.url()).pathname === '/v1/models' && response.request().method() === 'GET')
  98  |   await page.getByRole('button', { name: '手动转交', exact: true }).click()
  99  |   const form = page.locator('form').filter({ has: page.getByRole('textbox', { name: '本次转交请求' }) })
  100 |   await form.getByRole('textbox', { name: '本次转交请求' }).fill(prompt)
  101 |   await expect(form.getByRole('button', { name: '建立转交请求', exact: true })).toBeDisabled()
  102 |   const catalog = await catalogPromise
  103 |   expect(catalog.status()).toBe(200)
  104 |   const model = (await catalog.json() as { providers: { id: string; name: string; available: boolean }[] }).providers.find(provider => provider.id === target.id)
  105 |   expect(model).toMatchObject({ id: target.id, name: target.name, available: true })
  106 |   await form.getByRole('button', { name: '转交接收者：选择接收者', exact: true }).click()
  107 |   await page.getByRole('option', { name: new RegExp(target.name) }).click()
  108 |   const responsePromise = page.waitForResponse(response => new URL(response.url()).pathname === '/v1/workspace/commands' && response.request().postDataJSON()?.type === 'requestRun')
  109 |   await form.getByRole('button', { name: '建立转交请求', exact: true }).click()
  110 |   const response = await responsePromise
  111 |   expect(response.status()).toBe(200)
  112 |   const body = response.request().postDataJSON() as { id: string; type: string; thingId: string; agentId: string; prompt: string; kind: string; manualRecipient: Record<string, unknown> }
  113 |   expect(body).toMatchObject({ type: 'requestRun', thingId: taskId, agentId: 'manual', kind: 'ask', prompt, manualRecipient: { provider: target.id } })
  114 |   expect(Object.keys(body.manualRecipient)).toEqual(['provider'])
  115 |   expect(body).not.toHaveProperty('scope')
  116 |   expect(body).not.toHaveProperty('fingerprint')
  117 |   const state = await response.json() as State
  118 |   const run = state.runs.find(candidate => candidate.id === body.id)!
  119 |   expect(run).toMatchObject({ status: 'waiting', staleContext: false, prompt, contextTask: { recipient: { role: 'manual', provider: target.id } } })
  120 |   expect(run.contextTask!.recipient.route_fingerprint).toMatch(/^[a-f0-9]{64}$/)
  121 |   await expect(runRow(page, prompt).getByRole('button', { name: '预览交接内容', exact: true })).toBeVisible()
  122 |   return run
  123 | }
  124 | 
  125 | function assertPackage(pkg: Package, run: Run, source: SourceRef, marker: string, allowed: boolean) {
  126 |   expect(pkg.run_id).toBe(run.id)
  127 |   expect(pkg.attempt.id).toMatch(/^[a-f0-9-]{36}$/)
  128 |   expect(pkg.attempt.delivered_at).toBeTruthy()
  129 |   expect(pkg.attempt.external_receipt).toBe('unknown')
  130 |   expect(pkg.attempt.manifest.recipient).toEqual(run.contextTask!.recipient)
  131 |   expect(pkg.package).toContain(ownerText)
  132 |   expect(pkg.package.includes(marker)).toBe(allowed)
  133 |   expect(pkg.attempt.manifest.input.some(input => input.ref.id === source.id && input.ref.version === source.version && input.ref.kind === source.kind)).toBe(allowed)
  134 |   expect(run.brief).not.toContain(marker)
  135 | }
  136 | 
  137 | async function obtain(page: Page, row: Locator, run: Run, copy: boolean) {
  138 |   const responsePromise = page.waitForResponse(response => new URL(response.url()).pathname === `/v1/workspace/runs/${run.id}/package` && response.request().method() === 'GET')
  139 |   await row.getByRole('button', { name: copy ? '复制给它的内容' : '预览交接内容', exact: true }).click()
  140 |   const response = await responsePromise
  141 |   expect(response.status()).toBe(200)
  142 |   const pkg = await response.json() as Package
  143 |   if (copy) {
  144 |     await expect(row.getByRole('status')).toContainText('已复制，可以粘贴给')
  145 |     expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(pkg.package)
  146 |   } else await expect(row.locator('pre.act-brief')).toHaveText(pkg.package)
  147 |   await expect(row).toContainText('PCAS 无法确认对方是否收到')
  148 |   return pkg
```