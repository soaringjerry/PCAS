import { test, expect, type Page } from '@playwright/test'
import type { Memory, MemoryTrust, State } from '../src/domain/types'

// Everything here is made up: the people, the places and what was said.
const usedAt = new Date(Date.now() - 2 * 86400_000).toISOString()
let serial = 0
function memory(text: string, more: Partial<Memory> = {}): Memory {
  return {
    id: `memory-${++serial}`, recordVersion: 1, kind: 'fact', text,
    epistemic: 'sourced', confirmation: 'pending', acquisition: 'direct', sources: [], versions: [], visibleTo: ['model'], exposure: 1, lastUsedAt: usedAt, pinned: false,
    halfLifeDays: 30, reinforcementLimit: 8, mentions: [], groups: [], ...more,
  }
}
const said = (label: string, excerpt: string) => ({ sourceId: `source-${++serial}`, label, excerpt, at: usedAt })

function fixture() {
  const friday = memory('遮阳网的安装改到周五', { trust: 'stated', mergedFrom: 2 })
  const current = [
    friday,
    memory('咖啡只喝中烘的', { trust: 'repeated' }),
    memory('可能明年把摊位搬到河堤那边', { trust: 'tentative' }),
    memory('林栖说云岫镇的市集下个月要涨摊位费', { trust: 'reported' }),
    memory('大概习惯晚上处理账目', { trust: 'inferred', epistemic: 'inferred' }),
  ]
  const wednesday = memory('遮阳网周三装', { trust: 'stated', retired: 'superseded', retiredBy: friday.id })
  const merged = [
    memory('遮阳网安装挪到周五了', { trust: 'stated', retired: 'duplicate', retiredBy: friday.id, sources: [said('和秘书的对话', '那个遮阳网，挪到周五装吧')] }),
    memory('周五装遮阳网', { trust: 'stated', retired: 'duplicate', retiredBy: friday.id, sources: [said('随手记', '周五装遮阳网，别忘了')] }),
  ]
  return { current, retired: [wednesday, ...merged], friday, wednesday, merged }
}

async function backend(page: Page, current: Memory[], retired: Memory[] = []) {
  const state: State = {
    version: 1, revision: 1, budgetUsage: 0,
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: false, followUps: false, dailyReviewAt: '09:00', timezone: 'Asia/Shanghai' },
    tasks: [], ideas: [], projects: [], memories: current.slice(0, 200), memoryTotal: current.length,
    candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], notices: [], activity: [], excludedMemories: {},
    agents: [{ id: 'model', name: '演示模型', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false }],
  }
  const queries: URL[] = []
  const commands: Record<string, unknown>[] = []
  const undone: string[] = []
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  await page.route('**/v1/**', (route) => route.fulfill({ status: 500, json: { error: 'unexpected_trust_request' } }))
  await page.route((url) => url.pathname === '/v1/workspace', (route) => route.fulfill({ json: state }))
  await page.route((url) => url.pathname === '/v1/workspace/memory-facets', (route) => route.fulfill({ json: { groups: [], people: [], places: [] } }))
  await page.route((url) => url.pathname === '/v1/workspace/memories', (route) => {
    const url = new URL(route.request().url())
    queries.push(url)
    const trust = url.searchParams.get('trust'), q = url.searchParams.get('q'), by = url.searchParams.get('retiredBy')
    const from = url.searchParams.get('retired') === '1' ? retired : current
    const items = from.filter((m) => (!trust || m.trust === trust) && (!q || m.text.includes(q)) && (!by || m.retiredBy === by))
    return route.fulfill({ json: { items, next: '', total: items.length } })
  })
  await page.route((url) => /^\/v1\/workspace\/memories\/[^/]+$/.test(url.pathname), (route) => {
    const id = new URL(route.request().url()).pathname.split('/').at(-1)
    const found = [...current, ...retired].find((m) => m.id === id)
    return route.fulfill({ status: found ? 200 : 404, json: found ?? { error: 'not_found' } })
  })
  const restored = new Map<string, Memory>()
  await page.route((url) => url.pathname === '/v1/workspace/commands', (route) => {
    const command = route.request().postDataJSON()
    commands.push(command)
    if (command.type === 'undoAction') {
      undone.push(command.id)
      const was = restored.get(command.id)
      if (!was) return route.fulfill({ status: 400, json: { error: 'invalid_input' } })
      current.splice(current.findIndex((m) => m.id === was.id), 1)
      retired.push(was)
      state.memories = [...current]
      state.revision++
      return route.fulfill({ json: state })
    }
    if (command.type !== 'restoreMemory') return route.fulfill({ status: 400, json: { error: 'invalid_input' } })
    const at = retired.findIndex((m) => m.id === command.id)
    const [back] = retired.splice(at, 1)
    restored.set(command.requestId, { ...back })
    delete back.retired
    delete back.retiredBy
    current.push(back)
    state.memories = [...current]
    state.revision++
    return route.fulfill({ json: state })
  })
  return { state, queries, commands, undone, errors }
}

