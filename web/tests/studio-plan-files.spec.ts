import { test, expect, type Page } from '@playwright/test'
import type { ProjectFile, ProjectTimeline, TimelineItem } from '../src/domain/studio'
import type { State, Task } from '../src/domain/types'
import type { Action } from '../src/store/actions'

// The studio in phase 3, batch 2: the plan timeline and the files kept with a
// project. Every API call stays in the browser mock, and everything is made up.

type Command = Action & { requestId: string; expectedRevision: number }

const HOUR = 60 * 60 * 1000
const DAY = 24 * HOUR
const ZONE = 'Asia/Shanghai'
const ago = (ms: number) => new Date(Date.now() - ms).toISOString()
/** The calendar day `days` from today in the workspace zone, as the server sends start days. */
const day = (days: number) => new Intl.DateTimeFormat('en-CA', { timeZone: ZONE, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(Date.now() + days * DAY))
/** Noon of that day in the workspace zone (UTC+8, no daylight saving). */
const noon = (days: number) => `${day(days)}T04:00:00.000Z`

function task(id: string, title: string, patch: Partial<Task> = {}): Task {
  const at = ago(3 * DAY)
  return { id, title, notes: '', status: 'todo', projectId: 'project', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: at, updatedAt: at, ...patch }
}

function workspace(): State {
  const at = ago(3 * DAY)
  return {
    version: 1, revision: 1, budgetUsage: 0, notices: [],
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00', timezone: ZONE },
    tasks: [
      task('layout', '排版定稿', { due: noon(3), estimatedHours: 8, startDate: day(-2), effortSource: 'model' }),
      task('paper', '选纸', { due: noon(6) }),
      task('photos', '修图', { status: 'done', due: noon(-4), estimatedHours: 6, startDate: day(-6) }),
      task('quote', '催印厂报价', { due: noon(-1), estimatedHours: 2, startDate: day(-1) }),
      task('fair', '报名书展', { status: 'cancelled', due: noon(5), estimatedHours: 4, startDate: day(4) }),
      task('name', '想个书名'),
    ],
    ideas: [],
    projects: [{ id: 'project', name: '画册', goal: '十一月前印出第一批画册', status: 'active', updatedAt: at }],
    agents: [{ id: 'model', name: 'GPT', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false }],
    docs: [], runs: [], memories: [], candidates: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {},
  }
}

function timeline(state: State, extra: TimelineItem[] = []): ProjectTimeline {
  const dated = state.tasks.filter((t) => t.due)
  return {
    projectId: 'project',
    dailyHours: 4,
    items: [
      ...dated.map((t) => ({ id: t.id, title: t.title, startDate: t.startDate ?? null, due: t.due!, status: t.status, estimatedHours: t.estimatedHours ?? null, overdue: t.id === 'quote' })),
      ...extra,
    ],
    withoutDue: state.tasks.filter((t) => !t.due),
  }
}

function file(sourceId: string, name: string, mediaType: string, patch: Partial<ProjectFile> = {}): ProjectFile {
  return { sourceId, projectId: 'project', name, mediaType, size: 2048, createdAt: ago(HOUR), status: 'extracted', failureReason: '', jobId: null, openUrl: `/v1/memory/sources/${sourceId}/attachment?inline=true`, ...patch }
}

function files(): ProjectFile[] {
  return [
    file('f-cover', '封面.png', 'image/png', { size: 340 * 1024 }),
    file('f-quote', '报价单.pdf', 'application/pdf', { status: 'stored', size: 3 * 1024 * 1024 }),
    file('f-scan', '合同扫描.tiff', 'image/tiff', { status: 'failed', failureReason: '图片太模糊，读不出字', jobId: 'job-scan' }),
    file('f-sheet', '成本表.xlsx', 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'),
  ]
}

// A 1×1 PNG, so the in-page viewer has something real to show.
const PIXEL = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==', 'base64')

interface Backend {
  commands: Command[]
  errors: string[]
  uploads: string[]
  deletes: string[]
  snapshot: State
  files: ProjectFile[]
  timeline: ProjectTimeline
  /** What the next upload answers with instead of storing the file. */
  refuse?: { status: number; error: string }
}

async function mockBackend(page: Page, start: Partial<Pick<Backend, 'timeline'>> = {}): Promise<Backend> {
  const snapshot = workspace()
  const backend: Backend = { commands: [], errors: [], uploads: [], deletes: [], snapshot, files: files(), timeline: start.timeline ?? timeline(snapshot) }
  const bump = () => (backend.snapshot = { ...backend.snapshot, revision: backend.snapshot.revision + 1 })
  page.on('pageerror', (e) => backend.errors.push(e.message))
  await page.route('**/v1/**', (route) => {
    backend.errors.push(`unexpected request ${route.request().method()} ${route.request().url()}`)
    return route.fulfill({ status: 500, json: { error: 'unexpected_mock_request' } })
  })
  await page.route((url) => url.pathname === '/v1/workspace', (route) => route.fulfill({ json: backend.snapshot }))
  await page.route((url) => url.pathname === '/v1/desk/turns', (route) => route.fulfill({ json: { turns: [] } }))
  await page.route((url) => url.pathname === '/v1/workspace/projects/project/handover', (route) =>
    route.fulfill({ json: { projectId: 'project', writtenAt: null, stale: false, conclusion: [], blockers: [], nextSteps: [] } }),
  )
  await page.route((url) => url.pathname === '/v1/workspace/projects/project/timeline', (route) => route.fulfill({ json: backend.timeline }))
  await page.route((url) => url.pathname === '/v1/workspace/items/project/files', async (route) => {
    if (route.request().method() === 'GET') return route.fulfill({ json: { items: backend.files } })
    // The multipart body names the file; that is all the mock needs from it.
    const name = /filename="([^"]+)"/.exec(route.request().postDataBuffer()?.toString('utf8') ?? '')?.[1] ?? ''
    backend.uploads.push(name)
    if (backend.refuse) return route.fulfill({ status: backend.refuse.status, json: { error: backend.refuse.error } })
    const known = backend.files.find((f) => f.name === name)
    if (known) return route.fulfill({ status: 200, json: { file: known, alreadyExists: true } })
    const stored = file(`f-${backend.files.length}`, name, name.endsWith('.png') ? 'image/png' : 'text/plain', { status: 'stored', createdAt: new Date().toISOString() })
    backend.files = [stored, ...backend.files]
    bump()
    return route.fulfill({ status: 201, json: { file: stored, alreadyExists: false } })
  })
  await page.route((url) => /^\/v1\/workspace\/items\/project\/files\/[^/]+$/.test(url.pathname), (route) => {
    const url = new URL(route.request().url())
    backend.deletes.push(`${route.request().method()} ${url.pathname.split('/').at(-1)} confirmed=${url.searchParams.get('confirmed')}`)
    backend.files = backend.files.filter((f) => f.sourceId !== url.pathname.split('/').at(-1))
    bump()
    return route.fulfill({ json: { deleted: true } })
  })
  await page.route((url) => /^\/v1\/memory\/sources\/[^/]+\/attachment$/.test(url.pathname), (route) => route.fulfill({ contentType: 'image/png', body: PIXEL }))
  await page.route((url) => url.pathname === '/v1/workspace/commands', (route) => {
    const body = route.request().postDataJSON() as Command
    backend.commands.push(body)
    if (body.type === 'retryJob') backend.files = backend.files.map((f) => (f.jobId === body.id ? { ...f, status: 'stored', failureReason: '' } : f))
    return route.fulfill({ json: bump() })
  })
  return backend
}

