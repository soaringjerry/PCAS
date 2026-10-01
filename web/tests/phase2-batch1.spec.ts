import { test, expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import type { State } from '../src/domain/types'

const gold = JSON.parse(readFileSync(new URL('../../testdata/phase2/b1-gold.json', import.meta.url), 'utf8'))
const text: string = gold.fixtures.trip.text
const at = '2026-09-12T09:00:00Z'
const sourceId = '11111111-1111-4111-8111-111111111111'

async function backend(page: Page, outdated: boolean, withTimeline = false) {
  const state: State = {
    version: 1, revision: 1, budgetUsage: 0,
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: false, followUps: false, dailyReviewAt: '09:00', timezone: 'Asia/Shanghai' },
    tasks: [{ id: 'task', title: '检查旅行安排', notes: '', status: 'todo', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: at, updatedAt: at }],
    ideas: [], projects: [], memories: [], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {},
    agents: [{ id: 'model', name: '验收假模型', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false }],
  }
  const turn = {
    id: 'b1-turn', text: '我去成都想吃什么', reply: '春熙路火锅，见老王。',
    cards: [
      { kind: 'sources', items: [{ kind: 'source', memoryId: sourceId, version: 1, sourceId, sourceVersion: 1, text, at }] },
      ...(withTimeline ? [{ kind: 'timeline', items: [{ at, text: '此前陈述记录', status: 'open', memoryId: 'claim', thingId: null }] }] : []),
    ],
    receipts: [{ actionId: 'b1-action', op: 'create_task', text: '已建：检查旅行安排', thingId: 'task', undoable: true, undone: false, status: 'done' }],
    ask: null, agent: '验收假模型', createdAt: at, ...(outdated ? { outdated: true } : {}),
  }
  let conversation = ''
  const commands: Record<string, unknown>[] = []
  const opened: string[] = []
  const errors: string[] = []
  page.on('pageerror', e => errors.push(e.message))
  await page.route('**/v1/**', route => route.fulfill({ status: 500, json: { error: 'unexpected_b1_request' } }))
  await page.route(url => url.pathname === '/v1/workspace', route => route.fulfill({ json: state }))
  await page.route(url => url.pathname === '/v1/desk/turn', route => {
    conversation = route.request().postDataJSON().conversationId
    return route.fulfill({ json: { conversationId: conversation, turn, state } })
  })
  await page.route(url => url.pathname === '/v1/desk/turns', route => route.fulfill({ json: { conversationId: conversation, turns: [turn] } }))
  await page.route(url => url.pathname === '/v1/workspace/commands', route => {
    const command = route.request().postDataJSON()
    commands.push(command)
    if (command.type !== 'undoAction' || command.id !== 'b1-action') return route.fulfill({ status: 400, json: { error: 'unexpected_command' } })
    turn.receipts[0].undone = true
    state.tasks = []
    state.revision++
    return route.fulfill({ json: state })
  })
  await page.route(url => url.pathname === `/v1/memory/sources/${sourceId}`, route => {
    opened.push(route.request().url())
    return route.fulfill({ json: { source: { id: sourceId, version: 1, title: '秘书原话', text, recorded_at: at, representation: 'original', has_attachment: false, attachment_missing: false }, derived: [], processing: [] } })
  })
  await page.route(url => url.pathname === '/v1/memory/summary', route => route.fulfill({ json: { text: '合成资料摘要', coverage: { gaps: [] }, dependencies: [{ id: sourceId, version: 1 }] } }))
  return { turn, commands, opened, errors }
}
async function say(page: Page) {
  await page.goto('/')
  const input = page.getByRole('textbox', { name: '跟秘书说' })
  await input.fill('我去成都想吃什么')
  await input.press('Enter')
  await expect(page.locator('.sec-reply')).toHaveText('春熙路火锅，见老王。')
}

test('P1/R8 原话依据可见并能打开正确版本原文', async ({ page }) => {
  const mock = await backend(page, false)
  await say(page)
  const source = page.getByRole('list', { name: '依据' }).getByRole('button', { name: text })
  await expect(source).toBeVisible()
  await source.click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.getByText('展开原文', { exact: true }).click()
  await expect(page.getByRole('dialog').locator('pre').filter({ hasText: text })).toBeVisible()
  expect(mock.opened).toHaveLength(1)
  expect(new URL(mock.opened[0]).searchParams.get('version')).toBe('1')
  expect(mock.errors).toEqual([])
})

test('M1/M3/M5 依据已更新保留回答卡片回执，撤销刷新后仍在', async ({ page }) => {
  const mock = await backend(page, true)
  await say(page)
  const turn = page.locator('.sec-turn')
  await expect(turn).toContainText('依据已更新')
  await expect(turn.getByRole('list', { name: '依据' })).toContainText(text)
  const receipt = turn.locator('.sec-receipt')
  await expect(receipt).toContainText('已建：检查旅行安排')
  await receipt.getByRole('button', { name: '撤销', exact: true }).click()
  await expect(receipt).toContainText('已撤销')
  expect(mock.commands).toHaveLength(1)
  expect(mock.commands[0]).toMatchObject({ type: 'undoAction', id: 'b1-action' })
  await page.reload()
  await expect(turn).toContainText('依据已更新')
  await expect(turn.locator('.sec-reply')).toHaveText(mock.turn.reply)
  await expect(turn.getByRole('list', { name: '依据' })).toContainText(text)
  await expect(receipt).toContainText('已撤销')
  await expect(receipt.getByRole('button', { name: '撤销', exact: true })).toHaveCount(0)
  expect(mock.errors).toEqual([])
})

test('M8/P1 正常轮无更新标记，原话不混入时间轴，390px不溢出', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const mock = await backend(page, false, true)
  await say(page)
  await expect(page.getByText('依据已更新', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('list', { name: '依据' })).toContainText(text)
  await expect(page.locator('.sec-timeline')).not.toContainText(text)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  await page.getByRole('list', { name: '依据' }).getByRole('button', { name: text }).click()
  await page.getByText('展开原文', { exact: true }).click()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
  expect(mock.errors).toEqual([])
})
