import { Route, Routes } from 'react-router'
import { Layout } from './components/Layout'
import { AgentsPage } from './pages/AgentsPage'
import { HandoffPage } from './pages/HandoffPage'
import { HandoffsPage } from './pages/HandoffsPage'
import { IdeasPage } from './pages/IdeasPage'
import { InboxPage } from './pages/InboxPage'
import { MemoryPage } from './pages/MemoryPage'
import { NotFound } from './pages/NotFound'
import { ProjectPage } from './pages/ProjectPage'
import { ProjectsPage } from './pages/ProjectsPage'
import { SourcesPage } from './pages/SourcesPage'
import { TasksPage } from './pages/TasksPage'
import { TodayPage } from './pages/TodayPage'
import { TrainingPage } from './pages/TrainingPage'

export function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<TodayPage />} />
        <Route path="inbox" element={<InboxPage />} />
        <Route path="tasks" element={<TasksPage />} />
        <Route path="ideas" element={<IdeasPage />} />
        <Route path="projects" element={<ProjectsPage />} />
        <Route path="projects/:id" element={<ProjectPage />} />
        <Route path="memory" element={<MemoryPage />} />
        <Route path="handoffs" element={<HandoffsPage />} />
        <Route path="handoffs/:id" element={<HandoffPage />} />
        <Route path="agents" element={<AgentsPage />} />
        <Route path="sources" element={<SourcesPage />} />
        <Route path="training" element={<TrainingPage />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  )
}
