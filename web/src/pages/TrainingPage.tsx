import { useState } from 'react'
import { Download } from 'lucide-react'
import { EpistemicBadge } from '../components/bits'
import { Badge, Button, Card, Empty, Switch, Tabs } from '../components/ui'
import { sampleStateLabel } from '../domain/labels'
import { formatAgo } from '../domain/time'
import type { SampleState, TrainingSample } from '../domain/types'
import { useStore } from '../store/context'

const kindText: Record<TrainingSample['kind'], string> = {
  correction: '用户纠正',
  'adopted-result': '采纳结果',
  conversation: '对话',
}

function exportable(s: TrainingSample, confirmedOnly: boolean): boolean {
  return s.state === 'included' && !s.stale && (!confirmedOnly || s.epistemic === 'confirmed')
}

function download(samples: TrainingSample[]) {
  const lines = samples.map((s) =>
    JSON.stringify({
      messages: [
        { role: 'user', content: s.prompt },
        { role: 'assistant', content: s.response },
      ],
      metadata: { id: s.id, kind: s.kind, origin: s.origin.label, version: s.version },
    }),
  )
  const blob = new Blob([lines.join('\n') + '\n'], { type: 'application/jsonl' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `pcas-training-${new Date().toISOString().slice(0, 10)}.jsonl`
  a.click()
  URL.revokeObjectURL(url)
}

export function TrainingPage() {
  const { state, dispatch } = useStore()
  const [tab, setTab] = useState<SampleState | 'all'>('all')
  const [confirmedOnly, setConfirmedOnly] = useState(true)
  const samples = state.samples.filter((s) => tab === 'all' || s.state === tab)
  const ready = state.samples.filter((s) => exportable(s, confirmedOnly))

  return (
    <main className="page">
      <div className="page-head">
        <div>
          <h1>训练数据</h1>
          <p>对话、纠正和采纳结果整理成的样本。每条保留来源和版本；未确认的推测和过期内容默认不导出。</p>
        </div>
        <Tabs
          value={tab}
          onChange={setTab}
          items={[
            { value: 'all', label: '全部', count: state.samples.length },
            ...(['candidate', 'included', 'excluded'] as SampleState[]).map((s) => ({
              value: s,
              label: sampleStateLabel[s].text,
              count: state.samples.filter((x) => x.state === s).length,
            })),
          ]}
        />
      </div>

      <div className="stack">
        <Card pad>
          <div className="row-between" style={{ flexWrap: 'wrap' }}>
            <div className="row">
              <Switch label="只导出已确认内容" checked={confirmedOnly} onChange={setConfirmedOnly} />
              <span>只导出已确认的内容</span>
            </div>
            <div className="row">
              <span className="small muted">{ready.length} 条可导出</span>
              <Button variant="primary" icon={<Download size={15} />} disabled={ready.length === 0} onClick={() => download(ready)}>
                导出 JSONL
              </Button>
            </div>
          </div>
        </Card>

        <Card>
          {samples.length === 0 ? (
            <Empty>这里还没有样本。采纳一次交接结果，或纠正一条记忆，就会出现候选。</Empty>
          ) : (
            <div className="list">
              {samples.map((s) => (
                <div key={s.id} className="list-item">
                  <div className="grow stack-sm" style={{ gap: 6 }}>
                    <div className="row">
                      <Badge tone={sampleStateLabel[s.state].tone}>{sampleStateLabel[s.state].text}</Badge>
                      <Badge>{kindText[s.kind]}</Badge>
                      <EpistemicBadge value={s.epistemic} />
                      {s.stale && <Badge tone="danger">来源已变化</Badge>}
                    </div>
                    <div>
                      <div className="small muted">输入</div>
                      <div className="ink">{s.prompt}</div>
                    </div>
                    <div>
                      <div className="small muted">输出</div>
                      <div className="pre">{s.response}</div>
                    </div>
                    <div className="meta" style={{ marginTop: 0 }}>
                      <span>{s.origin.label}</span>
                      <span>v{s.version}</span>
                      <span>{formatAgo(s.createdAt)}</span>
                    </div>
                  </div>
                  <div className="stack-sm" style={{ flex: 'none' }}>
                    {s.state !== 'included' && (
                      <Button size="sm" disabled={s.stale} onClick={() => dispatch({ type: 'setSampleState', id: s.id, state: 'included' })}>
                        纳入
                      </Button>
                    )}
                    {s.state !== 'excluded' && (
                      <Button size="sm" variant="ghost" onClick={() => dispatch({ type: 'setSampleState', id: s.id, state: 'excluded' })}>
                        排除
                      </Button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>
    </main>
  )
}
