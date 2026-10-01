import { test, expect } from '@playwright/test'
import { command, evidence, fixture, login, reply, say, snapshot } from './support/real'

test.use({ timezoneId: 'Australia/Melbourne', viewport: { width: 1440, height: 1000 } })
test.afterEach(async ({ page }, info) => { await evidence(page, info) })

test('F6 first login defaults to the browser; settings, validation, hint and today agree with real receipts', async ({ page }) => {
  // Run on the runner's fresh database before the other real-backend suites.
  await login(page)
  expect((await snapshot(page)).settings.timezone).toBe('Australia/Melbourne')
  await expect(page.locator('.timezone-hint')).toHaveCount(0)

  await page.goto('/settings')
  await page.getByRole('button', { name: '时区', exact: true }).click()
  await page.getByRole('searchbox', { name: '搜索时区' }).fill('Shanghai')
  await page.getByRole('option').click()
  await expect(page.getByRole('button', { name: '时区', exact: true })).toHaveText('Asia/Shanghai')
  await page.reload()
  await expect(page.getByRole('button', { name: '时区', exact: true })).toHaveText('Asia/Shanghai')
  for (const timezone of ['Australia/Unknown', '', 'Local', null, 8]) {
    const before = await snapshot(page)
    const response = await page.request.post('/v1/workspace/commands', { data: {
      type: 'updateSettings', patch: { timezone }, requestId: crypto.randomUUID(), expectedRevision: before.revision,
    } })
    expect(response.status()).toBe(400)
    expect(await response.json()).toEqual({ error: 'invalid_timezone' })
    expect((await snapshot(page)).settings.timezone).toBe('Asia/Shanghai')
    expect((await snapshot(page)).revision).toBe(before.revision)
  }
  const invalidHeader = await page.request.get('/v1/workspace', { headers: { 'X-PCAS-Timezone': 'Australia/Unknown' } })
  expect(invalidHeader.status()).toBe(400)
  expect(await invalidHeader.json()).toEqual({ error: 'invalid_timezone' })

  // Existing settings survive a browser refresh. The hint dismissal survives reload.
  await page.goto('/')
  await expect(page.locator('.timezone-hint')).toBeVisible()
  await page.getByRole('button', { name: '关闭时区提示' }).click()
  await page.reload()
  await expect(page.locator('.hall-today')).toBeVisible()
  await expect(page.locator('.timezone-hint')).toHaveCount(0)
  expect((await snapshot(page)).settings.timezone).toBe('Asia/Shanghai')
  await page.evaluate(() => localStorage.removeItem('pcas.timezone-hint.dismissed'))
  await page.reload()
  await page.getByRole('button', { name: '改成 Australia/Melbourne' }).click()
  await expect(page.locator('.timezone-hint')).toHaveCount(0)
  expect((await snapshot(page)).settings.timezone).toBe('Australia/Melbourne')

  // An absolute instant on different calendar dates in the two zones.
  await page.clock.install({ time: new Date('2026-10-03T14:30:00Z') })
  // Remount the clock after changing browser time; NowLine normally updates once a minute.
  await page.reload()
  await expect(page.locator('.hall-today')).toBeVisible()
  // Only the browser clock is frozen; keep the real worker from firing dated test reminders.
  const projectId = crypto.randomUUID()
  const createdTaskIds: string[] = []
  try {
    await command(page, { type: 'updateSettings', patch: { timezone: 'Asia/Shanghai', followUps: false } })
    const projectState = await command(page, { type: 'addProject', id: projectId, name: 'F8 跨页项目' })
    const project = projectState.projects.find(p => p.id === projectId)!
    const title = 'F6 跨日回信'
    await fixture(page, [reply('安排跨日回信', [{ op: 'create_task', title, due: '2026-10-03T23:00', remind: '-30m', project: 'P1' }])])
    const response = await say(page, '安排跨日回信')
    const taskId = response.turn.receipts[0].thingId
    createdTaskIds.push(taskId)
    const task = response.state.tasks.find((t: { id: string }) => t.id === taskId)
    expect(task.title).toBe(title)
    const receipt = page.locator('.sec-receipt').filter({ hasText: title })
    await expect(receipt).toContainText('23:00')
    await expect(receipt).toContainText('22:30 提醒')
    expect(task.projectId).toBe(projectId)
    expect(new Date(task.due).toISOString()).toBe('2026-10-03T15:00:00.000Z')
    expect(new Date(task.triggers[0].nextAt).toISOString()).toBe('2026-10-03T14:30:00.000Z')
    const today = page.locator('.hall-today')
    const row = today.locator('.hall-task').filter({ hasText: title })
    await expect(today.locator('.hall-head')).toContainText('10月3日')
    await expect(row.locator('.hall-time')).toHaveText('23:00')
    await expect(today.getByRole('separator')).toHaveAttribute('aria-label', '现在 22:30')
    // The model fake supplies only the requested show operation; cards come from real stored tasks.
    await fixture(page, [reply('显示跨页安排', [], { show: ['T1'] })])
    await say(page, '显示跨页安排')
    await expect(page.locator('.sec-tasks .k-meta')).toContainText('今天 23:00')
    const historicalReceipt = await receipt.locator('.r-what').innerText()
    await page.goto(`/t/${task.id}`)
    await expect(page.locator('.info-line')).toContainText('今天 23:00 截止')
    await page.goto(`/t/${project.id}`)
    await expect(page.locator('.item-row .reason')).toHaveText('今天 23:00 截止')
    await page.goto('/')
    await page.getByRole('button', { name: '改成 Australia/Melbourne' }).click()
    await expect(today.locator('.hall-head')).toContainText('10月4日')
    await expect(row.locator('.hall-time')).toHaveText('01:00')
    await expect(today.getByRole('separator')).toHaveAttribute('aria-label', '现在 00:30')
    await expect(page.locator('.sec-tasks .k-meta')).toContainText('今天 01:00')
    await expect(receipt.locator('.r-what')).toHaveText(historicalReceipt)
    await page.reload()
    await expect(page.locator('.sec-tasks .k-meta')).toContainText('今天 01:00')
    await expect(receipt.locator('.r-what')).toHaveText(historicalReceipt)
    await expect(receipt).toHaveCount(1)
    await page.goto(`/t/${task.id}`)
    await expect(page.locator('.info-line')).toContainText('今天 01:00 截止')
    await page.goto(`/t/${project.id}`)
    await expect(page.locator('.item-row .reason')).toHaveText('今天 01:00 截止')
    const unchanged = (await snapshot(page)).tasks.find(t => t.id === task.id)!
    expect(unchanged.due).toBe(task.due)
    expect(unchanged.triggers[0].nextAt).toBe(task.triggers[0].nextAt)
    await page.goto('/')
    // The Melbourne secretary uses the same instant/clock as the hall.
    await fixture(page, [reply('再按墨尔本时间安排', [{ op: 'create_task', title: 'F6 墨尔本回信', due: '2026-10-04T01:00', remind: '-30m' }])])
    const melbourne = await say(page, '再按墨尔本时间安排')
    const melbourneTaskId = melbourne.turn.receipts[0].thingId
    createdTaskIds.push(melbourneTaskId)
    const melbourneTask = melbourne.state.tasks.find((t: { id: string }) => t.id === melbourneTaskId)
    expect(melbourneTask.title).toBe('F6 墨尔本回信')
    expect(melbourneTask.due).toBe(task.due)
    await expect(page.locator('.sec-receipt').filter({ hasText: 'F6 墨尔本回信' })).toContainText('01:00')
    await evidence(page, test.info(), 'melbourne')
  } finally {
    // Suites share this workspace. Finish only our records, including on assertion failure,
    // so this project no longer occupies P1 in the later secretary fixtures.
    for (const id of createdTaskIds) await command(page, { type: 'setTaskStatus', id, status: 'done' })
    if ((await snapshot(page)).projects.some(p => p.id === projectId)) {
      await command(page, { type: 'updateProject', id: projectId, patch: { status: 'done' } })
    }
    await command(page, { type: 'updateSettings', patch: { followUps: true } })
    const cleaned = await snapshot(page)
    expect(cleaned.projects.find(p => p.id === projectId)?.status).toBe('done')
    for (const id of createdTaskIds) expect(cleaned.tasks.find(t => t.id === id)?.status).toBe('done')
    expect(cleaned.settings.followUps).toBe(true)
  }
})
