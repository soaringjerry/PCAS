import { test, expect, type Page } from '@playwright/test'
import type { State } from '../src/domain/types'

// Melbourne browser against a Shanghai workspace, including the DST change.
test.use({ timezoneId: 'Australia/Melbourne', viewport: { width: 1440, height: 1000 } })
const now = '2026-10-03T14:30:00Z' // Oct 4 00:30 in Melbourne, Oct 3 22:30 in Shanghai.
function workspace(timezone = 'Asia/Shanghai'): State {
  return {
    version: 1, revision: 1, budgetUsage: 0, notices: [],
    settings: { timezone, dailyBudget: 10, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00' },
    tasks: [], ideas: [], projects: [], agents: [], memories: [], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {},
  }
}
async function mock(page: Page, state = workspace(), reject = false) {
  const commands: Record<string, unknown>[] = []
  const errors: string[] = []
  const zones: string[] = []
  page.on('pageerror', e => errors.push(e.message))
  await page.route('**/v1/**', route => route.fulfill({ json: {} }))
  await page.route('**/v1/connectors', route => route.fulfill({ json: [] }))
  await page.route('**/v1/notify/config', route => route.fulfill({ json: { webPush: { publicKey: '', subscriptions: 0 }, telegram: { configured: false, chatId: '' } } }))
  await page.route('**/v1/models/openai', route => route.fulfill({ json: null }))
  await page.route('**/v1/desk/turns?*', route => route.fulfill({ json: { turns: [] } }))
  await page.route('**/v1/workspace', route => {
    zones.push(route.request().headers()['x-pcas-timezone'])
    return route.fulfill({ json: state })
  })
  await page.route('**/v1/workspace/commands', route => {
    const command = route.request().postDataJSON()
    commands.push(command)
    if (reject) return route.fulfill({ status: 400, json: { error: 'invalid_timezone' } })
    Object.assign(state.settings, command.patch)
    state.revision++
    return route.fulfill({ json: state })
  })
  return { state, commands, errors, zones }
}

test('settings searches IANA zones, puts common cities first, and persists through updateSettings', async ({ page }) => {
  const backend = await mock(page)
  await page.goto('/settings')
  await page.getByRole('button', { name: '时区', exact: true }).click()
  const options = page.getByRole('listbox', { name: '时区选项' })
  await expect(options.getByRole('option').first()).toContainText('Australia/Melbourne')
  await page.getByRole('searchbox', { name: '搜索时区' }).fill('墨尔本')
  await expect(options.getByRole('option')).toHaveCount(1)
  await options.getByRole('option').click()
  await expect(page.getByRole('button', { name: '时区', exact: true })).toHaveText('Australia/Melbourne')
  expect(backend.commands[0]).toMatchObject({ type: 'updateSettings', patch: { timezone: 'Australia/Melbourne' }, expectedRevision: 1 })
  expect(backend.zones[0]).toBe('Australia/Melbourne')
  await page.reload()
  await expect(page.getByRole('button', { name: '时区', exact: true })).toHaveText('Australia/Melbourne')
  await page.getByRole('button', { name: '时区', exact: true }).click()
  await page.getByRole('searchbox', { name: '搜索时区' }).fill('Europe/Berlin')
  await expect(options.getByRole('option')).toHaveCount(1)
  expect(backend.errors).toEqual([])
})

test('a timezone save failure explains the fix and keeps the saved zone', async ({ page }) => {
  const backend = await mock(page, workspace(), true)
  await page.goto('/settings')
  await page.getByRole('button', { name: '时区', exact: true }).click()
  await page.getByRole('searchbox', { name: '搜索时区' }).fill('Melbourne')
  await page.getByRole('option').click()
  await expect(page.locator('.connection-banner')).toContainText('请选择有效的 IANA 时区')
  await expect(page.getByRole('button', { name: '时区', exact: true })).toHaveText('Asia/Shanghai')
  expect(backend.state.settings.timezone).toBe('Asia/Shanghai')
})

test('a mismatch prompt is at the top and changes the workspace in one click', async ({ page }) => {
  const backend = await mock(page)
  await page.goto('/')
  const hint = page.locator('.timezone-hint')
  await expect(hint).toContainText('你的时区好像是 Australia/Melbourne，要改成 Australia/Melbourne 吗？')
  expect((await hint.boundingBox())!.y).toBeLessThan((await page.locator('.hall-today').boundingBox())!.y)
  await hint.getByRole('button', { name: '改成 Australia/Melbourne' }).click()
  await expect(hint).toHaveCount(0)
  expect(backend.commands[0]).toMatchObject({ type: 'updateSettings', patch: { timezone: 'Australia/Melbourne' } })
  await page.reload()
  await expect(hint).toHaveCount(0)
  expect(backend.errors).toEqual([])
})

test('closing a mismatch persists in this browser across reload and navigation, including mobile', async ({ page }) => {
  const backend = await mock(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  await expect(page.locator('.timezone-hint')).toBeVisible()
  await page.getByRole('button', { name: '关闭时区提示' }).click()
  await expect(page.locator('.timezone-hint')).toHaveCount(0)
  await page.reload()
  await expect(page.locator('.hall-today')).toBeVisible()
  await expect(page.locator('.timezone-hint')).toHaveCount(0)
  await page.goto('/settings')
  await page.goto('/')
  await expect(page.locator('.timezone-hint')).toHaveCount(0)
  expect(backend.commands).toEqual([])
})

test('matching timezone and IANA aliases do not prompt', async ({ page }) => {
  await mock(page, workspace('Australia/Victoria'))
  await page.goto('/')
  await expect(page.locator('.hall-today')).toBeVisible()
  await expect(page.locator('.timezone-hint')).toHaveCount(0)
})

test('the same due/scheduled/follow-up tasks group by workspace date, including a DST day', async ({ page }) => {
  await page.clock.install({ time: new Date(now) })
  const state = workspace()
  const task = { id: 'due', title: '跨日截止', status: 'todo' as const, notes: '', due: '2026-10-03T15:00:00Z', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: now, updatedAt: now }
  state.tasks = [task, { ...task, id: 'scheduled', title: '跨日安排', due: undefined, scheduled: task.due },
    { ...task, id: 'follow', title: '跨日跟进', status: 'waiting', due: undefined, triggers: [{ id: 'follow-up', kind: 'time', description: '跟进', active: true, nextAt: '2026-10-03T17:00:00Z' }] },
    { ...task, id: 'dst', title: '夏令时后', due: '2026-10-04T12:30:00Z' }]
  const backend = await mock(page, state)
  await page.goto('/')
  const today = page.locator('.hall-today')
  const row = (title: string) => today.locator('.hall-task').filter({ hasText: title })
  await expect(today.locator('.hall-head')).toContainText('10月3日')
  await expect(row(task.title).locator('.hall-time')).toHaveText('23:00')
  await expect(row('跨日安排').locator('.hall-time')).toHaveText('23:00')
  await expect(row('跨日跟进')).toHaveCount(0)
  await expect(row('夏令时后').locator('.h-note')).toHaveText('明天截止')
  await expect(today.getByRole('separator')).toHaveAttribute('aria-label', '现在 22:30')
  await page.getByRole('button', { name: '改成 Australia/Melbourne' }).click()
  await expect(today.locator('.hall-head')).toContainText('10月4日')
  await expect(row(task.title).locator('.hall-time')).toHaveText('01:00')
  await expect(row('跨日安排').locator('.hall-time')).toHaveText('01:00')
  await expect(row('跨日跟进').locator('.h-note')).toContainText('该跟进了')
  await expect(row('夏令时后').locator('.hall-time')).toHaveText('23:30')
  await expect(today.getByRole('separator')).toHaveAttribute('aria-label', '现在 00:30')
  expect(backend.errors).toEqual([])
})
