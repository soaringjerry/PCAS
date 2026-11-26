import { useSearchParams } from 'react-router'
import { ObserveActivityTab } from '../components/observe/ObserveActivityTab'
import { ObserveBackgroundTab } from '../components/observe/ObserveBackgroundTab'
import { ObserveMemoryTab } from '../components/observe/ObserveMemoryTab'
import { ObserveUsageTab } from '../components/observe/ObserveUsageTab'
import { Seg } from '../components/ui'
import { civilDate } from '../domain/time'
import { useStore } from '../store/context'
import '../styles/observe.css'

type ObserveTab = 'activity' | 'usage' | 'background' | 'memory'

const validTabs: Record<string, ObserveTab> = {
  activity: 'activity',
  usage: 'usage',
  background: 'background',
  memory: 'memory',
}

export function ObservePage() {
  const { state } = useStore()
  const tz = state.settings.timezone ?? 'UTC'
  const [params, setParams] = useSearchParams()

  const rawTab = params.get('tab') ?? 'activity'
  const tab: ObserveTab = validTabs[rawTab] ?? 'activity'
  const dateParam = params.get('date')
  const date = dateParam && /^\d{4}-\d{2}-\d{2}$/.test(dateParam) ? dateParam : civilDate(0, tz)
  const highlightedCallId = params.get('call') ?? ''

  const failedJobs = state.jobs.filter((j) => j.status === 'failed').length

  const setTab = (next: ObserveTab) => {
    setParams(
      (current) => {
        const updated = new URLSearchParams(current)
        if (next === 'activity') updated.delete('tab')
        else updated.set('tab', next)
        if (next !== 'usage') updated.delete('call')
        return updated
      },
      { replace: true },
    )
  }

  const setDate = (nextDate: string) => {
    setParams(
      (current) => {
        const updated = new URLSearchParams(current)
        updated.delete('tab')
        updated.delete('call')
        updated.set('date', nextDate)
        return updated
      },
      { replace: true },
    )
  }

  const inspectCall = (usageId: string) => {
    setParams(
      { tab: 'usage', call: usageId },
      { replace: false },
    )
  }

  const clearHighlight = () => {
    setParams(
      (current) => {
        const updated = new URLSearchParams(current)
        updated.delete('call')
        return updated
      },
      { replace: true },
    )
  }

  return (
    <main className="page page-narrow observe">
      <div className="page-head">
        <div>
          <h1>观察面板</h1>
          <p>看清团队做了什么、为什么这么做、花了多少，以及哪里延后或失败需要恢复。</p>
        </div>
      </div>

      <div className="obs-tabs">
        <Seg<ObserveTab>
          label="观察面板分区"
          value={tab}
          onChange={setTab}
          items={[
            { value: 'activity', label: '按日活动' },
            { value: 'usage', label: '花费与调用' },
            { value: 'background', label: failedJobs > 0 ? `后台与恢复 · ${failedJobs} 项失败` : '后台与恢复' },
            { value: 'memory', label: '记忆与权限' },
          ]}
        />
      </div>

      {tab === 'activity' && <ObserveActivityTab date={date} onDateChange={setDate} onInspectCall={inspectCall} />}
      {tab === 'usage' && (
        <ObserveUsageTab
          highlightedCallId={highlightedCallId}
          onSelectDate={setDate}
          onClearHighlight={clearHighlight}
        />
      )}
      {tab === 'background' && <ObserveBackgroundTab />}
      {tab === 'memory' && <ObserveMemoryTab />}
    </main>
  )
}
