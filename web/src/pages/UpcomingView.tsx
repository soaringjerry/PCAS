import { TaskList } from '../components/TaskList'
import { upcomingGroups } from '../domain/views'
import { useStore } from '../store/context'

export function UpcomingView() {
  const { state } = useStore()
  return (
    <div className="view">
      <div className="view-head">
        <div>
          <h1>接下来</h1>
          <p>之后几天的安排，和还没排期的待办。</p>
        </div>
      </div>
      <TaskList groups={upcomingGroups(state)} empty="接下来没有安排。" />
    </div>
  )
}
