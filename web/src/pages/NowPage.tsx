import { useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { Check, FileUp, X } from 'lucide-react'
import { FromLine, ProjectLink, TaskBox } from '../components/Marks'
import { Button, Empty, Progress, Sheet } from '../components/ui'
import { attentionFor, isDecision, type Attention } from '../domain/attention'
import { memoryKindLabel } from '../domain/labels'
import { isLiveIdea, isOpenTask } from '../domain/things'
import { dayOffset, formatWhen, isOverdue } from '../domain/time'
import type { Candidate, CandidateKind, MemoryKind, Task } from '../domain/types'
import { useStore } from '../store/context'
import { useToast } from '../store/toast'

function greeting(): string {
  const h = new Date().getHours()
  if (h < 5) return '夜深了'
  if (h < 11) return '早上好'
  if (h < 13) return '中午好'
  if (h < 18) return '下午好'
  return '晚上好'
}

const kindWords: Record<CandidateKind, string> = { task: '待办', idea: '想法', memory: '一条记忆' }

function CandidateRow({ candidate }: { candidate: Candidate }) {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const [open, setOpen] = useState(false)
  const [kind, setKind] = useState<CandidateKind>(candidate.kind)
  const [memoryKind, setMemoryKind] = useState<MemoryKind>(candidate.memoryKind ?? 'fact')
  const [projectId, setProjectId] = useState(candidate.projectId ?? '')
  const [target, setTarget] = useState('')

  const targets =
    kind === 'task'
      ? state.tasks.filter(isOpenTask).map((t) => ({ id: t.id, label: t.title }))
      : kind === 'idea'
        ? state.ideas.filter(isLiveIdea).map((i) => ({ id: i.id, label: i.title }))
        : state.memories.map((m) => ({ id: m.id, label: m.text }))
  const project = state.projects.find((p) => p.id === projectId)
  const guessText = [kind === 'memory' ? memoryKindLabel[memoryKind] : kindWords[kind], project?.name, kind === 'task' && candidate.due ? `截止 ${formatWhen(candidate.due)}` : '']
    .filter(Boolean)
    .join(' · ')

  const accept = () => {
    dispatch({
      type: 'acceptCandidate',
      id: candidate.id,
      kind,
      text: candidate.text,
      memoryKind: kind === 'memory' ? memoryKind : undefined,
      projectId: projectId || undefined,
      due: kind === 'task' ? candidate.due : undefined,
    })
    toast.show(`收下了，放进${kind === 'memory' ? '资料库' : project ? `「${project.name}」` : '事情'}`)
  }

  return (
    <div className="item confirm-row">
      <div className="grow">
        <div className="item-title guess">{candidate.text}</div>
        <div className="meta">
          <span className="ink-2">{guessText}</span>
          <span>来自{candidate.source.label}</span>
        </div>
        {open && (
          <div className="stack-sm" style={{ marginTop: 10 }}>
            <div className="meta" style={{ marginTop: 0 }}>
              <span>其实是</span>
              <select className="inline-select" value={kind} onChange={(e) => setKind(e.target.value as CandidateKind)} aria-label="类型">
                {(Object.keys(kindWords) as CandidateKind[]).map((k) => (
                  <option key={k} value={k}>
                    {kindWords[k]}
                  </option>
                ))}
              </select>
              {kind === 'memory' && (
                <select className="inline-select" value={memoryKind} onChange={(e) => setMemoryKind(e.target.value as MemoryKind)} aria-label="记忆类型">
                  {(Object.keys(memoryKindLabel) as MemoryKind[]).map((k) => (
                    <option key={k} value={k}>
                      {memoryKindLabel[k]}
                    </option>
                  ))}
                </select>
              )}
              <span>属于</span>
              <select className="inline-select" value={projectId} onChange={(e) => setProjectId(e.target.value)} aria-label="项目">
                <option value="">不属于项目</option>
                {state.projects.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>
            {candidate.source.excerpt && candidate.source.sourceId !== 'src_capture' && <FromLine source={candidate.source} />}
            <div className="meta" style={{ marginTop: 0 }}>
              <span>或者并进已有的</span>
              <select className="inline-select" style={{ maxWidth: 280 }} value={target} onChange={(e) => setTarget(e.target.value)} aria-label="合并到">
                <option value="">选一条…</option>
                {targets.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.label}
                  </option>
                ))}
              </select>
              <Button
                size="sm"
                disabled={!target}
                onClick={() => {
                  dispatch({ type: 'mergeCandidate', id: candidate.id, targetId: target })
                  toast.show('合并好了，来源也一起记上了')
                }}
              >
                合并
              </Button>
            </div>
          </div>
        )}
      </div>
      <div className="confirm-actions">
        <Button size="sm" variant="primary" icon={<Check size={14} />} onClick={accept}>
          收下
        </Button>
        <Button size="sm" variant="quiet" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
          {open ? '收起' : '调整'}
        </Button>
        <Button size="sm" variant="quiet" icon={<X size={14} />} aria-label="不要" title="不要" onClick={() => dispatch({ type: 'ignoreCandidate', id: candidate.id })} />
      </div>
    </div>
  )
}

