import { Navigate, Route, Routes } from 'react-router'
import { Workbench } from './components/Workbench'
import { IdeasView } from './pages/IdeasView'
import { InboxView } from './pages/InboxView'
import { LibraryPage } from './pages/LibraryPage'
import { NotFound } from './pages/NotFound'
import { SettingsPage } from './pages/SettingsPage'
import { ThingView } from './pages/ThingView'
import { TodayView } from './pages/TodayView'
import { UpcomingView } from './pages/UpcomingView'

export function App() {
  return (
    <Routes>
      <Route element={<Workbench />}>
        <Route index element={<Navigate to="/today" replace />} />
        <Route path="today" element={<TodayView />} />
        <Route path="inbox" element={<InboxView />} />
        <Route path="upcoming" element={<UpcomingView />} />
        <Route path="ideas" element={<IdeasView />} />
        <Route path="t/:id" element={<ThingView />} />
        <Route path="library" element={<LibraryPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="things" element={<Navigate to="/today" replace />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  )
}
