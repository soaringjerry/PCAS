import { test, expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'

// Frozen before implementation: whitepaper §16 phase3.5's three scenarios.
// Selector/API adapters align to #276/#277/#278/#281/#282 and the rendered UI. Never
// replace these results with values returned by the backend under acceptance.
const manifestPath = process.env.PCAS_PHASE35_BROWSER_MANIFEST
const manifest = manifestPath ? JSON.parse(readFileSync(manifestPath, 'utf8')) : null
const force = process.env.PCAS_PHASE35_RUN_FINDINGS === '1'

async function realAPI(page: Page) {
  if (!manifest?.ownedDisposable || new URL(manifest.backendURL).hostname !== '127.0.0.1') throw new Error('Owned disposable real controller required')
  const failures: string[] = [], requests: string[] = []
  await page.route('**/*', async route => {
    const url = new URL(route.request().url())
    if (!['127.0.0.1', 'localhost'].includes(url.hostname)) return route.abort()
    if (!url.pathname.startsWith('/v1/')) return route.continue()
    requests.push(url.pathname + url.search)
    const response = await route.fetch({ url: manifest.backendURL + url.pathname + url.search, headers: { ...route.request().headers(), authorization: 'Bearer phase35-fictitious' } })
    if (response.status() >= 400) failures.push(`${route.request().method()} ${url.pathname} ${response.status()} ${await response.text()}`)
    await route.fulfill({ response })
  })
  return { failures, requests }
}
async function control(page: Page, path: string) {
  const response = await page.request.post(`${manifest.backendURL}${path}`)
  expect(response.status(), await response.text()).toBe(204)
}
async function state(page: Page) {
  const response = await page.request.get(`${manifest.backendURL}/v1/workspace`)
  expect(response.status()).toBe(200)
  return response.json()
}
async function say(page: Page, text: string) {
  const response = page.waitForResponse(r => new URL(r.url()).pathname === '/v1/desk/turn' && r.request().method() === 'POST')
  await page.getByRole('textbox', { name: '跟秘书说', exact: true }).fill(text)
  await page.getByRole('textbox', { name: '跟秘书说', exact: true }).press('Enter')
  expect((await response).status()).toBe(200)
}

test.use({ serviceWorkers: 'block', timezoneId: 'UTC', viewport: { width: 1280, height: 900 } })
test('T5 scene 1: spoken next-Wednesday appointment appears in that day home timeline', async ({ page }) => {
  test.skip(!force, 'finding S-P35-005')
  const api = await realAPI(page)
  await page.goto('/')
  await say(page, manifest.appointmentText)
  await control(page, '/phase35-drain')
  // Simulate opening the home page on the occurrence day. No database time
  // rewrite and no fabricated schedule response; frontend asks the real API.
  await page.clock.install({ time: new Date(`${manifest.appointmentDay}T08:00:00Z`) })
  await page.reload()
  const today = page.getByRole('region', { name: '今天', exact: true })
  await expect(today.getByText('和导师过虚构青岚方案', { exact: true })).toBeVisible()
  const st = await state(page)
  expect(st.tasks.some((x: { title: string }) => x.title === '和导师过虚构青岚方案')).toBe(false)
  expect(api.requests.some(x => x.startsWith('/v1/workspace/schedule?'))).toBe(true)
  expect(api.failures).toEqual([])
})

test('T5 scene 2: current chat direct task appears automatically with receipt and undo', async ({ page }) => {
  test.skip(!force, 'finding S-P35-010')
  const api = await realAPI(page)
  // Contract B2: this is an attached CURRENT conversation; old archive import
  // must not create today's work (covered independently by B2 Go tests).
  await control(page, '/phase35-current-chat')
  await page.goto('/')
  await expect(page.getByText(manifest.taskText, { exact: true }).first()).toBeVisible()
  const away = page.getByRole('region', { name: '你不在的时候', exact: true })
  await away.getByRole('button').first().click()
  const receipt = away.locator('li', { hasText: manifest.taskText })
  await expect(receipt).toHaveCount(1)
  await expect(receipt.getByRole('button', { name: '撤销', exact: true })).toBeVisible()
  await receipt.getByRole('button', { name: '撤销', exact: true }).click()
  await expect.poll(async () => (await state(page)).tasks.filter((x: { title: string }) => x.title === manifest.taskText).length).toBe(0)
  expect(api.failures).toEqual([])
})

test('T5 scene 3: multi-step goal creates project, groups steps, project undo detaches them', async ({ page }) => {
  test.skip(!force, 'finding S-P35-005')
  const api = await realAPI(page)
  await page.goto('/')
  await say(page, '虚构青岚多步目标：月底办展览，先征集陶瓷标本再排出方案。')
  await expect(page.getByText(/建了项目「虚构青岚展览」/).first()).toBeVisible()
  const before = await state(page)
  const project = before.projects.find((x: { name: string }) => x.name === '虚构青岚展览')
  expect(project).toBeTruthy()
  const steps = before.tasks.filter((x: { projectId: string }) => x.projectId === project.id)
  expect(steps.map((x: { title: string }) => x.title).sort()).toEqual(['虚构征集陶瓷标本', '虚构排出展览方案'].sort())
  // The project's own create_project receipt has a separate undo from the
  // task receipts; select it by its announced result, never DOM position.
  const receipt = page.locator('li.sec-receipt', { hasText: /建了项目「虚构青岚展览」/ }).filter({ has: page.getByRole('button', { name: '撤销', exact: true }) }).last()
  await receipt.getByRole('button', { name: '撤销', exact: true }).click()
  await expect.poll(async () => (await state(page)).projects.some((x: { id: string }) => x.id === project.id)).toBe(false)
  const after = await state(page)
  for (const step of steps) expect(after.tasks.find((x: { id: string }) => x.id === step.id)?.projectId ?? '').toBe('')
  expect(api.failures).toEqual([])
})

test('A3 overdue completion changes source memory and preserves original speech without creating a todo', async ({ page }) => {
  test.skip(!force, 'finding S-P35-006')
  const api = await realAPI(page)
  const beforeWork = await state(page)
  const before = await (await page.request.get(`${manifest.backendURL}/phase35-completion-state`)).json()
  await page.goto('/')
  const late = page.getByRole('region', { name: '今天', exact: true }).locator('.hall-group').filter({ has: page.getByRole('heading', { name: '在等你，或已经晚了', exact: true }) })
  await expect(late).toBeVisible()
  const expand = late.getByRole('button', { name: /^还有 \d+ 条/ })
  if (await expand.count()) await expand.click()
  await late.getByText(manifest.overdueTitle, { exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: `做完了：${manifest.overdueTitle}`, exact: true }).click()
  await expect.poll(async () => {
    const response = await page.request.get(`${manifest.backendURL}/v1/workspace/schedule?from=${manifest.appointmentDay}&to=${manifest.appointmentDay}`)
    expect(response.status()).toBe(200)
    return (await response.json()).overdue.some((x: { source: { deadlineId: string } }) => x.source.deadlineId === manifest.overdueId)
  }).toBe(false)
  const after = await (await page.request.get(`${manifest.backendURL}/phase35-completion-state`)).json()
  expect(after.memoryDigest).not.toBe(before.memoryDigest)
  expect(after.originalSourcesDigest).toBe(before.originalSourcesDigest)
  expect((await state(page)).tasks.length).toBe(beforeWork.tasks.length)
  expect(api.failures).toEqual([])
})