function AttentionRow({ item }: { item: Attention }) {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const navigate = useNavigate()

  switch (item.kind) {
    case 'wake':
      return (
        <div className="item">
          <span className="glyph glyph-wake">✦</span>
          <div className="grow">
            <Link to={`/t/${item.idea.id}`} className="item-title">
              <span className="hl">{item.idea.title}</span>
            </Link>
            <p className="small" style={{ marginTop: 4 }}>
              {item.idea.wake?.reason}
            </p>
            <div className="item-actions">
              <Button
                size="sm"
                variant="primary"
                onClick={() => {
                  dispatch({ type: 'ideaPromote', id: item.idea.id })
                  toast.show('转成待办了', { to: `/t/${item.idea.id}`, label: '打开' })
                }}
              >
                转成待办
              </Button>
              <Button size="sm" onClick={() => dispatch({ type: 'ideaContinue', id: item.idea.id })}>
                继续想
              </Button>
              <Button size="sm" variant="quiet" onClick={() => dispatch({ type: 'ideaSnooze', id: item.idea.id, days: 7 })}>
                一周后再说
              </Button>
              <Button size="sm" variant="quiet" onClick={() => dispatch({ type: 'ideaStopReminders', id: item.idea.id })}>
                别再提醒
              </Button>
            </div>
          </div>
        </div>
      )

    case 'due':
      return (
        <div className="item">
          <span className="glyph glyph-due">!</span>
          <div className="grow">
            <Link to={`/t/${item.task.id}`} className="item-title">
              {item.task.title}
            </Link>
            <div className="meta">
              <span className="late">
                {isOverdue(item.task.due!) ? '已经过了截止时间' : '今天截止'} · {formatWhen(item.task.due!)}
              </span>
              <ProjectLink id={item.task.projectId} />
            </div>
            <div className="item-actions">
              <Button
                size="sm"
                variant="primary"
                onClick={() => {
                  dispatch({ type: 'setTaskStatus', id: item.task.id, status: 'done' })
                  toast.show('完成一件 ✓')
                }}
              >
                做完了
              </Button>
              <Button size="sm" onClick={() => dispatch({ type: 'deferTask', id: item.task.id, days: 1 })}>
                明天再做
              </Button>
            </div>
          </div>
        </div>
      )

    case 'result': {
      const agent = state.agents.find((a) => a.id === item.handoff.agentId)?.name
      return (
        <div className="item">
          <span className="glyph glyph-result">↩</span>
          <div className="grow">
            <div className="item-title">
              {agent} 回复了「{item.handoff.title}」
            </div>
            <p className="small muted" style={{ marginTop: 2 }}>
              看一眼，改一改，采纳后会写回原来的事情。
            </p>
            <div className="item-actions">
              <Button size="sm" variant="primary" onClick={() => navigate(`/handoff/${item.handoff.id}`)}>
                看结果
              </Button>
            </div>
          </div>
        </div>
      )
    }

    case 'candidate':
      return <CandidateRow candidate={item.candidate} />

    case 'confirm':
      return (
        <div className="item confirm-row">
          <div className="grow">
            <div className="item-title guess">{item.memory.text}</div>
            <div className="meta">
              <span className="ink-2">{memoryKindLabel[item.memory.kind]}</span>
              <span>AI 从{item.memory.sources[0]?.label ?? '资料'}里读出来的，确认前不会给别的 AI</span>
            </div>
          </div>
          <div className="confirm-actions">
            <Button
              size="sm"
              variant="primary"
              icon={<Check size={14} />}
              onClick={() => {
                dispatch({ type: 'confirmMemory', id: item.memory.id })
                toast.show('记住了')
              }}
            >
              没错
            </Button>
            <Button size="sm" variant="quiet" onClick={() => navigate(`/library?m=${item.memory.id}`)}>
              改
            </Button>
            <Button
              size="sm"
              variant="quiet"
              icon={<X size={14} />}
              aria-label="不对，删掉"
              title="不对，删掉"
              onClick={() => dispatch({ type: 'deleteMemory', id: item.memory.id })}
            />
          </div>
        </div>
      )

    case 'job':
      return (
        <div className="item">
          <span className="glyph glyph-due">⚠</span>
          <div className="grow">
            <div className="item-title">{item.job.title}没成功</div>
            <p className="small muted" style={{ marginTop: 2 }}>
              {item.job.detail}
              {item.job.recovery && `。建议：${item.job.recovery}`}
            </p>
            <div className="item-actions">
              <Button size="sm" onClick={() => dispatch({ type: 'retryJob', id: item.job.id })}>
                重试
              </Button>
            </div>
          </div>
        </div>
      )
  }
}

