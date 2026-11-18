import { Navigate, Route, Routes } from 'react-router-dom'
import { AppShell } from '@/components/app-shell'
import { DashboardPage } from '@/pages/dashboard'
import { ProjectPage } from '@/pages/project'
import { ProjectsPage } from '@/pages/projects'
import { ApprovalsPage } from '@/pages/approvals'
import { DrawingPage } from '@/pages/drawing'
import { TaskPage } from '@/pages/task'
import { TasksPage } from '@/pages/tasks'
import { ActivityPage } from '@/pages/activity'
import { EmbedViewerPage } from '@/pages/embed-viewer'

export default function App() {
  return (
    <Routes>
      <Route path="embed/viewer" element={<EmbedViewerPage />} />
      <Route element={<AppShell />}>
        <Route index element={<DashboardPage />} />
        <Route path="proyectos" element={<ProjectsPage />} />
        <Route path="tareas" element={<TasksPage />} />
        <Route path="aprobaciones" element={<ApprovalsPage />} />
        <Route path="actividad" element={<ActivityPage />} />
        <Route path="p/:projectId" element={<ProjectPage />} />
        <Route path="p/:projectId/d/:drawingId" element={<DrawingPage />} />
        <Route path="p/:projectId/t/:taskId" element={<TaskPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
