import { memoryKindLabel } from './labels'
import { findThing, isOpenTask, thingTitle, type Thing } from './things'
import type { Agent, Memory, RunKind, State } from './types'

// Until the backend exists, agents are simulated here. The brief is real: it
// is exactly what a model would receive, built from the thing and the memories
// the user allowed. Outputs are deterministic stand-ins.

export const quickActions: Record<Thing['kind'], { kind: RunKind; label: string; prompt: string }[]> = {
  task: [
    { kind: 'plan', label: '写方案', prompt: '为这件事写一份可执行的方案' },
    { kind: 'breakdown', label: '拆成子任务', prompt: '把这件事拆成具体的子任务' },
    { kind: 'draft', label: '起草', prompt: '起草这件事需要的文字（邮件、说明或文档）' },
    { kind: 'summary', label: '总结进度', prompt: '总结这件事目前的进度和卡点' },
  ],
  idea: [
    { kind: 'plan', label: '评估可行性', prompt: '评估这个想法现在是否值得做，给出方案' },
    { kind: 'breakdown', label: '拆成步骤', prompt: '如果要做，拆成几个待办' },
  ],
  project: [
    { kind: 'summary', label: '总结进度', prompt: '总结项目目前的进度、卡点和下一步' },
    { kind: 'breakdown', label: '规划下一步', prompt: '根据现状规划接下来要做的几件事' },
    { kind: 'plan', label: '写方案', prompt: '为项目接下来的阶段写一份方案' },
  ],
}

export function memoriesFor(state: State, agent: Agent, projectId?: string): Memory[] {
  return state.memories.filter(
    (m) =>
      agent.memoryKinds.includes(m.kind) &&
      (agent.includeInferred || m.epistemic !== 'inferred') &&
      (!projectId || !m.projectId || m.projectId === projectId),
  )
}

function projectOf(thing: Thing): string | undefined {
  return thing.kind === 'project' ? thing.id : thing.item.projectId
}

/** Memories relevant to a thing, each marked with whether the agent may see it. */
export function contextFor(state: State, thing: Thing, agent: Agent) {
  const projectId = projectOf(thing)
  const allowed = new Set(memoriesFor(state, agent, projectId).map((m) => m.id))
  const excluded = new Set(state.excludedMemories[thing.id] ?? [])
  const relevant = state.memories
    .filter((m) => (projectId ? m.projectId === projectId || !m.projectId : !m.projectId))
    .sort((a, b) => b.exposure - a.exposure)
  return relevant.map((m) => {
    const kindBlocked = !agent.memoryKinds.includes(m.kind)
    const unconfirmed = !kindBlocked && m.epistemic === 'inferred' && !agent.includeInferred
    return { memory: m, allowed: allowed.has(m.id), included: allowed.has(m.id) && !excluded.has(m.id), kindBlocked, unconfirmed }
  })
}

