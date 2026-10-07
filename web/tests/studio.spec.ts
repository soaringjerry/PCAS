import { test, expect, type Page } from '@playwright/test'
import type { DocDiff, DocVersion, Handover } from '../src/domain/studio'
import type { Run, State } from '../src/domain/types'
import type { Action } from '../src/store/actions'

// The studio (a project's page) in phase 3, batch 1: the status block, document
// versions and what changed between two. Every API call stays in the browser
// mock, and everything in it is made up.

type Command = Action & { requestId: string; expectedRevision: number }

const MIN = 60 * 1000
const HOUR = 60 * MIN
const ago = (ms: number) => new Date(Date.now() - ms).toISOString()

function run(): Run {
  return {
    id: 'run-1', thingId: 'project', agentId: 'model', kind: 'draft', prompt: '把预算部分改保守一点', brief: '', contextMemoryIds: [],
    status: 'done', output: '预算按保守口径重算了。', staleContext: false, cost: 0, createdAt: ago(2 * HOUR), finishedAt: ago(2 * HOUR),
    adopted: { as: 'doc', at: ago(2 * HOUR), edited: false, actionId: 'adopt-1', auto: true },
    used: { documents: [{ id: 'doc', version: 4 }], handoverWrittenAt: ago(5 * HOUR) },
  }
}

function workspace(): State {
  const at = ago(3 * 24 * HOUR)
  return {
    version: 1, revision: 1, budgetUsage: 0, notices: [],
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00', timezone: 'Asia/Shanghai' },
    tasks: [
      { id: 'quote', title: '等印厂回报价', notes: '', status: 'waiting', waitingFor: '印厂', projectId: 'project', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: at, updatedAt: at },
    ],
    ideas: [],
    projects: [
      // The retired fields are still sent; nothing on screen may come from them.
      { id: 'project', name: '画册', goal: '十一月前印出第一批画册', status: 'active', progress: '旧的进展文字', nextSteps: ['旧的下一步'], handoverNext: '周五前确认纸张', updatedAt: at },
      { id: 'bare', name: '搬家', goal: '月底前搬完', status: 'active', progress: '旧的进展文字', nextSteps: ['旧的下一步'], updatedAt: at },
    ],
    agents: [{ id: 'model', name: 'GPT', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false }],
    docs: [{ id: 'doc', thingId: 'project', title: '印刷方案', body: '# 印刷方案\n\n纸张用 157 克铜版纸。\n\n预算一万二。\n\n月底交付。', by: 'ai', createdAt: at, updatedAt: ago(HOUR) }],
    runs: [run()],
    memories: [], candidates: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {},
  }
}

function handover(patch: Partial<Handover> = {}): Handover {
  return {
    writtenAt: ago(3 * HOUR),
    stale: false,
    conclusion: [{ text: '方案定了第五版，预算一万二。', evidence: [{ kind: 'document', id: 'doc', version: 5 }, { kind: 'run', id: 'run-1' }] }],
    blockers: [{ text: '印厂还没回报价。', evidence: [{ kind: 'item', id: 'quote' }, { kind: 'memory', id: 'm-1', text: '印厂说周三回报价', at: ago(2 * 24 * HOUR), sourceId: 'src-1', sourceVersion: 1, excerpt: '周三给你回报价' }] }],
    nextSteps: [{ text: '周五前确认纸张。', evidence: [{ kind: 'item', id: 'quote' }] }, { text: '报价到了就定印量。', evidence: [{ kind: 'item', id: 'quote' }] }],
    ...patch,
  }
}

/** Five versions: the user saved three times in one sitting, then the assistant revised version 3, then the user again. */
function versions(): DocVersion[] {
  return [
    { version: 5, writtenAt: ago(HOUR), author: 'user', basedOn: 4 },
    { version: 4, writtenAt: ago(2 * HOUR), author: 'deputy', basedOn: 2, runId: 'run-1' },
    { version: 3, writtenAt: ago(26 * HOUR), author: 'user', basedOn: 2 },
    { version: 2, writtenAt: ago(26 * HOUR + 5 * MIN), author: 'user', basedOn: 1 },
    { version: 1, writtenAt: ago(26 * HOUR + 9 * MIN), author: 'user' },
  ]
}

