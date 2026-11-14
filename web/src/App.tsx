import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from '@/components/app-shell'
import { DashboardPage } from '@/pages/dashboard'
import { ProjectPage } from '@/pages/project'
import { DrawingPage } from '@/pages/drawing'
import { TaskPage } from '@/pages/task'
import { TasksPage } from '@/pages/tasks'
import { ActivityPage } from '@/pages/activity'

export default function App() {
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<DashboardPage />} />
        <Route path="tareas" element={<TasksPage />} />
        <Route path="actividad" element={<ActivityPage />} />
        <Route path="p/:projectId" element={<ProjectPage />} />
        <Route path="p/:projectId/d/:drawingId" element={<DrawingPage />} />
        <Route path="p/:projectId/t/:taskId" element={<TaskPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