const plan = (page: Page) => page.getByRole('region', { name: '计划' })
const row = (page: Page, title: string) => plan(page).locator('.plan-row').filter({ hasText: title })
const fileArea = (page: Page) => page.getByRole('region', { name: '文件' })
const fileRow = (page: Page, name: string) => fileArea(page).locator('.file-row').filter({ hasText: name })
const secretaryInput = (page: Page) => page.getByRole('textbox', { name: '跟秘书说' })
const box = async (page: Page, selector: string, within = plan(page)) => (await within.locator(selector).first().boundingBox())!
const sideways = (page: Page) => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)

test('the plan draws each dated item from its start day to its due day, with a line at now', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  await expect(plan(page).locator('.plan-row')).toHaveCount(5)
  // Started two days ago, due in three: now falls inside its bar.
  const bar = (await row(page, '排版定稿').locator('.plan-bar').boundingBox())!
  const now = await box(page, '.plan-now')
  expect(now.x).toBeGreaterThan(bar.x)
  expect(now.x).toBeLessThan(bar.x + bar.width)
  await expect(plan(page).locator('.plan-now')).toHaveText('现在')
  // Six whole days, start to due, out of the axis's days.
  const axis = await box(page, '.plan-axis')
  const days = await plan(page).locator('.plan-tick').count()
  expect(bar.width / (axis.width / days)).toBeCloseTo(6, 1)
  // Done and overdue are marked; a cancelled one stays on the axis, struck through.
  await expect(row(page, '修图')).toHaveClass(/done/)
  await expect(row(page, '修图')).toContainText('已完成')
  await expect(row(page, '催印厂报价')).toHaveClass(/late/)
  await expect(row(page, '催印厂报价')).toContainText('已过截止')
  await expect(row(page, '报名书展')).toHaveClass(/cancelled/)
  await expect(row(page, '报名书展')).toContainText('已取消')
  // The overdue bar ends left of now; the done one before that.
  const late = (await row(page, '催印厂报价').locator('.plan-bar').boundingBox())!
  expect(late.x + late.width).toBeLessThan(now.x)
  // With no estimate there is no start day: only the due day is drawn.
  await expect(row(page, '选纸').locator('.plan-bar')).toHaveClass(/due-only/)
  const dueOnly = (await row(page, '选纸').locator('.plan-bar').boundingBox())!
  expect(dueOnly.width / (axis.width / days)).toBeCloseTo(1, 1)
  // What has no due time is not on the axis; it is listed apart.
  await expect(plan(page).locator('.plan-row').filter({ hasText: '想个书名' })).toHaveCount(0)
  await expect(plan(page).getByRole('group', { name: '没定截止的' }).getByRole('link', { name: '想个书名' })).toHaveAttribute('href', '/t/name')
  expect(backend.errors).toEqual([])
})

