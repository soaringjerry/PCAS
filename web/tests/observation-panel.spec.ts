import { expect, test, type Page } from '@playwright/test'
import type { Agent, Memory, State, Task } from '../src/domain/types'
import type { Action } from '../src/store/actions'

type Command = Action & { requestId: string; expectedRevision: number }

const at = '2026-11-26T02:00:00Z'
const dayOne = '2026-11-26'
const dayZero = '2026-11-25'

function makeAgent(over: Partial<Agent> = {}): Agent {
  return {
    id: 'api',
    name: 'gpt-fake',
    enabled: true,
    available: true,
    default: true,
    channel: 'api',
    note: '',
    inputPrice: 2,
    outputPrice: 8,
    maxOutput: 100,
    memoryKinds: ['fact', 'preference'],
    includeInferred: false,
    ...over,
  }
}

function makeTask(over: Partial<Task> = {}): Task {
  return {
    id: 'task-1',
    title: '准备周五周报',
    notes: '',
    status: 'todo',
    dependsOn: [],
    checklist: [],
    triggers: [],
    sources: [],
    history: [],
    createdAt: at,
    updatedAt: at,
    ...over,
  }
}

function makeMemory(id: string, text: string, over: Partial<Memory> = {}): Memory {
  return {
    id,
    kind: 'preference',
    text,
    epistemic: 'confirmed',
    recordVersion: 2,
    pinned: false,
    halfLifeDays: 30,
    reinforcementLimit: 8,
    sources: [],
    versions: [{ at, by: 'user', text }],
    visibleTo: ['api'],
    exposure: 0.9,
    lastUsedAt: at,
    ...over,
  }
}

function baseState(over: Partial<State> = {}): State {
  return {
    version: 1,
    revision: 1,
    budgetUsage: 0.25,
    memoryTotal: 2,
    notices: [],
    settings: {
      timezone: 'Asia/Shanghai',
      city: '上海',
      dailyBudget: 10,
      autoAccept: false,
      wakeIdeas: true,
      followUps: true,
      dailyReviewAt: '09:00',
    },
    tasks: [makeTask()],
    ideas: [],
    projects: [],
    agents: [makeAgent()],
    memories: [makeMemory('mem-recent', '喜欢无糖乌龙茶')],
    candidates: [],
    docs: [],
    runs: [],
    samples: [],
    sources: [
      { id: 'src-failed', name: '扫描合同.pdf', kind: 'file', single: true, status: 'failed', note: 'OCR 失败，请重试', itemCount: 1, lastSyncAt: at },
    ],
    jobs: [
      { id: 'job-done', title: '建立检索索引', trigger: 'event', status: 'done', detail: '「扫描合同.pdf」', createdAt: at },
      { id: 'job-fail', title: '解析附件文字', trigger: 'event', status: 'failed', detail: '「扫描合同.pdf」', recovery: '检查模型配置后重试', createdAt: at },
    ],
    activity: [],
    excludedMemories: {},
    organize: { done: 42, total: 50, version: 3 },
    ...over,
  }
}

