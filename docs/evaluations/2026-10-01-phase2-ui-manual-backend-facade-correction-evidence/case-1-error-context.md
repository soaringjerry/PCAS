# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: phase2_manual_backend.spec.ts >> real_revoke_clear_refuse_regenerate_submit
- Location: tests/phase2_manual_backend.spec.ts:185:1

# Error details

```
Test timeout of 30000ms exceeded.
```

```
Error: locator.click: Test timeout of 30000ms exceeded.
Call log:
  - waiting for locator('form').filter({ has: getByRole('textbox', { name: '本次转交请求' }) }).getByRole('button', { name: '转交接收者', exact: true })

```

# Page snapshot

```yaml
- generic [ref=f1e3]:
  - banner [ref=f1e4]:
    - link "回到大厅" [ref=f1e5] [cursor=pointer]:
      - /url: /
      - text: PCAS
    - generic [aria-hidden] [ref=f1e6]: /
    - generic [ref=f1e7]: 独立手动交接-0a448ecc-d4a1-4747-a8c3-38e4ab4e129d
    - navigation "导航" [ref=f1e8]:
      - button "搜索" [ref=f1e9] [cursor=pointer]:
        - generic [ref=f1e14]: Ctrl K
      - link "资料库" [ref=f1e15] [cursor=pointer]:
        - /url: /library
      - link "设置" [ref=f1e19] [cursor=pointer]:
        - /url: /settings
  - main [ref=f1e24]:
    - generic [ref=f1e25]:
      - link "大厅" [ref=f1e27] [cursor=pointer]:
        - /url: /
      - generic [ref=f1e30]:
        - button "做完了" [ref=f1e31] [cursor=pointer]
        - textbox "标题" [ref=f1e32]: 独立手动交接-0a448ecc-d4a1-4747-a8c3-38e4ab4e129d
        - button [aria-hidden] [ref=f1e33] [cursor=pointer]
      - group "这件事的情况，点一项就跟秘书说怎么改" [ref=f1e37]:
        - button "待办" [ref=f1e38] [cursor=pointer]
        - button "定个截止时间" [ref=f1e39] [cursor=pointer]
        - button "加个提醒" [ref=f1e40] [cursor=pointer]
      - textbox "说明" [ref=f1e41]:
        - /placeholder: 加点说明（点这里直接写）
        - text: OWNER 合成独立说明 cobalt-602，继续保留这段原文。
      - generic [ref=f1e42]:
        - generic [ref=f1e43]: 子任务
        - textbox "加一步" [ref=f1e48]
      - button "新建文档" [ref=f1e50] [cursor=pointer]
      - generic [ref=f1e52]:
        - generic [ref=f1e53]: 动态
        - generic [ref=f1e54]:
          - button "收起转交请求" [expanded] [ref=f1e55] [cursor=pointer]
          - generic [ref=f1e56]:
            - paragraph [ref=f1e57]: 选择你要转交给谁，再说这次想让它做什么。更换接收者会建立新的请求。
            - button "转交接收者：选择接收者" [ref=f1e58] [cursor=pointer]:
              - generic [ref=f1e59]: 选择接收者
            - textbox "本次转交请求" [active] [ref=f1e63]:
              - /placeholder: 这次想让它做什么？
              - text: 根据合成海棠交接资料-0a448ecc-d4a1-4747-a8c3-38e4ab4e129d和这件事的独立说明，给出一段建议。
            - button "建立转交请求" [disabled] [ref=f1e65]
        - list [ref=f1e66]:
          - listitem [ref=f1e67]:
            - generic [ref=f1e68]:
              - generic [ref=f1e69]: 你
              - generic [ref=f1e70]: 更新说明
              - generic [ref=f1e71]: 刚刚
          - listitem [ref=f1e72]:
            - generic [ref=f1e73]:
              - generic [ref=f1e74]: 你
              - generic [ref=f1e75]: 创建
              - generic [ref=f1e76]: 刚刚
          - listitem [ref=f1e77]:
            - generic [ref=f1e78]:
              - generic [ref=f1e79]: 你
              - generic [ref=f1e80]: 创建
              - generic [ref=f1e81]: 刚刚
      - region "秘书" [ref=f1e82]:
        - generic [ref=f1e84]:
          - textbox "跟秘书说" [ref=f1e85]:
            - /placeholder: 关于这件事，说一句
          - button "发送" [disabled] [ref=f1e86]
```

# Test source