test('effort reads 约 n 小时 and is changed by telling the secretary; a bar goes to its item', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  await row(page, '排版定稿').getByRole('button', { name: '约 8 小时' }).click()
  await expect(secretaryInput(page)).toHaveValue('「排版定稿」的工作量改成：')
  await expect(secretaryInput(page)).toBeFocused()
  // An open item without an estimate offers to be given one, the same way.
  await row(page, '选纸').getByRole('button', { name: '说个工作量' }).click()
  await expect(secretaryInput(page)).toHaveValue('「选纸」大概要做：')
  await expect(row(page, '修图').getByRole('button', { name: '约 6 小时' })).toBeVisible()
  // There is no field, picker or slider for hours or dates anywhere in the plan.
  await expect(plan(page).locator('input, select, textarea')).toHaveCount(0)
  expect(backend.commands).toEqual([])
  await row(page, '排版定稿').getByRole('link', { name: '排版定稿' }).click()
  await expect(page).toHaveURL(/\/t\/layout$/)
  // The item says the same about itself: its effort starts a sentence, its start day is only read.
  const info = page.locator('.info-line')
  await expect(info).toContainText('开工')
  await info.getByRole('button', { name: '约 8 小时' }).click()
  await expect(secretaryInput(page)).toHaveValue('这件事的工作量改成：')
  expect(backend.errors).toEqual([])
})

test('a project with dated items none yet says so and still lists the rest', async ({ page }) => {
  const snapshot = workspace()
  await mockBackend(page, { timeline: { projectId: 'project', dailyHours: 4, items: [], withoutDue: snapshot.tasks.filter((t) => !t.due) } })
  await page.goto('/t/project')
  await expect(plan(page)).toContainText('还没有定了截止的事。')
  await expect(plan(page).locator('.plan-scroll')).toHaveCount(0)
  await expect(plan(page).getByRole('link', { name: '想个书名' })).toBeVisible()
})