function diff(from: number, to: number): DocDiff {
  if (from === 4 && to === 5) {
    return { from, to, blocks: [
      { op: 'same', text: '纸张用 157 克铜版纸。' },
      { op: 'changed', before: '预算一万五。', after: '预算一万二。' },
      { op: 'removed', text: '加急费另算。' },
      { op: 'added', text: '月底交付。' },
    ] }
  }
  if (from === to) return { from, to, blocks: [{ op: 'same', text: '纸张用 157 克铜版纸。' }] }
  return { from, to, blocks: [{ op: 'same', text: '纸张用 157 克铜版纸。' }, { op: 'added', text: `第 ${to} 版比第 ${from} 版多的一段。` }] }
}

interface Backend {
  commands: Command[]
  errors: string[]
  requests: string[]
  snapshot: State
  handover: Handover | null | 'broken'
}

async function mockBackend(page: Page, start: Partial<Pick<Backend, 'handover' | 'snapshot'>> = {}): Promise<Backend> {
  const backend: Backend = { commands: [], errors: [], requests: [], snapshot: start.snapshot ?? workspace(), handover: start.handover === undefined ? handover() : start.handover }
  page.on('pageerror', (e) => backend.errors.push(e.message))
  await page.route('**/v1/**', (route) => {
    backend.errors.push(`unexpected request ${route.request().url()}`)
    return route.fulfill({ status: 500, json: { error: 'unexpected_mock_request' } })
  })
  await page.route((url) => url.pathname === '/v1/workspace', (route) => route.fulfill({ json: backend.snapshot }))
  await page.route((url) => url.pathname === '/v1/desk/turns', (route) => route.fulfill({ json: { turns: [] } }))
  await page.route((url) => /^\/v1\/workspace\/projects\/[^/]+\/handover$/.test(url.pathname), (route) => {
    const id = new URL(route.request().url()).pathname.split('/')[4]
    if (backend.handover === 'broken') return route.fulfill({ status: 503, json: { error: 'unavailable' } })
    return route.fulfill({ json: { handover: id === 'project' ? backend.handover : null } })
  })
  await page.route((url) => url.pathname === '/v1/workspace/documents/doc/versions', (route) => route.fulfill({ json: { versions: versions() } }))
  await page.route((url) => /^\/v1\/workspace\/documents\/doc\/versions\/\d+$/.test(url.pathname), (route) => {
    const version = Number(new URL(route.request().url()).pathname.split('/').at(-1))
    return route.fulfill({ json: { version, body: `这是第 ${version} 版的正文。` } })
  })
  await page.route((url) => url.pathname === '/v1/workspace/documents/doc/diff', (route) => {
    const q = new URL(route.request().url()).searchParams
    backend.requests.push(`diff ${q.get('from')}→${q.get('to')}`)
    return route.fulfill({ json: diff(Number(q.get('from')), Number(q.get('to'))) })
  })
  await page.route((url) => url.pathname === '/v1/memory/sources/src-1', (route) =>
    route.fulfill({ json: { derived: [], processing: [], source: { id: 'src-1', version: 1, title: '和印厂的通话', text: '我们这边排期满了，周三给你回报价，纸张你们先定。', recorded_at: ago(2 * 24 * HOUR), has_attachment: false, attachment_missing: false, representation: 'original' } } }),
  )
  await page.route((url) => url.pathname === '/v1/workspace/commands', (route) => {
    backend.commands.push(route.request().postDataJSON() as Command)
    backend.snapshot = { ...backend.snapshot, revision: backend.snapshot.revision + 1 }
    return route.fulfill({ json: backend.snapshot })
  })
  return backend
}

const status = (page: Page) => page.getByRole('region', { name: '现状' })
const part = (page: Page, name: string) => status(page).getByRole('group', { name, exact: true })

