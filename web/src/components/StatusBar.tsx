import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router'
import { jobStatusLabel } from '../domain/labels'
import { useStore } from '../store/context'
import { Progress, Tag } from './ui'

export function StatusBar() {
  const { state } = useStore()
  const [rect, setRect] = useState<DOMRect | null>(null)
  const open = rect !== null
  const anchor = useRef<HTMLButtonElement>(null)
  const pop = useRef<HTMLDivElement>(null)
  const busy = state.jobs.filter((j) => j.status === 'running' || j.status === 'queued')
  const failed = state.jobs.filter((j) => j.status === 'failed')
  const working = state.runs.filter((r) => r.status === 'running')
  const waiting = state.runs.filter((r) => r.status === 'waiting')

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (!pop.current?.contains(e.target as Node) && !anchor.current?.contains(e.target as Node)) setRect(null)
    }
    window.addEventListener('mousedown', onDown)
    return () => window.removeEventListener('mousedown', onDown)
  }, [open])

  return (
    <footer className="statusbar">
      <button type="button" ref={anchor} onClick={(e) => setRect(open ? null : e.currentTarget.getBoundingClientRect())} aria-expanded={open}>
        <span className={`dot${failed.length ? ' bad' : busy.length ? ' busy' : ''}`} />
        {failed.length ? `后台 ${failed.length} 项出错` : busy.length ? `后台 ${busy.length} 项进行中` : '后台空闲'}
      </button>
      {working.length > 0 && (
        <span className="row-nowrap">
          <span className="dot busy" />
          {state.agents.find((a) => a.id === working[0].agentId)?.name} 正在工作
          {working.length > 1 && ` 等 ${working.length} 项`}
        </span>
      )}
      {waiting.length > 0 && <span>{waiting.length} 份手动交接等你贴回结果</span>}
      <span className="demo" title="还没有接后端，AI 的输出是按规则生成的示例">演示模式 · AI 输出为模拟</span>
      <span className="right">
        <span>
          <kbd>⌘K</kbd> 命令
        </span>
        <span>
          <kbd>⌘\</kbd> 侧栏
        </span>
        <span>
          <kbd>⌘.</kbd> 上下文
        </span>
      </span>

      {open && rect && (
        <div className="popover" ref={pop} style={{ left: rect.left, bottom: window.innerHeight - rect.top + 6 }}>
          <div className="palette-group">后台作业（不是你的待办）</div>
          {state.jobs
            .filter((j) => j.status !== 'done')
            .map((job) => (
              <div key={job.id} className="stack-sm" style={{ gap: 3, padding: '6px 10px' }}>
                <div className="spread">
                  <span className="ink small ellipsis">{job.title}</span>
                  <Tag tone={jobStatusLabel[job.status].tone}>{jobStatusLabel[job.status].text}</Tag>
                </div>
                <span className="tiny muted">{job.detail}</span>
                {job.status === 'running' && job.progress !== undefined && <Progress value={job.progress} />}
              </div>
            ))}
          <div style={{ padding: '6px 10px 2px' }}>
            <Link to="/library?tab=sources" className="small" onClick={() => setRect(null)}>
              全部来源与作业
            </Link>
          </div>
        </div>
      )}
    </footer>
  )
}
