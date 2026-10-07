import { test, expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'

// Oracle committed before reading implementation: phase3_contract.json.
// Golden paths use the actual Go Store/HTTP processor with the owned fixture.
// Boundary mode is ONLY a UI probe and never certifies end-to-end acceptance.
const force = process.env.PCAS_PHASE3_RUN_FINDINGS === '1'
const manifestPath = process.env.PCAS_PHASE3_BROWSER_MANIFEST
const manifest = manifestPath ? JSON.parse(readFileSync(manifestPath, 'utf8')) : undefined
const boundary = process.env.PCAS_PHASE3_BROWSER_BOUNDARY === '1'
const project = '33000000-0001-4000-8000-000000000000'
const document = '33000000-0003-4000-8000-000000000000'
const stamp = '2026-10-06T12:00:00Z'
const body2 = '虚构目标：青湾展览。\n\n固定段：样品编号。\n\n预算：200虚构单位。\n\n固定段：验收清单。\n\n新增段：改用青湾仓库。'
const handover = {
  projectId: project, writtenAt: stamp, stale: false,
  conclusion: [{ text: '虚构结论：方案第二版预算是200单位。', evidence: [{ kind: 'documentVersion', id: document, version: 2 }] }],
  blockers: [{ text: '虚构卡点：运输问题已解决，核验事项已完成。', evidence: [{ kind: 'item', id: '33000000-0002-4000-8000-000000000000' }] }],
  nextSteps: [{ text: '虚构下一步：按第二版方案准备青湾仓库。', evidence: [{ kind: 'documentVersion', id: document, version: 2 }] }],
}

async function isolatedAPI(page: Page, mode: 'real' | 'boundary' = 'real') {
  const failures: string[] = [], requests: string[] = []
  page.on('pageerror', e => failures.push(e.message))
  if (mode === 'real') {
    expect(manifest?.ownedDisposable).toBe(true)
    expect(new URL(manifest.backendURL).hostname).toBe('127.0.0.1')
  }
  const state = {
    version: 1, revision: 1, memoryRevision: 1, snapshotRevision: 1, budgetUsage: 0,
    settings: { dailyBudget: 100, autoAccept: false, wakeIdeas: false, followUps: true, dailyReviewAt: '09:00', timezone: 'UTC' },
    tasks: [{ id: '33000000-0002-4000-8000-000000000000', itemKind: 'task', title: '虚构已解决事项', status: 'done', projectId: project, notes: '', due: '', checklist: [], triggers: [], dependsOn: [], sources: [], history: [], createdAt: stamp, updatedAt: stamp }],
    projects: Array.from({ length: 30 }, (_, i) => ({ id: i ? `fictitious-project-${i}` : project, itemKind: 'project', name: i ? `虚构项目${i}` : '虚构青湾项目00', title: i ? `虚构项目${i}` : '虚构青湾项目00', status: 'active', goal: '完成虚构青湾展览', progress: '旧progress不该显示', nextSteps: ['旧nextSteps不该显示'], projectHandover: i ? null : handover, checklist: [], triggers: [], dependsOn: [], sources: [], history: [], createdAt: '2026-09-22T12:00:00Z', updatedAt: stamp })),
    docs: [{ id: document, thingId: project, title: '虚构方案 00-00', body: body2, by: 'user', version: 2, basedOn: null, createdAt: stamp, updatedAt: stamp }],
    ideas: [], memories: [], memoryTotal: 5000, candidates: [], runs: [], samples: [], sources: [], jobs: [], notices: [], activity: [], excludedMemories: {},
    agents: [{ id: 'phase3', name: '虚构验收模型', channel: 'api', enabled: true, available: true, default: true, note: '', memoryKinds: ['fact'], includeInferred: false, inputPrice: 0, outputPrice: 0, maxOutput: 8192 }],
  }
  await page.route('**/*', async route => {
    const url = new URL(route.request().url())
    if (!['127.0.0.1', 'localhost'].includes(url.hostname)) return route.abort()
    if (!url.pathname.startsWith('/v1/')) return route.continue()
    requests.push(url.pathname + url.search)
    if (mode === 'real') return route.fulfill({ response: await route.fetch({ url: manifest.backendURL + url.pathname + url.search }) })
    const path = url.pathname
    if (path === '/v1/workspace') return route.fulfill({ json: state })
    if (path.endsWith('/handover')) return route.fulfill({ json: path.includes('/projects/') ? handover : { body: '虚构全局交接资料', builtAt: stamp, stale: false } })
    if (path === '/v1/desk/turns') return route.fulfill({ json: { conversationId: '', turns: [] } })
    if (path.endsWith('/retained-writing')) return route.fulfill({ json: { fields: [], blocks: [] } })
    if (['/v1/connectors', '/v1/connectors/imports'].includes(path)) return route.fulfill({ json: path.endsWith('/imports') ? { items: [] } : [] })
    if (path.startsWith('/v1/models')) return route.fulfill({ json: {} })
    if (path === '/v1/notify/config') return route.fulfill({ json: { webPush: { publicKey: '', subscriptions: 0 }, telegram: { configured: false, chatId: '' } } })
    if (path.endsWith('/memories') || path.endsWith('/versions') || path.endsWith('/files') || path.endsWith('/memory-groups') || path.endsWith('/deadlines')) return route.fulfill({ json: { items: [] } })
    failures.push(`unhandled owned boundary API ${path}`)
    return route.fulfill({ status: 500, json: { error: 'unhandled_phase3_test_route' } })
  })
  return { failures, requests }
}

test.use({ serviceWorkers: 'block', timezoneId: 'UTC', viewport: { width: 1280, height: 900 } })
test('T5 boundary: opening-to-readable three parts under ten seconds', async ({ page }) => {
  test.skip(!force || !boundary, 'finding S-P3-006')
  const api = await isolatedAPI(page, 'boundary')
  const start = performance.now()
  await page.goto(`/t/${project}`)
  for (const title of ['结论', '卡点', '下一步']) await expect(page.getByText(title, { exact: true }).first()).toBeVisible({ timeout: 9000 })
  for (const sentence of [...handover.conclusion, ...handover.blockers, ...handover.nextSteps]) await expect(page.getByText(sentence.text, { exact: true })).toBeVisible()
  const elapsed = performance.now() - start
  console.log(JSON.stringify({ phase3: 'T5_boundary_open_to_readable_ms', elapsed }))
  expect(elapsed).toBeLessThan(10000)
  await expect(page.getByRole('textbox', { name: /结论|卡点|下一步|进展/ })).toHaveCount(0)
  expect(api.failures).toEqual([])
})

test('T5 golden path 4: resume a two-week-old project through the real API', async ({ page }) => {
  test.skip(!force || !manifest, 'finding S-P3-006')
  const api = await isolatedAPI(page)
  const started = performance.now()
  await page.goto(`/t/${manifest.projectId}`)
  for (const title of ['结论', '卡点', '下一步']) await expect(page.getByText(title, { exact: true }).first()).toBeVisible({ timeout: 9000 })
  await expect(page.getByText('虚构项目当前方案为第2版，预算200虚构单位。')).toBeVisible()
  await expect(page.getByText('虚构卡点已经解决，该事项状态为done。')).toBeVisible()
  await expect(page.getByText('根据虚构最新更正和最新预算续做。')).toBeVisible()
  const readable = performance.now() - started
  test.info().annotations.push({ type: 'open_to_readable_ms', description: String(readable) })
  expect(readable).toBeLessThan(10000)
  await expect(page.getByText(/写于/).first()).toBeVisible()
  await expect(page.getByRole('textbox', { name: /结论|卡点|下一步|进展/ })).toHaveCount(0)
  expect(api.requests.some(p => p.endsWith('/handover'))).toBe(true)
  await page.getByText('虚构项目当前方案为第2版，预算200虚构单位。', { exact: true }).click()
  await expect.poll(() => api.requests.some(p => p.includes(`/documents/${manifest.documentId}/versions/2`))).toBe(true)
  await expect(page.getByText('预算：200虚构单位。', { exact: true }).first()).toBeVisible()
  expect(api.failures).toEqual([])
})

test('T5 golden path 5: request a revision of version two, inspect version three and diff, undo', async ({ page }) => {
  test.skip(!force || !manifest, 'finding S-P3-006')
  const api = await isolatedAPI(page)
  await page.goto(`/t/${manifest.projectId}`)
  const textbox = page.getByRole('textbox', { name: /说一句|秘书|消息|说点/ }).first()
  await textbox.fill('把第二版方案里的预算部分改保守一点')
  await textbox.press('Enter')
  await expect.poll(async () => {
    const response = await page.request.get(`${manifest.backendURL}/v1/workspace/documents/${manifest.documentId}/versions`)
    return (await response.json()).currentVersion
  }, { timeout: 30000 }).toBe(3)
  await page.getByRole('button', { name: /虚构方案 00-00/ }).click()
  await page.getByRole('button', { name: /版本/ }).first().click()
  await expect(page.getByText(/第\s*3\s*版|版本\s*3/).first()).toBeVisible()
  await page.getByRole('button', { name: /差异|比较/ }).first().click()
  await expect(page.getByText('预算：200虚构单位。', { exact: true })).toBeVisible()
  await expect(page.getByText('预算：80虚构单位。', { exact: true })).toBeVisible()
  expect(api.requests.some(p => p.includes('/diff'))).toBe(true)
  await page.getByRole('button', { name: '撤销', exact: true }).last().click()
  await expect.poll(async () => {
    const response = await page.request.get(`${manifest.backendURL}/v1/workspace/documents/${manifest.documentId}/versions`)
    return (await response.json()).currentVersion
  }).toBe(2)
  const retained = await (await page.request.get(`${manifest.backendURL}/v1/workspace/documents/${manifest.documentId}/versions/3`)).json()
  expect(retained.body).toBe(manifest.goldVersion3)
  expect(api.failures).toEqual([])
})