test('on a phone a long plan scrolls sideways inside its own box, and nothing else does', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const snapshot = workspace()
  const far: TimelineItem = { id: 'print', title: '下厂印刷', startDate: day(30), due: noon(40), status: 'todo', estimatedHours: 24, overdue: false }
  const backend = await mockBackend(page, { timeline: timeline(snapshot, [far]) })
  await page.goto('/t/project')
  const scroller = plan(page).locator('.plan-scroll')
  await expect(row(page, '下厂印刷')).toHaveCount(1)
  const { scrollWidth, clientWidth } = await scroller.evaluate((el) => ({ scrollWidth: el.scrollWidth, clientWidth: el.clientWidth }))
  expect(scrollWidth).toBeGreaterThan(clientWidth * 2)
  expect(await sideways(page)).toBeLessThanOrEqual(0)
  // It opens with now in view.
  await expect(plan(page).locator('.plan-now')).toBeInViewport()
  // The far end is reached by scrolling the box.
  await scroller.evaluate((el) => (el.scrollLeft = el.scrollWidth))
  await expect(row(page, '下厂印刷').locator('.plan-bar')).toBeInViewport()
  expect(await sideways(page)).toBeLessThanOrEqual(0)
  // The files below fit the width as well.
  await expect(fileRow(page, '合同扫描.tiff')).toContainText('图片太模糊，读不出字')
  expect(await sideways(page)).toBeLessThanOrEqual(0)
  expect(backend.errors).toEqual([])
})

test('files are listed with what became of them; a failed one says why and can be tried again', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  await expect(fileArea(page).locator('.file-row')).toHaveCount(4)
  await expect(fileRow(page, '封面.png')).toContainText('已读过')
  await expect(fileRow(page, '封面.png')).toContainText('340 KB')
  await expect(fileRow(page, '报价单.pdf')).toContainText('已存好，还没读')
  await expect(fileRow(page, '报价单.pdf')).toContainText('3 MB')
  await expect(fileRow(page, '合同扫描.tiff')).toContainText('没读成：图片太模糊，读不出字')
  // Only the failed one offers another try, in place.
  await expect(fileArea(page).getByRole('button', { name: '重试' })).toHaveCount(1)
  await fileRow(page, '合同扫描.tiff').getByRole('button', { name: '重试' }).click()
  await expect.poll(() => backend.commands.at(-1)).toMatchObject({ type: 'retryJob', id: 'job-scan' })
  await expect(fileRow(page, '合同扫描.tiff')).toContainText('已存好，还没读')
  await expect(fileArea(page).getByRole('button', { name: '重试' })).toHaveCount(0)
  expect(backend.errors).toEqual([])
})

test('a file the browser can show opens in the page; any other is downloaded', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  await fileRow(page, '封面.png').getByRole('button', { name: '封面.png', exact: true }).click()
  const sheet = page.getByRole('dialog')
  await expect(sheet.getByRole('img', { name: '封面.png' })).toHaveAttribute('src', '/v1/memory/sources/f-cover/attachment?inline=true')
  await expect(page).toHaveURL(/\/t\/project$/)
  await page.keyboard.press('Escape')
  await expect(sheet).toHaveCount(0)
  await fileRow(page, '报价单.pdf').getByRole('button', { name: '报价单.pdf', exact: true }).click()
  await expect(page.getByRole('dialog').locator('iframe')).toHaveAttribute('src', '/v1/memory/sources/f-quote/attachment?inline=true')
  await page.keyboard.press('Escape')
  // A spreadsheet is a download link, named after the file.
  const link = fileRow(page, '成本表.xlsx').getByRole('link', { name: '成本表.xlsx' })
  await expect(link).toHaveAttribute('download', '成本表.xlsx')
  await expect(link).toHaveAttribute('href', '/v1/memory/sources/f-sheet/attachment?inline=true')
  expect(backend.errors).toEqual([])
})