test('the status block leads the studio: three parts, when it was written, and nothing to edit', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  await expect(status(page)).toContainText('写于 3 小时前')
  await expect(part(page, '结论')).toContainText('方案定了第五版，预算一万二。')
  await expect(part(page, '卡点')).toContainText('印厂还没回报价。')
  await expect(part(page, '下一步').getByRole('listitem')).toHaveCount(2)
  // It comes before everything the project holds.
  const order = await page.locator('.doc-page > section').evaluateAll((all) => all.map((s) => s.getAttribute('aria-label') ?? s.querySelector('.section-label')?.textContent ?? ''))
  expect(order[0]).toBe('现状')
  // Its text is the model's: no field, no editable box, no pencil.
  await expect(status(page).locator('input, textarea, [contenteditable], [role="textbox"]')).toHaveCount(0)
  await expect(status(page).getByRole('button', { name: /改|编辑|保存/ })).toHaveCount(0)
  // The retired fields are sent but not shown, and no fact offers to update progress.
  await expect(page.getByText('旧的进展文字')).toHaveCount(0)
  await expect(page.getByText('旧的下一步')).toHaveCount(0)
  await expect(page.locator('.info-line')).not.toContainText('进度')
  expect(backend.commands).toEqual([])
  expect(backend.errors).toEqual([])
})

test('each sentence opens onto what it rests on: a memory to the words said, an item to the item', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  const sentence = part(page, '卡点').getByRole('button', { name: '印厂还没回报价。' })
  await expect(sentence).toHaveAttribute('aria-expanded', 'false')
  await sentence.click()
  const evidence = part(page, '卡点').getByRole('list', { name: '依据' })
  await expect(evidence.getByRole('listitem')).toHaveCount(2)
  // The memory opens the original at the passage it came from.
  await evidence.getByRole('button', { name: /印厂说周三回报价/ }).click()
  const sheet = page.getByRole('dialog')
  await expect(sheet).toContainText('和印厂的通话')
  await expect(sheet.locator('mark')).toHaveText('周三给你回报价')
  await page.keyboard.press('Escape')
  await expect(sheet).toHaveCount(0)
  // The item goes to the item.
  await evidence.getByRole('link', { name: /等印厂回报价/ }).click()
  await expect(page).toHaveURL(/\/t\/quote$/)
  await expect(page.getByRole('textbox', { name: '标题' })).toHaveValue('等印厂回报价')
  expect(backend.errors).toEqual([])
})

test('a document version in the evidence opens that version; a piece of work opens its record', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  await part(page, '结论').getByRole('button', { name: /方案定了第五版/ }).click()
  const evidence = part(page, '结论').getByRole('list', { name: '依据' })
  await evidence.getByRole('link', { name: /印刷方案.*第 5 版/ }).click()
  await expect(page).toHaveURL(/doc=doc&v=5/)
  const doc = page.locator('.doc-row')
  await expect(doc.getByRole('button', { name: /第 5 版/ })).toHaveAttribute('aria-pressed', 'true')
  await expect(doc.getByRole('button', { name: /第 4 版/ })).toHaveAttribute('aria-pressed', 'true')
  await expect(doc.locator('.diff')).toBeVisible()

  await page.goto('/t/project')
  await part(page, '结论').getByRole('button', { name: /方案定了第五版/ }).click()
  await part(page, '结论').getByRole('link', { name: /把预算部分改保守一点/ }).click()
  await expect(page).toHaveURL(/run=run-1/)
  const record = page.locator('.activity > li.asked')
  await expect(record).toContainText('已保存为文档')
  // The record says what the work was done from.
  await expect(record.locator('.act-used')).toContainText('「印刷方案」第 4 版')
  await expect(record.locator('.act-used')).toContainText('的现状')
  await expect(record).toBeInViewport()
  expect(backend.errors).toEqual([])
})