async function setupObserveMock(page: Page, state = baseState()) {
  const commands: Command[] = []
  const activityPosts: unknown[] = []
  const memoryQueries: string[] = []
  const dayRequests: string[] = []
  const errors: string[] = []
  let current = state
  let actionUndone = false

  page.on('pageerror', (e) => errors.push(e.message))
  await page.route('**/v1/**', (route) => route.fulfill({ status: 500, json: { error: 'unexpected_mock_request' } }))

  const on = (path: string, handler: Parameters<Page['route']>[1]) => page.route((url) => url.pathname === path, handler)
  await on('/v1/workspace', (route) => route.fulfill({ json: current }))

  await on('/v1/workspace/activity/day', (route) => {
    const url = new URL(route.request().url())
    const date = url.searchParams.get('date') ?? dayOne
    const offset = Number(url.searchParams.get('offset') ?? '0')
    dayRequests.push(`${date}:${offset}`)

    if (date === dayZero) {
      return route.fulfill({
        json: {
          date: dayZero,
          timezone: 'Asia/Shanghai',
          eventCount: 1,
          omittedEvents: 0,
          kinds: { call: 1 },
          events: [
            {
              at: '2026-11-25T05:00:00Z',
              kind: 'call',
              eventId: 'call-prev',
              executionId: 'exec-prev',
              rootExecutionId: 'exec-prev',
              function: 'extraction',
              outcome: 'returned',
              summary: 'extract',
              reason: '',
              cost: 0.0042,
              costKnown: true,
              evidence: { provider: 'openai', model: 'gpt-fake', accounting: 'settled', inputTokens: 800, outputTokens: 120, durationMs: 920 },
            },
          ],
          cost: {
            settled: 0.0042,
            invocations: 1,
            estimatedCostInvocations: 0,
            unknownCostInvocations: 0,
            heldReservations: 0,
            byFunction: [{ function: 'extraction', invocations: 1, returned: 1, settled: 0.0042, unknownCost: 0, inputTokens: 800, outputTokens: 120 }],
          },
          gaps: [],
        },
      })
    }

    const firstPageEvents = [
      {
        at: '2026-11-26T01:10:00Z',
        kind: 'call',
        eventId: 'call-sec',
        executionId: 'exec-sec-1',
        rootExecutionId: 'exec-sec-1',
        function: 'secretary',
        outcome: 'returned',
        summary: 'answer',
        reason: '',
        cost: 0.015,
        costKnown: true,
        evidence: {
          provider: 'openai',
          model: 'gpt-fake',
          prompt: 'secretary-system',
          usageId: 'usage-sec-1',
          accounting: 'settled',
          inputTokens: 1200,
          outputTokens: 240,
          durationMs: 1450,
        },
      },
      {
        at: '2026-11-26T01:10:01Z',
        kind: 'action',
        eventId: 'act-create-1',
        executionId: 'exec-sec-1',
        rootExecutionId: 'exec-sec-1',
        function: 'desk',
        outcome: actionUndone ? 'undone' : 'executed',
        summary: '新增待办「准备周五周报」',
        reason: '',
        cost: null,
        costKnown: false,
        evidence: {
          changes: 1,
          items: ['task-1'],
          undoneAt: actionUndone ? '2026-11-26T01:20:00Z' : undefined,
        },
      },
      {
        at: '2026-11-26T01:15:00Z',
        kind: 'stage',
        eventId: 'stage-def-1',
        executionId: 'job-topic-1',
        rootExecutionId: 'job-topic-1',
        function: 'memory.topic_project',
        outcome: 'deferred',
        summary: '',
        reason: 'topic_project_hourly_limit',
        cost: null,
        costKnown: false,
        evidence: { count: 3 },
      },
      {
        at: '2026-11-26T01:18:00Z',
        kind: 'call',
        eventId: 'call-vision-unknown',
        executionId: 'exec-vis-1',
        rootExecutionId: 'exec-vis-1',
        function: 'vision',
        outcome: 'unknown',
        summary: 'image',
        reason: 'provider_outcome_unknown',
        cost: null,
        costKnown: false,
        evidence: { provider: 'openai', model: 'gpt-vision', accounting: 'held', heldReservation: 0.02, attempt: 2, attemptLimit: 5, recoveryState: 'exhausted' },
      },
    ]

    const secondPageEvents = [
      {
        at: '2026-11-26T02:00:00Z',
        kind: 'decision',
        eventId: 'dec-1',
        executionId: null,
        rootExecutionId: null,
        function: 'date_tidy',
        outcome: 'kept',
        summary: '老王成都行程',
        reason: '仍在未来',
        cost: null,
        costKnown: false,
        evidence: { claim: 'mem-recent', claimVersion: 2, class: 'dated' },
      },
    ]

    return route.fulfill({
      json: {
        date,
        timezone: 'Asia/Shanghai',
        eventCount: 5,
        omittedEvents: offset === 0 ? 1 : 0,
        kinds: { call: 2, action: 1, stage: 1, decision: 1 },
        events: offset === 0 ? firstPageEvents : secondPageEvents,
        cost: {
          settled: 0.015,
          invocations: 2,
          estimatedCostInvocations: 0,
          unknownCostInvocations: 1,
          heldReservations: 0.02,
          byFunction: [
            { function: 'secretary', invocations: 1, returned: 1, settled: 0.015, unknownCost: 0, inputTokens: 1200, outputTokens: 240 },
            { function: 'vision', invocations: 1, returned: 0, settled: 0, unknownCost: 1, inputTokens: 0, outputTokens: 0 },
          ],
        },
        gaps: [
          'an action has no recorded link to the model call that proposed it; both carry the execution identity when the action came from a secretary turn',
        ],
      },
    })
  })

  await on('/v1/workspace/usage', (route) =>
    route.fulfill({
      json: {
        days: [
          {
            date: dayZero,
            purposes: [{ purpose: 'extraction', calls: 1, inputTokens: 800, outputTokens: 120, cost: 0.0042, estimatedTokenCalls: 0, estimatedCostCalls: 0, durationMs: { measuredCalls: 1, median: 920, max: 920 } }],
            timings: [],
          },
          {
            date: dayOne,
            purposes: [{ purpose: 'secretary', calls: 2, inputTokens: 2400, outputTokens: 480, cost: 0.03, estimatedTokenCalls: 0, estimatedCostCalls: 1, durationMs: { measuredCalls: 2, median: 1300, max: 1600 } }],
            timings: [
              {
                kind: 'secretary',
                tier: 'standard',
                executions: 2,
                prepareMs: { median: 120, max: 180 },
                answerMs: { median: 1100, max: 1400 },
                selfcheckMs: { median: 0, max: 0 },
                writebackMs: { median: 40, max: 60 },
                modelMs: { median: 1100, max: 1400 },
                totalMs: { median: 1300, max: 1600 },
                otherMs: { median: 20, max: 30 },
              },
            ],
          },
        ],
      },
    }),
  )

  await on('/v1/workspace/usage/calls', (route) => {
    const before = new URL(route.request().url()).searchParams.get('before')
    if (before) {
      return route.fulfill({
        json: {
          items: [
            {
              id: 'usage-older-1',
              at: '2026-11-25T05:00:00Z',
              purpose: 'extraction',
              agentId: 'openai',
              model: 'gpt-fake',
              inputTokens: 800,
              outputTokens: 120,
              cost: 0.0042,
              turnId: null,
              runId: null,
              jobId: 'job-1',
              refs: [],
              tier: '',
              inputEstimated: false,
              outputEstimated: false,
              costEstimated: false,
              durationMs: 920,
            },
          ],
          next: '',
        },
      })
    }
    return route.fulfill({
      json: {
        items: [
          {
            id: 'usage-sec-1',
            at: '2026-11-26T01:10:00Z',
            purpose: 'secretary',
            agentId: 'openai',
            model: 'gpt-fake',
            inputTokens: 1200,
            outputTokens: 240,
            cost: 0.015,
            turnId: 'turn-1',
            runId: null,
            jobId: null,
            refs: [
              { id: 'src-orig-1', version: 1, kind: 'source', text: '周五前把周报发给组长', deleted: false },
              { id: 'mem-recent', version: 2, kind: 'claim', text: '喜欢无糖乌龙茶', deleted: false },
              { id: 'mem-deleted', version: 1, kind: 'claim', text: '', deleted: true },
            ],
            tier: 'standard',
            inputEstimated: false,
            outputEstimated: false,
            costEstimated: false,
            durationMs: 1450,
            executionTiming: { kind: 'secretary', tier: 'standard', prepareMs: 120, answerMs: 1250, selfcheckMs: 0, writebackMs: 50, modelMs: 1250, totalMs: 1450, otherMs: 30 },
          },
        ],
        next: 'cursor-page-2',
      },
    })
  })

  await page.route((url) => url.pathname.startsWith('/v1/workspace/usage/calls/'), (route) => {
    const id = decodeURIComponent(new URL(route.request().url()).pathname.split('/').pop() ?? '')
    if (id === 'usage-old-99') {
      return route.fulfill({
        json: {
          id: 'usage-old-99',
          at: '2026-11-10T01:00:00Z',
          purpose: 'query_embedding',
          agentId: 'embed',
          model: 'text-embedding-3-small',
          inputTokens: 42,
          outputTokens: 0,
          cost: 0.0001,
          turnId: null,
          runId: null,
          jobId: null,
          tier: '',
          inputEstimated: false,
          outputEstimated: false,
          costEstimated: false,
          durationMs: 145,
          refs: [{ id: 'src-orig-1', version: 1, kind: 'source', text: '十天前的历史检索引用', deleted: false }],
        },
      })
    }
    return route.fulfill({ status: 404, json: { error: 'not_found' } })
  })

  await on('/v1/memory/sources/src-orig-1', (route) =>
    route.fulfill({
      json: {
        derived: [],
        processing: [],
        source: { id: 'src-orig-1', version: 1, title: '秘书原话', text: '周五前把周报发给组长', recorded_at: at, has_attachment: false, attachment_missing: false, representation: 'original' },
      },
    }),
  )

  await on('/v1/workspace/background', (route) =>
    route.fulfill({
      json: {
        stages: [
          {
            stage: 'memory.topic_project',
            hourlyBudget: 2,
            success: 1,
            deferred: 3,
            failure: 0,
            overflow: 0,
            deferredBy: { topic_project_hourly_limit: 3 },
            lastFailure: '',
            withdrawn: { deadlines: 0, requirements: 0 },
            calls: 2,
          },
          {
            stage: 'memory.organize',
            hourlyBudget: 40,
            success: 12,
            deferred: 0,
            failure: 1,
            overflow: 2,
            deferredBy: {},
            lastFailure: 'schedule_background_write_busy',
            withdrawn: { deadlines: 1, requirements: 0 },
            calls: 13,
          },
        ],
        overflow: [{ stage: 'secretary', reason: 'recall_budget', count: 6 }],
      },
    }),
  )

  await on('/v1/workspace/activity/explanations', (route) =>
    route.fulfill({
      json: {
        pausedJobs: [
          {
            jobId: 'job-paused-1',
            stage: 'source.extract',
            baseStage: 'source.extract',
            recordId: 'src-failed',
            recordVersion: 1,
            state: 'failed',
            errorCode: 'chain_limit',
            attempts: 5,
            chainDepth: 201,
            rootJobId: 'job-root-1',
            createdAt: at,
            updatedAt: at,
            recoveryState: 'exhausted',
            recoveryReason: 'provider_recovery_exhausted',
            attempt: 5,
            attemptLimit: 5,
            requiresOwnerRecovery: true,
          },
        ],
        topicDecisions: [
          {
            topicId: 'topic-chengdu',
            topicName: '成都旅行',
            topicKind: 'topic',
            page: 0,
            decision: 'skip',
            reason: '去年的行程已经结束，不是当前在做的项目',
            createdProject: false,
          },
        ],
        dateDecisions: [
          {
            claimId: 'mem-recent',
            claimVersion: 2,
            class: 'past',
            decision: 'drop',
            reason: '日期已经过去且无需继续提醒',
            entryTitle: '成都航班',
            createdAt: at,
          },
        ],
      },
    }),
  )

  await page.route((url) => url.pathname.startsWith('/v1/workspace/activity/trace/'), (route) => {
    const id = decodeURIComponent(new URL(route.request().url()).pathname.split('/').pop() ?? '')
    return route.fulfill({
      json: {
        anchorId: id,
        calls: [
          {
            id: 'call-sec',
            executionId: 'exec-sec-1',
            rootExecutionId: 'exec-sec-1',
            attempt: 1,
            attemptLimit: 1,
            recoveryState: 'active',
            function: 'secretary',
            stage: 'answer',
            provider: 'openai',
            model: 'gpt-fake',
            promptName: 'secretary-system',
            instructionHash: 'abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890',
            schemaName: 'secretary-answer',
            schemaHash: '1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef',
            contextBuilderVersion: 'ctx-v2',
            inputManifest: { memoryCount: 4, omitted: 0, version: 'interactive-input-v1' },
            outcome: 'returned',
            accountingState: 'settled',
            usageId: 'usage-sec-1',
            cost: 0.015,
            costKnown: true,
            inputTokens: 1200,
            outputTokens: 240,
            durationMs: 1450,
            createdAt: '2026-11-26T01:10:00Z',
          },
        ],
        events: [
          {
            id: 'ev-root',
            type: 'work.queued',
            stage: 'source.chunk',
            objectId: 'src-failed',
            objectVersion: 1,
            triggerSource: 'schedule_or_request',
            jobId: 'job-root-1',
            rootId: 'job-root-1',
            chainDepth: 0,
            occurredAt: '2026-11-26T01:09:00Z',
            jobState: 'done',
            jobAttempts: 1,
          },
          {
            id: 'ev-child',
            type: 'work.queued',
            stage: 'source.extract',
            objectId: 'src-failed',
            objectVersion: 1,
            triggerSource: 'job',
            jobId: 'job-paused-1',
            rootId: 'job-root-1',
            causeId: 'job-root-1',
            chainDepth: 1,
            occurredAt: '2026-11-26T01:09:05Z',
            jobState: 'failed',
            jobErrorCode: 'chain_limit',
            jobAttempts: 5,
          },
        ],
        actions: [
          {
            id: 'act-create-1',
            source: 'desk',
            summary: '新增待办「准备周五周报」',
            outcome: actionUndone ? 'undone' : 'executed',
            createdAt: '2026-11-26T01:10:01Z',
            changes: 1,
            items: ['task-1'],
          },
        ],
        runs: [],
        stages: [
          { id: 'st-1', at: '2026-11-26T01:09:06Z', stage: 'source.extract', outcome: 'failure', reason: 'chain_limit', count: 1, jobId: 'job-paused-1' },
        ],
        gaps: ['input_manifest stores reference identities and coverage counts only; private prompt text stays outside the call journal'],
      },
    })
  })

  await on('/v1/workspace/memories', (route) => {
    const url = new URL(route.request().url())
    const q = url.searchParams.get('q') ?? ''
    const cursor = url.searchParams.get('cursor') ?? ''
    memoryQueries.push(`${q}:${cursor}`)
    if (q === '成都') {
      return route.fulfill({
        json: {
          items: [makeMemory('mem-old-chengdu', '去年说过想去成都吃火锅', { halfLifeDays: 60, reinforcementLimit: 12 })],
          total: 1,
          next: '',
        },
      })
    }
    if (cursor) {
      return route.fulfill({
        json: {
          items: [makeMemory('mem-page-2', '分页第二页的记忆')],
          total: 2,
          next: '',
        },
      })
    }
    return route.fulfill({
      json: {
        items: [makeMemory('mem-recent', '喜欢无糖乌龙茶')],
        total: 2,
        next: 'mem-cursor-2',
      },
    })
  })

  await on('/v1/memory/activity', (route) => {
    activityPosts.push(route.request().postDataJSON())
    return route.fulfill({ json: { ok: true } })
  })

  await on('/v1/workspace/commands', (route) => {
    const body = route.request().postDataJSON() as Command
    commands.push(body)
    if (body.type === 'undoAction') {
      actionUndone = true
    } else if (body.type === 'retryJob') {
      current = {
        ...current,
        jobs: current.jobs.map((j) => (j.id === body.id ? { ...j, status: 'queued' } : j)),
      }
    } else if (body.type === 'updateAgent') {
      current = {
        ...current,
        agents: current.agents.map((a) => (a.id === body.id ? { ...a, ...body.patch } : a)),
      }
    }
    current = { ...current, revision: current.revision + 1 }
    return route.fulfill({ json: current })
  })

  return { commands, activityPosts, memoryQueries, dayRequests, errors }
}

