import { Navigate, Route, Routes } from 'react-router'
import { Shell } from './components/Shell'
import { HandoffPage } from './pages/HandoffPage'
import { LibraryPage } from './pages/LibraryPage'
import { NotFound } from './pages/NotFound'
import { NowPage } from './pages/NowPage'
import { SettingsPage } from './pages/SettingsPage'
import { ThingPage } from './pages/ThingPage'
import { ThingsPage } from './pages/ThingsPage'

export function App() {
  return (
    <Routes>
      <Route element={<Shell />}>
        <Route index element={<NowPage />} />
        <Route path="things" element={<ThingsPage />} />
        <Route path="t/:id" element={<ThingPage />} />
        <Route path="handoff/:id" element={<HandoffPage />} />
        <Route path="library" element={<LibraryPage />} />
        <Route path="settings" element={<SettingsPage />} />
        {/* Old prototype paths */}
        <Route path="inbox" element={<Navigate to="/" replace />} />
        <Route path="memory" element={<Navigate to="/library" replace />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  )
}