export function sourcesFor(state: State, thing: Thing) {
  const seen = new Set<string>()
  return collectSources(state, thing).filter((s) => {
    const key = `${s.label}|${s.at}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function collectSources(state: State, thing: Thing) {
  if (thing.kind === 'project') {
    return state.tasks.filter((t) => t.projectId === thing.id).flatMap((t) => t.sources)
  }
  const own = thing.item.sources
  if (thing.kind === 'task' && thing.item.ideaId) {
    const idea = state.ideas.find((i) => i.id === (thing.item as { ideaId?: string }).ideaId)
    const met = idea?.conditions.flatMap((c) => (c.metBy ? [c.metBy] : [])) ?? []
    return [...own, ...(idea?.sources ?? []), ...met]
  }
  if (thing.kind === 'idea') return [...own, ...thing.item.conditions.flatMap((c) => (c.metBy ? [c.metBy] : []))]
  return own
}

function background(state: State, thing: Thing): string[] {
  const project = state.projects.find((p) => p.id === projectOf(thing))
  const lines: string[] = []
  if (project && thing.kind !== 'project') lines.push(`所属项目「${project.name}」：${project.goal}`)
  if (thing.kind === 'project') lines.push(thing.item.goal)
  if (thing.kind === 'task') {
    if (thing.item.notes) lines.push(thing.item.notes)
    const idea = state.ideas.find((i) => i.id === thing.item.ideaId)
    if (idea?.shelvedReason) lines.push(`它来自一个曾被搁置的想法，原因：${idea.shelvedReason}`)
    if (idea?.wake) lines.push(`重新拿起它的原因：${idea.wake.reason}`)
  }
  if (thing.kind === 'idea') {
    lines.push(thing.item.body)
    if (thing.item.shelvedReason) lines.push(`搁置原因：${thing.item.shelvedReason}`)
    if (thing.item.wake) lines.push(`重新唤醒的原因：${thing.item.wake.reason}`)
  }
  return lines.filter(Boolean)
}

function progress(state: State, thing: Thing): string[] {
  if (thing.kind === 'project') {
    const open = state.tasks.filter((t) => t.projectId === thing.id && isOpenTask(t))
    return [thing.item.progress, open.length ? `进行中的事：${open.map((t) => t.title).join('；')}` : ''].filter(Boolean)
  }
  if (thing.kind === 'task') {
    const done = thing.item.checklist.filter((c) => c.done).map((c) => c.text)
    const todo = thing.item.checklist.filter((c) => !c.done).map((c) => c.text)
    return [done.length ? `已完成：${done.join('；')}` : '', todo.length ? `未完成：${todo.join('；')}` : ''].filter(Boolean)
  }
  return []
}

/** The exact text the agent receives. */
export function buildBrief(state: State, thing: Thing, prompt: string, memories: Memory[]): string {
  const parts = [`# 任务\n${prompt}`, `## 这件事\n${thingTitle(thing)}`]
  const bg = background(state, thing)
  if (bg.length) parts.push(`## 背景\n${bg.join('\n')}`)
  const pr = progress(state, thing)
  if (pr.length) parts.push(`## 当前进度\n${pr.join('\n')}`)
  const decisions = memories.filter((m) => m.kind === 'decision' && m.epistemic === 'confirmed')
  if (decisions.length) parts.push(`## 已确定的决定\n${decisions.map((m) => `- ${m.text}`).join('\n')}`)
  const others = memories.filter((m) => !decisions.includes(m))
  if (others.length) parts.push(`## 相关记忆\n${others.map((m) => `- [${memoryKindLabel[m.kind]}] ${m.text}`).join('\n')}`)
  parts.push('## 要求\n只使用这里提供的信息；不确定的地方直接说明，不要猜测。')
  return parts.join('\n\n')
}

const aboutSmallModel = (thing: Thing) => thingTitle(thing).includes('小模型')

function known(memories: Memory[]): string {
  const facts = memories.filter((m) => m.kind === 'fact').slice(0, 4)
  return facts.length ? `## 已知信息\n${facts.map((m) => `- ${m.text}`).join('\n')}\n\n` : ''
}

/** Deterministic stand-in for a model's answer. */
export function mockOutput(state: State, thingId: string, kind: RunKind, prompt: string, memories: Memory[]): string {
  const thing = findThing(state, thingId)
  if (!thing) return '找不到这件事。'
  const title = thingTitle(thing)
  const hasLocalCost = memories.some((m) => m.text.includes('¥0.004'))

  if (kind === 'breakdown') {
    let items: string[]
    if (aboutSmallModel(thing)) {
      items = ['准备 200 段标注集（Yufolo 周会转录 + Claude 导出）', '写统一的提取提示词', '本地跑 Qwen3-4B', '云端模型跑对照组', '对比准确率、漏提率和单次成本']
    } else if (thing.kind === 'project' && thing.item.nextSteps.length) {
      items = thing.item.nextSteps.map((s) => s)
    } else if (thing.kind === 'task' && thing.item.title.includes('数据模型')) {
      items = ['画出四层之间的关系图', '定下记忆的版本与可信度字段', '写清删除如何传播到派生数据', '请一位朋友过一遍']
    } else {
      items = ['理清目标和完成标准', '列出需要的资料和依赖', '完成第一版', '请人过一遍并修改']
    }
    return `建议拆成这几步：\n\n${items.map((i) => `- [ ] ${i}`).join('\n')}`
  }

  if (kind === 'summary') {
    const lines = progress(state, thing)
    const waiting =
      thing.kind === 'project'
        ? state.tasks.filter((t) => t.projectId === thing.id && t.status === 'waiting').map((t) => `${t.title}（等 ${t.waitingFor ?? '回复'}）`)
        : []
    const next = thing.kind === 'project' ? thing.item.nextSteps[0] : undefined
    return [
      `## ${title}：进度小结`,
      ...lines.map((l) => `- ${l}`),
      waiting.length ? `- 卡在：${waiting.join('；')}` : '- 目前没有卡住的事',
      next ? `- 建议下一步：${next}` : '',
    ]
      .filter(Boolean)
      .join('\n')
  }

  if (kind === 'plan') {
    if (aboutSmallModel(thing)) {
      return `# ${title}：评测方案

## 结论先行
${hasLocalCost ? '按目前的成本数据（本地约 ¥0.004 / 次，云端约 ¥0.03 / 次），值得先做一次小规模评测，再决定是否切换。' : '目前缺少本地推理的成本数据，建议先测成本，再决定要不要做。'}

${known(memories)}## 步骤
1. 从 Yufolo 周会转录和 Claude 导出里抽 200 段，人工标注待办、事实和决定
2. 本地 Qwen3-4B 和云端模型用同一套提示词各跑一遍
3. 比较准确率、漏提率和单次成本
4. 差距小于 5% 就切到本地，难例回退云端

## 风险
- 标注集太小，结论不稳：先 200 段，不够再加
- 本地显存和并发有限：批量提取放到夜间

## 需要你确认
- 标注由谁来做，预计 2 小时`
    }
    const decisions = memories.filter((m) => m.kind === 'decision' && m.epistemic === 'confirmed').slice(0, 3)
    return `# ${title}：方案

## 目标
${thing.kind === 'project' ? thing.item.goal : `把「${title}」做完，并且结果可以直接用。`}

${known(memories)}## 步骤
1. 先对齐范围和完成标准
2. 做出第一版，边做边记下取舍
3. 请人过一遍，按反馈修改后收尾
${decisions.length ? `\n## 要遵守的决定\n${decisions.map((m) => `- ${m.text}`).join('\n')}\n` : ''}
## 需要你确认
- 完成标准是否就是上面这些`
  }

  if (kind === 'draft') {
    if (title.includes('回复')) {
      return `您好，

感谢来信，也很高兴您对课堂实时字幕感兴趣。下学期在 3 门课上试用没有问题，我们可以先约 30 分钟聊一下课程时间和教室网络情况，再定具体方案。

这周四或周五下午您方便吗？

祝好`
    }
    return `# ${title}

（草稿）

${background(state, thing).join('\n\n') || '这里是初稿，按你的需要改。'}

## 待补充
- 具体数字和时间点`
  }

  const related = memories.slice(0, 3)
  return `关于「${prompt}」：

${related.length ? `从这件事现有的记忆看：\n${related.map((m) => `- ${m.text}`).join('\n')}\n\n` : ''}我的建议是先把最不确定的一点弄清楚，再动手。如果需要，我可以直接写方案或拆成子任务。`
}

/** "- [ ] item" lines from a breakdown output. */
export function parseChecklist(output: string): string[] {
  return output
    .split('\n')
    .map((l) => l.match(/^\s*- \[[ x]\]\s+(.+)$/)?.[1]?.trim())
    .filter((l): l is string => Boolean(l))
}
