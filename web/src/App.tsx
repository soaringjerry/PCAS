import { Navigate, Route, Routes } from 'react-router'
import { Shell } from './components/Shell'
import { HallPage } from './pages/HallPage'
import { FormerAbout, LibraryPage } from './pages/LibraryPage'
import { NotFound } from './pages/NotFound'
import { SettingsPage } from './pages/SettingsPage'
import { ThingPage } from './pages/ThingPage'

// Paths from earlier prototypes that now live on the home screen.
const retired = ['today', 'inbox', 'upcoming', 'ideas', 'things']

export function App() {
  return (
    <Routes>
      <Route element={<Shell />}>
        <Route index element={<HallPage />} />
        <Route path="t/:id" element={<ThingPage />} />
        <Route path="library" element={<LibraryPage />} />
        {/* The former page of its own; its links land on the same memories in the library. */}
        <Route path="about" element={<FormerAbout />} />
        <Route path="settings" element={<SettingsPage />} />
        {retired.map((p) => (
          <Route key={p} path={p} element={<Navigate to="/" replace />} />
        ))}
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  )
}