const open = (page: Page, search = '') => page.goto(`/library?tab=memory${search}`)
const card = (page: Page, text: string) => page.locator('.mem-entry').filter({ has: page.getByText(text, { exact: true }) })
const lastQuery = (mock: { queries: URL[] }, name: string) => mock.queries.at(-1)?.searchParams.get(name) ?? null
const fits = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)
const memoryTab = (page: Page) => page.locator('main.page')
/** The older words for how sure a memory is, and words for how the comparing works inside. */
const gone = /待确认|原话有据|推测|规则版本|过期|重建|实体/
const labels: Record<MemoryTrust, string> = { stated: '你说的', repeated: '多次说过', tentative: '带保留', reported: '转述', inferred: '推断' }

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 1280, height: 900 } })
test.afterEach(async ({ page }, info) => {
  if (info.status === 'passed') await expect(memoryTab(page)).not.toContainText(gone)
})

test('R2-11 卡片和详情显示可信度的中文叫法，不再有「待确认 / 原话有据 / 推测」', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, f.current, f.retired)
  await open(page)
  await expect(page.locator('.mem-entry')).toHaveCount(5)
  for (const m of f.current) await expect(card(page, m.text).locator('.tag')).toHaveText([labels[m.trust!]])
  // Only what the model worked out is marked as not the user's own words.
  await expect(page.locator('.mem-text.guess')).toHaveText(['大概习惯晚上处理账目'])
  // The memories that were replaced or merged away are not in the list.
  for (const m of f.retired) await expect(page.getByText(m.text, { exact: true })).toHaveCount(0)
  expect(lastQuery(mock, 'retired')).toBeNull()

  await page.getByText('可能明年把摊位搬到河堤那边', { exact: true }).click()
  const sheet = page.getByRole('dialog')
  await expect(sheet.locator('.side-sheet-head .tag')).toHaveText(['带保留'])
  await expect(sheet).not.toContainText(gone)
  expect(mock.errors).toEqual([])
})

test('R2-11 筛选换成五种可信度，传给接口；清掉筛选恢复', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, f.current, f.retired)
  await open(page)
  const filter = page.getByRole('radiogroup', { name: '可信度' })
  await expect(filter.getByRole('radio')).toHaveText(['都看', '你说的', '多次说过', '带保留', '转述', '推断'])
  await filter.getByRole('radio', { name: '转述' }).click()
  await expect.poll(() => lastQuery(mock, 'trust')).toBe('reported')
  expect(lastQuery(mock, 'epistemic')).toBeNull()
  await expect(page.locator('.mem-entry')).toHaveCount(1)
  await expect(page.getByText('林栖说云岫镇的市集下个月要涨摊位费', { exact: true })).toBeVisible()
  expect(new URL(page.url()).searchParams.get('trust')).toBe('reported')
  await page.getByRole('button', { name: '清掉筛选' }).click()
  await expect(page.locator('.mem-entry')).toHaveCount(5)
  expect(lastQuery(mock, 'trust')).toBeNull()
  expect(mock.errors).toEqual([])
})

test('R2-14 有一个入口看已被替代或合并的；每条写明被哪一条替代；恢复后回到现在的记忆，可以撤销', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, f.current, f.retired)
  await open(page)
  await page.getByRole('button', { name: '看已被替代或合并的' }).click()
  await expect.poll(() => lastQuery(mock, 'retired')).toBe('1')
  expect(new URL(page.url()).searchParams.get('retired')).toBe('1')
  await expect(page.getByRole('status', { name: '已被替代或合并的' })).toContainText('已被替代或合并的记忆')
  await expect(page.locator('.mem-entry')).toHaveCount(3)
  // The filters for current memories do not apply here.
  await expect(page.getByRole('radiogroup', { name: '可信度' })).toHaveCount(0)

  const old = card(page, '遮阳网周三装')
  await expect(old).toContainText('被后来的这条替代：遮阳网的安装改到周五')
  await expect(card(page, '周五装遮阳网')).toContainText('和这条说的是一回事，已经并进去：遮阳网的安装改到周五')

  await old.getByRole('button', { name: '恢复', exact: true }).click()
  await expect.poll(() => mock.commands.at(-1)).toMatchObject({ type: 'restoreMemory', id: f.wednesday.id })
  await expect(page.locator('.mem-entry')).toHaveCount(2)
  await expect(page.getByText('遮阳网周三装', { exact: true })).toHaveCount(0)
  // Restoring is not something to confirm first; it can be taken back.
  await expect(page.getByRole('dialog')).toHaveCount(0)
  const toast = page.locator('.toast').filter({ hasText: '恢复了' })
  await toast.getByRole('button', { name: '撤销' }).click()
  await expect.poll(() => mock.undone.length).toBe(1)
  await expect(page.locator('.mem-entry')).toHaveCount(3)
  await expect(page.getByText('遮阳网周三装', { exact: true })).toBeVisible()

  await card(page, '遮阳网周三装').getByRole('button', { name: '恢复', exact: true }).click()
  await expect(page.locator('.mem-entry')).toHaveCount(2)
  await page.getByRole('button', { name: '回到现在的记忆' }).click()
  await expect(page.locator('.mem-entry')).toHaveCount(6)
  await expect(page.getByText('遮阳网周三装', { exact: true })).toBeVisible()
  expect(mock.errors).toEqual([])
})

