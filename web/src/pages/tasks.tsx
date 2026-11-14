import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { GitBranch, Plus, Waypoints } from 'lucide-react'
import { api, type Branch } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/separator'
import { PageHeader, EmptyState } from '@/components/page-header'
import { BranchRow } from '@/pages/project'
import { NewTaskDialog } from '@/components/dialogs'
import { cn } from '@/lib/utils'

type BranchRowT = Branch & { projectName: string }

const filters = [
  { id: 'active', label: 'En curso' },
  { id: 'review', label: 'Para revisar' },
  { id: 'merged', label: 'Integradas' },
  { id: 'all', label: 'Todas' }
] as const

export function TasksPage() {
  const [rows, setRows] = useState<BranchRowT[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<(typeof filters)[number]['id']>('active')
  const [newTask, setNewTask] = useState(false)
  const [projects, setProjects] = useState<{ id: string; name: string }[]>([])
  const navigate = useNavigate()

  useEffect(() => {
    api.projects().then((p) => setProjects(p.map((x) => ({ id: x.id, name: x.name }))))
  }, [])

  useEffect(() => {
    let alive = true
    setLoading(true)
    fetch(`/api/branches?status=${filter === 'active' ? 'active' : filter}`)
      .then((r) => r.json())
      .then((d) => alive && setRows(d ?? []))
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [filter])

  const grouped = useMemo(() => {
    const map = new Map<string, { name: string; items: BranchRowT[] }>()
    for (const b of rows) {
      const entry = map.get(b.projectId) ?? { name: b.projectName, items: [] }
      entry.items.push(b)
      map.set(b.projectId, entry)
    }
    return [...map.entries()]
  }, [rows])

  return (
    <div className="scrollbar-thin flex-1 overflow-y-auto">
      <PageHeader
        eyebrow={
          <>
            <Waypoints className="size-3.5" /> flujo de trabajo
          </>
        }
        title="Tareas"
        description="Cada tarea es una rama aislada: se descarga el DWG, se edita afuera y se vuelve a subir. main solo cambia cuando integramos."
        actions={
          <Button size="sm" onClick={() => setNewTask(true)} disabled={projects.length === 0}>
            <Plus className="size-4" /> Nueva tarea
          </Button>
        }
      />

      <div className="flex items-center gap-1 px-6 pb-4">
        {filters.map((f) => (
          <button
            key={f.id}
            onClick={() => setFilter(f.id)}
            className={cn(
              'rounded-md px-2.5 py-1 text-[12.5px] font-medium transition-colors',
              filter === f.id
                ? 'bg-accent text-accent-foreground'
                : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
            )}
          >
            {f.label}
          </button>
        ))}
        <Badge variant="outline" className="ml-2 font-mono text-[10px]">
          {rows.length}
        </Badge>
      </div>

      <div className="px-6 pb-10">
        {loading ? (
          <div className="space-y-2">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-16 w-full" />
            ))}
          </div>
        ) : rows.length === 0 ? (
          <EmptyState
            icon={GitBranch}
            title="No hay tareas en este filtro"
            description="Abrí una tarea desde un plano o desde un proyecto para empezar el ciclo descargar → editar → subir."
          />
        ) : (
          <div className="space-y-6">
            {grouped.map(([projectId, group]) => (
              <div key={projectId}>
                <div className="mb-2 flex items-center gap-2 px-1">
                  <Link
                    to={`/p/${projectId}`}
                    className="text-[11px] font-semibold tracking-[0.14em] uppercase text-muted-foreground hover:text-foreground"
                  >
                    {group.name}
                  </Link>
                  <span className="text-muted-foreground/50 font-mono text-[10px]">/{projectId}</span>
                  <span className="text-muted-foreground ml-auto text-[11px]">
                    {group.items.length} rama(s)
                  </span>
                </div>
                <div className="divide-y overflow-hidden rounded-lg border bg-card">
                  {group.items.map((b) => (
                    <BranchRow key={b.id} branch={b} projectId={projectId} />
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <NewTaskDialog
        open={newTask}
        onOpenChange={setNewTask}
        projectId={projects[0]?.id ?? ''}
        projects={projects}
        onCreated={(b) => navigate(`/p/${b.projectId}/t/${b.id}`)}
      />
    </div>
  )
}