function lineWhen(task: Task): string {
  if (task.status === 'waiting') return `等 ${task.waitingFor ?? ''}`
  if (task.due) {
    const d = dayOffset(task.due)
    return d < 0 ? '已过期' : d === 0 ? '今天' : d === 1 ? '明天' : formatWhen(task.due)
  }
  if (task.scheduled) return `安排在 ${formatWhen(task.scheduled)}`
  return ''
}

const statusRank = { doing: 0, todo: 1, waiting: 2, done: 3, cancelled: 4 } as const

function InMotion() {
  const { state } = useStore()
  const groups = state.projects
    .filter((p) => p.status === 'active')
    .map((p) => ({
      project: p,
      tasks: state.tasks.filter((t) => t.projectId === p.id && isOpenTask(t)).sort((a, b) => statusRank[a.status] - statusRank[b.status]),
      ideas: state.ideas.filter((i) => i.projectId === p.id && i.status === 'active'),
    }))
    .filter((g) => g.tasks.length + g.ideas.length > 0)
  const loose = state.tasks.filter((t) => !t.projectId && isOpenTask(t))

  if (groups.length === 0 && loose.length === 0) return <Empty>手上没有在推进的事。</Empty>

  return (
    <>
      {groups.map(({ project, tasks, ideas }) => (
        <div key={project.id} className="motion-group">
          <div className="motion-head">
            <Link to={`/t/${project.id}`}>{project.name}</Link>
            {project.nextSteps[0] && <span className="small muted">下一步：{project.nextSteps[0]}</span>}
          </div>
          {tasks.slice(0, 4).map((t) => (
            <Link key={t.id} to={`/t/${t.id}`} className="motion-line">
              <TaskBox task={t} />
              <span className="grow" style={{ fontWeight: t.status === 'doing' ? 600 : 400 }}>
                {t.title}
              </span>
              <span className="when">{lineWhen(t)}</span>
            </Link>
          ))}
          {ideas.map((i) => (
            <Link key={i.id} to={`/t/${i.id}`} className="motion-line">
              <span className="kind kind-idea" style={{ width: 18, justifyContent: 'center' }} />
              <span className="grow">{i.title}</span>
              <span className="when">想法</span>
            </Link>
          ))}
          {tasks.length > 4 && (
            <Link to={`/t/${project.id}`} className="small muted" style={{ paddingLeft: 28 }}>
              还有 {tasks.length - 4} 件…
            </Link>
          )}
        </div>
      ))}
      {loose.length > 0 && (
        <div className="motion-group">
          <div className="motion-head">
            <span className="hand ink" style={{ fontSize: 17, fontWeight: 400 }}>零散的</span>
          </div>
          {loose.map((t) => (
            <Link key={t.id} to={`/t/${t.id}`} className="motion-line">
              <TaskBox task={t} />
              <span className="grow">{t.title}</span>
              <span className="when">{lineWhen(t)}</span>
            </Link>
          ))}
        </div>
      )}
    </>
  )
}

