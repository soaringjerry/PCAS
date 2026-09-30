import { test, expect, type Page } from '@playwright/test'

function emptyState() {
  return { version: 1, revision: 1, budgetUsage: 0, settings: { dailyBudget: 10, timezone: 'UTC' }, tasks: [], ideas: [], projects: [], memories: [], candidates: [], agents: [{ id: 'model', name: 'Model', enabled: true, available: true, default: true, channel: 'api', memoryKinds: ['fact'], includeInferred: false }], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {} }
}

async function hall(page: Page) {
  await page.route('**/v1/workspace', route => route.fulfill({ json: emptyState() }))
  await page.goto('/')
  await expect(page.getByLabel('导办台', { exact: true })).toBeVisible()
}

test('clear delegation starts once and reload does not submit again', async ({ page }) => {
  let starts = 0
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'delegate', confidence: 0.99 } }))
  await page.route('**/v1/workspace/commands', async route => {
    expect(route.request().postDataJSON().type).toBe('delegateTask')
    starts++
    await route.fulfill({ json: { ...emptyState(), revision: 2 } })
  })
  await hall(page)
  await page.getByLabel('导办台', { exact: true }).fill('帮我起草项目计划')
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await expect(page.getByText('副手开始做了', { exact: true })).toBeVisible()
  expect(starts).toBe(1)
  await expect(page.getByRole('group', { name: '这句要怎么处理' })).toHaveCount(0)
  await page.reload()
  await expect(page.getByLabel('导办台', { exact: true })).toBeVisible()
  expect(starts).toBe(1)
})

test('ambiguous discussion does not become a task even if routing overstates certainty', async ({ page }) => {
  let commands = 0
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'delegate', confidence: 0.99 } }))
  await page.route('**/v1/workspace/commands', route => { commands++; return route.fulfill({ json: emptyState() }) })
  await hall(page)
  await page.getByLabel('导办台', { exact: true }).fill('帮我考虑一下是否要换工作')
  await page.getByRole('button', { name: '交给她', exact: true }).click()
  await expect(page.getByRole('group', { name: '这句要怎么处理' })).toBeVisible()
  expect(commands).toBe(0)
})

test('consecutive ask edit reuse submits server turn IDs', async ({ page }) => {
  const questions: { question: string; history: { id: string; a: string }[] }[] = []
  await page.route('**/v1/desk/route', route => route.fulfill({ json: { intent: 'ask', confidence: 0.99 } }))
  await page.route('**/v1/desk/answer', route => {
    questions.push(route.request().postDataJSON())
    return route.fulfill({ json: { id: `turn-${questions.length}`, answer: `Plan revision ${questions.length}`, agent: 'Model', used: [], links: [], searches: [] } })
  })
  await hall(page)
  for (const [i, text] of ['Write a plan?', 'Edit paragraph two', 'Use the previous plan'].entries()) {
    await page.getByLabel('导办台', { exact: true }).fill(text)
    await page.getByRole('button', { name: i === 0 ? '交给她' : '接着问', exact: true }).click()
    await expect(page.getByText(`Plan revision ${i + 1}`, { exact: true })).toBeVisible()
  }
  expect(questions[1].history).toEqual([{ id: 'turn-1', q: 'Write a plan?', a: '' }])
  expect(questions[2].history.map(t => t.id)).toEqual(['turn-1', 'turn-2'])
})