test('while a new one is being written the last one stays, and says so', async ({ page }) => {
  await mockBackend(page, { handover: handover({ stale: true }) })
  await page.goto('/t/project')
  await expect(status(page).getByRole('status')).toHaveText('正在更新，下面是上一份')
  await expect(status(page)).toContainText('写于 3 小时前')
  await expect(part(page, '结论')).toContainText('方案定了第五版')
})

test('with no handover the block says there is none, and puts nothing in its place', async ({ page }) => {
  const backend = await mockBackend(page, { handover: null })
  await page.goto('/t/project')
  await expect(status(page)).toContainText('还没有现状。')
  await expect(status(page).getByRole('group')).toHaveCount(0)
  await expect(status(page)).not.toContainText('写于')
  expect(backend.errors).toEqual([])
})

test('a part the model left empty is shown as empty', async ({ page }) => {
  await mockBackend(page, { handover: handover({ blockers: [] }) })
  await page.goto('/t/project')
  await expect(part(page, '卡点')).toContainText('这一段没有内容')
  await expect(part(page, '卡点').getByRole('button')).toHaveCount(0)
})

test('a handover that cannot be read says so and can be read again; a later failure keeps what is shown', async ({ page }) => {
  const backend = await mockBackend(page, { handover: 'broken' })
  await page.goto('/t/project')
  await expect(status(page).getByRole('alert')).toContainText('现状没读出来')
  backend.handover = handover()
  await status(page).getByRole('button', { name: '再读一次' }).click()
  await expect(part(page, '结论')).toContainText('方案定了第五版')
  // The workspace moves on and the next read fails: the block keeps the one it has.
  backend.handover = 'broken'
  backend.snapshot = { ...backend.snapshot, revision: backend.snapshot.revision + 1 }
  await page.waitForResponse((r) => r.url().includes('/handover') && r.status() === 503)
  await expect(part(page, '结论')).toContainText('方案定了第五版')
  await expect(status(page).getByRole('alert')).toHaveCount(0)
})

test('a change in the workspace reads the handover again', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  await expect(part(page, '卡点')).toContainText('印厂还没回报价。')
  backend.handover = handover({ writtenAt: ago(MIN), blockers: [{ text: '报价到了，等你定印量。', evidence: [{ kind: 'item', id: 'quote' }] }] })
  backend.snapshot = { ...backend.snapshot, revision: backend.snapshot.revision + 1 }
  await expect(part(page, '卡点')).toContainText('报价到了，等你定印量。', { timeout: 10_000 })
  await expect(part(page, '卡点')).not.toContainText('印厂还没回报价。')
})

test('a document lists its versions, with one sitting of saves folded into a line that opens', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  const doc = page.locator('.doc-row')
  await doc.getByRole('button', { name: /印刷方案/ }).first().click()
  await doc.getByRole('button', { name: '版本', exact: true }).click()
  const list = doc.getByRole('list', { name: '「印刷方案」的版本' })
  // 5 by the user, 4 by the assistant, and 1–3 saved within fifteen minutes as one line.
  await expect(list.getByRole('listitem')).toHaveCount(3)
  await expect(list.getByRole('listitem').nth(0)).toContainText('第 5 版')
  await expect(list.getByRole('listitem').nth(0)).toContainText('你')
  await expect(list.getByRole('listitem').nth(1)).toContainText('第 4 版')
  await expect(list.getByRole('listitem').nth(1)).toContainText('副手')
  await expect(list.getByRole('listitem').nth(1)).toContainText('基于第 2 版')
  await expect(list.getByRole('listitem').nth(1).getByRole('link', { name: '看这次工作' })).toHaveAttribute('href', /run=run-1/)
  await expect(list.getByRole('listitem').nth(2)).toContainText('第 1–3 版')
  await list.getByRole('button', { name: '连着改了 3 次' }).click()
  await expect(list.getByRole('listitem')).toHaveCount(5)
  await expect(list.getByRole('listitem').nth(4)).toContainText('第 1 版')
  await list.getByRole('button', { name: '收起' }).click()
  await expect(list.getByRole('listitem')).toHaveCount(3)
  expect(backend.errors).toEqual([])
})