function TryIt() {
  const { state, runDemoImport } = useStore()
  const job = state.jobs.find((j) => j.id === 'j_demo')
  const running = job?.status === 'running'
  if (state.demo.costReportImported && !running) return null
  return (
    <div className="try">
      <FileUp size={22} style={{ color: 'var(--accent)', flex: 'none' }} />
      <div className="grow">
        <div className="hand ink" style={{ fontSize: 17, fontWeight: 400 }}>
          试一下：搁置的想法怎么回来
        </div>
        {running ? (
          <div className="stack-sm" style={{ marginTop: 6 }}>
            <span className="small muted">{job.detail}</span>
            <Progress value={job.progress ?? 0} />
          </div>
        ) : (
          <p className="small muted">「用本地小模型做记忆提取」因为缺成本数据被放下了。导入一份成本测算，看它会不会自己回来。</p>
        )}
      </div>
      {!running && (
        <Button variant="primary" onClick={runDemoImport}>
          导入成本测算
        </Button>
      )}
    </div>
  )
}

export function NowPage() {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const [text, setText] = useState('')
  const [showAll, setShowAll] = useState(false)
  const all = attentionFor(state)
  const decisions = all.filter(isDecision)
  const confirms = all.filter((i) => !isDecision(i))
  const shownConfirms = showAll ? confirms : confirms.slice(0, 3)
  const shelved = state.ideas.filter((i) => i.status === 'shelved').length
  const inMotion = state.tasks.filter((t) => t.status === 'doing' || t.status === 'todo').length
  const date = new Date().toLocaleDateString('zh-CN', { month: 'long', day: 'numeric', weekday: 'long' })

  return (
    <main className="page page-narrow">
      <div className="stack">
        <div>
          <h1 className="greeting">{greeting()}</h1>
          <p className="greeting-sub">
            {date} · {decisions.length ? `${decisions.length} 件事等你拿主意` : '没有要你拿主意的事'}，{inMotion} 件在推进
          </p>
        </div>

        <form
          className="sheet sheet-pad"
          onSubmit={(e) => {
            e.preventDefault()
            if (!text.trim()) return
            dispatch({ type: 'capture', text: text.trim() })
            setText('')
            toast.show('记下了，在“等你确认”里')
          }}
        >
          <input
            className="bare hand"
            style={{ fontSize: 18, color: 'var(--ink)' }}
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="想到什么，先记一句…"
            aria-label="记一笔"
          />
        </form>

        <TryIt />

        <section className="section">
          <h2 className="section-title">
            <span className="squiggle">需要你看</span>
            <span className="n">{decisions.length}</span>
          </h2>
          <Sheet>
            {decisions.length ? (
              <div className="list">
                {decisions.map((item) => (
                  <AttentionRow key={item.key} item={item} />
                ))}
              </div>
            ) : (
              <Empty>没有要你拿主意的事，今天很清爽。</Empty>
            )}
          </Sheet>
        </section>

        {confirms.length > 0 && (
          <section className="section">
            <h2 className="section-title">
              <span className="squiggle">等你确认</span>
              <span className="n">{confirms.length} · 波浪线是 AI 猜的，点一下就行</span>
            </h2>
            <Sheet>
              <div className="list">
                {shownConfirms.map((item) => (
                  <AttentionRow key={item.key} item={item} />
                ))}
              </div>
              {confirms.length > 3 && (
                <button type="button" className="more" onClick={() => setShowAll((v) => !v)}>
                  {showAll ? '收起' : `还有 ${confirms.length - 3} 条`}
                </button>
              )}
            </Sheet>
          </section>
        )}

        <section className="section">
          <h2 className="section-title">
            <span className="squiggle">正在推进</span>
          </h2>
          <Sheet>
            <InMotion />
          </Sheet>
          {shelved > 0 && (
            <p className="small muted">
              另有 {shelved} 个想法放着，条件满足时会自己回来。<Link to="/things?view=shelved">看看都有哪些</Link>
            </p>
          )}
        </section>
      </div>
    </main>
  )
}