test('top navigation and command palette open the Observation Panel, and daily activity traces calls, actions, costs, deferrals, undo, and gaps', async ({ page }) => {
  const m = await setupObserveMock(page)
  await page.goto('/observe?date=' + dayOne)

  await expect(page.getByRole('heading', { level: 1, name: '观察面板' })).toBeVisible()
  await expect(page.getByRole('link', { name: '观察' })).toHaveAttribute('href', '/observe')

  // Overview tiles reflect settled cost, unknown cost honesty, held reservation, and distinct failure vs deferral counts.
  const overview = page.getByRole('group', { name: '当日概览' })
  await expect(overview).toContainText('5 条')
  await expect(overview).toContainText('¥0.0150')
  await expect(overview).toContainText('1 次花费未知')
  await expect(overview).toContainText('¥0.02')
  await expect(overview).toContainText('失败/未知 1 · 延后等待 1')

  // Execution cross-filtering links secretary call and its created action without fabricating links for other rows.
  await page.getByRole('button', { name: '追踪同次执行' }).first().click()
  await expect(page.getByRole('status')).toContainText('正在按执行 ID 关联查看')
  await expect(page.getByText('新增待办「准备周五周报」')).toBeVisible()
  await expect(page.getByText('达到自动主题项目每小时限额，延后等待')).toHaveCount(0)
  await page.getByRole('button', { name: '清除关联筛选' }).click()

  // Action links to its affected item and supports inline undo through command.Undo.
  await expect(page.getByRole('link', { name: '事项：准备周五周报' })).toHaveAttribute('href', '/t/task-1')
  await page.getByRole('button', { name: '撤销', exact: true }).click()
  await expect.poll(() => m.commands.at(-1)).toMatchObject({ type: 'undoAction', id: 'act-create-1' })

  // Pagination loads remaining omitted events.
  await page.getByRole('button', { name: '再加载更多（还有 1 条）' }).click()
  await expect(page.getByText('老王成都行程')).toBeVisible()

  // Explicit gaps fold discloses unlinked records honestly.
  await page.getByRole('button', { name: /已知记录缺口说明/ }).click()
  await expect(page.getByText(/来自秘书轮次的动作与调用共享同一个执行 ID/)).toBeVisible()

  // Jump from a call row directly to Usage tab highlighting its cited memory/source refs.
  await page.getByRole('button', { name: '查看引用记忆' }).click()
  await expect(page).toHaveURL(/\/observe\?tab=usage&call=usage-sec-1/)
  await expect(page.locator('.obs-trace-banner')).toContainText('已定位调用记录：usage-se')
  await expect(page.getByText('周五前把周报发给组长')).toBeVisible()
  await expect(page.getByText('原记录已删除，仅保留当时引用编号。')).toBeVisible()

  // Clicking a source ref opens SourceSheet in place.
  await page.getByRole('button', { name: '看原文' }).click()
  await expect(page.getByRole('dialog')).toContainText('周五前把周报发给组长')
  await page.keyboard.press('Escape')

  // Daily cost chart fills zero-usage days across the continuous 14-day window.
  expect(await page.locator('.obs-bar-hit').count()).toBeGreaterThanOrEqual(14)
  expect(await page.locator('.obs-bar-rect.zero').count()).toBeGreaterThanOrEqual(12)

  // Clicking a bar in the daily cost chart navigates straight to that day's activity stream.
  await page.getByRole('button', { name: new RegExp(`^${dayZero}`) }).click()
  await expect(page).toHaveURL(new RegExp(`date=${dayZero}`))
  await expect(page.getByRole('group', { name: '当日概览' })).toContainText('¥0.0042')

  // Command palette includes the Observation Panel entry.
  await page.keyboard.press('Control+k')
  const palette = page.getByRole('dialog', { name: '搜索' })
  await expect(palette.getByText('观察面板')).toBeVisible()
  expect(m.errors).toEqual([])
})