test('R2-14 有并入的卡片显示「合并了 N 条」，点开看到并入的那几条和各自的原话，并错的能恢复', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, f.current, f.retired)
  await open(page)
  const chip = card(page, f.friday.text).getByRole('button', { name: '合并了 2 条' })
  await expect(page.getByRole('button', { name: /合并了/ })).toHaveCount(1)
  await chip.click()
  const sheet = page.getByRole('dialog')
  const section = sheet.getByRole('group', { name: '合并了 2 条' })
  await expect(section).toBeInViewport()
  await expect.poll(() => mock.queries.some((q) => q.searchParams.get('retiredBy') === f.friday.id && q.searchParams.get('retired') === '1')).toBeTruthy()
  const rows = section.getByRole('listitem')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0)).toContainText('遮阳网安装挪到周五了')
  await expect(rows.nth(0)).toContainText('那个遮阳网，挪到周五装吧')
  await expect(rows.nth(1)).toContainText('周五装遮阳网')
  await expect(rows.nth(1)).toContainText('周五装遮阳网，别忘了')
  // The one replaced, not merged, is not listed among them.
  await expect(section).not.toContainText('遮阳网周三装')

  await rows.nth(1).getByRole('button', { name: '恢复', exact: true }).click()
  await expect.poll(() => mock.commands.at(-1)).toMatchObject({ type: 'restoreMemory', id: f.merged[1].id })
  await expect(rows).toHaveCount(1)
  await sheet.getByRole('button', { name: '关闭' }).click()
  await expect(page.locator('.mem-entry')).toHaveCount(6)
  expect(mock.errors).toEqual([])
})

test('R2-14 已退出的记忆点开，详情里写明被哪条替代，能跳到那一条，也能恢复', async ({ page }) => {
  const f = fixture()
  const mock = await backend(page, f.current, f.retired)
  await open(page, '&retired=1')
  await page.getByText('遮阳网周三装', { exact: true }).click()
  const sheet = page.getByRole('dialog')
  await expect(sheet.locator('.mem-retired')).toContainText('被后来的这条替代：遮阳网的安装改到周五')
  await expect(sheet.locator('.mem-retired').getByRole('button', { name: '恢复', exact: true })).toBeVisible()
  await sheet.locator('.mem-retired').getByRole('button', { name: '遮阳网的安装改到周五' }).click()
  await expect(sheet.getByRole('textbox', { name: '内容' })).toHaveValue('遮阳网的安装改到周五')
  await expect(sheet.locator('.mem-retired')).toHaveCount(0)
  expect(mock.errors).toEqual([])
})

test('没有可信度的旧数据不显示标签；没有被替代或合并的时说清楚；手机宽度不撑破', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const plain = memory('旧数据里的一条记忆')
  const long = memory('被替代的这条写得特别长'.repeat(6), { trust: 'stated', retired: 'superseded', retiredBy: plain.id })
  const retired = [long]
  const mock = await backend(page, [plain, memory('并了很多条的记忆', { trust: 'repeated', mergedFrom: 12 })], retired)
  await open(page)
  await expect(card(page, plain.text).locator('.tag')).toHaveCount(0)
  await expect(page.getByRole('radiogroup', { name: '可信度' })).toBeVisible()
  expect(await fits(page)).toBeTruthy()
  await page.getByRole('button', { name: '看已被替代或合并的' }).click()
  await expect(page.locator('.mem-entry')).toHaveCount(1)
  expect(await fits(page)).toBeTruthy()
  await page.locator('.mem-entry').getByRole('button', { name: '恢复', exact: true }).click()
  await expect(page.getByText('没有被替代或合并的记忆。')).toBeVisible()
  expect(mock.errors).toEqual([])
})

// Not a check: writes the pictures handed over with the work. Run with PCAS_SHOT_DIR set.
test('截图', async ({ page }) => {
  const dir = process.env.PCAS_SHOT_DIR
  test.skip(!dir, 'PCAS_SHOT_DIR 没设')
  for (const [name, width, height] of [['desktop', 1280, 900], ['phone', 390, 1100]] as const) {
    const f = fixture()
    await page.unrouteAll()
    await backend(page, f.current, f.retired)
    await page.setViewportSize({ width, height })
    await open(page)
    await expect(page.locator('.mem-entry')).toHaveCount(5)
    await page.screenshot({ path: `${dir}/trust-${name}.png`, fullPage: true })
    await open(page, '&retired=1')
    await expect(page.locator('.mem-entry')).toHaveCount(3)
    await page.screenshot({ path: `${dir}/trust-retired-${name}.png`, fullPage: true })
    await open(page, `&m=${f.friday.id}&merged=1`)
    await expect(page.getByRole('dialog').getByRole('group', { name: '合并了 2 条' }).getByRole('listitem')).toHaveCount(2)
    // The sheet slides in; the picture waits for it to settle.
    await page.waitForTimeout(600)
    await page.screenshot({ path: `${dir}/trust-merged-${name}.png` })
  }
})