test('the latest is compared with the one before it; any two can be picked; only three marks are used', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  const doc = page.locator('.doc-row')
  await doc.getByRole('button', { name: /印刷方案/ }).first().click()
  await doc.getByRole('button', { name: '版本', exact: true }).click()
  const diffBox = doc.locator('.diff')
  await expect(doc).toContainText('第 4 版 → 第 5 版')
  await expect(diffBox.locator('.diff-block.changed')).toContainText('预算一万五。')
  await expect(diffBox.locator('.diff-block.changed')).toContainText('预算一万二。')
  await expect(diffBox.locator('.diff-block.removed')).toContainText('加急费另算。')
  await expect(diffBox.locator('.diff-block.added')).toContainText('月底交付。')
  await expect(diffBox.locator('.diff-mark')).toHaveText(['改', '删', '增'])
  expect(backend.requests).toEqual(['diff 4→5'])
  // Picking the folded line takes its newest version; the pair is always the last two touched.
  await doc.getByRole('button', { name: /第 1–3 版/ }).click()
  await expect(doc).toContainText('第 3 版 → 第 5 版')
  await doc.getByRole('button', { name: '连着改了 3 次' }).click()
  await doc.getByRole('button', { name: /^第 1 版/ }).click()
  await expect(doc).toContainText('第 1 版 → 第 3 版')
  await expect(diffBox).toContainText('第 3 版比第 1 版多的一段。')
  await expect(doc.locator('.ver-pick[aria-pressed="true"]')).toHaveCount(2)
  // Letting one go leaves a single version, which is read as it was.
  await doc.getByRole('button', { name: /^第 3 版/ }).click()
  await expect(doc).toContainText('再点一版看差异')
  await expect(diffBox).toContainText('这是第 1 版的正文。')
  // Closing the versions gives the document back, still edited in place.
  await doc.getByRole('button', { name: '收起版本' }).click()
  await expect(doc.getByRole('textbox', { name: '印刷方案 的内容' })).toContainText('预算一万二')
  expect(backend.commands).toEqual([])
  expect(backend.errors).toEqual([])
})

test('a project card in the hall shows the first next step, or the goal when no handover is written', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/')
  const cards = page.locator('.hall-card')
  await expect(cards.filter({ hasText: '画册' })).toContainText('下一步：周五前确认纸张')
  await expect(cards.filter({ hasText: '画册' })).not.toContainText('十一月前印出第一批画册')
  await expect(cards.filter({ hasText: '搬家' })).toContainText('月底前搬完')
  await expect(cards.filter({ hasText: '搬家' })).not.toContainText('下一步')
  await expect(page.getByText('旧的进展文字')).toHaveCount(0)
  await expect(page.getByText('旧的下一步')).toHaveCount(0)
  expect(backend.errors).toEqual([])
})

test('a summary kept on a project goes into its memory, not into a progress field', async ({ page }) => {
  const snapshot = workspace()
  snapshot.runs = [{ ...run(), kind: 'summary', prompt: '总结一下这周', output: '这周定了纸张。', adopted: undefined, used: undefined }]
  const backend = await mockBackend(page, { snapshot })
  await page.goto('/t/project')
  const row = page.locator('.activity > li').filter({ hasText: '总结一下这周' })
  await expect(row.getByRole('button', { name: '记进项目记忆' })).toBeVisible()
  await expect(page.getByRole('button', { name: '更新项目进度' })).toHaveCount(0)
  expect(backend.errors).toEqual([])
})

test('on a phone the studio does not scroll sideways, with evidence and a comparison open', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockBackend(page)
  await page.goto('/t/project')
  await part(page, '卡点').getByRole('button', { name: '印厂还没回报价。' }).click()
  const doc = page.locator('.doc-row')
  await doc.getByRole('button', { name: /印刷方案/ }).first().click()
  await doc.getByRole('button', { name: '版本', exact: true }).click()
  await expect(doc.locator('.diff-block.changed')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)
})