test('usage tab locates historical calls outside the newest 50 directly by ID and reports missing calls cleanly', async ({ page }) => {
  const m = await setupObserveMock(page)
  await page.goto('/observe?tab=usage&call=usage-old-99')

  await expect(page.getByRole('status')).toContainText('已定位调用记录：usage-ol')
  await expect(page.getByRole('status')).toContainText('早于最近 50 条，已按 ID 直接调取')
  await expect(page.getByText('检索向量化')).toBeVisible()
  await expect(page.getByText('十天前的历史检索引用')).toBeVisible()

  await page.goto('/observe?tab=usage&call=usage-missing-00')
  await expect(page.getByRole('status')).toContainText('未找到编号 usage-mi 的调用记录')
  expect(m.errors).toEqual([])
})

test('daily activity keeps normal overflow, replaced retries, and deleted things calm and accurate across pagination', async ({ page }) => {
  const m = await setupObserveMock(page)
  await page.route((url) => url.pathname === '/v1/workspace/activity/day', (route) =>
    route.fulfill({
      json: {
        date: dayOne,
        timezone: 'Asia/Shanghai',
        eventCount: 520,
        omittedEvents: 517,
        summary: { failures: 0, deferrals: 1, overflows: 48 },
        kinds: { call: 1, stage: 49, action: 1 },
        events: [
          {
            at: '2026-11-26T01:09:00Z',
            kind: 'call',
            eventId: 'call-retried',
            executionId: 'exec-r',
            rootExecutionId: 'exec-r',
            function: 'secretary',
            outcome: 'failed',
            summary: 'answer',
            reason: 'model_call_failed',
            cost: 0,
            costKnown: true,
            evidence: { provider: 'chatgpt', model: 'gpt-fake', accounting: 'not_reserved', recoveryState: 'replaced', recoveryReason: 'secretary_transient_retry' },
          },
          {
            at: '2026-11-26T01:09:05Z',
            kind: 'stage',
            eventId: 'st-overflow',
            executionId: null,
            rootExecutionId: null,
            function: 'secretary',
            outcome: 'overflow',
            summary: '',
            reason: 'recall_budget',
            cost: null,
            costKnown: false,
            evidence: { count: 6 },
          },
          {
            at: '2026-11-26T01:09:10Z',
            kind: 'action',
            eventId: 'act-deleted-item',
            executionId: 'exec-r',
            rootExecutionId: 'exec-r',
            function: 'desk',
            outcome: 'executed',
            summary: '修改临时事项',
            reason: '',
            cost: null,
            costKnown: false,
            evidence: { changes: 1, items: ['deleted-task-999'] },
          },
        ],
        cost: { settled: 0, invocations: 1, estimatedCostInvocations: 0, unknownCostInvocations: 0, heldReservations: 0, byFunction: [] },
        gaps: [],
      },
    }),
  )
  await page.goto('/observe?date=' + dayOne)

  const overview = page.getByRole('group', { name: '当日概览' })
  // Whole-day summary is shown cleanly: 0 anomalies, 1 deferral, 48 calm budget truncations; no danger tile.
  await expect(overview).toContainText('1 项延后')
  await expect(overview).toContainText('失败/未知 0 · 延后等待 1 · 预算裁剪 48')
  await expect(overview.locator('.obs-tile.danger')).toHaveCount(0)
  // Real backend reason labels render in Chinese.
  await expect(page.getByText('已重试替换')).toBeVisible()
  await expect(page.getByText('秘书遇到临时错误，已自动重试一次')).toBeVisible()
  await expect(page.getByText('回忆到的记忆超出本次上下文预算，已按相关性裁剪')).toBeVisible()
  // Deleted work item renders a non-clickable badge instead of a dead /t/:id link.
  await expect(page.getByText('原事项已删除（delete）')).toBeVisible()
  await expect(page.locator('a[href="/t/deleted-task-999"]')).toHaveCount(0)
  expect(m.errors).toEqual([])
})