```ts
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
  48  |       expect(errors).toEqual([])
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
> 106 |   await form.getByRole('button', { name: '转交接收者', exact: true }).click()
      |                                                                  ^ Error: locator.click: Test timeout of 30000ms exceeded.
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
  149 | }
  150 | 
  151 | async function submit(page: Page, row: Locator, run: Run, text: string) {
  152 |   await row.getByRole('textbox', { name: '贴回回答' }).fill(text)
  153 |   const responsePromise = page.waitForResponse(response => new URL(response.url()).pathname === '/v1/workspace/commands' && response.request().postDataJSON()?.type === 'pasteRunResult' && response.request().postDataJSON()?.id === run.id)
  154 |   await row.getByRole('button', { name: '保存并加入这件事', exact: true }).click()
  155 |   const response = await responsePromise
  156 |   expect(response.status()).toBe(200)
  157 |   expect(response.request().postDataJSON()).toMatchObject({ type: 'pasteRunResult', id: run.id, output: text })
  158 |   const state = await snapshot(page)
  159 |   expect(state.runs.find(candidate => candidate.id === run.id)).toMatchObject({ status: 'done', output: text, adopted: { auto: true, as: 'doc' } })
  160 |   expect(state.docs.filter(doc => doc.runId === run.id)).toHaveLength(1)
  161 |   expect(state.docs.find(doc => doc.runId === run.id)!.body).toBe(text)
  162 |   expect(state.tasks.find(task => task.id === run.thingId)!.notes).toBe(ownerText)
  163 |   await expect(page.getByText(ownerText, { exact: true })).toBeVisible()
  164 |   return state
  165 | }
  166 | 
  167 | test('real_target_package_clipboard_submit', async ({ page }, info) => {
  168 |   const http = observeHTTP(page)
  169 |   try {
  170 |     const data = await setup(page)
  171 |     const run = await createThroughUI(page, data.taskId, data.prompt)
  172 |     const row = runRow(page, data.prompt)
  173 |     const preview = await obtain(page, row, run, false)
  174 |     assertPackage(preview, run, data.source, data.marker, true)
  175 |     const copied = await obtain(page, row, run, true)
  176 |     assertPackage(copied, run, data.source, data.marker, true)
  177 |     expect(copied.attempt.id).not.toBe(preview.attempt.id)
  178 |     await submit(page, row, run, '合成贴回回答：请保留独立原文，按一个普通步骤继续。')
  179 |     expect((await events(page)).filter(event => event.kind === 'model' && event.role === 'assistant')).toEqual([])
  180 |     await info.attach('two-real-packages', { body: JSON.stringify({ syntheticData: data, preview, copied, originalRun: run }, null, 2), contentType: 'application/json' })
  181 |     await evidence(page, info, 'manual-real-completed')
  182 |   } finally { await http.attach(info) }
  183 | })
  184 | 
  185 | test('real_revoke_clear_refuse_regenerate_submit', async ({ page }, info) => {
  186 |   const http = observeHTTP(page)
  187 |   try {
  188 |     const data = await setup(page)
  189 |     const run = await createThroughUI(page, data.taskId, data.prompt)
  190 |     await expect(handoffRows(page, data.prompt)).toHaveCount(1)
  191 |     const oldRow = handoffRows(page, data.prompt).first()
  192 |     const lawful = await obtain(page, oldRow, run, false)
  193 |     assertPackage(lawful, run, data.source, data.marker, true)
  194 |     const clipboardSentinel = 'OWNER synthetic clipboard sentinel before revocation'
  195 |     await page.evaluate(text => navigator.clipboard.writeText(text), clipboardSentinel)
  196 |     await oldRow.getByRole('textbox', { name: '贴回回答' }).fill('这份旧回答不得保存')
  197 |     const revoke = await page.request.delete(`/v1/memory/sources/${data.source.id}/authorization`, { data: { request_id: crypto.randomUUID(), source: data.source, policy_id: data.policy.id, expected_policy_revision: data.policy.revision, recipient: data.policy.recipient, purpose: data.policy.purpose, scope: data.policy.scope, revoke: true } })
  198 |     expect(revoke.status()).toBe(200)
  199 |     expect((await revoke.json() as { authorization: Policy }).authorization).toMatchObject({ id: data.policy.id, revoked: true, revision: data.policy.revision + 1 })
  200 |     await expect(oldRow.getByRole('alert')).toContainText('资料、授权或接收者已经变化', { timeout: 12000 })
  201 |     await expect(oldRow.locator('pre.act-brief')).toHaveCount(0)
  202 |     await expect(oldRow.getByRole('button', { name: '复制给它的内容', exact: true })).toBeDisabled()
  203 |     await expect(oldRow.getByRole('button', { name: '保存并加入这件事', exact: true })).toBeDisabled()
  204 |     expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(clipboardSentinel)
  205 |     const oldPackage = await page.request.get(`/v1/workspace/runs/${run.id}/package`)
  206 |     expect(oldPackage.status()).toBe(409)
```