test('deleting a file asks first; only a yes removes it', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  await fileRow(page, '报价单.pdf').getByRole('button', { name: '删除文件：报价单.pdf' }).click()
  const dialog = page.getByRole('dialog', { name: '删除「报价单.pdf」？' })
  await expect(dialog).toContainText('找不回来')
  // Leaving it sends nothing.
  await dialog.getByRole('button', { name: '留着' }).click()
  await expect(dialog).toHaveCount(0)
  await expect(fileRow(page, '报价单.pdf')).toHaveCount(1)
  expect(backend.deletes).toEqual([])
  await fileRow(page, '报价单.pdf').getByRole('button', { name: '删除文件：报价单.pdf' }).click()
  await dialog.getByRole('button', { name: '删掉' }).click()
  await expect(fileRow(page, '报价单.pdf')).toHaveCount(0)
  expect(backend.deletes).toEqual(['DELETE f-quote confirmed=true'])
  // It is gone for good: the receipt offers no undo.
  await expect(page.locator('.toast')).toContainText('删掉了「报价单.pdf」')
  await expect(page.locator('.toast').getByRole('button', { name: '撤销' })).toHaveCount(0)
  expect(backend.errors).toEqual([])
})

test('files are chosen or dropped in; one already there is said to be there', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  await fileArea(page).getByLabel('选择文件').setInputFiles([
    { name: '会议记录.txt', mimeType: 'text/plain', buffer: Buffer.from('周三和印厂开会。') },
    { name: '封面.png', mimeType: 'image/png', buffer: PIXEL },
  ])
  await expect(fileRow(page, '会议记录.txt')).toContainText('已存好，还没读')
  await expect(page.locator('.toast')).toContainText('已经有了：封面.png')
  await expect(fileArea(page).locator('.file-row')).toHaveCount(5)
  expect(backend.uploads).toEqual(['会议记录.txt', '封面.png'])
  // Dropping a file on the area stores it the same way.
  await fileArea(page).evaluate((area) => {
    const data = new DataTransfer()
    data.items.add(new File(['纸样到了。'], '纸样说明.txt', { type: 'text/plain' }))
    area.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: data }))
    area.dispatchEvent(new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: data }))
  })
  await expect(fileRow(page, '纸样说明.txt')).toHaveCount(1)
  expect(backend.uploads).toEqual(['会议记录.txt', '封面.png', '纸样说明.txt'])
  expect(backend.errors).toEqual([])
})

test('a file that cannot be stored says which one and why, and the others still go', async ({ page }) => {
  const backend = await mockBackend(page)
  await page.goto('/t/project')
  // Too big and empty are turned down before anything is sent.
  await fileArea(page).getByLabel('选择文件').setInputFiles([
    { name: '全书高清.pdf', mimeType: 'application/pdf', buffer: Buffer.alloc(20 * 1024 * 1024 + 1) },
    { name: '空白.txt', mimeType: 'text/plain', buffer: Buffer.alloc(0) },
    { name: '附言.txt', mimeType: 'text/plain', buffer: Buffer.from('请走顺丰。') },
  ])
  await expect(fileArea(page).getByRole('alert').filter({ hasText: '全书高清.pdf' })).toHaveText('「全书高清.pdf」超过 20 MB，没有存')
  await expect(fileArea(page).getByRole('alert').filter({ hasText: '空白.txt' })).toHaveText('「空白.txt」是空的，没有存')
  await expect(fileRow(page, '附言.txt')).toHaveCount(1)
  expect(backend.uploads).toEqual(['附言.txt'])
  // What the server turns down is named with its reason.
  backend.refuse = { status: 409, error: 'reimport_blocked' }
  await fileArea(page).getByLabel('选择文件').setInputFiles([{ name: '旧合同.txt', mimeType: 'text/plain', buffer: Buffer.from('作废。') }])
  await expect(fileArea(page).getByRole('alert')).toHaveText('「旧合同.txt」没存上：这份文件删过，已经不再接收')
  await expect(fileRow(page, '旧合同.txt')).toHaveCount(0)
})