test('background health separates deferrals from failures, keeps normal overflow folded calmly, and retries failed jobs', async ({ page }) => {
  const m = await setupObserveMock(page)
  await page.goto('/observe?tab=background')

  // Overflow is NOT shown as a scary red role="alert" banner; it lives in a calm folded info panel.
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.getByText('近 1 小时阶段失败')).toBeVisible()
  await expect(page.getByText('另有 6 次正常预算裁剪')).toBeVisible()
  await page.getByRole('button', { name: /近 1 小时预算与上下文裁剪记录/ }).click()
  await expect(page.getByText('回忆到的记忆超出本次上下文预算，已按相关性裁剪')).toBeVisible()

  await expect(page.getByText('延后等待 3')).toBeVisible()
  await expect(page.getByText('失败 1')).toBeVisible()
  await expect(page.getByText('近 1 小时最近失败：调度让行：正在等待你的操作写入提交')).toBeVisible()
  await expect(page.getByText('小时调用 2 / 2')).toBeVisible()

  // Completed jobs stay collapsed until requested; failed job retries via retryJob command.
  await expect(page.getByText('解析附件文字')).toBeVisible()
  await expect(page.getByText('建立检索索引')).toHaveCount(0)
  await page.getByRole('button', { name: '已完成 1 项，点开看' }).click()
  await expect(page.getByText('建立检索索引')).toBeVisible()
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect.poll(() => m.commands.at(-1)).toMatchObject({ type: 'retryJob', id: 'job-fail' })

  expect(m.errors).toEqual([])
})

test('memory and access tab debounces server-side memory search, paginates, and saves activity and agent access settings', async ({ page }) => {
  const m = await setupObserveMock(page)
  await page.goto('/observe?tab=memory')

  // Pagination loads beyond the initial slice.
  await expect(page.getByRole('option', { name: /喜欢无糖乌龙茶/ })).toBeVisible()
  await page.getByRole('button', { name: '再看 50 条' }).click()
  await expect(page.getByRole('option', { name: /分页第二页的记忆/ })).toBeVisible()

  // Rapid typing is debounced rather than firing on every keystroke.
  const beforeSearchCount = m.memoryQueries.length
  const box = page.getByRole('searchbox', { name: '搜索要调节权重的记忆' })
  await box.pressSequentially('成都', { delay: 30 })
  const hit = page.getByRole('option', { name: /去年说过想去成都吃火锅/ })
  await expect(hit).toBeVisible()
  expect(m.memoryQueries.length - beforeSearchCount).toBe(1)

  await hit.click()
  await page.getByRole('button', { name: '保存权重设置' }).click()
  await expect.poll(() => m.activityPosts.length).toBe(1)
  expect(m.activityPosts[0]).toMatchObject({
    ref: { id: 'mem-old-chengdu', version: 2, kind: 'claim' },
    half_life_days: 60,
    reinforcement_limit: 12,
  })
  await expect(page.getByRole('status')).toContainText('已保存')

  // Agent memory-access toggle dispatches updateAgent.
  await page.getByRole('switch', { name: 'gpt-fake 包含推测' }).click()
  await expect.poll(() => m.commands.at(-1)).toMatchObject({ type: 'updateAgent', id: 'api', patch: { includeInferred: true } })

  // Responsive check on mobile width: no horizontal overflow.
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  expect(m.errors).toEqual([])
})

test('execution trace drawer opens from daily activity or deep-link and shows prompt hashes, input manifest, causal chain, and gaps', async ({ page }) => {
  const m = await setupObserveMock(page)
  await page.goto('/observe?date=' + dayOne)

  // Open trace drawer directly from an activity row.
  await page.getByRole('button', { name: '详细溯源' }).first().click()
  await expect(page).toHaveURL(/trace=exec-sec-1/)
  const drawer = page.getByRole('dialog')
  await expect(drawer).toContainText('执行与因果溯源')
  await expect(drawer).toContainText('secretary-system')
  await expect(drawer).toContainText('#abcdef1234')
  await expect(drawer).toContainText('secretary-answer')
  await expect(drawer).toContainText('L0')
  await expect(drawer).toContainText('L1')
  await expect(drawer).toContainText('上游任务触发')
  await expect(drawer).toContainText('输入清单仅记录引用 ID 与覆盖/省略统计')

  // Expand input_manifest metadata fold inside the drawer.
  await drawer.getByRole('button', { name: /输入清单元数据/ }).click()
  await expect(drawer).toContainText('interactive-input-v1')

  // Jump from the trace drawer straight to Usage tab highlighting cited sources/memories.
  await drawer.getByRole('button', { name: '查看引用原文与记忆' }).click()
  await expect(page).toHaveURL(/tab=usage&call=usage-sec-1/)
  await expect(page.getByText('周五前把周报发给组长')).toBeVisible()
  expect(m.errors).toEqual([])
})

test('background tab authorizes terminal recovery retries explicitly and explains skipped topic projects and date review decisions', async ({ page }) => {
  const m = await setupObserveMock(page)
  await page.goto('/observe?tab=background')

  // Terminal paused job requires explicit owner confirmation before dispatching retryJob.
  await expect(page.getByText('需你授权恢复')).toBeVisible()
  await expect(page.getByText('链深 L201')).toBeVisible()
  await page.getByRole('button', { name: '授权新一轮恢复重试' }).click()
  const modal = page.getByRole('dialog', { name: '授权新一轮恢复重试？' })
  await expect(modal).toContainText('保留原有历史调用与计费记录')
  await modal.getByRole('button', { name: '确认授权并重排' }).click()
  await expect.poll(() => m.commands.at(-1)).toMatchObject({ type: 'retryJob', id: 'job-paused-1' })

  // Non-action explanations show skipped topic project reason and date review decision reason.
  await expect(page.getByText('成都旅行')).toBeVisible()
  await expect(page.getByText('跳过未建项目')).toBeVisible()
  await expect(page.getByText('去年的行程已经结束，不是当前在做的项目')).toBeVisible()
  await expect(page.getByRole('link', { name: '看主题记忆' })).toHaveAttribute('href', '/library?group=topic-chengdu')

  await page.getByRole('radio', { name: /日期复查/ }).click()
  await expect(page.getByText('成都航班')).toBeVisible()
  await expect(page.getByText('已关闭日期')).toBeVisible()
  await expect(page.getByText('日期已经过去且无需继续提醒')).toBeVisible()
  expect(m.errors).toEqual([])
})

test('reverse trace links from ThingPage deputy runs and HallPage feed open the Observation Panel trace drawer', async ({ page }) => {
  const state = baseState({
    runs: [
      {
        id: 'run-trace-1',
        thingId: 'task-1',
        agentId: 'api',
        kind: 'breakdown',
        prompt: '拆成三步',
        brief: '',
        contextMemoryIds: [],
        status: 'done',
        output: '1. 写大纲',
        adopted: { as: 'subtasks', at, edited: false, actionId: 'act-adopt-1' },
        staleContext: false,
        cost: 0.01,
        createdAt: at,
        finishedAt: at,
      },
    ],
  })
  const m = await setupObserveMock(page, state)
  await page.route((url) => url.pathname === '/v1/desk/turns', (route) => route.fulfill({ json: { turns: [] } }))
  await page.route((url) => url.pathname === '/v1/workspace/schedule', (route) => route.fulfill({ json: { items: [] } }))
  await page.route((url) => url.pathname === '/v1/workspace/in-progress', (route) => route.fulfill({ json: { items: [] } }))

  await page.goto('/t/task-1')
  const traceLink = page.getByRole('link', { name: '溯源', exact: true }).first()
  await expect(traceLink).toHaveAttribute('href', '/observe?trace=run-trace-1')
  await traceLink.click()
  await expect(page).toHaveURL(/\/observe\?trace=run-trace-1/)
  await expect(page.getByRole('dialog')).toContainText('执行与因果溯源')
  expect(m.errors).toEqual([])
})
